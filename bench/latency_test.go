package bench

import (
	"strings"
	"testing"
)

func TestLatencyRecorderPercentiles(t *testing.T) {
	rec := NewLatencyRecorder(100)
	for i := int64(1); i <= 100; i++ {
		rec.Add(i * 1000)
	}
	p := rec.Percentiles()
	if p.P50 <= 0 || p.P95 <= 0 || p.P99 <= 0 {
		t.Fatalf("percentiles should be positive: %+v", p)
	}
	if !(p.P50 < p.P95 && p.P95 < p.P99) {
		t.Fatalf("monotonic order broken: %+v", p)
	}
	if p.Max != 100000 {
		t.Errorf("max: %v", p.Max)
	}
}

func TestLatencyRecorderEmpty(t *testing.T) {
	rec := NewLatencyRecorder(0)
	p := rec.Percentiles()
	if p.P50 != 0 || p.P99 != 0 {
		t.Errorf("empty recorder should return zero: %+v", p)
	}
}

func TestLatencyApplyTo(t *testing.T) {
	rec := NewLatencyRecorder(10)
	for i := 1; i <= 10; i++ {
		rec.Add(int64(i))
	}
	r := BenchResult{Name: "X"}
	rec.Percentiles().ApplyTo(&r)
	if r.P50Ns == 0 || r.P99Ns == 0 {
		t.Errorf("not applied: %+v", r)
	}
}

func TestParseBenchLineWithTagAndPercentiles(t *testing.T) {
	line := "BenchmarkX/aes-gcm-1KB-8  100  1234 ns/op  500 p50-ns/op  900 p99-ns/op  64 B/op  2 allocs/op"
	results, err := ParseBenchOutput(strings.NewReader(line))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results: %d", len(results))
	}
	r := results[0]
	if r.Name != "BenchmarkX" {
		t.Errorf("name: %q", r.Name)
	}
	if r.Tag != "aes-gcm-1KB" {
		t.Errorf("tag: %q", r.Tag)
	}
	if r.P50Ns != 500 || r.P99Ns != 900 {
		t.Errorf("percentiles: %+v", r)
	}
	if r.BytesPerOp != 64 || r.AllocsPerOp != 2 {
		t.Errorf("mem stats: %+v", r)
	}
}

func TestBenchResultKeyWithTag(t *testing.T) {
	r1 := BenchResult{Name: "B", Package: "engine/bench", Tag: "aes-gcm"}
	r2 := BenchResult{Name: "B", Package: "engine/bench", Tag: "plaintext"}
	if r1.Key() == r2.Key() {
		t.Errorf("tags should differentiate key: %s vs %s", r1.Key(), r2.Key())
	}
	r3 := BenchResult{Name: "B", Package: "engine/bench"}
	if !strings.HasSuffix(r3.Key(), "B") {
		t.Errorf("untagged key malformed: %s", r3.Key())
	}
}

func TestHTMLReportWithPercentiles(t *testing.T) {
	base := &Baseline{
		Results: map[string]BenchResult{
			"BenchmarkLatency": {Name: "BenchmarkLatency", NsPerOp: 1000, P50Ns: 900, P95Ns: 1200, P99Ns: 2000},
		},
	}
	cur := []BenchResult{
		{Name: "BenchmarkLatency", NsPerOp: 1100, P50Ns: 950, P95Ns: 1300, P99Ns: 2100},
	}
	r := Compare(base, cur, DefaultThresholds())
	html := string(HTMLReport(r, base))
	for _, s := range []string{"Latency Percentiles", "P99=", "<svg"} {
		if !strings.Contains(html, s) {
			t.Errorf("missing snippet %q", s)
		}
	}
}
