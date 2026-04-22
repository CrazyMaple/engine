package stress

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// --- 自动化压测框架 ---

// ScenarioConfig 压测场景配置（DSL）
type ScenarioConfig struct {
	Name        string        `json:"name"`         // 场景名称
	Target      string        `json:"target"`       // 目标地址 (host:port)
	Concurrency int           `json:"concurrency"`  // 并发虚拟玩家数
	Duration    time.Duration `json:"duration"`      // 持续时间
	RampUp      time.Duration `json:"ramp_up"`       // 预热期（逐步增加并发）
	MsgRate     int           `json:"msg_rate"`      // 每个 Bot 每秒消息数
	Actions     []BotAction   `json:"actions"`       // Bot 行为序列
	Baseline    *Baseline     `json:"baseline"`      // CI 对比基线（可选）
}

// BotAction Bot 行为定义
type BotAction struct {
	Type    string                 `json:"type"`    // 行为类型: "send", "wait", "loop"
	Message string                 `json:"message"` // 消息类型名
	Args    map[string]interface{} `json:"args"`    // 消息参数
	DelayMs int                    `json:"delay_ms"` // 行为间隔
	Count   int                    `json:"count"`    // loop 时的循环次数
}

// Baseline CI 基线阈值
type Baseline struct {
	MaxP99Ms    float64 `json:"max_p99_ms"`     // P99 延迟上限（毫秒）
	MinTPS      float64 `json:"min_tps"`        // 最低 TPS
	MaxErrorPct float64 `json:"max_error_pct"`  // 最大错误率百分比
}

