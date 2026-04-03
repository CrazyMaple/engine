package gate

import (
	"encoding/json"
	"testing"
)

func TestNegotiateSuccess(t *testing.T) {
	vn := NewVersionNegotiator(1, 3, "engine-1.5.0")
	req := &HandshakeRequest{
		Type:              "__handshake__",
		ProtocolVersion:   2,
		ClientSDK:         "ts-1.0.0",
		SupportedVersions: []int{1, 2},
	}

	resp := vn.Negotiate(req)
	if resp.Status != "ok" {
		t.Fatalf("expected ok, got %s: %s", resp.Status, resp.Message)
	}
	if resp.ProtocolVersion != 2 {
		t.Errorf("expected version 2, got %d", resp.ProtocolVersion)
	}
	if resp.ServerVersion != "engine-1.5.0" {
		t.Errorf("expected server version engine-1.5.0, got %s", resp.ServerVersion)
	}
}

func TestNegotiatePicksHighest(t *testing.T) {
	vn := NewVersionNegotiator(1, 5, "v1.5")
	req := &HandshakeRequest{
		SupportedVersions: []int{1, 3, 5, 7},
	}
	resp := vn.Negotiate(req)
	if resp.Status != "ok" {
		t.Fatalf("expected ok, got %s", resp.Status)
	}
	if resp.ProtocolVersion != 5 {
		t.Errorf("expected version 5, got %d", resp.ProtocolVersion)
	}
}

func TestNegotiateFallbackToProtocolVersion(t *testing.T) {
	vn := NewVersionNegotiator(1, 3, "v1.5")
	req := &HandshakeRequest{
		ProtocolVersion: 2,
		// SupportedVersions 为空
	}
	resp := vn.Negotiate(req)
	if resp.Status != "ok" {
		t.Fatalf("expected ok, got %s", resp.Status)
	}
	if resp.ProtocolVersion != 2 {
		t.Errorf("expected version 2, got %d", resp.ProtocolVersion)
	}
}

func TestNegotiateVersionMismatch(t *testing.T) {
	vn := NewVersionNegotiator(3, 5, "v1.5")
	req := &HandshakeRequest{
		ProtocolVersion:   1,
		SupportedVersions: []int{1, 2},
	}
	resp := vn.Negotiate(req)
	if resp.Status != "version_mismatch" {
		t.Fatalf("expected version_mismatch, got %s", resp.Status)
	}
}

func TestIsHandshakeRequest(t *testing.T) {
	// 有效握手
	data, _ := json.Marshal(HandshakeRequest{Type: "__handshake__", ProtocolVersion: 1})
	if !isHandshakeRequest(data) {
		t.Error("should detect handshake request")
	}

	// 非握手消息
	data2 := []byte(`{"type":"LoginRequest","username":"test"}`)
	if isHandshakeRequest(data2) {
		t.Error("should not detect non-handshake as handshake")
	}

	// 太短
	if isHandshakeRequest([]byte("{}")) {
		t.Error("should not detect short data as handshake")
	}
}

func TestHandshakeResponseJSON(t *testing.T) {
	resp := HandshakeResponse{
		Type:            "__handshake_ack__",
		ProtocolVersion: 2,
		ServerVersion:   "v1.5",
		Status:          "ok",
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var parsed HandshakeResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.ProtocolVersion != 2 {
		t.Errorf("expected version 2, got %d", parsed.ProtocolVersion)
	}
	if parsed.Status != "ok" {
		t.Errorf("expected ok, got %s", parsed.Status)
	}
}
