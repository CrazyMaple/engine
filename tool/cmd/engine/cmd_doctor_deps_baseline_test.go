package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile 直接落一个文本文件
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeJSON 序列化任意结构到文件
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(data))
}

// fakeRepoForBaseline 构造一个最小三层仓库，用于 baseline 行为测试
func fakeRepoForBaseline(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// 基本目录结构 engine / gamelib / tool
	writeGo(t, filepath.Join(root, "engine", "actor", "actor.go"), "actor")
	writeGo(t, filepath.Join(root, "gamelib", "scene", "scene.go"), "scene")
	writeGo(t, filepath.Join(root, "tool", "bench", "bench.go"), "bench")
	// 参考目录（应被 exclude_paths 排除）
	writeGo(t, filepath.Join(root, "better", "leaf", "leaf.go"), "leaf")
	return root
}

// TestPathExcluded 验证 exclude_paths 的目录前缀递归 / 文件精确语义
func TestPathExcluded(t *testing.T) {
	excludes := []string{"better/", "engine_sample/", "doc/special.md"}
	cases := []struct {
		rel  string
		want bool
	}{
		{"better/leaf/x.go", true},                                        // 目录前缀递归
		{"better/protoactor-go-dev/examples/x/go.mod", true},              // 任意深度
		{"better", false},                                                 // 没 "/" 收尾不算前缀
		{"betterX/foo.go", false},                                         // 前缀不能误命中
		{"engine_sample/basics/go.mod", true},                             // 第二个 exclude
		{"doc/special.md", true},                                          // 文件精确
		{"doc/other.md", false},                                           // 同目录其他文件不命中
	}
	for _, c := range cases {
		if got := pathExcluded(c.rel, excludes); got != c.want {
			t.Errorf("pathExcluded(%q, %+v) = %v, want %v", c.rel, excludes, got, c.want)
		}
	}
}

// TestImportMatches 验证 import_baseline key 前缀匹配语义
func TestImportMatches(t *testing.T) {
	cases := []struct {
		imp, key string
		want     bool
	}{
		{"engine/router", "engine/router", true},
		{"engine/router/sub", "engine/router", true},
		{"engine/router2", "engine/router", false}, // 不能误匹配
		{"engine/r", "engine/router", false},
		{"engine/routerx", "engine/router", false},
	}
	for _, c := range cases {
		if got := importMatches(c.imp, c.key); got != c.want {
			t.Errorf("importMatches(%q, %q) = %v, want %v", c.imp, c.key, got, c.want)
		}
	}
}

// TestFileBaselineEntryExists 验证文件/目录两种条目的存在性判断
func TestFileBaselineEntryExists(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "engine", "codec", "stream_codec.go"), "package codec\n")
	if err := os.MkdirAll(filepath.Join(root, "engine", "cluster", "canary"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		entry string
		want  bool
	}{
		{"engine/codec/stream_codec.go", true},     // 精确文件存在
		{"engine/codec/missing.go", false},         // 文件不存在
		{"engine/cluster/canary/", true},           // 目录存在
		{"engine/cluster/missing/", false},         // 目录不存在
	}
	for _, c := range cases {
		if got := fileBaselineEntryExists(root, c.entry); got != c.want {
			t.Errorf("fileBaselineEntryExists(%q) = %v, want %v", c.entry, got, c.want)
		}
	}
}

// TestSnapshotImportHits 在 baseline 已声明 engine/router 后，扫描出 tool 中的引用
func TestSnapshotImportHits(t *testing.T) {
	root := fakeRepoForBaseline(t)
	// tool/example 文件 import engine/router
	writeGo(t, filepath.Join(root, "tool", "example", "ex.go"), "example", "engine/router")
	writeGo(t, filepath.Join(root, "tool", "example", "ex2.go"), "example", "engine/pubsub")

	b := &Baseline{
		Mode: baselineModeFreeze,
		ImportBaseline: map[string][]string{
			"engine/router": {"tool/example/ex.go"},
			"engine/pubsub": {"tool/example/ex2.go"},
		},
	}
	snap, err := snapshotForBaseline(root, b)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.ImportHits["engine/router"]; len(got) != 1 || got[0] != "tool/example/ex.go" {
		t.Errorf("engine/router hits = %v, want [tool/example/ex.go]", got)
	}
	if got := snap.ImportHits["engine/pubsub"]; len(got) != 1 || got[0] != "tool/example/ex2.go" {
		t.Errorf("engine/pubsub hits = %v, want [tool/example/ex2.go]", got)
	}
}

