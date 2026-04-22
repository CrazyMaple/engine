package codegen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// StabilityLevel API 稳定性分级
type StabilityLevel string

const (
	StabilityStable       StabilityLevel = "Stable"       // 稳定 API，向后兼容承诺
	StabilityBeta         StabilityLevel = "Beta"         // Beta，可能有小幅调整
	StabilityExperimental StabilityLevel = "Experimental" // 实验性，可能大改
	StabilityDeprecated   StabilityLevel = "Deprecated"   // 已弃用
	StabilityUnmarked     StabilityLevel = ""             // 未标注
)

// APISymbol 被扫描的公共符号
type APISymbol struct {
	Package    string          // 包路径相对 repo 根，如 "actor"
	File       string          // 绝对文件路径
	Line       int             // 声明所在行
	Name       string          // 符号名，如 "ActorCell.Send"
	Kind       string          // "func" | "method" | "type" | "var" | "const"
	Stability  StabilityLevel  // 稳定性标签
	Since      string          // 首次引入版本，如 "v1.8"
	Deprecated string          // 弃用原因（如果 StabilityDeprecated）
	DocComment string          // 去除稳定性标注后的文档
}

// StabilityReport API 稳定性扫描结果
type StabilityReport struct {
	Root     string      // 扫描根目录
	Symbols  []APISymbol // 按 Package+Name 排序
	ByLevel  map[StabilityLevel]int
	Packages []string // 涉及的包
}

// Summary 生成可读摘要
func (r *StabilityReport) Summary() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("API Stability Report\n"))
	b.WriteString(fmt.Sprintf("  Root:     %s\n", r.Root))
	b.WriteString(fmt.Sprintf("  Packages: %d\n", len(r.Packages)))
	b.WriteString(fmt.Sprintf("  Symbols:  %d\n", len(r.Symbols)))
	b.WriteString("  Breakdown:\n")
	order := []StabilityLevel{
		StabilityStable, StabilityBeta, StabilityExperimental,
		StabilityDeprecated, StabilityUnmarked,
	}
	for _, lvl := range order {
		name := string(lvl)
		if name == "" {
			name = "Unmarked"
		}
		b.WriteString(fmt.Sprintf("    %-14s %d\n", name, r.ByLevel[lvl]))
	}
	return b.String()
}

