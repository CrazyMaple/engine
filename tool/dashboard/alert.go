package dashboard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"engine/log"
)

// AlertSeverity 告警严重级别
type AlertSeverity string

const (
	SeverityInfo     AlertSeverity = "info"
	SeverityWarning  AlertSeverity = "warning"
	SeverityCritical AlertSeverity = "critical"
)

// CompareOp 比较操作符
type CompareOp string

const (
	OpGreater CompareOp = ">"
	OpLess    CompareOp = "<"
	OpEqual   CompareOp = "=="
)

// AlertRule 告警规则定义
type AlertRule struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Metric     string        `json:"metric"`   // 指标名称（如 "cpu_percent" / "deadletter_rate"）
	Op         CompareOp     `json:"op"`       // 比较符
	Threshold  float64       `json:"threshold"` // 阈值
	Duration   time.Duration `json:"duration"` // 连续超阈值多久后触发（防抖）
	Severity   AlertSeverity `json:"severity"`
	Enabled    bool          `json:"enabled"`
	Annotation string        `json:"annotation,omitempty"`
}

// AlertEvent 告警事件实例
type AlertEvent struct {
	ID         string        `json:"id"`
	RuleID     string        `json:"rule_id"`
	RuleName   string        `json:"rule_name"`
	Metric     string        `json:"metric"`
	Value      float64       `json:"value"`
	Threshold  float64       `json:"threshold"`
	Severity   AlertSeverity `json:"severity"`
	FiredAt    time.Time     `json:"fired_at"`
	ResolvedAt *time.Time    `json:"resolved_at,omitempty"`
	AckedAt    *time.Time    `json:"acked_at,omitempty"`
	AckedBy    string        `json:"acked_by,omitempty"`
}

// AlertNotifier 告警通知渠道
type AlertNotifier interface {
	Notify(event AlertEvent) error
}

// WebhookNotifier 通过 HTTP POST 推送告警 JSON
type WebhookNotifier struct {
	URL    string
	client *http.Client
}