// TestFreezeAllowsExisting freeze 模式下既有命中允许保留
func TestFreezeAllowsExisting(t *testing.T) {
	root := fakeRepoForBaseline(t)
	writeGo(t, filepath.Join(root, "tool", "example", "ex.go"), "example", "engine/router")

	b := &Baseline{
		Mode: baselineModeFreeze,
		ImportBaseline: map[string][]string{
			"engine/router": {"tool/example/ex.go"},
		},
	}
	snap, _ := snapshotForBaseline(root, b)
	vs := computeViolations(b, snap)
	if len(vs) != 0 {
		t.Errorf("expected 0 violations in freeze with existing hit, got %+v", vs)
	}
}

// TestFreezeBlocksNew freeze 模式下新增引用必须被拦截
func TestFreezeBlocksNew(t *testing.T) {
	root := fakeRepoForBaseline(t)
	writeGo(t, filepath.Join(root, "tool", "example", "ex.go"), "example", "engine/router")
	writeGo(t, filepath.Join(root, "tool", "example", "new.go"), "example", "engine/router") // 新增

	b := &Baseline{
		Mode: baselineModeFreeze,
		ImportBaseline: map[string][]string{
			"engine/router": {"tool/example/ex.go"},
		},
	}
	snap, _ := snapshotForBaseline(root, b)
	vs := computeViolations(b, snap)
	var found bool
	for _, v := range vs {
		if v.Kind == "import" && strings.Contains(v.Subject, "tool/example/new.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected import violation for new.go, got %+v", vs)
	}
}

// TestZeroRequiresEmpty zero 模式下任何命中都是违规
func TestZeroRequiresEmpty(t *testing.T) {
	root := fakeRepoForBaseline(t)
	writeGo(t, filepath.Join(root, "tool", "example", "ex.go"), "example", "engine/router")

	b := &Baseline{
		Mode: baselineModeZero,
		ImportBaseline: map[string][]string{
			"engine/router": {}, // zero 模式应为空
		},
	}
	snap, _ := snapshotForBaseline(root, b)
	vs := computeViolations(b, snap)
	if len(vs) == 0 {
		t.Errorf("expected violations under zero mode, got 0")
	}
}

// TestZeroFlagsBaselineLeftover zero 模式下 baseline.import_baseline.value 残留也是违规
func TestZeroFlagsBaselineLeftover(t *testing.T) {
	root := fakeRepoForBaseline(t)
	// 仓库中已无该 import
	b := &Baseline{
		Mode: baselineModeZero,
		ImportBaseline: map[string][]string{
			"engine/router": {"tool/example/ex.go"}, // 但 baseline 还残留登记
		},
	}
	snap, _ := snapshotForBaseline(root, b)
	vs := computeViolations(b, snap)
	if len(vs) == 0 {
		t.Errorf("expected violation about baseline leftover, got 0")
	}
}

// TestFreezeFileBaselineExisting freeze 下 should_be_removed 中既有条目仍允许
func TestFreezeFileBaselineExisting(t *testing.T) {
	root := fakeRepoForBaseline(t)
	writeFile(t, filepath.Join(root, "engine", "codec", "stream_codec.go"), "package codec\n")

	b := &Baseline{
		Mode: baselineModeFreeze,
		FileBaseline: FileBaselineRules{
			ShouldBeRemoved: []string{"engine/codec/stream_codec.go"},
		},
	}
	snap, _ := snapshotForBaseline(root, b)
	vs := computeViolations(b, snap)
	if len(vs) != 0 {
		t.Errorf("freeze should allow existing should_be_removed entry, got %+v", vs)
	}
}

