package middleware

import (
	"bytes"
	"strings"
	"testing"
)

func TestMetricsRegistry_IncCounter(t *testing.T) {
	r := NewMetricsRegistry()

	labels := map[string]string{"type": "*MyMsg"}
	r.IncCounter("engine_actor_message_total", "Total messages", labels, 1)
	r.IncCounter("engine_actor_message_total", "Total messages", labels, 1)
	r.IncCounter("engine_actor_message_total", "Total messages", labels, 3)

	var buf bytes.Buffer
	r.WritePrometheus(&buf)

	output := buf.String()
	if !strings.Contains(output, `engine_actor_message_total{type="*MyMsg"} 5`) {
		t.Errorf("expected counter value 5, got output:\n%s", output)
	}
	if !strings.Contains(output, "# HELP engine_actor_message_total Total messages") {
		t.Error("missing HELP header")
	}
	if !strings.Contains(output, "# TYPE engine_actor_message_total counter") {
		t.Error("missing TYPE header")
	}
}

func TestMetricsRegistry_MultipleLabels(t *testing.T) {
	r := NewMetricsRegistry()

	r.IncCounter("test_counter", "test", map[string]string{"type": "A"}, 10)
	r.IncCounter("test_counter", "test", map[string]string{"type": "B"}, 20)

	var buf bytes.Buffer
	r.WritePrometheus(&buf)

	output := buf.String()
	if !strings.Contains(output, `type="A"} 10`) {
		t.Errorf("missing type A counter, output:\n%s", output)
	}
	if !strings.Contains(output, `type="B"} 20`) {
		t.Errorf("missing type B counter, output:\n%s", output)
	}
}

func TestMetricsRegistry_NoLabels(t *testing.T) {
	r := NewMetricsRegistry()
	r.IncCounter("simple_counter", "simple test", nil, 42)

	var buf bytes.Buffer
	r.WritePrometheus(&buf)

	output := buf.String()
	if !strings.Contains(output, "simple_counter 42") {
		t.Errorf("expected no-label counter, got:\n%s", output)
	}
}

func TestMetricsRegistry_RegisterGauge(t *testing.T) {
	r := NewMetricsRegistry()

	r.RegisterGauge("engine_goroutine_count", "Number of goroutines", func() []GaugeValue {
		return []GaugeValue{{Value: 100}}
	})

	r.RegisterGauge("engine_cluster_member_count", "Cluster members by status", func() []GaugeValue {
		return []GaugeValue{
			{Labels: map[string]string{"status": "alive"}, Value: 3},
			{Labels: map[string]string{"status": "dead"}, Value: 1},
		}
	})

	var buf bytes.Buffer
	r.WritePrometheus(&buf)

	output := buf.String()
	if !strings.Contains(output, "engine_goroutine_count 100") {
		t.Errorf("missing goroutine gauge, output:\n%s", output)
	}
	if !strings.Contains(output, `engine_cluster_member_count{status="alive"} 3`) {
		t.Errorf("missing alive member gauge, output:\n%s", output)
	}
	if !strings.Contains(output, `engine_cluster_member_count{status="dead"} 1`) {
		t.Errorf("missing dead member gauge, output:\n%s", output)
	}
	if !strings.Contains(output, "# TYPE engine_goroutine_count gauge") {
		t.Error("missing gauge TYPE header")
	}
}

func TestMetricsRegistry_ConcurrentInc(t *testing.T) {
	r := NewMetricsRegistry()
	labels := map[string]string{"type": "test"}
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				r.IncCounter("concurrent_test", "test", labels, 1)
			}
			done <- struct{}{}
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	var buf bytes.Buffer
	r.WritePrometheus(&buf)
	output := buf.String()
	if !strings.Contains(output, `concurrent_test{type="test"} 1000`) {
		t.Errorf("expected 1000, got:\n%s", output)
	}
}

func TestLabelsToString(t *testing.T) {
	tests := []struct {
		labels map[string]string
		want   string
	}{
		{nil, ""},
		{map[string]string{}, ""},
		{map[string]string{"a": "1"}, `a="1"`},
		{map[string]string{"b": "2", "a": "1"}, `a="1",b="2"`}, // sorted
	}
	for _, tt := range tests {
		got := labelsToString(tt.labels)
		if got != tt.want {
			t.Errorf("labelsToString(%v) = %q, want %q", tt.labels, got, tt.want)
		}
	}
}
