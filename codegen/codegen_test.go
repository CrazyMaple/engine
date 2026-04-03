package codegen

import (
	"strings"
	"testing"
)

func TestParseFile(t *testing.T) {
	msgs, err := ParseFile("testdata/sample_messages.go")
	if err != nil {
		t.Fatal(err)
	}

	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// LoginRequest
	if msgs[0].Name != "LoginRequest" {
		t.Errorf("expected LoginRequest, got %s", msgs[0].Name)
	}
	if len(msgs[0].Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(msgs[0].Fields))
	}
	if msgs[0].Fields[0].Name != "Username" || msgs[0].Fields[0].Type != "string" {
		t.Errorf("unexpected field: %+v", msgs[0].Fields[0])
	}

	// LoginResponse
	if msgs[1].Name != "LoginResponse" {
		t.Errorf("expected LoginResponse, got %s", msgs[1].Name)
	}
	if len(msgs[1].Fields) != 3 {
		t.Errorf("expected 3 fields, got %d", len(msgs[1].Fields))
	}

	// ChatMessage
	if msgs[2].Name != "ChatMessage" {
		t.Errorf("expected ChatMessage, got %s", msgs[2].Name)
	}

	// ID 应该连续
	if msgs[0].ID != 1001 || msgs[1].ID != 1002 || msgs[2].ID != 1003 {
		t.Errorf("unexpected IDs: %d, %d, %d", msgs[0].ID, msgs[1].ID, msgs[2].ID)
	}
}

func TestParseFileSkipsUnmarked(t *testing.T) {
	msgs, err := ParseFile("testdata/sample_messages.go")
	if err != nil {
		t.Fatal(err)
	}

	for _, msg := range msgs {
		if msg.Name == "NotAMessage" {
			t.Error("NotAMessage should not be parsed")
		}
	}
}

func TestParseFileJSONNames(t *testing.T) {
	msgs, err := ParseFile("testdata/sample_messages.go")
	if err != nil {
		t.Fatal(err)
	}

	// LoginRequest.Username 有 json:"username" tag
	if msgs[0].Fields[0].JSONName != "username" {
		t.Errorf("expected JSONName 'username', got %q", msgs[0].Fields[0].JSONName)
	}
	// LoginResponse.UserID 有 json:"user_id" tag
	if msgs[1].Fields[2].JSONName != "user_id" {
		t.Errorf("expected JSONName 'user_id', got %q", msgs[1].Fields[2].JSONName)
	}
}

func TestGenerateGo(t *testing.T) {
	msgs, err := ParseFile("testdata/sample_messages.go")
	if err != nil {
		t.Fatal(err)
	}

	code, err := GenerateGo(msgs, "mygame")
	if err != nil {
		t.Fatal(err)
	}

	output := string(code)

	if !strings.Contains(output, "package mygame") {
		t.Error("missing package declaration")
	}
	if !strings.Contains(output, "MsgID_LoginRequest") {
		t.Error("missing LoginRequest const")
	}
	if !strings.Contains(output, "MsgID_ChatMessage") {
		t.Error("missing ChatMessage const")
	}
	if !strings.Contains(output, "MessageTypes") {
		t.Error("missing MessageTypes map")
	}
	if !strings.Contains(output, "RegisterMessages") {
		t.Error("missing RegisterMessages func")
	}
}

func TestGenerateTS(t *testing.T) {
	msgs, err := ParseFile("testdata/sample_messages.go")
	if err != nil {
		t.Fatal(err)
	}

	code, err := GenerateTS(msgs)
	if err != nil {
		t.Fatal(err)
	}

	output := string(code)

	if !strings.Contains(output, "export interface LoginRequest") {
		t.Error("missing LoginRequest interface")
	}
	if !strings.Contains(output, "Username: string") {
		t.Error("missing Username field")
	}
	if !strings.Contains(output, "UserID: number") {
		t.Error("missing UserID field as number")
	}
	if !strings.Contains(output, "Success: boolean") {
		t.Error("missing Success field as boolean")
	}
	if !strings.Contains(output, "MessageIDs") {
		t.Error("missing MessageIDs const")
	}
}