// TestZeroFileBaselineMustVanish zero 下 should_be_removed 中存在的条目必须违规
func TestZeroFileBaselineMustVanish(t *testing.T) {
	root := fakeRepoForBaseline(t)
	writeFile(t, filepath.Join(root, "engine", "codec", "stream_codec.go"), "package codec\n")

	b := &Baseline{
		Mode: baselineModeZero,
		FileBaseline: FileBaselineRules{
			ShouldBeRemoved: []string{"engine/codec/stream_codec.go"},
		},
	}
	snap, _ := snapshotForBaseline(root, b)
	vs := computeViolations(b, snap)
	var found bool
	for _, v := range vs {
		if v.Kind == "file" && v.Subject == "engine/codec/stream_codec.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("zero must flag existing should_be_removed file, got %+v", vs)
	}
}

// TestSymbolBaselineDetectsType / Func / Const
func TestSymbolBaselineDetectsTypeFuncConst(t *testing.T) {
	root := t.TempDir()
	src := `package log

type LogSink struct{}
type LogEntry struct{}

func NewRingBufferSink() *LogSink { return nil }
const hmacSize = 32
const innocent = 1
`
	writeFile(t, filepath.Join(root, "engine", "log", "x.go"), src)

	b := &Baseline{
		Mode: baselineModeFreeze,
		SymbolBaseline: map[string]SymbolRules{
			"engine/log": {
				ForbiddenSymbols:   []string{"LogSink", "NewRingBufferSink"},
				ForbiddenConstants: []string{"hmacSize"},
			},
		},
	}
	snap, err := snapshotForBaseline(root, b)
	if err != nil {
		t.Fatal(err)
	}
	hits := snap.SymbolHits["engine/log"]
	if len(hits["symbol"]) != 2 {
		t.Errorf("expected 2 symbol hits, got %+v", hits["symbol"])
	}
	if len(hits["constant"]) != 1 {
		t.Errorf("expected 1 constant hit, got %+v", hits["constant"])
	}
	// LogEntry / innocent 不在规则里，不应被命中
	for _, h := range hits["symbol"] {
		if h.Name == "LogEntry" {
			t.Errorf("LogEntry should not be hit (not in rule)")
		}
	}
}

// TestSymbolForbiddenKeywords keyword 命中（用于 grain gossip/splitbrain 守护）
func TestSymbolForbiddenKeywords(t *testing.T) {
	root := t.TempDir()
	src := `package grain

import "fmt"

func use() { fmt.Println("hello SplitBrain world") }
`
	writeFile(t, filepath.Join(root, "engine", "grain", "x.go"), src)

	b := &Baseline{
		Mode: baselineModeFreeze,
		SymbolBaseline: map[string]SymbolRules{
			"engine/grain": {
				ForbiddenKeywords: []string{"gossip", "Gossip", "splitbrain", "SplitBrain"},
			},
		},
	}
	snap, _ := snapshotForBaseline(root, b)
	if got := len(snap.SymbolHits["engine/grain"]["keyword"]); got == 0 {
		t.Errorf("expected at least 1 keyword hit, got 0")
	}
}

// TestExcludePathsHonored exclude_paths 中的目录递归排除
func TestExcludePathsHonored(t *testing.T) {
	root := t.TempDir()
	// better/ 下的文件即使 import 引擎类，也不应被纳入 hits
	writeGo(t, filepath.Join(root, "better", "leaf", "x.go"), "leaf", "engine/router")
	writeGo(t, filepath.Join(root, "tool", "real", "y.go"), "real", "engine/router")

	b := &Baseline{
		Mode:         baselineModeFreeze,
		ExcludePaths: []string{"better/"},
		ImportBaseline: map[string][]string{
			"engine/router": {"tool/real/y.go"},
		},
	}
	snap, _ := snapshotForBaseline(root, b)
	if got := snap.ImportHits["engine/router"]; len(got) != 1 || got[0] != "tool/real/y.go" {
		t.Errorf("better/ should be excluded; got %v", got)
	}
}

