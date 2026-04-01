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
