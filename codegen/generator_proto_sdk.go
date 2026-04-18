package codegen

import (
	"bytes"
	"fmt"
	"text/template"
)

// GenerateTSProtoSDK 生成 TypeScript Protobuf SDK（TypeRegistry + Codec Adapter）
// 与服务端 remote/zero_copy.go 的 TypeURL 解码路径对齐，
// 使用方需自行通过 protobuf.load 加载 .proto 并注册到 TypeRegistry
func GenerateTSProtoSDK(msgs []MessageDef) ([]byte, error) {
	tmpl, err := template.New("tsprotosdk").Funcs(template.FuncMap{
		"tsType": goTypeToTS,
	}).Parse(tsProtoSDKTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse ts proto sdk template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, msgs); err != nil {
		return nil, fmt.Errorf("execute ts proto sdk template: %w", err)
	}
	return buf.Bytes(), nil
}

// GenerateCSharpProtoSDK 生成 C# Protobuf SDK（ProtoTypeRegistry + Codec Adapter）
// 基于 Google.Protobuf 的 IMessage/MessageParser
func GenerateCSharpProtoSDK(msgs []MessageDef, namespace string) ([]byte, error) {
	tmpl, err := template.New("csprotosdk").Parse(csharpProtoSDKTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse cs proto sdk template: %w", err)
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
		return nil, fmt.Errorf("execute cs proto sdk template: %w", err)
	}
	return buf.Bytes(), nil
}

// GenerateProtoRegistryGo 生成服务端使用的 Proto 消息 ID 常量表
// 单一数据源，确保客户端与服务端 ID 映射一致
func GenerateProtoRegistryGo(msgs []MessageDef, pkg string) ([]byte, error) {
	tmpl, err := template.New("protoregistry").Parse(protoRegistryExampleTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse proto registry template: %w", err)
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
		return nil, fmt.Errorf("execute proto registry template: %w", err)
	}
	return buf.Bytes(), nil
}

// GenerateTSProtoExample 生成 TypeScript 示例工程入口文件 main.ts
func GenerateTSProtoExample() []byte {
	return []byte(tsExampleMainTemplate)
}

// GenerateUnityProtoExample 生成 Unity Protobuf 接入示例 MonoBehaviour
func GenerateUnityProtoExample(namespace string) ([]byte, error) {
	tmpl, err := template.New("unityexample").Parse(unityProtoExampleTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse unity example template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct{ Namespace string }{Namespace: namespace}); err != nil {
		return nil, fmt.Errorf("execute unity example template: %w", err)
	}
	return buf.Bytes(), nil
}
