package stress

import (
	"path/filepath"
	"testing"
	"time"
)

func TestBaselines_UpdateAndDiff(t *testing.T) {
	dir := t.TempDir()
	bs, err := LoadPerfBaselines(filepath.Join(dir, "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	bs.Update("login", map[string]float64{"p99_ms": 100, "tps": 1000})

	diff := bs.Diff("login", map[string]float64{"p99_ms": 110, "tps": 950})
	if got := diff["p99_ms"]; got < 9.9 || got > 10.1 {
		t.Fatalf("p99 diff want ~10%%, got %.3f", got)
	}
	if got := diff["tps"]; got > -4.9 || got < -5.1 {
		t.Fatalf("tps diff want ~-5%%, got %.3f", got)
	}

	report := bs.Regression("login",
		map[string]float64{"p99_ms": 110, "tps": 950},
		map[string]float64{"p99_ms": 5, "tps": 10})
	if _, ok := report["p99_ms"]; !ok {
		t.Fatal("expected p99_ms regression")
	}
	if _, ok := report["tps"]; ok {
		t.Fatal("tps within threshold, should not be regression")
	}
}

func TestBaselines_PersistRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	bs, _ := LoadPerfBaselines(path)
	bs.Update("a", map[string]float64{"x": 1.5})
	if err := bs.Save(); err != nil {
		t.Fatal(err)
	}
	bs2, err := LoadPerfBaselines(path)
	if err != nil {
		t.Fatal(err)
	}
	if bs2.Items["a"].Metrics["x"] != 1.5 {
		t.Fatalf("loaded value mismatch: %+v", bs2.Items["a"])
	}
	if names := bs2.Names(); len(names) != 1 || names[0] != "a" {
		t.Fatalf("names mismatch: %v", names)
	}
}

func TestBaselines_HistoryAccumulates(t *testing.T) {
	bs, _ := LoadPerfBaselines(filepath.Join(t.TempDir(), "b.json"))
	bs.Update("svc", map[string]float64{"x": 1})
	bs.Update("svc", map[string]float64{"x": 2})
	bs.Update("svc", map[string]float64{"x": 3})
	if got := len(bs.Hist["svc"]); got != 2 {
		t.Fatalf("history want 2, got %d", got)
	}
}

func TestAdaptiveTimeout(t *testing.T) {
	if got := AdaptiveTimeout(3, 10*time.Second, 5*time.Second); got != 25*time.Second {
		t.Fatalf("AdaptiveTimeout(3) = %v, want 25s", got)
	}
	if got := AdaptiveTimeout(0, 10*time.Second, 5*time.Second); got != 10*time.Second {
		t.Fatalf("AdaptiveTimeout(0) = %v, want 10s", got)
	}
}

func TestRetry_ShortCircuit(t *testing.T) {
	calls := 0
	ok := Retry(3, time.Millisecond, func() bool {
		calls++
		return calls == 2
	})
	if !ok {
		t.Fatal("retry should succeed on second try")
	}
	if calls != 2 {
		t.Fatalf("want 2 calls, got %d", calls)
	}
}

func TestRetry_AllFail(t *testing.T) {
	calls := 0
	ok := Retry(2, time.Millisecond, func() bool {
		calls++
		return false
	})
	if ok {
		t.Fatal("retry should fail after exhausting attempts")
	}
	if calls != 2 {
		t.Fatalf("want 2 calls, got %d", calls)
	}
}