// LoadConfig 从 JSON 文件加载压测配置
func LoadConfig(path string) (*ScenarioConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	// 先解析为 raw 以处理 duration 字符串
	var raw struct {
		ScenarioConfig
		DurationStr string `json:"duration"`
		RampUpStr   string `json:"ramp_up"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg := raw.ScenarioConfig
	if raw.DurationStr != "" {
		cfg.Duration, _ = time.ParseDuration(raw.DurationStr)
	}
	if raw.RampUpStr != "" {
		cfg.RampUp, _ = time.ParseDuration(raw.RampUpStr)
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	if cfg.MsgRate <= 0 {
		cfg.MsgRate = 10
	}
	return &cfg, nil
}

// --- 指标收集 ---

// Metrics 压测指标收集器
type Metrics struct {
	TotalRequests  atomic.Int64
	TotalErrors    atomic.Int64
	TotalBytes     atomic.Int64
	latencies      []int64 // 微秒，受 mu 保护
	mu             sync.Mutex
	startTime      time.Time
	endTime        time.Time
}

// NewMetrics 创建指标收集器
func NewMetrics() *Metrics {
	return &Metrics{
		latencies: make([]int64, 0, 10000),
	}
}

// RecordRequest 记录一次请求
func (m *Metrics) RecordRequest(latencyUs int64, err bool) {
	m.TotalRequests.Add(1)
	if err {
		m.TotalErrors.Add(1)
	}
	m.mu.Lock()
	m.latencies = append(m.latencies, latencyUs)
	m.mu.Unlock()
}

// RecordBytes 记录传输字节数
func (m *Metrics) RecordBytes(n int64) {
	m.TotalBytes.Add(n)
}

// Report 压测报告
type Report struct {
	Scenario    string        `json:"scenario"`
	StartTime   time.Time     `json:"start_time"`
	Duration    time.Duration `json:"duration"`
	Concurrency int           `json:"concurrency"`

	// 吞吐量
	TotalRequests int64   `json:"total_requests"`
	TPS           float64 `json:"tps"`
	TotalBytes    int64   `json:"total_bytes"`

	// 延迟分布（毫秒）
	LatencyAvg float64 `json:"latency_avg_ms"`
	LatencyP50 float64 `json:"latency_p50_ms"`
	LatencyP90 float64 `json:"latency_p90_ms"`
	LatencyP95 float64 `json:"latency_p95_ms"`
	LatencyP99 float64 `json:"latency_p99_ms"`
	LatencyMax float64 `json:"latency_max_ms"`

	// 错误
	TotalErrors int64   `json:"total_errors"`
	ErrorRate   float64 `json:"error_rate_pct"`

	// CI 基线对比
	BaselinePass *bool   `json:"baseline_pass,omitempty"`
	BaselineMsg  string  `json:"baseline_msg,omitempty"`
}

// Snapshot 生成报告快照
func (m *Metrics) Snapshot(cfg *ScenarioConfig) *Report {
	m.mu.Lock()
	lats := make([]int64, len(m.latencies))
	copy(lats, m.latencies)
	m.mu.Unlock()

	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })

	dur := m.endTime.Sub(m.startTime)
	if dur <= 0 {
		dur = time.Second
	}

	total := m.TotalRequests.Load()
	errors := m.TotalErrors.Load()

	r := &Report{
		Scenario:      cfg.Name,
		StartTime:     m.startTime,
		Duration:      dur,
		Concurrency:   cfg.Concurrency,
		TotalRequests: total,
		TPS:           float64(total) / dur.Seconds(),
		TotalBytes:    m.TotalBytes.Load(),
		TotalErrors:   errors,
	}

	if total > 0 {
		r.ErrorRate = float64(errors) / float64(total) * 100
	}

	if len(lats) > 0 {
		var sum int64
		for _, v := range lats {
			sum += v
		}
		r.LatencyAvg = float64(sum) / float64(len(lats)) / 1000.0
		r.LatencyP50 = percentile(lats, 50) / 1000.0
		r.LatencyP90 = percentile(lats, 90) / 1000.0
		r.LatencyP95 = percentile(lats, 95) / 1000.0
		r.LatencyP99 = percentile(lats, 99) / 1000.0
		r.LatencyMax = float64(lats[len(lats)-1]) / 1000.0
	}

	// CI 基线对比
	if cfg.Baseline != nil {
		pass := true
		msgs := make([]string, 0)

		if cfg.Baseline.MaxP99Ms > 0 && r.LatencyP99 > cfg.Baseline.MaxP99Ms {
			pass = false
			msgs = append(msgs, fmt.Sprintf("P99 %.2fms > baseline %.2fms", r.LatencyP99, cfg.Baseline.MaxP99Ms))
		}
		if cfg.Baseline.MinTPS > 0 && r.TPS < cfg.Baseline.MinTPS {
			pass = false
			msgs = append(msgs, fmt.Sprintf("TPS %.0f < baseline %.0f", r.TPS, cfg.Baseline.MinTPS))
		}
		if cfg.Baseline.MaxErrorPct > 0 && r.ErrorRate > cfg.Baseline.MaxErrorPct {
			pass = false
			msgs = append(msgs, fmt.Sprintf("ErrorRate %.2f%% > baseline %.2f%%", r.ErrorRate, cfg.Baseline.MaxErrorPct))
		}

		r.BaselinePass = &pass
		if len(msgs) > 0 {
			r.BaselineMsg = fmt.Sprintf("FAIL: %v", msgs)
		} else {
			r.BaselineMsg = "PASS"
		}
	}

	return r
}

func percentile(sorted []int64, pct int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(pct)/100.0*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return float64(sorted[idx])
}

// --- Bot（虚拟玩家） ---

// BotConnector Bot 网络连接接口（由使用者实现具体的 TCP/WebSocket 连接）
type BotConnector interface {
	Connect(addr string) error
	Send(msgType string, args map[string]interface{}) (latencyUs int64, err error)
	Close() error
}

// Bot 虚拟玩家
type Bot struct {
	ID        int
	Connector BotConnector
	Actions   []BotAction
	MsgRate   int
	metrics   *Metrics
}

// Run 执行 Bot 行为序列
func (b *Bot) Run(ctx context.Context) {
	if b.Connector == nil {
		return
	}

	interval := time.Second / time.Duration(b.MsgRate)

	for {
		for _, action := range b.Actions {
			select {
			case <-ctx.Done():
				return
			default:
			}

			switch action.Type {
			case "send":
				start := time.Now()
				lat, err := b.Connector.Send(action.Message, action.Args)
				if lat == 0 {
					lat = time.Since(start).Microseconds()
				}
				b.metrics.RecordRequest(lat, err != nil)

			case "wait":
				delay := time.Duration(action.DelayMs) * time.Millisecond
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}

			case "loop":
				count := action.Count
				if count <= 0 {
					count = 1
				}
				for i := 0; i < count; i++ {
					select {
					case <-ctx.Done():
						return
					default:
					}
					lat, err := b.Connector.Send(action.Message, action.Args)
					b.metrics.RecordRequest(lat, err != nil)
					time.Sleep(interval)
				}
			}

			if action.DelayMs > 0 && action.Type != "wait" {
				time.Sleep(time.Duration(action.DelayMs) * time.Millisecond)
			}
		}
	}
}

// --- 压测引擎 ---

// BotFactory Bot 连接工厂函数
type BotFactory func(botID int) BotConnector

// Runner 压测运行器
type Runner struct {
	Config  *ScenarioConfig
	Factory BotFactory
	metrics *Metrics
}

// NewRunner 创建压测运行器
func NewRunner(cfg *ScenarioConfig, factory BotFactory) *Runner {
	return &Runner{
		Config:  cfg,
		Factory: factory,
		metrics: NewMetrics(),
	}
}

// Run 运行压测，返回报告
func (r *Runner) Run(ctx context.Context) (*Report, error) {
	cfg := r.Config
	r.metrics.startTime = time.Now()

	ctx, cancel := context.WithTimeout(ctx, cfg.Duration)
	defer cancel()

	var wg sync.WaitGroup
	rampDelay := time.Duration(0)
	if cfg.RampUp > 0 && cfg.Concurrency > 1 {
		rampDelay = cfg.RampUp / time.Duration(cfg.Concurrency)
	}

	for i := 0; i < cfg.Concurrency; i++ {
		// 预热期间逐步增加并发
		if rampDelay > 0 && i > 0 {
			select {
			case <-ctx.Done():
				break
			case <-time.After(rampDelay):
			}
		}

		connector := r.Factory(i)
		if connector == nil {
			continue
		}

		bot := &Bot{
			ID:        i,
			Connector: connector,
			Actions:   cfg.Actions,
			MsgRate:   cfg.MsgRate,
			metrics:   r.metrics,
		}

		if err := connector.Connect(cfg.Target); err != nil {
			r.metrics.RecordRequest(0, true)
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer connector.Close()
			bot.Run(ctx)
		}()
	}

	wg.Wait()
	r.metrics.endTime = time.Now()

	return r.metrics.Snapshot(cfg), nil
}

// RunFromFile 从配置文件运行压测
func RunFromFile(ctx context.Context, path string, factory BotFactory) (*Report, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	runner := NewRunner(cfg, factory)
	return runner.Run(ctx)
}

// --- 报告输出 ---

// WriteJSON 将报告写入 JSON 文件
func (r *Report) WriteJSON(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// WriteHTML 将报告写入 HTML 文件
func (r *Report) WriteHTML(path string) error {
	html := fmt.Sprintf(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Stress Test Report: %s</title>
<style>
body{font-family:sans-serif;margin:40px;background:#f5f5f5}
.card{background:white;border-radius:8px;padding:24px;margin:16px 0;box-shadow:0 2px 4px rgba(0,0,0,0.1)}
h1{color:#333} h2{color:#555;border-bottom:2px solid #eee;padding-bottom:8px}
table{width:100%%;border-collapse:collapse;margin:12px 0}
td,th{padding:8px 12px;text-align:left;border-bottom:1px solid #eee}
th{background:#f8f8f8;font-weight:600}
.pass{color:#2e7d32;font-weight:bold} .fail{color:#c62828;font-weight:bold}
.metric{font-size:28px;font-weight:bold;color:#1565c0}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:16px}
</style></head><body>
<h1>Stress Test Report</h1>
<div class="card">
<h2>%s</h2>
<p>Start: %s &nbsp;|&nbsp; Duration: %s &nbsp;|&nbsp; Concurrency: %d</p>
</div>

<div class="grid">
<div class="card"><div>TPS</div><div class="metric">%.0f</div></div>
<div class="card"><div>Total Requests</div><div class="metric">%d</div></div>
<div class="card"><div>Error Rate</div><div class="metric">%.2f%%</div></div>
<div class="card"><div>P99 Latency</div><div class="metric">%.2f ms</div></div>
</div>

<div class="card">
<h2>Latency Distribution</h2>
<table>
<tr><th>Percentile</th><th>Latency (ms)</th></tr>
<tr><td>P50</td><td>%.2f</td></tr>
<tr><td>P90</td><td>%.2f</td></tr>
<tr><td>P95</td><td>%.2f</td></tr>
<tr><td>P99</td><td>%.2f</td></tr>
<tr><td>Max</td><td>%.2f</td></tr>
<tr><td>Avg</td><td>%.2f</td></tr>
</table>
</div>
`,
		r.Scenario, r.Scenario,
		r.StartTime.Format(time.RFC3339), r.Duration, r.Concurrency,
		r.TPS, r.TotalRequests, r.ErrorRate, r.LatencyP99,
		r.LatencyP50, r.LatencyP90, r.LatencyP95, r.LatencyP99, r.LatencyMax, r.LatencyAvg,
	)

	if r.BaselinePass != nil {
		cls := "pass"
		if !*r.BaselinePass {
			cls = "fail"
		}
		html += fmt.Sprintf(`<div class="card">
<h2>CI Baseline</h2>
<p class="%s">%s</p>
</div>`, cls, r.BaselineMsg)
	}

	html += `</body></html>`
	return os.WriteFile(path, []byte(html), 0644)
}

// CheckBaseline 检查是否通过 CI 基线，用于 CI 集成（返回 exit code）
func (r *Report) CheckBaseline() bool {
	if r.BaselinePass == nil {
		return true
	}
	return *r.BaselinePass
}
