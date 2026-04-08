package codegen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseProtoFile(t *testing.T) {
	content := `syntax = "proto3";

package test;
option go_package = "test/proto";

// PID Actor 进程标识
message PID {
  string address = 1;
  string id = 2;
}

enum Status {
  UNKNOWN = 0;
  ACTIVE = 1;
}

// LoginRequest 登录请求消息
message LoginRequest {
  string username = 1;  // 用户名
  string password = 2;  // 密码
  int32 version = 3;
}

// PlayerInfo 玩家信息
message PlayerInfo {
  int64 player_id = 1;
  string name = 2;
  repeated string items = 3;
  bool is_online = 4;
  double score = 5;
  map<string, int32> attributes = 6;
}
`
	dir := t.TempDir()
	protoFile := filepath.Join(dir, "test.proto")
	if err := os.WriteFile(protoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	msgs, err := ParseProtoFile(protoFile)
	if err != nil {
		t.Fatalf("ParseProtoFile failed: %v", err)
	}

	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// PID
	pid := msgs[0]
	if pid.Name != "PID" {
		t.Errorf("expected PID, got %s", pid.Name)
	}
	if pid.ID != 2001 {
		t.Errorf("expected ID 2001, got %d", pid.ID)
	}
	if len(pid.Fields) != 2 {
		t.Errorf("PID: expected 2 fields, got %d", len(pid.Fields))
	}
	if pid.Comment != "PID Actor 进程标识" {
		t.Errorf("PID: unexpected comment: %s", pid.Comment)
	}

	// LoginRequest
	login := msgs[1]
	if login.Name != "LoginRequest" {
		t.Errorf("expected LoginRequest, got %s", login.Name)
	}
	if len(login.Fields) != 3 {
		t.Errorf("LoginRequest: expected 3 fields, got %d", len(login.Fields))
	}
	if login.Fields[0].Name != "Username" || login.Fields[0].Type != "string" {
		t.Errorf("field 0: got %s %s", login.Fields[0].Name, login.Fields[0].Type)
	}
	if login.Fields[0].JSONName != "username" {
		t.Errorf("field 0 json name: got %s", login.Fields[0].JSONName)
	}
	if login.Fields[0].Comment != "用户名" {
		t.Errorf("field 0 comment: got %q", login.Fields[0].Comment)
	}

	// PlayerInfo
	player := msgs[2]
	if player.Name != "PlayerInfo" {
		t.Errorf("expected PlayerInfo, got %s", player.Name)
	}
	if len(player.Fields) != 6 {
		t.Fatalf("PlayerInfo: expected 6 fields, got %d", len(player.Fields))
	}
	// player_id -> PlayerID (PascalCase)
	if player.Fields[0].Name != "PlayerID" || player.Fields[0].Type != "int64" {
		t.Errorf("field 0: got %s %s", player.Fields[0].Name, player.Fields[0].Type)
	}
	// repeated string -> []string
	if player.Fields[2].Type != "[]string" {
		t.Errorf("field 2 (items): expected []string, got %s", player.Fields[2].Type)
	}
	// double -> float64
	if player.Fields[4].Type != "float64" {
		t.Errorf("field 4 (score): expected float64, got %s", player.Fields[4].Type)
	}
	// map<string, int32> -> map[string]int32
	if player.Fields[5].Type != "map[string]int32" {
		t.Errorf("field 5 (attributes): expected map[string]int32, got %s", player.Fields[5].Type)
	}
}

func TestParseProtoFileEmpty(t *testing.T) {
	content := `syntax = "proto3";
package empty;
`
	dir := t.TempDir()
	protoFile := filepath.Join(dir, "empty.proto")
	if err := os.WriteFile(protoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	msgs, err := ParseProtoFile(protoFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestParseProtoFileMalformed(t *testing.T) {
	content := `syntax = "proto3";
message Incomplete {
  string name = 1;
`
	dir := t.TempDir()
	protoFile := filepath.Join(dir, "bad.proto")
	if err := os.WriteFile(protoFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseProtoFile(protoFile)
	if err == nil {
		t.Fatal("expected error for malformed proto file")
	}
}

func TestProtoTypeMapping(t *testing.T) {
	cases := []struct {
		proto    string
		repeated bool
		want     string
	}{
		{"double", false, "float64"},
		{"float", false, "float32"},
		{"int32", false, "int32"},
		{"int64", false, "int64"},
		{"uint32", false, "uint32"},
		{"uint64", false, "uint64"},
		{"bool", false, "bool"},
		{"string", false, "string"},
		{"bytes", false, "[]byte"},
		{"string", true, "[]string"},
		{"int32", true, "[]int32"},
		{"MyMessage", false, "MyMessage"},
	}
	for _, c := range cases {
		got := protoTypeToGoType(c.proto, c.repeated)
		if got != c.want {
			t.Errorf("protoTypeToGoType(%q, %v) = %q, want %q", c.proto, c.repeated, got, c.want)
		}
	}
}

func TestProtoNameToGoName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"player_id", "PlayerID"},
		{"username", "Username"},
		{"is_online", "IsOnline"},
		{"msg_type", "MsgType"},
		{"api_key", "APIKey"},
	}
	for _, c := range cases {
		got := protoNameToGoName(c.input)
		if got != c.want {
			t.Errorf("protoNameToGoName(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