// Markdown 生成 Markdown 表格，按包分组
func (r *StabilityReport) Markdown() string {
	var b strings.Builder
	b.WriteString("# API Stability Index\n\n")

	byPkg := make(map[string][]APISymbol)
	for _, s := range r.Symbols {
		byPkg[s.Package] = append(byPkg[s.Package], s)
	}

	pkgs := make([]string, 0, len(byPkg))
	for p := range byPkg {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)

	for _, pkg := range pkgs {
		syms := byPkg[pkg]
		b.WriteString(fmt.Sprintf("## %s\n\n", pkg))
		b.WriteString("| Symbol | Kind | Stability | Since | Note |\n")
		b.WriteString("|---|---|---|---|---|\n")
		for _, s := range syms {
			note := s.DocComment
			if s.Deprecated != "" {
				note = "DEPRECATED: " + s.Deprecated
			}
			stability := string(s.Stability)
			if stability == "" {
				stability = "—"
			}
			since := s.Since
			if since == "" {
				since = "—"
			}
			b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | %s |\n",
				s.Name, s.Kind, stability, since, escapeMarkdown(note)))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ScanStability 扫描目录下所有 Go 源文件，提取带稳定性标注的公共 API
//
// 稳定性注解规则（出现在符号的 doc comment 中）：
//   // Stable: ...
//   // Beta: ...
//   // Experimental: ...
//   // Deprecated: 原因
//   // Since: v1.8
//
// skipDirs 中列出的目录名会被跳过（默认包含 better、testdata、vendor）。
func ScanStability(root string, skipDirs []string) (*StabilityReport, error) {
	report := &StabilityReport{
		Root:    root,
		ByLevel: make(map[StabilityLevel]int),
	}
	skips := map[string]bool{
		"better":   true,
		"testdata": true,
		"vendor":   true,
		".git":     true,
		"node_modules": true,
	}
	for _, d := range skipDirs {
		skips[d] = true
	}
	pkgSet := make(map[string]bool)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skips[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		pkgDir := filepath.Dir(rel)
		pkgSet[pkgDir] = true

		syms, err := parseStabilityFile(path, pkgDir)
		if err != nil {
			// 单文件解析失败不中断整体扫描
			return nil
		}
		report.Symbols = append(report.Symbols, syms...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, s := range report.Symbols {
		report.ByLevel[s.Stability]++
	}
	sort.Slice(report.Symbols, func(i, j int) bool {
		if report.Symbols[i].Package != report.Symbols[j].Package {
			return report.Symbols[i].Package < report.Symbols[j].Package
		}
		return report.Symbols[i].Name < report.Symbols[j].Name
	})
	report.Packages = make([]string, 0, len(pkgSet))
	for p := range pkgSet {
		report.Packages = append(report.Packages, p)
	}
	sort.Strings(report.Packages)
	return report, nil
}

func parseStabilityFile(path, pkgDir string) ([]APISymbol, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var syms []APISymbol
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			sym := APISymbol{
				Package: pkgDir,
				File:    path,
				Line:    fset.Position(d.Pos()).Line,
				Name:    funcDeclName(d),
				Kind:    "func",
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				sym.Kind = "method"
			}
			applyStabilityDoc(d.Doc, &sym)
			syms = append(syms, sym)

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if !s.Name.IsExported() {
						continue
					}
					sym := APISymbol{
						Package: pkgDir,
						File:    path,
						Line:    fset.Position(s.Pos()).Line,
						Name:    s.Name.Name,
						Kind:    "type",
					}
					// 优先使用 spec 自身 doc，其次 GenDecl 级 doc
					doc := s.Doc
					if doc == nil {
						doc = d.Doc
					}
					applyStabilityDoc(doc, &sym)
					syms = append(syms, sym)

				case *ast.ValueSpec:
					for _, name := range s.Names {
						if !name.IsExported() {
							continue
						}
						kind := "var"
						if d.Tok == token.CONST {
							kind = "const"
						}
						sym := APISymbol{
							Package: pkgDir,
							File:    path,
							Line:    fset.Position(name.Pos()).Line,
							Name:    name.Name,
							Kind:    kind,
						}
						doc := s.Doc
						if doc == nil {
							doc = d.Doc
						}
						applyStabilityDoc(doc, &sym)
						syms = append(syms, sym)
					}
				}
			}
		}
	}
	return syms, nil
}

// applyStabilityDoc 从注释块中解析稳定性标签，填充 APISymbol
func applyStabilityDoc(cg *ast.CommentGroup, sym *APISymbol) {
	if cg == nil {
		return
	}
	var docLines []string
	for _, c := range cg.List {
		txt := strings.TrimPrefix(c.Text, "//")
		txt = strings.TrimPrefix(txt, "/*")
		txt = strings.TrimSuffix(txt, "*/")
		txt = strings.TrimSpace(txt)

		// 多行 comment 中每行都处理
		for _, line := range strings.Split(txt, "\n") {
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "*")
			line = strings.TrimSpace(line)
			if matched := matchStabilityTag(line, sym); matched {
				continue
			}
			if line != "" {
				docLines = append(docLines, line)
			}
		}
	}
	sym.DocComment = strings.Join(docLines, " ")
}

// matchStabilityTag 尝试匹配一行为稳定性标签；匹配成功返回 true
func matchStabilityTag(line string, sym *APISymbol) bool {
	for _, p := range []struct {
		prefix string
		level  StabilityLevel
	}{
		{"Stable:", StabilityStable},
		{"Stable ", StabilityStable},
		{"Beta:", StabilityBeta},
		{"Beta ", StabilityBeta},
		{"Experimental:", StabilityExperimental},
		{"Experimental ", StabilityExperimental},
	} {
		if strings.HasPrefix(line, p.prefix) {
			sym.Stability = p.level
			return true
		}
	}
	if strings.HasPrefix(line, "Deprecated:") {
		sym.Stability = StabilityDeprecated
		sym.Deprecated = strings.TrimSpace(strings.TrimPrefix(line, "Deprecated:"))
		return true
	}
	if strings.HasPrefix(line, "Since:") {
		sym.Since = strings.TrimSpace(strings.TrimPrefix(line, "Since:"))
		return true
	}
	return false
}

func funcDeclName(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return d.Name.Name
	}
	recv := d.Recv.List[0].Type
	var recvName string
	switch t := recv.(type) {
	case *ast.Ident:
		recvName = t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			recvName = "*" + id.Name
		}
	}
	return recvName + "." + d.Name.Name
}

func escapeMarkdown(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
