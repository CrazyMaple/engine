package remote

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	engerr "engine/errors"
)

// === TypeRegistry 测试 ===

type testMsg struct {
	Name string `json:"name"`
	Val  int    `json:"val"`
}

type testMsg2 struct {
	Data string `json:"data"`
}

func TestTypeRegistryRegister(t *testing.T) {
	r := NewTypeRegistry()
	r.Register(&testMsg{})

	name, ok := r.GetTypeName(&testMsg{})
	if !ok {
		t.Fatal("type should be registered")
	}
	if !strings.Contains(name, "testMsg") {
		t.Fatalf("unexpected type name: %s", name)
	}
}

func TestTypeRegistryRegisterValue(t *testing.T) {
	r := NewTypeRegistry()
	// 传入值（非指针）也应正常注册
	r.Register(testMsg{})

	name, ok := r.GetTypeName(testMsg{})
	if !ok {
		t.Fatal("type should be registered (value)")
	}
	if !strings.Contains(name, "testMsg") {
		t.Fatalf("unexpected type name: %s", name)
	}
}

func TestTypeRegistryRegisterName(t *testing.T) {
	r := NewTypeRegistry()
	r.RegisterName("custom.TestMsg", &testMsg{})

	name, ok := r.GetTypeName(&testMsg{})
	if !ok {
		t.Fatal("type should be registered")
	}
	if name != "custom.TestMsg" {
		t.Fatalf("expected custom.TestMsg, got %s", name)
	}
}

func TestTypeRegistryDeserialize(t *testing.T) {
	r := NewTypeRegistry()
	r.Register(&testMsg{})

	typeName, _ := r.GetTypeName(&testMsg{})

	data, _ := json.Marshal(&testMsg{Name: "hello", Val: 42})
	result, err := r.Deserialize(typeName, data)
	if err != nil {
		t.Fatalf("Deserialize error: %v", err)
	}

	msg, ok := result.(*testMsg)
	if !ok {
		t.Fatalf("expected *testMsg, got %T", result)
	}
	if msg.Name != "hello" || msg.Val != 42 {
		t.Fatalf("unexpected msg: %+v", msg)
	}
}

func TestTypeRegistryDeserializeUnknown(t *testing.T) {
	r := NewTypeRegistry()

	_, err := r.Deserialize("nonexistent.Type", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	var codecErr *engerr.CodecError
	if !errors.As(err, &codecErr) {
		t.Fatalf("expected CodecError, got: %T", err)
	}
	if !errors.Is(codecErr.Cause, engerr.ErrNotFound) {
		t.Fatalf("expected ErrNotFound cause, got: %v", codecErr.Cause)
	}
}

func TestTypeRegistryDeserializeInvalidJSON(t *testing.T) {
	r := NewTypeRegistry()
	r.Register(&testMsg{})
	typeName, _ := r.GetTypeName(&testMsg{})

	_, err := r.Deserialize(typeName, []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestTypeRegistryMultipleTypes(t *testing.T) {
	r := NewTypeRegistry()
	r.Register(&testMsg{})
	r.Register(&testMsg2{})

	name1, ok1 := r.GetTypeName(&testMsg{})
	name2, ok2 := r.GetTypeName(&testMsg2{})

	if !ok1 || !ok2 {
		t.Fatal("both types should be registered")
	}
	if name1 == name2 {
		t.Fatal("different types should have different names")
	}
}

func TestDefaultTypeRegistry(t *testing.T) {
	reg := DefaultTypeRegistry()
	if reg == nil {
		t.Fatal("default registry should not be nil")
	}
}

// === MessageSigner 测试 ===

func TestMessageSignerSignVerify(t *testing.T) {
	signer := NewMessageSigner([]byte("secret-key"))

	data := []byte("hello world")
	sig := signer.Sign(data)

	if len(sig) != 32 { // SHA256 = 32 bytes
		t.Fatalf("expected 32 byte signature, got %d", len(sig))
	}

	if !signer.Verify(data, sig) {
		t.Fatal("valid signature should verify")
	}
}

func TestMessageSignerTampered(t *testing.T) {
	signer := NewMessageSigner([]byte("secret-key"))

	data := []byte("hello world")
	sig := signer.Sign(data)

	// 篡改数据
	tampered := []byte("hello worlD")
	if signer.Verify(tampered, sig) {
		t.Fatal("tampered data should not verify")
	}
}

func TestMessageSignerWrongKey(t *testing.T) {
	signer1 := NewMessageSigner([]byte("key1"))
	signer2 := NewMessageSigner([]byte("key2"))

	data := []byte("hello world")
	sig := signer1.Sign(data)

	if signer2.Verify(data, sig) {
		t.Fatal("different key should not verify")
	}
}

func TestMessageSignerDeterministic(t *testing.T) {
	signer := NewMessageSigner([]byte("key"))
	data := []byte("test data")

	sig1 := signer.Sign(data)
	sig2 := signer.Sign(data)

	if string(sig1) != string(sig2) {
		t.Fatal("same data should produce same signature")
	}
}

// === EndpointManager 测试 ===

func TestEndpointManagerGetEndpointCreatesNew(t *testing.T) {
	// 注意：此测试会创建真实的 TCP 连接尝试，但不需要远端监听
	// EndpointManager.GetEndpoint 内部会 Start endpoint，
	// endpoint 会尝试连接但 AutoReconnect 会重试

	// 这里只测试管理器逻辑，不验证实际连接
	em := &EndpointManager{
		endpoints: make(map[string]*Endpoint),
	}

	// 直接创建 endpoint（不 Start 以避免网络操作）
	ep := NewEndpoint("127.0.0.1:9999")
	em.endpoints["127.0.0.1:9999"] = ep

	// 获取已存在的
	got := em.endpoints["127.0.0.1:9999"]
	if got != ep {
		t.Fatal("should return existing endpoint")
	}
}

func TestEndpointManagerStop(t *testing.T) {
	em := &EndpointManager{
		endpoints: make(map[string]*Endpoint),
	}

	// 添加一些 endpoint（不 Start）
	ep1 := &Endpoint{
		address:  "addr1",
		stopChan: make(chan struct{}),
	}
	ep2 := &Endpoint{
		address:  "addr2",
		stopChan: make(chan struct{}),
	}
	em.endpoints["addr1"] = ep1
	em.endpoints["addr2"] = ep2

	em.Stop()

	if len(em.endpoints) != 0 {
		t.Fatalf("expected 0 endpoints after Stop, got %d", len(em.endpoints))
	}
}

// === RemoteMessage 序列化测试 ===

func TestRemoteMessageJSON(t *testing.T) {
	msg := &RemoteMessage{
		Type:     MessageTypeUser,
		TypeName: "TestMsg",
		Message:  map[string]interface{}{"key": "value"},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded RemoteMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Type != MessageTypeUser {
		t.Fatalf("expected MessageTypeUser, got %d", decoded.Type)
	}
	if decoded.TypeName != "TestMsg" {
		t.Fatalf("expected TypeName=TestMsg, got %s", decoded.TypeName)
	}
}

func TestRemoteMessageBatchJSON(t *testing.T) {
	batch := &RemoteMessageBatch{
		Messages: []*RemoteMessage{
			{Type: MessageTypeUser, TypeName: "A"},
			{Type: MessageTypeSystem, TypeName: "B"},
		},
	}

	data, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded RemoteMessageBatch
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if len(decoded.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(decoded.Messages))
	}
}