// NewWebhookNotifier 构造一个 Webhook 通知器
func NewWebhookNotifier(url string) *WebhookNotifier {
	return &WebhookNotifier{
		URL:    url,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Notify 异步发送（写到 OS 缓冲后立即返回失败上层处理）
func (n *WebhookNotifier) Notify(event AlertEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	resp, err := n.client.Post(n.URL, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}

// MetricSample 单次指标采样输入
type MetricSample struct {
	Metric string
	Value  float64
}

// AlertManager 告警规则引擎，根据采样判断触发/恢复，支持静默/确认/历史
type AlertManager struct {
	mu        sync.RWMutex
	rules     map[string]*AlertRule
	active    map[string]*AlertEvent  // ruleID -> 当前未恢复事件
	history   []AlertEvent             // 历史事件（含已恢复）
	maxHist   int
	silenced  map[string]time.Time     // ruleID -> 静默到期时间
	notifiers []AlertNotifier
	pending   map[string]*pendingState // ruleID -> 持续超阈值起始时间
	seq       atomic.Int64

	logger log.Logger
}

type pendingState struct {
	since time.Time
	value float64
}

// NewAlertManager 创建告警管理器，maxHistory 建议 1024
func NewAlertManager(maxHistory int) *AlertManager {
	if maxHistory <= 0 {
		maxHistory = 1024
	}
	return &AlertManager{
		rules:    make(map[string]*AlertRule),
		active:   make(map[string]*AlertEvent),
		silenced: make(map[string]time.Time),
		pending:  make(map[string]*pendingState),
		maxHist:  maxHistory,
	}
}

// SetLogger 注入日志器
func (am *AlertManager) SetLogger(l log.Logger) { am.logger = l }

// AddNotifier 注册通知渠道
func (am *AlertManager) AddNotifier(n AlertNotifier) {
	am.mu.Lock()
	am.notifiers = append(am.notifiers, n)
	am.mu.Unlock()
}

// SetRule 新增/更新规则
func (am *AlertManager) SetRule(rule AlertRule) error {
	if rule.ID == "" {
		return errors.New("rule id required")
	}
	if rule.Op != OpGreater && rule.Op != OpLess && rule.Op != OpEqual {
		return fmt.Errorf("invalid op: %s", rule.Op)
	}
	am.mu.Lock()
	r := rule
	am.rules[rule.ID] = &r
	am.mu.Unlock()
	return nil
}

// DeleteRule 删除规则
func (am *AlertManager) DeleteRule(id string) {
	am.mu.Lock()
	delete(am.rules, id)
	delete(am.active, id)
	delete(am.pending, id)
	am.mu.Unlock()
}

// Rules 返回当前规则列表
func (am *AlertManager) Rules() []AlertRule {
	am.mu.RLock()
	defer am.mu.RUnlock()
	out := make([]AlertRule, 0, len(am.rules))
	for _, r := range am.rules {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Active 返回当前未恢复告警
func (am *AlertManager) Active() []AlertEvent {
	am.mu.RLock()
	defer am.mu.RUnlock()
	out := make([]AlertEvent, 0, len(am.active))
	for _, e := range am.active {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FiredAt.After(out[j].FiredAt) })
	return out
}

// History 返回历史告警，limit<=0 返回全部
func (am *AlertManager) History(limit int) []AlertEvent {
	am.mu.RLock()
	defer am.mu.RUnlock()
	n := len(am.history)
	if limit > 0 && limit < n {
		out := make([]AlertEvent, limit)
		copy(out, am.history[n-limit:])
		return out
	}
	out := make([]AlertEvent, n)
	copy(out, am.history)
	return out
}

// Silence 静默某条规则到 until 之前不再触发
func (am *AlertManager) Silence(ruleID string, until time.Time) {
	am.mu.Lock()
	am.silenced[ruleID] = until
	am.mu.Unlock()
}

// Unsilence 取消静默
func (am *AlertManager) Unsilence(ruleID string) {
	am.mu.Lock()
	delete(am.silenced, ruleID)
	am.mu.Unlock()
}

// Ack 确认告警
func (am *AlertManager) Ack(eventID, by string) bool {
	am.mu.Lock()
	defer am.mu.Unlock()
	for _, e := range am.active {
		if e.ID == eventID {
			now := time.Now()
			e.AckedAt = &now
			e.AckedBy = by
			return true
		}
	}
	return false
}

// Submit 上送一条指标采样，触发规则评估
func (am *AlertManager) Submit(sample MetricSample) {
	am.SubmitAt(sample, time.Now())
}

// SubmitAt 带时间戳的指标采样（便于测试）
func (am *AlertManager) SubmitAt(sample MetricSample, now time.Time) {
	am.mu.Lock()
	defer am.mu.Unlock()

	for _, rule := range am.rules {
		if !rule.Enabled || rule.Metric != sample.Metric {
			continue
		}
		if until, ok := am.silenced[rule.ID]; ok && now.Before(until) {
			continue
		}

		matched := compare(sample.Value, rule.Op, rule.Threshold)
		if matched {
			am.handleMatchLocked(rule, sample.Value, now)
		} else {
			am.handleResolveLocked(rule, sample.Value, now)
		}
	}
}

func (am *AlertManager) handleMatchLocked(rule *AlertRule, value float64, now time.Time) {
	if _, isActive := am.active[rule.ID]; isActive {
		return
	}
	state, ok := am.pending[rule.ID]
	if !ok {
		am.pending[rule.ID] = &pendingState{since: now, value: value}
		if rule.Duration <= 0 {
			am.fireLocked(rule, value, now)
		}
		return
	}
	state.value = value
	if now.Sub(state.since) >= rule.Duration {
		am.fireLocked(rule, value, now)
	}
}

func (am *AlertManager) handleResolveLocked(rule *AlertRule, value float64, now time.Time) {
	delete(am.pending, rule.ID)
	if ev, ok := am.active[rule.ID]; ok {
		t := now
		ev.ResolvedAt = &t
		am.appendHistoryLocked(*ev)
		delete(am.active, rule.ID)
		if am.logger != nil {
			am.logger.Info("alert resolved",
				"rule", rule.Name, "metric", rule.Metric,
				"value", value, "duration", now.Sub(ev.FiredAt).String())
		}
	}
}

func (am *AlertManager) fireLocked(rule *AlertRule, value float64, now time.Time) {
	delete(am.pending, rule.ID)
	ev := &AlertEvent{
		ID:        am.nextID(),
		RuleID:    rule.ID,
		RuleName:  rule.Name,
		Metric:    rule.Metric,
		Value:     value,
		Threshold: rule.Threshold,
		Severity:  rule.Severity,
		FiredAt:   now,
	}
	am.active[rule.ID] = ev
	if am.logger != nil {
		am.logger.Warn("alert fired",
			"rule", rule.Name, "metric", rule.Metric,
			"value", value, "threshold", rule.Threshold, "severity", string(rule.Severity))
	}
	notifiers := am.notifiers
	go func(notifiers []AlertNotifier, e AlertEvent) {
		for _, n := range notifiers {
			if err := n.Notify(e); err != nil && am.logger != nil {
				am.logger.Error("alert notify failed", "err", err.Error())
			}
		}
	}(notifiers, *ev)
}

func (am *AlertManager) appendHistoryLocked(ev AlertEvent) {
	am.history = append(am.history, ev)
	if len(am.history) > am.maxHist {
		am.history = am.history[len(am.history)-am.maxHist:]
	}
}

func (am *AlertManager) nextID() string {
	return fmt.Sprintf("alert-%d-%d", time.Now().UnixNano(), am.seq.Add(1))
}

func compare(v float64, op CompareOp, t float64) bool {
	switch op {
	case OpGreater:
		return v > t
	case OpLess:
		return v < t
	case OpEqual:
		return v == t
	}
	return false
}
