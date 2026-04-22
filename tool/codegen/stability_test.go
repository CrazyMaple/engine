package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const stabilitySample = `package sample

// UserService 稳定接口
// Stable: 自 v1.0 起
// Since: v1.0
type UserService struct{}

// CreateUser 创建用户
// Stable: 通用 API
func (s *UserService) CreateUser(name string) error { return nil }

// LegacyMethod 老方法
// Deprecated: 请改用 CreateUser，将在 v2.0 移除
func (s *UserService) LegacyMethod() {}

// NewFeature 实验能力
// Experimental: 接口可能调整
// Since: v1.9
func NewFeature() {}

// BetaConfig Beta 配置
// Beta: 欢迎反馈
type BetaConfig struct{}

// PublicButUntagged 没有稳定性标注
type PublicButUntagged struct{}

// lowerCasePrivate 未导出，应跳过
type lowerCasePrivate struct{}

// MaxConns 最大连接数
// Stable: 自 v1.0 起
const MaxConns = 1000

// internal 未导出常量
const internal = 0
`

func writeStabilitySample(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "sample")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "sample.go"), []byte(stabilitySample), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestScanStability(t *testing.T) {
	root := writeStabilitySample(t)
	report, err := ScanStability(root, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 期望的符号名 → 级别
	wantLevels := map[string]StabilityLevel{
		"UserService":          StabilityStable,
		"*UserService.CreateUser": StabilityStable,
		"*UserService.LegacyMethod": StabilityDeprecated,
		"NewFeature":           StabilityExperimental,
		"BetaConfig":           StabilityBeta,
		"PublicButUntagged":    StabilityUnmarked,
		"MaxConns":             StabilityStable,
	}
	got := map[string]APISymbol{}
	for _, s := range report.Symbols {
		got[s.Name] = s
	}
	for name, want := range wantLevels {
		sym, ok := got[name]
		if !ok {
			t.Errorf("symbol %q not found; have: %v", name, keysOf(got))
			continue
		}
		if sym.Stability != want {
			t.Errorf("%s level = %q, want %q", name, sym.Stability, want)
		}
	}

	// 未导出的应跳过
	if _, ok := got["lowerCasePrivate"]; ok {
		t.Error("private type should be skipped")
	}
	if _, ok := got["internal"]; ok {
		t.Error("private const should be skipped")
	}

	// Since 提取
	if got["UserService"].Since != "v1.0" {
		t.Errorf("UserService.Since = %q", got["UserService"].Since)
	}
	if got["NewFeature"].Since != "v1.9" {
		t.Errorf("NewFeature.Since = %q", got["NewFeature"].Since)
	}

	// Deprecated 原因
	if !strings.Contains(got["*UserService.LegacyMethod"].Deprecated, "请改用 CreateUser") {
		t.Errorf("deprecated reason: %q", got["*UserService.LegacyMethod"].Deprecated)
	}

	// 分级统计
	if report.ByLevel[StabilityStable] < 3 {
		t.Errorf("stable count: %d", report.ByLevel[StabilityStable])
	}
	if report.ByLevel[StabilityDeprecated] != 1 {
		t.Errorf("deprecated count: %d", report.ByLevel[StabilityDeprecated])
	}
	if report.ByLevel[StabilityUnmarked] < 1 {
		t.Errorf("unmarked count: %d", report.ByLevel[StabilityUnmarked])
	}
}

func TestScanStabilitySkipsTestFiles(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "sample")
	_ = os.MkdirAll(pkgDir, 0755)
	// 正文件
	os.WriteFile(filepath.Join(pkgDir, "a.go"), []byte(`package sample
// Stable:
type A struct{}
`), 0644)
	// test 文件应跳过
	os.WriteFile(filepath.Join(pkgDir, "a_test.go"), []byte(`package sample
// Stable:
type B struct{}
`), 0644)

	report, err := ScanStability(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	foundA, foundB := false, false
	for _, s := range report.Symbols {
		if s.Name == "A" {
			foundA = true
		}
		if s.Name == "B" {
			foundB = true
		}
	}
	if !foundA {
		t.Error("A should be scanned")
	}
	if foundB {
		t.Error("B in _test.go should be skipped")
	}
}

func TestScanStabilitySkipsDirs(t *testing.T) {
	root := t.TempDir()
	// better 目录应被跳过
	betterDir := filepath.Join(root, "better")
	_ = os.MkdirAll(betterDir, 0755)
	os.WriteFile(filepath.Join(betterDir, "ref.go"), []byte(`package better
// Stable:
type Ref struct{}
`), 0644)

	normalDir := filepath.Join(root, "normal")
	_ = os.MkdirAll(normalDir, 0755)
	os.WriteFile(filepath.Join(normalDir, "n.go"), []byte(`package normal
// Stable:
type N struct{}
`), 0644)

	report, err := ScanStability(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range report.Symbols {
		if s.Package == "better" || strings.Contains(s.File, "/better/") {
			t.Errorf("better/ should be skipped, got %s", s.File)
		}
	}
	foundN := false
	for _, s := range report.Symbols {
		if s.Name == "N" {
			foundN = true
		}
	}
	if !foundN {
		t.Error("normal dir should be scanned")
	}
}

func TestStabilityReportMarkdown(t *testing.T) {
	root := writeStabilitySample(t)
	report, err := ScanStability(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	md := report.Markdown()
	for _, s := range []string{
		"# API Stability Index",
		"| Symbol |",
		"UserService",
		"Stable",
		"Deprecated",
	} {
		if !strings.Contains(md, s) {
			t.Errorf("markdown missing %q", s)
		}
	}
}

func TestStabilityReportSummary(t *testing.T) {
	root := writeStabilitySample(t)
	report, _ := ScanStability(root, nil)
	text := report.Summary()
	for _, s := range []string{
		"API Stability Report",
		"Stable",
		"Deprecated",
		"Unmarked",
	} {
		if !strings.Contains(text, s) {
			t.Errorf("summary missing %q", s)
		}
	}
}

func TestChangelogFormatting(t *testing.T) {
	// 模拟 git log 输出
	log := `abc1234|2026-04-15|feat(actor): add mailbox priority
def5678|2026-04-14|fix(remote): resolve leak
ghi9012|2026-04-13|perf!: breaking rewrite of dispatcher
jkl3456|2026-04-12|chore: update deps
mno7890|2026-04-11|random commit without prefix`

	out := formatChangelog(log, "v1.10.0")
	for _, s := range []string{
		"# v1.10.0",
		"Features",
		"Bug Fixes",
		"⚠ Breaking Changes",
		"Chore",
		"Other",
		"actor",
		"remote",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("changelog missing %q in:\n%s", s, out)
		}
	}
}

func TestParseGitLogHandlesMalformedLines(t *testing.T) {
	log := `abc|2026-04-15|feat: ok
malformed line without separators
def|2026-04-14|fix: another`
	entries := parseGitLog(log)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestClassifyCommitBreaking(t *testing.T) {
	e := ChangelogEntry{Subject: "feat(actor)!: breaking API"}
	classifyCommit(&e)
	if e.Kind != "BREAKING" {
		t.Errorf("kind: %q", e.Kind)
	}
	if e.Scope != "actor" {
		t.Errorf("scope: %q", e.Scope)
	}

	e2 := ChangelogEntry{Subject: "BREAKING CHANGE: rewrite"}
	classifyCommit(&e2)
	if e2.Kind != "BREAKING" {
		t.Errorf("kind: %q", e2.Kind)
	}
}

func keysOf(m map[string]APISymbol) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