// TestWriteBaselineSnapshotRefresh 验证写出 baseline 时 import value 被刷新
func TestWriteBaselineSnapshotRefresh(t *testing.T) {
	root := fakeRepoForBaseline(t)
	writeGo(t, filepath.Join(root, "tool", "real", "y.go"), "real", "engine/router")

	skel := &Baseline{
		Version:      "v1.13",
		Mode:         baselineModeFreeze,
		ExcludePaths: []string{"better/"},
		ImportBaseline: map[string][]string{
			"engine/router": {"tool/old/stale.go"}, // stale，应被覆盖
		},
		FileBaseline: FileBaselineRules{
			ShouldBeRemoved: []string{"engine/codec/stream_codec.go"},
		},
		SymbolBaseline: map[string]SymbolRules{
			"engine/log": {ForbiddenSymbols: []string{"LogSink"}},
		},
	}
	bp := filepath.Join(root, "baseline.json")
	writeJSON(t, bp, skel)
	if err := runBaselineSnapshot(root, bp); err != nil {
		t.Fatal(err)
	}
	got, err := loadBaseline(bp)
	if err != nil {
		t.Fatal(err)
	}
	if files := got.ImportBaseline["engine/router"]; len(files) != 1 || files[0] != "tool/real/y.go" {
		t.Errorf("import_baseline 未被刷新，got %v", files)
	}
	// 规则段保持不变
	if got.FileBaseline.ShouldBeRemoved[0] != "engine/codec/stream_codec.go" {
		t.Errorf("file_baseline.should_be_removed 不应被改写")
	}
	if got.SymbolBaseline["engine/log"].ForbiddenSymbols[0] != "LogSink" {
		t.Errorf("symbol_baseline 不应被改写")
	}
}

// TestSymbolFreezeAllowsExistingHit current_hits 已登记的命中在 freeze 下允许保留
func TestSymbolFreezeAllowsExistingHit(t *testing.T) {
	root := t.TempDir()
	src := `package log

type LogSink struct{}

func NewRingBufferSink() *LogSink { return nil }
const hmacSize = 32
`
	writeFile(t, filepath.Join(root, "engine", "log", "x.go"), src)

	b := &Baseline{
		Mode: baselineModeFreeze,
		SymbolBaseline: map[string]SymbolRules{
			"engine/log": {
				ForbiddenSymbols:   []string{"LogSink", "NewRingBufferSink"},
				ForbiddenConstants: []string{"hmacSize"},
				CurrentHits: &SymbolHitsSnapshot{
					Symbol: []HitEntry{
						{Name: "LogSink", File: "engine/log/x.go", Line: 3},
						{Name: "NewRingBufferSink", File: "engine/log/x.go", Line: 5},
					},
					Constant: []HitEntry{
						{Name: "hmacSize", File: "engine/log/x.go", Line: 6},
					},
				},
			},
		},
	}
	snap, err := snapshotForBaseline(root, b)
	if err != nil {
		t.Fatal(err)
	}
	vs := computeViolations(b, snap)
	if len(vs) != 0 {
		t.Errorf("expected 0 violations when all hits registered, got %+v", vs)
	}
}

// TestSymbolFreezeBlocksNewLocation 同名 forbidden symbol 出现在新文件 → 必须违规
func TestSymbolFreezeBlocksNewLocation(t *testing.T) {
	root := t.TempDir()
	old := `package log

type LogSink struct{}
`
	writeFile(t, filepath.Join(root, "engine", "log", "x.go"), old)
	// 同名符号被搬到了另一个文件——freeze 必须拦截
	another := `package log

type LogSink2 = LogSink
type LogSink struct{ moved bool }
`
	writeFile(t, filepath.Join(root, "engine", "log", "y.go"), another)

	b := &Baseline{
		Mode: baselineModeFreeze,
		SymbolBaseline: map[string]SymbolRules{
			"engine/log": {
				ForbiddenSymbols: []string{"LogSink"},
				CurrentHits: &SymbolHitsSnapshot{
					Symbol: []HitEntry{
						{Name: "LogSink", File: "engine/log/x.go", Line: 3},
					},
				},
			},
		},
	}
	snap, _ := snapshotForBaseline(root, b)
	vs := computeViolations(b, snap)
	var found bool
	for _, v := range vs {
		if v.Kind == "symbol" && strings.Contains(v.Subject, "engine/log/y.go") && strings.Contains(v.Subject, "LogSink") {
			found = true
		}
	}
	if !found {
		t.Errorf("freeze must flag LogSink in new file y.go, got %+v", vs)
	}
}

