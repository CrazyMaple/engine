package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// cmd_doctor_deps_config.go — Phase 4 反向门禁：ClusterConfig 弃用注解检查
//
// 手册 §3 Phase 4 / §4.3：
//
//   ClusterConfig 含未标注 Deprecated 的 Gossip* 字段、SeedNodes 字段、
//   WithSeedNodes 方法或 WithGossipInterval 方法时报警。
//
// 实现：解析 engine/cluster/config.go 的 AST，对：
//
//   - struct ClusterConfig 的字段名以 "Gossip" 开头者，或字段名为 "SeedNodes"；
//   - 函数（接收者 *ClusterConfig 或 ClusterConfig）名为 "WithSeedNodes" / "WithGossipInterval"；
//
// 校验其 doc 注释中是否包含 "Deprecated:" 行（按 Go 标准 deprecation 注解约定）。

// configDeprecationFile 反向门禁固定扫描的源文件（仓库根相对路径）。
const configDeprecationFile = "engine/cluster/config.go"

// configDeprecationFields ClusterConfig 中要求标注 Deprecated 的字段名。
//
// 字段名以 "Gossip" 开头的字段动态判定；这里只列名固定字段。
var configDeprecationFields = []string{"SeedNodes"}

// configDeprecationMethods ClusterConfig 上要求标注 Deprecated 的方法名。
var configDeprecationMethods = []string{"WithSeedNodes", "WithGossipInterval"}

// checkClusterConfigDeprecations 解析 engine/cluster/config.go，
// 返回未标注 Deprecated 的违规清单。文件不存在时返回空（视为已删除）。
func checkClusterConfigDeprecations(root string) ([]baselineViolation, error) {
	full := filepath.Join(root, filepath.FromSlash(configDeprecationFile))
	if _, err := os.Stat(full); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, full, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", full, err)
	}

	var vs []baselineViolation

	requiredFields := toSet(configDeprecationFields)
	requiredMethods := toSet(configDeprecationMethods)

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name == nil || ts.Name.Name != "ClusterConfig" {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					continue
				}
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						if name == nil {
							continue
						}
						n := name.Name
						match := false
						if _, ok := requiredFields[n]; ok {
							match = true
						}
						if strings.HasPrefix(n, "Gossip") {
							match = true
						}
						if !match {
							continue
						}
						if !hasDeprecatedAnnotation(field.Doc) {
							pos := fset.Position(name.Pos())
							vs = append(vs, baselineViolation{
								Kind:    "config",
								Pkg:     "engine/cluster.ClusterConfig",
								Subject: fmt.Sprintf("%s:%d field %s", configDeprecationFile, pos.Line, n),
								Detail: fmt.Sprintf(
									"Phase 4 反向门禁：ClusterConfig.%s 字段必须标注 // Deprecated:（删除或保留二选一）",
									n,
								),
							})
						}
					}
				}
			}
		case *ast.FuncDecl:
			if d.Name == nil || d.Recv == nil {
				continue
			}
			if !isClusterConfigReceiver(d.Recv) {
				continue
			}
			n := d.Name.Name
			if _, ok := requiredMethods[n]; !ok {
				continue
			}
			if !hasDeprecatedAnnotation(d.Doc) {
				pos := fset.Position(d.Pos())
				vs = append(vs, baselineViolation{
					Kind:    "config",
					Pkg:     "engine/cluster.ClusterConfig",
					Subject: fmt.Sprintf("%s:%d method %s", configDeprecationFile, pos.Line, n),
					Detail: fmt.Sprintf(
						"Phase 4 反向门禁：(*ClusterConfig).%s 方法必须标注 // Deprecated:（删除或保留二选一）",
						n,
					),
				})
			}
		}
	}

	sortViolations(vs)
	return vs, nil
}

// isClusterConfigReceiver 判断函数接收者是否为 *ClusterConfig 或 ClusterConfig
func isClusterConfigReceiver(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) == 0 {
		return false
	}
	t := recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	id, ok := t.(*ast.Ident)
	if !ok {
		return false
	}
	return id.Name == "ClusterConfig"
}

// hasDeprecatedAnnotation 检查注释组中是否存在 "Deprecated:" 行（Go 官方 deprecation 约定）
func hasDeprecatedAnnotation(cg *ast.CommentGroup) bool {
	if cg == nil {
		return false
	}
	for _, c := range cg.List {
		text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(c.Text, "//"), "/*"))
		text = strings.TrimSuffix(text, "*/")
		text = strings.TrimSpace(text)
		if strings.HasPrefix(text, "Deprecated:") {
			return true
		}
	}
	return false
}
