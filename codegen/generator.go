package codegen

import (
	"bytes"
	"fmt"
	"text/template"
)

// GenerateGo 生成 Go 消息注册和路由代码
func GenerateGo(msgs []MessageDef, pkg string) ([]byte, error) {
	tmpl, err := template.New("go").Parse(goTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	data := struct {
		Package  string
		Messages []MessageDef
	}{
		Package:  pkg,
		Messages: msgs,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}
	return buf.Bytes(), nil
}

// GenerateTS 生成 TypeScript 类型定义
func GenerateTS(msgs []MessageDef) ([]byte, error) {
	tmpl, err := template.New("ts").Funcs(template.FuncMap{
		"tsType": goTypeToTS,
	}).Parse(tsTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, msgs); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}
	return buf.Bytes(), nil
}

func goTypeToTS(goType string) string {
	switch goType {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64":
		return "number"
	case "string":
		return "string"
	case "bool":
		return "boolean"
	default:
		if len(goType) > 2 && goType[:2] == "[]" {
			return goTypeToTS(goType[2:]) + "[]"
		}
		return "any"
	}
}
