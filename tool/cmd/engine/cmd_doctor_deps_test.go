package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// writeGo 写一个只含 import 段的最小 .go 文件
func writeGo(t *testing.T, path, pkg string, imports ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\n")
	if len(imports) > 0 {
		b.WriteString("import (\n")
		for _, imp := range imports {
			b.WriteString("\t\"" + imp + "\"\n")
		}
		b.WriteString(")\n\n")
		b.WriteString("var _ = \"" + strings.Join(imports, ",") + "\"\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// buildFakeRepo 在 tmp 下搭一个最小三层骨架
func buildFakeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// 合法文件
	writeGo(t, filepath.Join(root, "engine", "actor", "a.go"), "actor", "fmt")
	writeGo(t, filepath.Join(root, "gamelib", "scene", "s.go"), "scene", "engine/actor")
	writeGo(t, filepath.Join(root, "tool", "bench", "b.go"), "bench", "engine/actor", "gamelib/scene")
	// better/ 参考目录（不能被 import）
	writeGo(t, filepath.Join(root, "better", "leaf", "l.go"), "leaf", "fmt")
	return root
}

func TestDepsClean(t *testing.T) {
	root := buildFakeRepo(t)
	rep, err := runDepsCheck(root)
	if err != nil {
		t.Fatalf("runDepsCheck: %v", err)
	}
	if len(rep.Violations) != 0 {
		t.Fatalf("expected 0 violations, got %d: %+v", len(rep.Violations), rep.Violations)
	}
	for _, m := range []string{"engine", "gamelib", "tool"} {
		if rep.Scanned[m] == 0 {
			t.Errorf("module %s: scanned count is zero", m)
		}
	}
}

func TestDepsEngineToGamelib(t *testing.T) {
	root := buildFakeRepo(t)
	// 制造违规：engine 反向 import gamelib
	writeGo(t, filepath.Join(root, "engine", "actor", "bad.go"), "actor", "gamelib/scene")
	rep, err := runDepsCheck(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Violations) == 0 {
		t.Fatal("expected at least one violation")
	}
	var found bool
	for _, v := range rep.Violations {
		if v.Module == "engine" && v.Import == "gamelib/scene" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected engine→gamelib violation in %+v", rep.Violations)
	}
}

func TestDepsGamelibToTool(t *testing.T) {
	root := buildFakeRepo(t)
	writeGo(t, filepath.Join(root, "gamelib", "scene", "bad.go"), "scene", "tool/bench")
	rep, err := runDepsCheck(root)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, v := range rep.Violations {
		if v.Module == "gamelib" && v.Import == "tool/bench" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected gamelib→tool violation in %+v", rep.Violations)
	}
}

func TestDepsBetterForbidden(t *testing.T) {
	root := buildFakeRepo(t)
	writeGo(t, filepath.Join(root, "tool", "bench", "bad.go"), "bench", "better/leaf")
	rep, err := runDepsCheck(root)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, v := range rep.Violations {
		if v.Module == "tool" && strings.HasPrefix(v.Import, "better/") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected better/ violation in %+v", rep.Violations)
	}
}

func TestDepsSortedStable(t *testing.T) {
	root := buildFakeRepo(t)
	writeGo(t, filepath.Join(root, "engine", "z.go"), "engine", "tool/bench")
	writeGo(t, filepath.Join(root, "engine", "a.go"), "engine", "gamelib/scene")
	rep, err := runDepsCheck(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Violations) < 2 {
		t.Fatalf("expected >=2 violations, got %d", len(rep.Violations))
	}
	// 同 module 下按 File 排序
	files := make([]string, 0, len(rep.Violations))
	for _, v := range rep.Violations {
		if v.Module == "engine" {
			files = append(files, v.File)
		}
	}
	if !sort.StringsAreSorted(files) {
		t.Errorf("violations should be sorted by file within a module, got %v", files)
	}
}

func TestViolatesRules(t *testing.T) {
	cases := []struct {
		mod, imp string
		want     bool
	}{
		{"engine", "gamelib/log", true},
		{"engine", "tool/bench", true},
		{"engine", "engine/actor", false},
		{"engine", "fmt", false},
		{"gamelib", "tool/bench", true},
		{"gamelib", "engine/actor", false},
		{"tool", "engine/actor", false},
		{"tool", "gamelib/scene", false},
		{"tool", "better/leaf", true},
		{"engine", "better/leaf", true},
	}
	for _, c := range cases {
		_, bad := violates(c.mod, c.imp)
		if bad != c.want {
			t.Errorf("violates(%q, %q) = %v, want %v", c.mod, c.imp, bad, c.want)
		}
	}
}
