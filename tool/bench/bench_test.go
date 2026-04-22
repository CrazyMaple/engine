package bench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleOutput = `goos: linux
goarch: amd64
pkg: engine/actor
BenchmarkActorSend-8         	 1000000	      1234 ns/op	     256 B/op	       3 allocs/op
BenchmarkActorReceive-8      	  500000	      2345 ns/op	     128 B/op	       1 allocs/op
BenchmarkMailbox-8           	 2000000	       654 ns/op
PASS
ok  	engine/actor	2.345s
pkg: engine/remote
BenchmarkRemoteSend-8        	  200000	      5678 ns/op	    1024 B/op	       5 allocs/op
PASS
ok  	engine/remote	1.234s
`

func TestParseBenchOutput(t *testing.T) {
	results, err := ParseBenchOutput(strings.NewReader(sampleOutput))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	// 第一个：ActorSend
	if results[0].Name != "BenchmarkActorSend" {
		t.Errorf("name: %q", results[0].Name)
	}
	if results[0].Package != "engine/actor" {
		t.Errorf("package: %q", results[0].Package)
	}
	if results[0].Iterations != 1000000 {
		t.Errorf("iters: %d", results[0].Iterations)
	}
	if results[0].NsPerOp != 1234 {
		t.Errorf("ns/op: %v", results[0].NsPerOp)
	}
	if results[0].BytesPerOp != 256 || results[0].AllocsPerOp != 3 {
		t.Errorf("mem: %d B/op, %d allocs", results[0].BytesPerOp, results[0].AllocsPerOp)
	}

	// 最后一个来自另一个 package
	if results[3].Package != "engine/remote" {
		t.Errorf("pkg: %q", results[3].Package)
	}
	if results[3].Name != "BenchmarkRemoteSend" {
		t.Errorf("remote name: %q", results[3].Name)
	}
}

func TestBaselineStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")
	store := NewBaselineStore(path)
	if _, err := store.Load(); err != nil {
		t.Fatal(err)
	}

	results := []BenchResult{
		{Name: "BenchmarkA", Package: "engine/actor", NsPerOp: 100, BytesPerOp: 16, AllocsPerOp: 1, Iterations: 100},
		{Name: "BenchmarkB", Package: "engine/remote", NsPerOp: 200, Iterations: 50},
	}
	store.Update(results, "abc1234")
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}

	reload := NewBaselineStore(path)
	data, err := reload.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Results) != 2 {
		t.Errorf("results: %d", len(data.Results))
	}
	if data.Results["engine/actor.BenchmarkA"].NsPerOp != 100 {
		t.Errorf("A mismatch")
	}
	if data.Commit != "abc1234" {
		t.Errorf("commit: %q", data.Commit)
	}
}

func TestBaselineHistoryAccumulates(t *testing.T) {
	store := NewBaselineStore(filepath.Join(t.TempDir(), "b.json"))
	store.Load()

	store.Update([]BenchResult{{Name: "X", NsPerOp: 100, Iterations: 100}}, "v1")
	store.Update([]BenchResult{{Name: "X", NsPerOp: 110, Iterations: 100}}, "v2")
	store.Update([]BenchResult{{Name: "X", NsPerOp: 105, Iterations: 100}}, "v3")

	data, _ := store.Data()
	if data.Results["X"].NsPerOp != 105 {
		t.Errorf("current X: %v", data.Results["X"].NsPerOp)
	}
	if len(data.History["X"]) != 2 {
		t.Errorf("history len: %d", len(data.History["X"]))
	}
	if data.History["X"][0].NsPerOp != 100 || data.History["X"][1].NsPerOp != 110 {
		t.Errorf("history content: %+v", data.History["X"])
	}
}

func TestCompareNoRegression(t *testing.T) {
	base := &Baseline{
		Results: map[string]BenchResult{
			"BenchmarkA": {Name: "BenchmarkA", NsPerOp: 1000, BytesPerOp: 100, AllocsPerOp: 5},
		},
	}
	cur := []BenchResult{
		{Name: "BenchmarkA", NsPerOp: 1010}, // 1% 变化
	}
	r := Compare(base, cur, DefaultThresholds())
	if r.MajorCount != 0 || r.MinorCount != 0 {
		t.Errorf("unexpected regression: %+v", r)
	}
}

func TestCompareMinorRegression(t *testing.T) {
	base := &Baseline{
		Results: map[string]BenchResult{
			"BenchmarkA": {Name: "BenchmarkA", NsPerOp: 1000},
		},
	}
	cur := []BenchResult{
		{Name: "BenchmarkA", NsPerOp: 1080}, // 8% 回退
	}
	r := Compare(base, cur, DefaultThresholds())
	if r.MinorCount != 1 || r.MajorCount != 0 {
		t.Errorf("minor expected, got: %+v", r)
	}
	if r.Entries[0].Level != RegressionMinor {
		t.Errorf("level: %v", r.Entries[0].Level)
	}
}

