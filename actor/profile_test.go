package actor

import (
	"testing"
	"time"
)

func TestHotActorProfiler_Disabled(t *testing.T) {
	p := NewHotActorProfiler(HotActorProfilerConfig{})
	p.Record("pid-1", 100*time.Millisecond)
	if s := p.SnapshotFor("pid-1"); s != nil {
		t.Errorf("disabled profiler should not record, got %+v", s)
	}
}

func TestHotActorProfiler_BasicPercentiles(t *testing.T) {
	p := NewHotActorProfiler(HotActorProfilerConfig{WindowSize: 100, HotP99Threshold: 10 * time.Millisecond, MinSamples: 10})
	p.Enable()
	for i := 1; i <= 100; i++ {
		p.Record("pid-a", time.Duration(i)*time.Millisecond)
	}
	snap := p.SnapshotFor("pid-a")
	if snap == nil {
		t.Fatal("no snapshot")
	}
	if snap.Samples != 100 {
		t.Errorf("samples=%d", snap.Samples)
	}
	// P50 近似 50ms，P99 近似 99ms
	if snap.P50Ns < 45e6 || snap.P50Ns > 55e6 {
		t.Errorf("P50 out of range: %.0f ns", snap.P50Ns)
	}
	if snap.P99Ns < 95e6 {
		t.Errorf("P99 too low: %.0f ns", snap.P99Ns)
	}
	if !snap.IsHot {
		t.Errorf("should be hot: P99=%.0fns threshold=%v", snap.P99Ns, p.cfg.HotP99Threshold)
	}
}

func TestHotActorProfiler_MinSamplesGate(t *testing.T) {
	p := NewHotActorProfiler(HotActorProfilerConfig{WindowSize: 10, HotP99Threshold: time.Millisecond, MinSamples: 50})
	p.Enable()
	for i := 0; i < 10; i++ {
		p.Record("pid-x", 100*time.Millisecond)
	}
	snap := p.SnapshotFor("pid-x")
	if snap.IsHot {
		t.Errorf("should not be hot with samples=%d < min=%d", snap.Samples, p.cfg.MinSamples)
	}
}

func TestHotActorProfiler_RingBufferOverflow(t *testing.T) {
	p := NewHotActorProfiler(HotActorProfilerConfig{WindowSize: 5, MinSamples: 1})
	p.Enable()
	// 先写 10 条不同值，ring buffer 大小 5，应保留后 5 条
	for i := 1; i <= 10; i++ {
		p.Record("pid-y", time.Duration(i)*time.Millisecond)
	}
	snap := p.SnapshotFor("pid-y")
	if snap.Samples != 5 {
		t.Errorf("expected window 5, got %d", snap.Samples)
	}
	if snap.MsgCount != 10 {
		t.Errorf("msg_count should be 10, got %d", snap.MsgCount)
	}
	// P50 在 [6..10] 中间，应接近 8ms
	if snap.P50Ns < 7e6 || snap.P50Ns > 9e6 {
		t.Errorf("P50 unexpected: %.0f ns", snap.P50Ns)
	}
}

func TestHotActorProfiler_TopNAndCandidates(t *testing.T) {
	p := NewHotActorProfiler(HotActorProfilerConfig{WindowSize: 50, HotP99Threshold: 30 * time.Millisecond, MinSamples: 10})
	p.Enable()

	// pid-fast：全 1ms（非热点）
	for i := 0; i < 30; i++ {
		p.Record("pid-fast", time.Millisecond)
	}
	// pid-hot：全 100ms（热点）
	for i := 0; i < 30; i++ {
		p.Record("pid-hot", 100*time.Millisecond)
	}
	// pid-medium：10ms（非热点）
	for i := 0; i < 30; i++ {
		p.Record("pid-medium", 10*time.Millisecond)
	}

	top := p.TopN(2, false)
	if len(top) != 2 {
		t.Fatalf("topN=2 returned %d", len(top))
	}
	if top[0].PID != "pid-hot" {
		t.Errorf("top[0] should be pid-hot, got %s", top[0].PID)
	}

	candidates := p.MigrationCandidates()
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d: %+v", len(candidates), candidates)
	}
	if candidates[0].PID != "pid-hot" {
		t.Errorf("candidate should be pid-hot: %s", candidates[0].PID)
	}
}

func TestHotActorProfiler_Reset(t *testing.T) {
	p := NewHotActorProfiler(HotActorProfilerConfig{MinSamples: 1})
	p.Enable()
	p.Record("pid-z", time.Millisecond)
	if p.SnapshotFor("pid-z") == nil {
		t.Fatal("missing before reset")
	}
	p.Reset()
	if p.SnapshotFor("pid-z") != nil {
		t.Errorf("reset should clear")
	}
}

func TestPercentileAt_EdgeCases(t *testing.T) {
	if percentileAt(nil, 0.5) != 0 {
		t.Errorf("nil should be 0")
	}
	if percentileAt([]int64{42}, 0.99) != 42 {
		t.Errorf("single sample should return itself")
	}
	// 已排序 [10,20,30,40,50]，P50 = 30 (正中间)
	v := percentileAt([]int64{10, 20, 30, 40, 50}, 0.50)
	if v != 30 {
		t.Errorf("P50 want 30 got %v", v)
	}
	// P100 应返回最大值
	if percentileAt([]int64{10, 20, 30, 40, 50}, 1.0) != 50 {
		t.Errorf("P100 should be max")
	}
}