func TestGenerateTSSDK(t *testing.T) {
	msgs, err := ParseFile("testdata/sample_messages.go")
	if err != nil {
		t.Fatal(err)
	}

	code, err := GenerateTSSDK(msgs)
	if err != nil {
		t.Fatal(err)
	}

	output := string(code)

	// 应包含消息接口
	if !strings.Contains(output, "export interface LoginRequest") {
		t.Error("missing LoginRequest interface")
	}
	// 应包含 MessageIDs
	if !strings.Contains(output, "MessageIDs") {
		t.Error("missing MessageIDs")
	}
	// 应包含 MessageMap 类型
	if !strings.Contains(output, "export interface MessageMap") {
		t.Error("missing MessageMap")
	}
	// 应包含 GameClient 类
	if !strings.Contains(output, "export class GameClient") {
		t.Error("missing GameClient class")
	}
	// 应包含连接方法
	if !strings.Contains(output, "connect(): Promise<void>") {
		t.Error("missing connect method")
	}
	// 应包含自动重连逻辑
	if !strings.Contains(output, "tryReconnect") {
		t.Error("missing reconnect logic")
	}
	// 应包含握手
	if !strings.Contains(output, "__handshake__") {
		t.Error("missing handshake support")
	}
	// 应使用 JSONName 作为字段名
	if !strings.Contains(output, "username: string") {
		t.Error("SDK should use json names for fields")
	}
}

func TestGenerateCSharp(t *testing.T) {
	msgs, err := ParseFile("testdata/sample_messages.go")
	if err != nil {
		t.Fatal(err)
	}

	code, err := GenerateCSharp(msgs, "MyGame.Protocol")
	if err != nil {
		t.Fatal(err)
	}

	output := string(code)

	if !strings.Contains(output, "namespace MyGame.Protocol") {
		t.Error("missing namespace")
	}
	if !strings.Contains(output, "public class LoginRequest") {
		t.Error("missing LoginRequest class")
	}
	if !strings.Contains(output, `[JsonProperty("username")]`) {
		t.Error("missing JsonProperty attribute")
	}
	if !strings.Contains(output, "public static class MessageIDs") {
		t.Error("missing MessageIDs class")
	}
	if !strings.Contains(output, "public static class MessageFactory") {
		t.Error("missing MessageFactory")
	}
	// C# 类型映射
	if !strings.Contains(output, "long") {
		t.Error("int64 should map to long in C#")
	}
}

func TestGenerateMarkdownDoc(t *testing.T) {
	msgs, err := ParseFile("testdata/sample_messages.go")
	if err != nil {
		t.Fatal(err)
	}

	code, err := GenerateMarkdownDoc(msgs)
	if err != nil {
		t.Fatal(err)
	}

	output := string(code)

	if !strings.Contains(output, "# Message API Reference") {
		t.Error("missing title")
	}
	if !strings.Contains(output, "LoginRequest") {
		t.Error("missing LoginRequest")
	}
	if !strings.Contains(output, "1001") {
		t.Error("missing message ID")
	}
	if !strings.Contains(output, "username") {
		t.Error("missing field JSON name")
	}
}

func TestGoTypeToTS(t *testing.T) {
	cases := map[string]string{
		"int":      "number",
		"int64":    "number",
		"float32":  "number",
		"string":   "string",
		"bool":     "boolean",
		"[]string": "string[]",
		"[]int":    "number[]",
		"Foo":      "any",
	}
	for goType, expected := range cases {
		got := goTypeToTS(goType)
		if got != expected {
			t.Errorf("goTypeToTS(%q) = %q, want %q", goType, got, expected)
		}
	}
}

func TestGoTypeToCSharp(t *testing.T) {
	cases := map[string]string{
		"int":            "int",
		"int64":          "long",
		"float32":        "float",
		"float64":        "double",
		"string":         "string",
		"bool":           "bool",
		"uint16":         "ushort",
		"[]string":       "List<string>",
		"[]int":          "List<int>",
		"map[string]int": "Dictionary<string, int>",
		"Foo":            "object",
	}
	for goType, expected := range cases {
		got := goTypeToCSharp(goType)
		if got != expected {
			t.Errorf("goTypeToCSharp(%q) = %q, want %q", goType, got, expected)
		}
	}
}

func TestExtractJSONName(t *testing.T) {
	cases := []struct {
		tag      string
		fallback string
		want     string
	}{
		{`json:"username"`, "Username", "username"},
		{`json:"user_id,omitempty"`, "UserID", "user_id"},
		{`json:"-"`, "Foo", "Foo"},
		{"", "Bar", "Bar"},
		{`bson:"test"`, "Baz", "Baz"},
	}
	for _, c := range cases {
		got := extractJSONName(c.tag, c.fallback)
		if got != c.want {
			t.Errorf("extractJSONName(%q, %q) = %q, want %q", c.tag, c.fallback, got, c.want)
		}
	}
}