func TestCompareMajorRegression(t *testing.T) {
	base := &Baseline{
		Results: map[string]BenchResult{
			"BenchmarkA": {Name: "BenchmarkA", NsPerOp: 1000},
		},
	}
	cur := []BenchResult{
		{Name: "BenchmarkA", NsPerOp: 1200}, // 20% 回退
	}
	r := Compare(base, cur, DefaultThresholds())
	if r.MajorCount != 1 {
		t.Errorf("major expected, got: %+v", r)
	}
	if !r.HasRegression() {
		t.Error("HasRegression should be true")
	}
}

func TestCompareImproved(t *testing.T) {
	base := &Baseline{
		Results: map[string]BenchResult{
			"BenchmarkA": {Name: "BenchmarkA", NsPerOp: 1000},
		},
	}
	cur := []BenchResult{
		{Name: "BenchmarkA", NsPerOp: 800}, // -20%
	}
	r := Compare(base, cur, DefaultThresholds())
	if r.ImprovedCount != 1 {
		t.Errorf("improved expected, got: %+v", r)
	}
	if r.Entries[0].Level != RegressionImproved {
		t.Errorf("level: %v", r.Entries[0].Level)
	}
}

func TestCompareMissing(t *testing.T) {
	base := &Baseline{Results: map[string]BenchResult{}}
	cur := []BenchResult{
		{Name: "NewBench", NsPerOp: 500},
	}
	r := Compare(base, cur, DefaultThresholds())
	if r.MissingCount != 1 {
		t.Errorf("missing expected: %+v", r)
	}
	if r.Entries[0].Level != RegressionMissing {
		t.Errorf("level: %v", r.Entries[0].Level)
	}
}

func TestCompareNilBaseline(t *testing.T) {
	cur := []BenchResult{
		{Name: "X", NsPerOp: 500},
	}
	r := Compare(nil, cur, DefaultThresholds())
	if r.MissingCount != 1 {
		t.Errorf("nil baseline should mark all missing: %+v", r)
	}
}

func TestCompareShortBenchIgnoresNoise(t *testing.T) {
	// ns/op 低于 100 的基准，20% 波动仍算 none
	base := &Baseline{
		Results: map[string]BenchResult{
			"Fast": {Name: "Fast", NsPerOp: 50},
		},
	}
	cur := []BenchResult{
		{Name: "Fast", NsPerOp: 60}, // +20% 但绝对值小
	}
	r := Compare(base, cur, DefaultThresholds())
	if r.MinorCount != 0 && r.MajorCount != 0 {
		t.Errorf("short bench should be ignored: %+v", r)
	}
}

func TestTextSummary(t *testing.T) {
	base := &Baseline{Results: map[string]BenchResult{
		"BenchmarkA": {Name: "BenchmarkA", NsPerOp: 1000},
	}}
	cur := []BenchResult{{Name: "BenchmarkA", NsPerOp: 1200}}
	r := Compare(base, cur, DefaultThresholds())
	text := r.TextSummary()
	if !strings.Contains(text, "BenchmarkA") {
		t.Error("summary missing name")
	}
	if !strings.Contains(text, "MAJOR") {
		t.Error("summary missing severity marker")
	}
}

func TestHTMLReport(t *testing.T) {
	base := &Baseline{
		Results: map[string]BenchResult{
			"BenchmarkA": {Name: "BenchmarkA", NsPerOp: 1000},
		},
		History: map[string][]BenchResult{
			"BenchmarkA": {
				{Name: "BenchmarkA", NsPerOp: 900},
				{Name: "BenchmarkA", NsPerOp: 950},
			},
		},
	}
	cur := []BenchResult{{Name: "BenchmarkA", NsPerOp: 1100}}
	r := Compare(base, cur, DefaultThresholds())
	html := string(HTMLReport(r, base))
	for _, s := range []string{
		"<title>Engine Bench Report</title>",
		"BenchmarkA",
		"<svg",
		"Totals",
	} {
		if !strings.Contains(html, s) {
			t.Errorf("missing HTML snippet %q", s)
		}
	}
}

func TestParseBenchOutputIgnoresBadLines(t *testing.T) {
	input := `BenchmarkGood-8  1000  1000 ns/op
this is not a benchmark
BenchmarkNoNs-8  100
BenchmarkValid-4  200  500 ns/op`
	results, err := ParseBenchOutput(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 valid results, got %d: %+v", len(results), results)
	}
	if results[0].Name != "BenchmarkGood" || results[1].Name != "BenchmarkValid" {
		t.Errorf("names: %q, %q", results[0].Name, results[1].Name)
	}
}
