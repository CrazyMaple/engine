package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// cmd_doctor_deps_baseline.go — v1.13 baseline 冻结 / 校验
//
// 在 v1.12 doctor deps（依赖方向铁律）之上，引入 v1.13 §3.0.A 描述的三类 baseline：
//
//   - import_baseline：禁止新增"待外迁"包的引用方文件（freeze）→ 全部清零（zero）
//   - file_baseline.should_be_removed：原文件须按 Phase 删除，不允许"重写为新文件"逃过
//   - symbol_baseline：在指定包内不允许出现某些导出符号 / 关键词 / 常量
//
// 路径匹配语义（与手册 §3.0.A 一致）：
//
//   - 路径条目均为仓库根相对路径
//   - exclude_paths 以"/"结尾视为目录前缀，递归生效；否则视为精确文件
//   - import_baseline.value 各项为精确文件路径
//   - file_baseline.should_be_removed 以"/"结尾视为目录条目（递归检查），否则精确文件
//   - symbol_baseline 的 key 为 import path（即仓库根下相对目录路径）

const (
	baselineModeFreeze = "freeze"
	baselineModeZero   = "zero"
)

// Baseline 表示 v1.13_dep_baseline.json 的整体 schema。
type Baseline struct {
	Version        string                 `json:"version"`
	Mode           string                 `json:"mode"`
	ExcludePaths   []string               `json:"exclude_paths"`
	ImportBaseline map[string][]string    `json:"import_baseline"`
	FileBaseline   FileBaselineRules      `json:"file_baseline"`
	SymbolBaseline map[string]SymbolRules `json:"symbol_baseline"`
}

// FileBaselineRules 描述 file_baseline 段。
type FileBaselineRules struct {
	ShouldBeRemoved []string `json:"should_be_removed"`
}

// SymbolRules 描述 symbol_baseline 中一个包的禁用清单。
//
// ForbiddenSymbols / ForbiddenKeywords / ForbiddenConstants 三段是"规则"——
// 描述哪些符号 / 关键词 / 常量在该包下被视为待清理的 forbidden 命中。
//
// CurrentHits 是可选的"快照"——记录基线落下时的实际命中位置（文件 + 行号）。
// 在 freeze 模式下，--check-baseline 用 (file, name) 二元组比较 snapshot
// 与当前扫描结果：
//
//   - 在 snapshot 中存在但当前已消失的命中：允许（freeze 允许减少）。
//   - 在 snapshot 中不存在但当前出现的命中：判违规（freeze 不允许新增）。
//
// 行号 Line 仅作为人工审阅信息；比较时不参与，避免格式化导致 baseline 频繁刷新。
//
// 若一个包的 SymbolRules.CurrentHits == nil，freeze 模式视为"零基线"：
// 该包当前的任何命中都判违规——这是 v1.13 新 baseline 的目标语义，
// 旧 baseline（仅有规则段）落地前必须用 `--baseline` 重新写一遍以补齐 current_hits。
type SymbolRules struct {
	ForbiddenSymbols   []string            `json:"forbidden_symbols,omitempty"`
	ForbiddenKeywords  []string            `json:"forbidden_keywords,omitempty"`
	ForbiddenConstants []string            `json:"forbidden_constants,omitempty"`
	CurrentHits        *SymbolHitsSnapshot `json:"current_hits,omitempty"`
}

// SymbolHitsSnapshot 记录一个包下三类命中（symbol / keyword / constant）的具体位置。
type SymbolHitsSnapshot struct {
	Symbol   []HitEntry `json:"symbol,omitempty"`
	Keyword  []HitEntry `json:"keyword,omitempty"`
	Constant []HitEntry `json:"constant,omitempty"`
}

// HitEntry 单条命中位置（仓库根相对路径）。
type HitEntry struct {
	Name string `json:"name"`
	File string `json:"file"`
	Line int    `json:"line,omitempty"`
}

// baselineSnapshot 是当前扫描出来的"实际状态"。
type baselineSnapshot struct {
	ImportHits map[string][]string                // import path → 引用此包的文件清单（仓库根相对路径，已排序去重）
	FileHits   []string                           // 仍然存在的 should_be_removed 条目（仓库根相对路径，目录条目以"/"结尾保持原样）
	SymbolHits map[string]map[string][]symbolHit  // pkg → category("symbol"|"keyword"|"constant") → 命中
}

