package dashboard

import (
	"sync/atomic"
	"testing"
	"time"
)

type captureNotifier struct {
	count atomic.Int64
	last  atomic.Value // AlertEvent
}

func (c *captureNotifier) Notify(e AlertEvent) error {
	c.count.Add(1)
	c.last.Store(e)
	return nil
}

func TestAlertManager_FireImmediateAndResolve(t *testing.T) {
	am := NewAlertManager(16)
	cn := &captureNotifier{}
	am.AddNotifier(cn)
	if err := am.SetRule(AlertRule{
		ID: "cpu-high", Name: "cpu-high", Metric: "cpu", Op: OpGreater,
		Threshold: 0.8, Severity: SeverityWarning, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	am.SubmitAt(MetricSample{Metric: "cpu", Value: 0.9}, now)

	if got := am.Active(); len(got) != 1 {
		t.Fatalf("want 1 active alert, got %d", len(got))
	}

	// 通知是异步的，给一个短窗口
	deadline := time.Now().Add(time.Second)
	for cn.count.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if cn.count.Load() == 0 {
		t.Fatal("notifier not invoked")
	}

	// 恢复
	am.SubmitAt(MetricSample{Metric: "cpu", Value: 0.5}, now.Add(time.Second))
	if got := am.Active(); len(got) != 0 {
		t.Fatalf("want 0 active alert after recovery, got %d", len(got))
	}
	if got := am.History(0); len(got) != 1 {
		t.Fatalf("history should contain 1 resolved entry, got %d", len(got))
	}
	if got := am.History(0)[0]; got.ResolvedAt == nil {
		t.Fatal("resolved time not set")
	}
}

func TestAlertManager_DurationDebounce(t *testing.T) {
	am := NewAlertManager(8)
	if err := am.SetRule(AlertRule{
		ID: "lat", Name: "latency", Metric: "lat", Op: OpGreater,
		Threshold: 100, Duration: 5 * time.Second, Severity: SeverityCritical, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	t0 := time.Now()
	am.SubmitAt(MetricSample{Metric: "lat", Value: 200}, t0) // 进入 pending
	if len(am.Active()) != 0 {
		t.Fatal("should not fire before duration met")
	}
	am.SubmitAt(MetricSample{Metric: "lat", Value: 150}, t0.Add(2*time.Second))
	if len(am.Active()) != 0 {
		t.Fatal("still pending")
	}
	am.SubmitAt(MetricSample{Metric: "lat", Value: 180}, t0.Add(6*time.Second))
	if len(am.Active()) != 1 {
		t.Fatal("should have fired after duration met")
	}
}

func TestAlertManager_SilenceAndAck(t *testing.T) {
	am := NewAlertManager(8)
	_ = am.SetRule(AlertRule{
		ID: "r1", Name: "r1", Metric: "m", Op: OpGreater,
		Threshold: 10, Enabled: true,
	})
	now := time.Now()

	// 静默 1s
	am.Silence("r1", now.Add(time.Second))
	am.SubmitAt(MetricSample{Metric: "m", Value: 100}, now)
	if len(am.Active()) != 0 {
		t.Fatal("silenced alert should not fire")
	}

	// 静默到期后再触发
	am.SubmitAt(MetricSample{Metric: "m", Value: 100}, now.Add(2*time.Second))
	active := am.Active()
	if len(active) != 1 {
		t.Fatalf("expected fire after silence: %d", len(active))
	}

	// Ack
	if !am.Ack(active[0].ID, "ops") {
		t.Fatal("ack failed")
	}
	if got := am.Active(); got[0].AckedBy != "ops" {
		t.Fatal("ack info not stored")
	}
}

func TestAlertManager_DeleteRule(t *testing.T) {
	am := NewAlertManager(4)
	_ = am.SetRule(AlertRule{ID: "x", Name: "x", Metric: "v", Op: OpEqual, Threshold: 1, Enabled: true})
	am.Submit(MetricSample{Metric: "v", Value: 1})
	if len(am.Active()) != 1 {
		t.Fatal("rule should fire")
	}
	am.DeleteRule("x")
	if len(am.Rules()) != 0 || len(am.Active()) != 0 {
		t.Fatal("rule + active should be cleared")
	}
}

func TestAlertManager_InvalidOp(t *testing.T) {
	am := NewAlertManager(4)
	if err := am.SetRule(AlertRule{ID: "bad", Op: "xx"}); err == nil {
		t.Fatal("expected validation error")
	}
}