// TestSymbolFreezeBlocksWhenSnapshotMissing current_hits 缺失（旧 baseline）→ 任何命中都视为新增
func TestSymbolFreezeBlocksWhenSnapshotMissing(t *testing.T) {
	root := t.TempDir()
	src := `package log

type LogSink struct{}
`
	writeFile(t, filepath.Join(root, "engine", "log", "x.go"), src)

	b := &Baseline{
		Mode: baselineModeFreeze,
		SymbolBaseline: map[string]SymbolRules{
			"engine/log": {
				ForbiddenSymbols: []string{"LogSink"},
				// CurrentHits 故意留空（旧 baseline）
			},
		},
	}
	snap, _ := snapshotForBaseline(root, b)
	vs := computeViolations(b, snap)
	if len(vs) == 0 {
		t.Errorf("freeze without current_hits must reject any hit, got 0")
	}
}

// TestSymbolFreezeBlocksNewKeyword 同名 keyword 出现在新文件 → 必须违规
func TestSymbolFreezeBlocksNewKeyword(t *testing.T) {
	root := t.TempDir()
	src := `package grain

import "fmt"

func use1() { fmt.Println("hello SplitBrain world") }
`
	writeFile(t, filepath.Join(root, "engine", "grain", "x.go"), src)
	src2 := `package grain

import "fmt"

func use2() { fmt.Println("another SplitBrain hit") }
`
	writeFile(t, filepath.Join(root, "engine", "grain", "y.go"), src2)

	b := &Baseline{
		Mode: baselineModeFreeze,
		SymbolBaseline: map[string]SymbolRules{
			"engine/grain": {
				ForbiddenKeywords: []string{"SplitBrain"},
				CurrentHits: &SymbolHitsSnapshot{
					Keyword: []HitEntry{
						{Name: "SplitBrain", File: "engine/grain/x.go", Line: 5},
					},
				},
			},
		},
	}
	snap, _ := snapshotForBaseline(root, b)
	vs := computeViolations(b, snap)
	var found bool
	for _, v := range vs {
		if v.Kind == "symbol" && strings.Contains(v.Subject, "engine/grain/y.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("freeze must flag SplitBrain keyword in new file y.go, got %+v", vs)
	}
}

// TestSymbolFreezeIgnoresLineDrift 行号变化但文件 + 名字不变 → 不违规
func TestSymbolFreezeIgnoresLineDrift(t *testing.T) {
	root := t.TempDir()
	// 命中在第 5 行而不是 baseline 登记的第 3 行
	src := `package log

// padding
// padding

type LogSink struct{}
`
	writeFile(t, filepath.Join(root, "engine", "log", "x.go"), src)

	b := &Baseline{
		Mode: baselineModeFreeze,
		SymbolBaseline: map[string]SymbolRules{
			"engine/log": {
				ForbiddenSymbols: []string{"LogSink"},
				CurrentHits: &SymbolHitsSnapshot{
					Symbol: []HitEntry{
						{Name: "LogSink", File: "engine/log/x.go", Line: 3},
					},
				},
			},
		},
	}
	snap, _ := snapshotForBaseline(root, b)
	vs := computeViolations(b, snap)
	if len(vs) != 0 {
		t.Errorf("freeze should ignore line drift, got %+v", vs)
	}
}

// TestWriteBaselineSnapshotRefreshSymbolHits 验证 snapshot 写入时 current_hits 被刷新，规则段保留
func TestWriteBaselineSnapshotRefreshSymbolHits(t *testing.T) {
	root := t.TempDir()
	src := `package log

type LogSink struct{}

func NewRingBufferSink() *LogSink { return nil }
const hmacSize = 32
`
	writeFile(t, filepath.Join(root, "engine", "log", "x.go"), src)

	skel := &Baseline{
		Version: "v1.13",
		Mode:    baselineModeFreeze,
		SymbolBaseline: map[string]SymbolRules{
			"engine/log": {
				ForbiddenSymbols:   []string{"LogSink", "NewRingBufferSink"},
				ForbiddenConstants: []string{"hmacSize"},
				// 故意写错的 stale 快照
				CurrentHits: &SymbolHitsSnapshot{
					Symbol: []HitEntry{{Name: "Stale", File: "stale.go", Line: 1}},
				},
			},
		},
	}
	bp := filepath.Join(root, "baseline.json")
	writeJSON(t, bp, skel)
	if err := runBaselineSnapshot(root, bp); err != nil {
		t.Fatal(err)
	}
	got, err := loadBaseline(bp)
	if err != nil {
		t.Fatal(err)
	}
	rules := got.SymbolBaseline["engine/log"]
	// 规则段保留
	if len(rules.ForbiddenSymbols) != 2 || rules.ForbiddenConstants[0] != "hmacSize" {
		t.Errorf("rule fields should be preserved, got %+v", rules)
	}
	// current_hits 被刷新为真实命中
	if rules.CurrentHits == nil {
		t.Fatalf("current_hits should be refreshed")
	}
	if len(rules.CurrentHits.Symbol) != 2 {
		t.Errorf("expect 2 symbol hits after refresh, got %+v", rules.CurrentHits.Symbol)
	}
	if len(rules.CurrentHits.Constant) != 1 || rules.CurrentHits.Constant[0].Name != "hmacSize" {
		t.Errorf("expect 1 constant hit hmacSize, got %+v", rules.CurrentHits.Constant)
	}
	for _, e := range rules.CurrentHits.Symbol {
		if e.File != "engine/log/x.go" {
			t.Errorf("hit file should be engine/log/x.go, got %s", e.File)
		}
		if e.Name == "Stale" {
			t.Errorf("stale snapshot should be replaced")
		}
	}
}

// TestSymbolFreezeRoundTrip --baseline 写出 → --check-baseline 立即验证应通过
func TestSymbolFreezeRoundTrip(t *testing.T) {
	root := t.TempDir()
	src := `package log

type LogSink struct{}
`
	writeFile(t, filepath.Join(root, "engine", "log", "x.go"), src)

	skel := &Baseline{
		Version: "v1.13",
		Mode:    baselineModeFreeze,
		SymbolBaseline: map[string]SymbolRules{
			"engine/log": {
				ForbiddenSymbols: []string{"LogSink"},
			},
		},
	}
	bp := filepath.Join(root, "baseline.json")
	writeJSON(t, bp, skel)
	if err := runBaselineSnapshot(root, bp); err != nil {
		t.Fatal(err)
	}
	violations, err := runBaselineCheck(root, bp)
	if err != nil {
		t.Fatal(err)
	}
	if violations != 0 {
		t.Errorf("round-trip should produce 0 violations, got %d", violations)
	}
}

// TestRunBaselineCheckPasses freeze 模式下完整流程不报错
func TestRunBaselineCheckPasses(t *testing.T) {
	root := fakeRepoForBaseline(t)
	writeGo(t, filepath.Join(root, "tool", "example", "ex.go"), "example", "engine/router")

	b := &Baseline{
		Version: "v1.13",
		Mode:    baselineModeFreeze,
		ImportBaseline: map[string][]string{
			"engine/router": {"tool/example/ex.go"},
		},
	}
	bp := filepath.Join(root, "baseline.json")
	writeJSON(t, bp, b)

	violations, err := runBaselineCheck(root, bp)
	if err != nil {
		t.Fatal(err)
	}
	if violations != 0 {
		t.Errorf("expected 0 violations, got %d", violations)
	}
}

// TestLoadBaselineRejectsInvalidMode
func TestLoadBaselineRejectsInvalidMode(t *testing.T) {
	root := t.TempDir()
	bp := filepath.Join(root, "bad.json")
	writeFile(t, bp, `{"version":"v1.13","mode":"unknown"}`)
	if _, err := loadBaseline(bp); err == nil {
		t.Fatalf("expected error for invalid mode")
	}
}