// symbolHit 单条符号命中。
type symbolHit struct {
	Name string
	File string // 仓库根相对路径
	Line int
}

// baselineViolation 单条违规记录。
type baselineViolation struct {
	Kind    string // "import" | "file" | "symbol"
	Pkg     string // import path 或 file_baseline 的条目
	Subject string // 具体 import 文件 / 符号名 / 文件路径
	Detail  string
}

// loadBaseline 解析 baseline JSON 文件。
func loadBaseline(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read baseline %s: %w", path, err)
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parse baseline %s: %w", path, err)
	}
	if b.Mode != baselineModeFreeze && b.Mode != baselineModeZero {
		return nil, fmt.Errorf("baseline mode %q invalid (want %q or %q)",
			b.Mode, baselineModeFreeze, baselineModeZero)
	}
	return &b, nil
}

// snapshotForBaseline 按 baseline 中 ImportBaseline / FileBaseline / SymbolBaseline 的
// key 集合，扫描当前仓库实际状态。
//
// snapshot 中的 ImportHits / FileHits / SymbolHits 仅覆盖 baseline 关心的 key，
// 不做"全仓符号清单"那种宽口径扫描。
func snapshotForBaseline(root string, b *Baseline) (*baselineSnapshot, error) {
	snap := &baselineSnapshot{
		ImportHits: map[string][]string{},
		FileHits:   nil,
		SymbolHits: map[string]map[string][]symbolHit{},
	}

	// import_baseline：扫描 engine/gamelib/tool 下所有 .go 文件，对每条 import
	// 检查是否落在 baseline 的 key 前缀下；命中则记录"该文件引用了此 key"。
	if len(b.ImportBaseline) > 0 {
		hits, err := collectImportHits(root, b)
		if err != nil {
			return nil, err
		}
		snap.ImportHits = hits
	}

	// file_baseline：逐条检查存在性
	for _, entry := range b.FileBaseline.ShouldBeRemoved {
		if fileBaselineEntryExists(root, entry) {
			snap.FileHits = append(snap.FileHits, entry)
		}
	}

	// symbol_baseline：按包扫描
	for pkg, rules := range b.SymbolBaseline {
		hits, err := collectSymbolHits(root, pkg, rules, b.ExcludePaths)
		if err != nil {
			return nil, err
		}
		if len(hits) > 0 {
			snap.SymbolHits[pkg] = hits
		}
	}

	return snap, nil
}

