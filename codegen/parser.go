package codegen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// MessageDef 消息结构体定义
type MessageDef struct {
	Name    string
	ID      int       // 自动分配的消息 ID
	Fields  []FieldDef
	Comment string
}

// FieldDef 字段定义
type FieldDef struct {
	Name string
	Type string
	Tag  string
}

// ParseFile 解析 Go 源文件，提取带有 //msggen:message 注释的消息结构体
func ParseFile(filename string) ([]MessageDef, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}

	var msgs []MessageDef
	nextID := 1001

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			// 检查是否有 //msggen:message 注释
			if !hasMessageMarker(genDecl.Doc) && !hasMessageMarker(typeSpec.Doc) {
				continue
			}

			msg := MessageDef{
				Name:    typeSpec.Name.Name,
				ID:      nextID,
				Comment: extractComment(genDecl.Doc),
			}
			nextID++

			for _, field := range structType.Fields.List {
				if len(field.Names) == 0 {
					continue // 嵌入字段
				}
				fd := FieldDef{
					Name: field.Names[0].Name,
					Type: typeToString(field.Type),
				}
				if field.Tag != nil {
					fd.Tag = field.Tag.Value
				}
				msg.Fields = append(msg.Fields, fd)
			}

			msgs = append(msgs, msg)
		}
	}

	return msgs, nil
}

func hasMessageMarker(cg *ast.CommentGroup) bool {
	if cg == nil {
		return false
	}
	for _, c := range cg.List {
		if strings.Contains(c.Text, "msggen:message") {
			return true
		}
	}
	return false
}

func extractComment(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	var lines []string
	for _, c := range cg.List {
		text := strings.TrimPrefix(c.Text, "//")
		text = strings.TrimPrefix(text, " ")
		if !strings.Contains(text, "msggen:") {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, " ")
}

func typeToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return typeToString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + typeToString(t.X)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + typeToString(t.Elt)
		}
		return fmt.Sprintf("[%s]%s", typeToString(t.Len), typeToString(t.Elt))
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", typeToString(t.Key), typeToString(t.Value))
	case *ast.BasicLit:
		return t.Value
	default:
		return "interface{}"
	}
}
