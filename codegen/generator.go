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

// GenerateMarkdownDoc 生成 Markdown 格式的消息 API 参考文档
func GenerateMarkdownDoc(msgs []MessageDef) ([]byte, error) {
	tmpl, err := template.New("doc").Funcs(template.FuncMap{
		"jsonType": goTypeToJSONDesc,
	}).Parse(markdownDocTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse doc template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, msgs); err != nil {
		return nil, fmt.Errorf("execute doc template: %w", err)
	}
	return buf.Bytes(), nil
}

// GenerateCSharp 生成 C# 类型定义（Unity 客户端适配）
func GenerateCSharp(msgs []MessageDef, namespace string) ([]byte, error) {
	tmpl, err := template.New("csharp").Funcs(template.FuncMap{
		"csType": goTypeToCSharp,
	}).Parse(csharpTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse csharp template: %w", err)
	}

	data := struct {
		Namespace string
		Messages  []MessageDef
	}{
		Namespace: namespace,
		Messages:  msgs,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute csharp template: %w", err)
	}
	return buf.Bytes(), nil
}

// GenerateTSSDK 生成 TypeScript 完整 SDK（含 WebSocket 连接管理、自动重连、消息收发）
func GenerateTSSDK(msgs []MessageDef) ([]byte, error) {
	tmpl, err := template.New("tssdk").Funcs(template.FuncMap{
		"tsType": goTypeToTS,
	}).Parse(tsSDKTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse sdk template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, msgs); err != nil {
		return nil, fmt.Errorf("execute sdk template: %w", err)
	}
	return buf.Bytes(), nil
}

// GenerateTypeRegistry 生成 TypeRegistry 注册代码
// 用于将 proto 解析出的消息类型注册到 remote.TypeRegistry
func GenerateTypeRegistry(msgs []MessageDef, pkg string) ([]byte, error) {
	tmpl, err := template.New("registry").Parse(typeRegistryTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse registry template: %w", err)
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
		return nil, fmt.Errorf("execute registry template: %w", err)
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