// collectImportHits 扫描三 module 全部 .go 文件（受 exclude_paths 影响），
// 返回 baseline.ImportBaseline key → 引用此 key 的文件清单。
func collectImportHits(root string, b *Baseline) (map[string][]string, error) {
	keys := make([]string, 0, len(b.ImportBaseline))
	for k := range b.ImportBaseline {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	hits := map[string]map[string]struct{}{}
	for _, k := range keys {
		hits[k] = map[string]struct{}{}
	}

	for _, mod := range []string{"engine", "gamelib", "tool"} {
		modDir := filepath.Join(root, mod)
		if _, err := os.Stat(modDir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		err := filepath.WalkDir(modDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := d.Name()
				if name == "vendor" || strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if pathExcluded(rel, b.ExcludePaths) {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			imports, err := parseImports(path)
			if err != nil {
				return err
			}
			for _, imp := range imports {
				for _, k := range keys {
					if importMatches(imp, k) {
						hits[k][rel] = struct{}{}
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	out := make(map[string][]string, len(hits))
	for k, set := range hits {
		files := make([]string, 0, len(set))
		for f := range set {
			files = append(files, f)
		}
		sort.Strings(files)
		out[k] = files
	}
	return out, nil
}

// importMatches 判断 import 路径是否落在 baseline key 前缀下。
// 语义：等于 key，或以 key+"/" 开头（避免 engine/router 误匹配 engine/router2）。
func importMatches(imp, key string) bool {
	if imp == key {
		return true
	}
	return strings.HasPrefix(imp, key+"/")
}

// fileBaselineEntryExists 判断 should_be_removed 中一条条目是否仍存在。
//
// 语义按手册 §3.0.A：
//
//   - 以 "/" 结尾视为目录条目，目录本身或其下任意文件存在即判违规。
//   - 否则视为精确文件路径，仅当此文件存在时判违规。
//
// 由于目录的存在隐含"其下有内容"（os.Stat 命中目录即说明该目录尚未被删除），
// 这里统一退化为"路径存在即未删除"。
func fileBaselineEntryExists(root, entry string) bool {
	abs := filepath.Join(root, filepath.FromSlash(strings.TrimSuffix(entry, "/")))
	_, err := os.Stat(abs)
	return err == nil
}

// pathExcluded 判断仓库根相对路径是否落在 exclude_paths 之下。
//
//   - 以 "/" 结尾的条目按目录前缀匹配，递归命中（前缀匹配语义；rel 必须真的位于该目录之下）。
//   - 否则按精确文件路径匹配。
func pathExcluded(rel string, excludes []string) bool {
	for _, ex := range excludes {
		if strings.HasSuffix(ex, "/") {
			if strings.HasPrefix(rel, ex) {
				return true
			}
		} else {
			if rel == ex {
				return true
			}
		}
	}
	return false
}

// collectSymbolHits 扫描某个包目录下的 .go 文件，按规则收集命中。
func collectSymbolHits(root, pkg string, rules SymbolRules, excludes []string) (map[string][]symbolHit, error) {
	pkgDir := filepath.Join(root, filepath.FromSlash(pkg))
	if _, err := os.Stat(pkgDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	wantSym := toSet(rules.ForbiddenSymbols)
	wantConst := toSet(rules.ForbiddenConstants)
	keywords := rules.ForbiddenKeywords

	hits := map[string][]symbolHit{}
	add := func(cat string, h symbolHit) {
		hits[cat] = append(hits[cat], h)
	}

	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		full := filepath.Join(pkgDir, e.Name())
		rel, _ := filepath.Rel(root, full)
		rel = filepath.ToSlash(rel)
		if pathExcluded(rel, excludes) {
			continue
		}

		// 解析 AST 找声明的符号 / 常量
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, full, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", full, err)
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name == nil {
					continue
				}
				name := d.Name.Name
				if _, ok := wantSym[name]; ok {
					add("symbol", symbolHit{Name: name, File: rel, Line: fset.Position(d.Pos()).Line})
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if _, ok := wantSym[s.Name.Name]; ok {
							add("symbol", symbolHit{Name: s.Name.Name, File: rel, Line: fset.Position(s.Pos()).Line})
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if d.Tok == token.CONST {
								if _, ok := wantConst[n.Name]; ok {
									add("constant", symbolHit{Name: n.Name, File: rel, Line: fset.Position(n.Pos()).Line})
								}
								// 常量名也可能被纳入 forbidden_symbols（兼容）
								if _, ok := wantSym[n.Name]; ok {
									add("symbol", symbolHit{Name: n.Name, File: rel, Line: fset.Position(n.Pos()).Line})
								}
							} else {
								if _, ok := wantSym[n.Name]; ok {
									add("symbol", symbolHit{Name: n.Name, File: rel, Line: fset.Position(n.Pos()).Line})
								}
							}
						}
					}
				}
			}
		}

		// keyword：grep 文件文本（区分大小写）
		if len(keywords) > 0 {
			data, err := os.ReadFile(full)
			if err != nil {
				return nil, err
			}
			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				for _, kw := range keywords {
					if kw == "" {
						continue
					}
					if strings.Contains(line, kw) {
						add("keyword", symbolHit{Name: kw, File: rel, Line: i + 1})
						break // 同一行多关键词只记一次
					}
				}
			}
		}
	}

	for cat := range hits {
		sort.Slice(hits[cat], func(i, j int) bool {
			a, b := hits[cat][i], hits[cat][j]
			if a.File != b.File {
				return a.File < b.File
			}
			if a.Line != b.Line {
				return a.Line < b.Line
			}
			return a.Name < b.Name
		})
	}
	return hits, nil
}

func toSet(items []string) map[string]struct{} {
	m := make(map[string]struct{}, len(items))
	for _, x := range items {
		m[x] = struct{}{}
	}
	return m
}

// hitKey 把一条命中折叠为 (file, name) 二元组的字符串键。
// Line 不参与，避免格式化导致 baseline 频繁刷新。
func hitKey(h symbolHit) string {
	return h.File + "|" + h.Name
}

// allowedHitKeys 把 baseline.SymbolHitsSnapshot 转成 category → set(file|name)。
func allowedHitKeys(s *SymbolHitsSnapshot) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{
		"symbol":   {},
		"keyword":  {},
		"constant": {},
	}
	if s == nil {
		return out
	}
	for _, e := range s.Symbol {
		out["symbol"][e.File+"|"+e.Name] = struct{}{}
	}
	for _, e := range s.Keyword {
		out["keyword"][e.File+"|"+e.Name] = struct{}{}
	}
	for _, e := range s.Constant {
		out["constant"][e.File+"|"+e.Name] = struct{}{}
	}
	return out
}

// hitsToEntries 把扫描得到的 symbolHit 列表转为可序列化的 HitEntry 列表，按 (file, line, name) 排序。
func hitsToEntries(hits []symbolHit) []HitEntry {
	if len(hits) == 0 {
		return nil
	}
	out := make([]HitEntry, 0, len(hits))
	for _, h := range hits {
		out = append(out, HitEntry{Name: h.Name, File: h.File, Line: h.Line})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Name < b.Name
	})
	return out
}

// computeViolations 比较 snapshot 与 baseline，按 mode 给出违规清单。
//
//   - freeze：
//       import：snap.ImportHits[k] 的集合不得超过 baseline.ImportBaseline[k]（不允许新增）。
//       file：snap.FileHits 集合不得超过 baseline.FileBaseline.ShouldBeRemoved（已记录的允许继续存在）。
//       symbol：snap.SymbolHits 中的命中按 (file, name) 与 baseline.symbol_baseline.<pkg>.current_hits
//              对比；不在 current_hits 中的命中判违规。current_hits 缺失的包视为零基线。
//   - zero：snap.* 全为空。
func computeViolations(b *Baseline, snap *baselineSnapshot) []baselineViolation {
	switch b.Mode {
	case baselineModeZero:
		return computeZeroViolations(b, snap)
	default:
		return computeFreezeViolations(b, snap)
	}
}

func computeFreezeViolations(b *Baseline, snap *baselineSnapshot) []baselineViolation {
	var vs []baselineViolation

	// import：snap[k] - baseline[k] 不能为非空
	for k, want := range b.ImportBaseline {
		got := snap.ImportHits[k]
		wantSet := toSet(want)
		for _, f := range got {
			if _, ok := wantSet[f]; !ok {
				vs = append(vs, baselineViolation{
					Kind:    "import",
					Pkg:     k,
					Subject: f,
					Detail:  fmt.Sprintf("freeze: 文件 %s 新增了对 %s 的引用，未登记在 import_baseline", f, k),
				})
			}
		}
	}

	// file：snap.FileHits - baseline.ShouldBeRemoved 不能为非空
	wantFiles := toSet(b.FileBaseline.ShouldBeRemoved)
	for _, e := range snap.FileHits {
		if _, ok := wantFiles[e]; !ok {
			vs = append(vs, baselineViolation{
				Kind:    "file",
				Pkg:     "file_baseline",
				Subject: e,
				Detail:  fmt.Sprintf("freeze: 文件/目录 %s 出现在仓库中，但未登记在 should_be_removed", e),
			})
		}
	}

	// symbol：snap 命中的每条 (file, name) 必须出现在 baseline.current_hits 中。
	// current_hits 缺失视为"零基线"——任何命中都视作新增。
	for pkg, cats := range snap.SymbolHits {
		rules, registered := b.SymbolBaseline[pkg]
		if !registered {
			for cat, hs := range cats {
				for _, h := range hs {
					vs = append(vs, baselineViolation{
						Kind:    "symbol",
						Pkg:     pkg,
						Subject: fmt.Sprintf("%s:%d %s/%s", h.File, h.Line, cat, h.Name),
						Detail:  fmt.Sprintf("freeze: %s 包未登记在 symbol_baseline，但出现命中", pkg),
					})
				}
			}
			continue
		}
		allowed := allowedHitKeys(rules.CurrentHits)
		for cat, hs := range cats {
			for _, h := range hs {
				if _, ok := allowed[cat][hitKey(h)]; !ok {
					detail := fmt.Sprintf("freeze: %s 中 %s 命中 %q 出现在 %s:%d，但该位置未登记在 current_hits",
						pkg, cat, h.Name, h.File, h.Line)
					if rules.CurrentHits == nil {
						detail = fmt.Sprintf("freeze: %s 未冻结 current_hits（baseline 缺失快照），%s 命中 %q@%s:%d 视为新增",
							pkg, cat, h.Name, h.File, h.Line)
					}
					vs = append(vs, baselineViolation{
						Kind:    "symbol",
						Pkg:     pkg,
						Subject: fmt.Sprintf("%s:%d %s/%s", h.File, h.Line, cat, h.Name),
						Detail:  detail,
					})
				}
			}
		}
	}

	sortViolations(vs)
	return vs
}

func computeZeroViolations(b *Baseline, snap *baselineSnapshot) []baselineViolation {
	var vs []baselineViolation

	for k, files := range snap.ImportHits {
		for _, f := range files {
			vs = append(vs, baselineViolation{
				Kind:    "import",
				Pkg:     k,
				Subject: f,
				Detail:  fmt.Sprintf("zero: %s 仍被 %s 引用", k, f),
			})
		}
	}
	for _, e := range snap.FileHits {
		vs = append(vs, baselineViolation{
			Kind:    "file",
			Pkg:     "file_baseline",
			Subject: e,
			Detail:  fmt.Sprintf("zero: %s 仍存在于仓库中", e),
		})
	}
	for pkg, cats := range snap.SymbolHits {
		for cat, hs := range cats {
			for _, h := range hs {
				vs = append(vs, baselineViolation{
					Kind:    "symbol",
					Pkg:     pkg,
					Subject: fmt.Sprintf("%s:%d %s/%s", h.File, h.Line, cat, h.Name),
					Detail:  fmt.Sprintf("zero: %s 中 %s %q 仍存在", pkg, cat, h.Name),
				})
			}
		}
	}

	// 兼容 baseline 中 ImportBaseline value 还残留的情况：zero 模式应当是空数组
	for k, files := range b.ImportBaseline {
		if len(files) > 0 {
			for _, f := range files {
				vs = append(vs, baselineViolation{
					Kind:    "import",
					Pkg:     k,
					Subject: f,
					Detail:  fmt.Sprintf("zero: baseline.import_baseline[%s] 应为空数组，发现登记 %s", k, f),
				})
			}
		}
	}

	sortViolations(vs)
	return vs
}

func sortViolations(vs []baselineViolation) {
	sort.Slice(vs, func(i, j int) bool {
		a, b := vs[i], vs[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Pkg != b.Pkg {
			return a.Pkg < b.Pkg
		}
		return a.Subject < b.Subject
	})
}

// writeBaselineSnapshot 把当前扫描结果写为 baseline JSON：
// 用 snap 中的 ImportHits / FileHits / SymbolHits 反向重建 Baseline 结构（保留 baseline 中
// 描述性的"规则集合"如 forbidden_symbols 列表，因为这是规则不是观测）。
//
// 也就是说：
//
//   - import_baseline：每个 key 的 value 用扫描结果"当前哪些文件引用此 key"覆盖。
//   - file_baseline.should_be_removed：保持 baseline 原列表（这是规则）。
//   - symbol_baseline.<pkg>.forbidden_*：保持 baseline 原列表（这是规则）。
//   - symbol_baseline.<pkg>.current_hits：用扫描结果刷新（这是观测，freeze 比对用）。
//
// 这样，"重新冻结基线"会同步刷新 import_baseline.value 与 symbol_baseline.current_hits，
// 规则段（forbidden_*、should_be_removed）不会被覆盖。
func writeBaselineSnapshot(path string, b *Baseline, snap *baselineSnapshot) error {
	out := *b
	if out.Mode == "" {
		out.Mode = baselineModeFreeze
	}

	// 用 snapshot 重建 import_baseline.value
	if len(b.ImportBaseline) > 0 {
		out.ImportBaseline = make(map[string][]string, len(b.ImportBaseline))
		keys := make([]string, 0, len(b.ImportBaseline))
		for k := range b.ImportBaseline {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			files := snap.ImportHits[k]
			if files == nil {
				files = []string{}
			}
			sort.Strings(files)
			out.ImportBaseline[k] = files
		}
	}

	// 用 snapshot 重建 symbol_baseline.<pkg>.current_hits
	if len(b.SymbolBaseline) > 0 {
		out.SymbolBaseline = make(map[string]SymbolRules, len(b.SymbolBaseline))
		for pkg, rules := range b.SymbolBaseline {
			nr := SymbolRules{
				ForbiddenSymbols:   rules.ForbiddenSymbols,
				ForbiddenKeywords:  rules.ForbiddenKeywords,
				ForbiddenConstants: rules.ForbiddenConstants,
			}
			cats := snap.SymbolHits[pkg]
			snapshot := &SymbolHitsSnapshot{
				Symbol:   hitsToEntries(cats["symbol"]),
				Keyword:  hitsToEntries(cats["keyword"]),
				Constant: hitsToEntries(cats["constant"]),
			}
			if len(snapshot.Symbol) > 0 || len(snapshot.Keyword) > 0 || len(snapshot.Constant) > 0 {
				nr.CurrentHits = snapshot
			}
			out.SymbolBaseline[pkg] = nr
		}
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal baseline: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write baseline %s: %w", path, err)
	}
	return nil
}

// printBaselineReport 输出违规清单（人类可读）。
func printBaselineReport(b *Baseline, snap *baselineSnapshot, vs []baselineViolation, baselinePath string) {
	fmt.Printf("engine doctor deps --check-baseline %s — mode=%s\n\n", baselinePath, b.Mode)

	importKeys := make([]string, 0, len(b.ImportBaseline))
	for k := range b.ImportBaseline {
		importKeys = append(importKeys, k)
	}
	sort.Strings(importKeys)
	for _, k := range importKeys {
		fmt.Printf("  import_baseline[%s]: baseline=%d, current=%d\n",
			k, len(b.ImportBaseline[k]), len(snap.ImportHits[k]))
	}
	fmt.Printf("  file_baseline.should_be_removed: baseline=%d, still_present=%d\n",
		len(b.FileBaseline.ShouldBeRemoved), len(snap.FileHits))
	totalSymHits := 0
	for _, cats := range snap.SymbolHits {
		for _, hs := range cats {
			totalSymHits += len(hs)
		}
	}
	totalBaselineHits := 0
	for _, rules := range b.SymbolBaseline {
		if rules.CurrentHits == nil {
			continue
		}
		totalBaselineHits += len(rules.CurrentHits.Symbol) + len(rules.CurrentHits.Keyword) + len(rules.CurrentHits.Constant)
	}
	fmt.Printf("  symbol_baseline: pkgs=%d, baseline_hits=%d, current_hits=%d\n",
		len(b.SymbolBaseline), totalBaselineHits, totalSymHits)
	fmt.Println()

	if len(vs) == 0 {
		fmt.Println("[OK]   baseline 校验通过")
		return
	}
	fmt.Printf("[FAIL] baseline 违规 %d 条：\n", len(vs))
	for _, v := range vs {
		fmt.Printf("  - [%s] %s — %s\n", v.Kind, v.Pkg, v.Detail)
	}
}

// runBaselineCheck 主入口：读 baseline → 扫描 → 比对 → 报告。
// 返回违规数量；调用方决定是否非零退出。
//
// Phase 4 反向门禁：在 baseline 三类违规之外，额外校验
// engine/cluster.ClusterConfig 中 Gossip*/SeedNodes 字段及 WithSeedNodes /
// WithGossipInterval 方法是否标注 Deprecated:。
func runBaselineCheck(root, baselinePath string) (int, error) {
	b, err := loadBaseline(baselinePath)
	if err != nil {
		return 0, err
	}
	snap, err := snapshotForBaseline(root, b)
	if err != nil {
		return 0, err
	}
	vs := computeViolations(b, snap)
	cfgVs, err := checkClusterConfigDeprecations(root)
	if err != nil {
		return 0, err
	}
	vs = append(vs, cfgVs...)
	sortViolations(vs)
	printBaselineReport(b, snap, vs, baselinePath)
	return len(vs), nil
}

// runBaselineSnapshot 主入口：以现有 baseline 作为"规则模板"重新写一遍 import_baseline.value。
// 若 baselinePath 不存在，则需调用方先准备一份"骨架"；这里不做"无中生有"。
func runBaselineSnapshot(root, baselinePath string) error {
	b, err := loadBaseline(baselinePath)
	if err != nil {
		return err
	}
	snap, err := snapshotForBaseline(root, b)
	if err != nil {
		return err
	}
	return writeBaselineSnapshot(baselinePath, b, snap)
}
