package dashboard

import (
	"sync"
	"time"

	"gamelib/middleware"
)

const (
	defaultSampleInterval = 5 * time.Second
	defaultMaxPoints      = 60 // 5 分钟窗口（60 * 5s）
)

// MetricsPoint 单个采样点
type MetricsPoint struct {
	Timestamp time.Time          `json:"ts"`
	MsgRate   map[string]float64 `json:"msg_rate"`  // 每秒消息数
	MsgCount  map[string]int64   `json:"msg_count"` // 绝对计数
}

// MetricsHistory 消息流量历史记录，环形缓冲存储
type MetricsHistory struct {
	mu       sync.RWMutex
	points   []MetricsPoint
	head     int
	count    int
	maxSize  int
	metrics  *middleware.Metrics
	interval time.Duration
	stopCh   chan struct{}
	lastSnap middleware.MetricsSnapshot
	lastTime time.Time
}

// NewMetricsHistory 创建消息流量历史记录器
func NewMetricsHistory(metrics *middleware.Metrics) *MetricsHistory {
	return &MetricsHistory{
		points:   make([]MetricsPoint, defaultMaxPoints),
		maxSize:  defaultMaxPoints,
		metrics:  metrics,
		interval: defaultSampleInterval,
		stopCh:   make(chan struct{}),
	}
}

// Start 启动后台采样
func (h *MetricsHistory) Start() {
	if h.metrics == nil {
		return
	}
	// 初始化基线快照
	h.lastSnap = h.metrics.Snapshot()
	h.lastTime = time.Now()

	go h.sampleLoop()
}

// Stop 停止采样
func (h *MetricsHistory) Stop() {
	close(h.stopCh)
}

// GetHistory 返回所有采样点（按时间顺序）
func (h *MetricsHistory) GetHistory() []MetricsPoint {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.count == 0 {
		return nil
	}

	result := make([]MetricsPoint, h.count)
	for i := 0; i < h.count; i++ {
		idx := (h.head - h.count + i + h.maxSize) % h.maxSize
		result[i] = h.points[idx]
	}
	return result
}

func (h *MetricsHistory) sampleLoop() {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.sample()
		}
	}
}

func (h *MetricsHistory) sample() {
	now := time.Now()
	snap := h.metrics.Snapshot()
	elapsed := now.Sub(h.lastTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	// 计算每秒速率
	rates := make(map[string]float64)
	for typeName, count := range snap.MsgCount {
		prev := h.lastSnap.MsgCount[typeName]
		delta := count - prev
		if delta < 0 {
			delta = 0
		}
		rates[typeName] = float64(delta) / elapsed
	}

	point := MetricsPoint{
		Timestamp: now,
		MsgRate:   rates,
		MsgCount:  snap.MsgCount,
	}

	h.mu.Lock()
	h.points[h.head] = point
	h.head = (h.head + 1) % h.maxSize
	if h.count < h.maxSize {
		h.count++
	}
	h.mu.Unlock()

	h.lastSnap = snap
	h.lastTime = now
}
