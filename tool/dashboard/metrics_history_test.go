package dashboard

import (
	"testing"
	"time"

	"gamelib/middleware"
)

func TestMetricsHistory_GetHistory_Empty(t *testing.T) {
	h := NewMetricsHistory(nil)
	if pts := h.GetHistory(); pts != nil {
		t.Errorf("expected nil for empty history, got %d points", len(pts))
	}
}

func TestMetricsHistory_Sample(t *testing.T) {
	m := middleware.NewMetrics()
	h := NewMetricsHistory(m)
	h.lastSnap = m.Snapshot()
	h.lastTime = time.Now().Add(-5 * time.Second)

	// 模拟采样
	h.sample()

	pts := h.GetHistory()
	if len(pts) != 1 {
		t.Fatalf("expected 1 point, got %d", len(pts))
	}
	if pts[0].Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}
}

func TestMetricsHistory_CircularBuffer(t *testing.T) {
	m := middleware.NewMetrics()
	h := NewMetricsHistory(m)
	h.maxSize = 3
	h.points = make([]MetricsPoint, 3)
	h.lastSnap = m.Snapshot()
	h.lastTime = time.Now().Add(-time.Second)

	// 写入 5 个点（超过容量 3）
	for i := 0; i < 5; i++ {
		h.sample()
		h.lastTime = time.Now().Add(-time.Second)
	}

	pts := h.GetHistory()
	if len(pts) != 3 {
		t.Fatalf("expected 3 points (max), got %d", len(pts))
	}

	// 验证时间顺序
	for i := 1; i < len(pts); i++ {
		if pts[i].Timestamp.Before(pts[i-1].Timestamp) {
			t.Error("points should be in chronological order")
		}
	}
}
