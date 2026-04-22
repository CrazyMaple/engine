package codegen

import (
	"bytes"
	"fmt"
	"strings"
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

// RPCPair 表示一对 Request/Response 消息
type RPCPair struct {
	Request  string
	Response string
}

// DetectRPCPairs 从消息列表中识别 Request/Response 配对
//
// 识别规则：对任一以 "Request" 结尾的消息 XxxRequest，若存在同前缀的 XxxResponse
// 则配对为一个 RPC；否则忽略（保留为单向 push）
func DetectRPCPairs(msgs []MessageDef) []RPCPair {
	names := make(map[string]struct{}, len(msgs))
	for _, m := range msgs {
		names[m.Name] = struct{}{}
	}
	var pairs []RPCPair
	for _, m := range msgs {
		if !strings.HasSuffix(m.Name, "Request") {
			continue
		}
		resp := strings.TrimSuffix(m.Name, "Request") + "Response"
		if _, ok := names[resp]; ok {
			pairs = append(pairs, RPCPair{Request: m.Name, Response: resp})
		}
	}
	return pairs
}

// GenerateCSharpRPCEnhance 生成 C# 强类型 RPC / Push 增强层
// 追加在 C# Proto SDK 之后使用，与生成的 ProtoTypeRegistry + ProtobufCodecAdapter 协作
func GenerateCSharpRPCEnhance(namespace string) ([]byte, error) {
	tmpl, err := template.New("csrpc").Parse(csharpRPCEnhancedTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse cs rpc template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct{ Namespace string }{Namespace: namespace}); err != nil {
		return nil, fmt.Errorf("execute cs rpc template: %w", err)
	}
	return buf.Bytes(), nil
}

// GenerateTSRPCEnhance 生成 TypeScript 强类型 RPC / Push 增强层
//
// 使用要求：生成结果应与已生成的 TypeScript SDK（含 MessageMap/MessageIDs）合并导入，
// 即追加到 sdk-v2 或 proto-sdk 的输出文件尾部（同一模块内共享 MessageMap 类型）
func GenerateTSRPCEnhance(msgs []MessageDef) ([]byte, error) {
	tmpl, err := template.New("tsrpc").Parse(tsRPCEnhancedTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse ts rpc template: %w", err)
	}
	data := struct {
		Pairs []RPCPair
	}{
		Pairs: DetectRPCPairs(msgs),
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute ts rpc template: %w", err)
	}
	return buf.Bytes(), nil
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
