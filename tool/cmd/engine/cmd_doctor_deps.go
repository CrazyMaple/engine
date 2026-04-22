package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// cmd_doctor_deps.go — engine doctor deps 子命令
//
// 运行：`engine doctor deps [--root <repo-root>] [--format text|json]`
//
// 依据 v1.12 §2.4 的依赖方向铁律：
//
//   tool   ──▶ gamelib ──▶ engine
//    │                        ▲
//    └────────────────────────┘  (tool 可直接依赖 engine)
//
//   engine   不得 import gamelib / tool
//   gamelib  不得 import tool
//   better/  不被任何 module import
//
// 退出码：0=无违规；1=检测到违规（CI 应拦截）。

// depsViolation 单条违规记录
type depsViolation struct {
	Module  string `json:"module"`   // 当前 module 名（engine/gamelib/tool）
	File    string `json:"file"`     // 相对 root 的文件路径
	Import  string `json:"import"`   // 违规 import 路径
	Rule    string `json:"rule"`     // 触发哪条规则
}

// depsReport 整体体检结果
type depsReport struct {
	Root       string          `json:"root"`
	Violations []depsViolation `json:"violations"`
	Scanned    map[string]int  `json:"scanned"` // 每个 module 扫描的 .go 文件数
}

// 已知 module → 禁止 import 的前缀集合
var forbidPrefix = map[string][]string{
	"engine":  {"gamelib/", "tool/"},
	"gamelib": {"tool/"},
	"tool":    nil, // tool 允许依赖 engine 和 gamelib
}

// 不得被任何 module 内部 .go 引用的前缀（better/ 参考实现只读）
var forbidAny = []string{"better/"}

func cmdDoctorDeps(args []string) error {
	fs := flag.NewFlagSet("doctor-deps", flag.ExitOnError)
	root := fs.String("root", ".", "仓库根目录（含 engine/ gamelib/ tool/ 的容器根）")
	format := fs.String("format", "text", "输出格式：text|json")
	fs.Parse(args)

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		return fmt.Errorf("解析 root: %w", err)
	}

	rep, err := runDepsCheck(absRoot)
	if err != nil {
		return err
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	default:
		printDepsText(rep)
	}

	if len(rep.Violations) > 0 {
		os.Exit(1)
	}
	return nil
}

// runDepsCheck 扫描三个 module 根，返回所有违规记录
func runDepsCheck(root string) (*depsReport, error) {
	rep := &depsReport{
		Root:    root,
		Scanned: map[string]int{},
	}
	for _, mod := range []string{"engine", "gamelib", "tool"} {
		modDir := filepath.Join(root, mod)
		if _, err := os.Stat(modDir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", modDir, err)
		}
		count, vs, err := scanModule(root, mod, modDir)
		if err != nil {
			return nil, err
		}
		rep.Scanned[mod] = count
		rep.Violations = append(rep.Violations, vs...)
	}
	sort.Slice(rep.Violations, func(i, j int) bool {
		a, b := rep.Violations[i], rep.Violations[j]
		if a.Module != b.Module {
			return a.Module < b.Module
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Import < b.Import
	})
	return rep, nil
}

// scanModule 遍历单个 module 下全部 .go 文件，返回扫描数量与违规列表
func scanModule(root, mod, modDir string) (int, []depsViolation, error) {
	var (
		count int
		vs    []depsViolation
	)
	err := filepath.WalkDir(modDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// 跳过隐藏目录与 vendor
			name := d.Name()
			if name == "vendor" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		count++
		imports, err := parseImports(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, imp := range imports {
			if rule, bad := violates(mod, imp); bad {
				vs = append(vs, depsViolation{
					Module: mod,
					File:   filepath.ToSlash(rel),
					Import: imp,
					Rule:   rule,
				})
			}
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	return count, vs, nil
}

// violates 判断 import 路径是否违反规则；返回 (rule, true) 或 ("", false)
func violates(mod, imp string) (string, bool) {
	for _, p := range forbidAny {
		if strings.HasPrefix(imp, p) {
			return "better/ 为只读参考，禁止任何 module import", true
		}
	}
	for _, p := range forbidPrefix[mod] {
		if strings.HasPrefix(imp, p) {
			return fmt.Sprintf("%s 不得 import %s*", mod, strings.TrimSuffix(p, "/")), true
		}
	}
	return "", false
}

// parseImports 用 go/parser 抽取文件 import 列表（仅 import 段，不解析完整 AST）
func parseImports(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := make([]string, 0, len(f.Imports))
	for _, s := range f.Imports {
		if s.Path == nil {
			continue
		}
		p, err := strconv.Unquote(s.Path.Value)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func printDepsText(r *depsReport) {
	fmt.Printf("engine doctor deps — root=%s\n\n", r.Root)
	keys := make([]string, 0, len(r.Scanned))
	for k := range r.Scanned {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-8s scanned %d .go files\n", k, r.Scanned[k])
	}
	fmt.Println()

	if len(r.Violations) == 0 {
		fmt.Println("[OK]   依赖方向体检通过：engine→, gamelib→, better/ 三条铁律均无违反")
		return
	}
	fmt.Printf("[FAIL] 检测到 %d 处违规：\n", len(r.Violations))
	for _, v := range r.Violations {
		fmt.Printf("  - [%s] %s\n      import %q — %s\n",
			v.Module, v.File, v.Import, v.Rule)
	}
}
