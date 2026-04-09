package actor

import (
	"fmt"
	"sync"
	"time"
)

// DeadLetterMetrics 死信指标接口，由 middleware 包适配实现
type DeadLetterMetrics interface {
	IncCounter(name, help string, labels map[string]string, delta int64)
}

// DeadLetterMonitor 增强版死信监控，提供指标、告警和持久化能力
type DeadLetterMonitor struct {
	eventStream *EventStream
	sub         *Subscription
	metrics     DeadLetterMetrics

	// 统计
	mu         sync.RWMutex
	totalCount int64
	typeCounts map[string]int64

	// 告警
	alertThreshold int
	alertWindow    time.Duration
	windowCount    int64
	windowStart    time.Time

	// 持久化存储（最近 N 条）
	records  []DeadLetterRecord
	maxStore int
}

// DeadLetterRecord 死信记录
type DeadLetterRecord struct {
	Timestamp time.Time `json:"timestamp"`
	TargetPID string    `json:"target_pid"`
	SenderPID string    `json:"sender_pid,omitempty"`
	MsgType   string    `json:"msg_type"`
}

// DeadLetterAlertEvent 死信频率告警事件（通过 EventStream 发布）
type DeadLetterAlertEvent struct {
	Count  int64         `json:"count"`
	Window time.Duration `json:"window"`
	Time   time.Time     `json:"time"`
}

// DeadLetterMonitorConfig 死信监控配置
type DeadLetterMonitorConfig struct {
	// AlertThreshold 告警阈值（窗口内死信数，0 禁用告警）
	AlertThreshold int
	// AlertWindow 告警窗口时间（默认 1 分钟）
	AlertWindow time.Duration
	// MaxStoredRecords 最大存储记录数（默认 500）
	MaxStoredRecords int
	// Metrics 指标接口（可选，由 middleware.MetricsRegistry 适配）
	Metrics DeadLetterMetrics
}

// DeadLetterStats 死信统计信息
type DeadLetterStats struct {
	TotalCount int64            `json:"total_count"`
	TypeCounts map[string]int64 `json:"type_counts"`
}

// NewDeadLetterMonitor 创建死信监控器
func NewDeadLetterMonitor(es *EventStream, cfg DeadLetterMonitorConfig) *DeadLetterMonitor {
	if cfg.AlertWindow <= 0 {
		cfg.AlertWindow = time.Minute
	}
	if cfg.MaxStoredRecords <= 0 {
		cfg.MaxStoredRecords = 500
	}

	m := &DeadLetterMonitor{
		eventStream:    es,
		metrics:        cfg.Metrics,
		typeCounts:     make(map[string]int64),
		alertThreshold: cfg.AlertThreshold,
		alertWindow:    cfg.AlertWindow,
		windowStart:    time.Now(),
		records:        make([]DeadLetterRecord, 0, 64),
		maxStore:       cfg.MaxStoredRecords,
	}

	m.sub = es.Subscribe(func(event interface{}) {
		if dl, ok := event.(*DeadLetterEvent); ok {
			m.onDeadLetter(dl)
		}
	})

	return m
}

// Stop 停止监控
func (m *DeadLetterMonitor) Stop() {
	if m.sub != nil {
		m.sub.Unsubscribe()
	}
}

// Stats 返回死信统计快照
func (m *DeadLetterMonitor) Stats() DeadLetterStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tc := make(map[string]int64, len(m.typeCounts))
	for k, v := range m.typeCounts {
		tc[k] = v
	}
	return DeadLetterStats{
		TotalCount: m.totalCount,
		TypeCounts: tc,
	}
}

// RecentRecords 返回最近的 N 条死信记录
func (m *DeadLetterMonitor) RecentRecords(n int) []DeadLetterRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if n <= 0 || len(m.records) == 0 {
		return nil
	}
	start := len(m.records) - n
	if start < 0 {
		start = 0
	}
	result := make([]DeadLetterRecord, len(m.records)-start)
	copy(result, m.records[start:])
	return result
}

func (m *DeadLetterMonitor) onDeadLetter(dl *DeadLetterEvent) {
	msgType := fmt.Sprintf("%T", dl.Message)
	now := time.Now()

	m.mu.Lock()
	m.totalCount++
	m.typeCounts[msgType]++

	// 存储记录
	record := DeadLetterRecord{
		Timestamp: now,
		MsgType:   msgType,
	}
	if dl.PID != nil {
		record.TargetPID = dl.PID.String()
	}
	if dl.Sender != nil {
		record.SenderPID = dl.Sender.String()
	}

	if len(m.records) >= m.maxStore {
		drop := m.maxStore / 10
		if drop == 0 {
			drop = 1
		}
		m.records = m.records[drop:]
	}
	m.records = append(m.records, record)

	// 告警窗口检查
	shouldAlert := false
	if m.alertThreshold > 0 {
		if now.Sub(m.windowStart) > m.alertWindow {
			m.windowStart = now
			m.windowCount = 1
		} else {
			m.windowCount++
			if m.windowCount >= int64(m.alertThreshold) {
				shouldAlert = true
				m.windowCount = 0
				m.windowStart = now
			}
		}
	}
	m.mu.Unlock()

	// 更新指标（锁外）
	if m.metrics != nil {
		m.metrics.IncCounter(
			"engine_deadletter_total",
			"Total dead letter messages by type",
			map[string]string{"msg_type": msgType},
			1,
		)
	}

	// 发布告警事件（锁外）
	if shouldAlert {
		m.eventStream.Publish(&DeadLetterAlertEvent{
			Count:  int64(m.alertThreshold),
			Window: m.alertWindow,
			Time:   now,
		})
	}
}
