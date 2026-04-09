package codegen

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerateOpenAPIDoc(t *testing.T) {
	msgs := []MessageDef{
		{
			Name:    "LoginRequest",
			ID:      1001,
			Comment: "Login to server",
			Fields: []FieldDef{
				{Name: "Username", Type: "string", JSONName: "username"},
				{Name: "Token", Type: "string", JSONName: "token", Tag: `json:"token,omitempty"`},
			},
		},
		{
			Name:    "LoginResponse",
			ID:      1002,
			Comment: "Login response",
			Fields: []FieldDef{
				{Name: "Code", Type: "int", JSONName: "code"},
				{Name: "PlayerID", Type: "string", JSONName: "player_id"},
			},
		},
	}

	data, err := GenerateOpenAPIDoc(msgs, "TestGame", "1.0")
	if err != nil {
		t.Fatal(err)
	}

	var doc OpenAPIDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Info.Title != "TestGame" {
		t.Errorf("title = %q", doc.Info.Title)
	}
	if len(doc.Messages) != 2 {
		t.Errorf("messages count = %d", len(doc.Messages))
	}

	loginReq := doc.Messages["LoginRequest"]
	if loginReq.Direction != "C2S" {
		t.Errorf("LoginRequest direction = %q, want C2S", loginReq.Direction)
	}
	loginResp := doc.Messages["LoginResponse"]
	if loginResp.Direction != "S2C" {
		t.Errorf("LoginResponse direction = %q, want S2C", loginResp.Direction)
	}
}

func TestGenerateHTMLDoc(t *testing.T) {
	msgs := []MessageDef{
		{
			Name: "PingRequest", ID: 1,
			Fields: []FieldDef{{Name: "Seq", Type: "int", JSONName: "seq"}},
		},
		{
			Name: "PongResponse", ID: 2,
			Fields: []FieldDef{{Name: "Seq", Type: "int", JSONName: "seq"}},
		},
	}

	html, err := GenerateHTMLDoc(msgs, "Game Protocol", "1.0")
	if err != nil {
		t.Fatal(err)
	}

	content := string(html)
	if !strings.Contains(content, "Game Protocol") {
		t.Error("title not found in HTML")
	}
	if !strings.Contains(content, "PingRequest") {
		t.Error("PingRequest not in HTML")
	}
	if !strings.Contains(content, "[C2S]") {
		t.Error("direction marker not in HTML")
	}
}

func TestGenerateChangelog(t *testing.T) {
	oldManifest := &VersionManifest{
		ProtocolVersion: 1,
		Messages: []MessageVersion{
			{Name: "Msg1", Fields: []FieldDef{{Name: "A", Type: "int"}}},
			{Name: "Msg2", Fields: []FieldDef{{Name: "B", Type: "string"}}},
		},
	}
	newManifest := &VersionManifest{
		ProtocolVersion: 2,
		Messages: []MessageVersion{
			{Name: "Msg1", Fields: []FieldDef{
				{Name: "A", Type: "int64"}, // 类型变更
				{Name: "C", Type: "bool"},  // 新增字段
			}},
			// Msg2 删除
			{Name: "Msg3", Fields: []FieldDef{{Name: "D", Type: "float64"}}}, // 新增消息
		},
	}

	changelog, err := GenerateChangelog(oldManifest, newManifest)
	if err != nil {
		t.Fatal(err)
	}

	content := string(changelog)
	if !strings.Contains(content, "Msg3") {
		t.Error("added message Msg3 not in changelog")
	}
	if !strings.Contains(content, "~~`Msg2`~~") {
		t.Error("removed message Msg2 not in changelog")
	}
	if !strings.Contains(content, "int64") {
		t.Error("field type change not in changelog")
	}
	if !strings.Contains(content, "Added") {
		t.Error("added field not in changelog")
	}
}

func TestMockResponse(t *testing.T) {
	msg := MessageDef{
		Name: "LoginResponse",
		Fields: []FieldDef{
			{Name: "Code", Type: "int", JSONName: "code"},
			{Name: "Name", Type: "string", JSONName: "name"},
			{Name: "OK", Type: "bool", JSONName: "ok"},
			{Name: "Items", Type: "[]string", JSONName: "items"},
		},
	}

	mock := MockResponse(msg)
	if mock["code"] != 0 {
		t.Errorf("code = %v", mock["code"])
	}
	if mock["name"] != "mock_string" {
		t.Errorf("name = %v", mock["name"])
	}
	if mock["ok"] != false {
		t.Errorf("ok = %v", mock["ok"])
	}
	if items, ok := mock["items"].([]interface{}); !ok || len(items) != 1 {
		t.Errorf("items = %v", mock["items"])
	}
}

func TestGenerateMockServer(t *testing.T) {
	msgs := []MessageDef{
		{Name: "LoginRequest", ID: 1, Fields: []FieldDef{{Name: "User", Type: "string", JSONName: "user"}}},
		{Name: "LoginResponse", ID: 2, Fields: []FieldDef{{Name: "Code", Type: "int", JSONName: "code"}}},
	}

	code, err := GenerateMockServer(msgs, "mock")
	if err != nil {
		t.Fatal(err)
	}

	content := string(code)
	if !strings.Contains(content, "loginresponse") {
		t.Error("mock route for LoginResponse not generated")
	}
	// LoginRequest 不应生成 Mock（非 Response）
	if strings.Contains(content, "loginrequest") {
		t.Error("LoginRequest should not have mock handler")
	}
}

func TestInferDirection(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"LoginRequest", "C2S"},
		{"LoginResponse", "S2C"},
		{"ChatNotify", "S2C"},
		{"SystemEvent", "S2C"},
		{"Heartbeat", "Both"},
	}
	for _, tt := range tests {
		if got := inferDirection(tt.name); got != tt.want {
			t.Errorf("inferDirection(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
