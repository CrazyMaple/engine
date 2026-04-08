package remote

import (
	"encoding/json"
	"testing"

	"engine/actor"
)

func TestDefaultRemoteCodecMarshalEnvelope(t *testing.T) {
	rc := DefaultRemoteCodec()
	if rc.CodecType() != "json" {
		t.Fatalf("expected json, got %s", rc.CodecType())
	}

	msg := &RemoteMessage{
		Target:   &actor.PID{Address: "localhost:8080", Id: "test"},
		Type:     MessageTypeUser,
		TypeName: "TestMsg",
		Message:  map[string]interface{}{"key": "value"},
	}

	data, err := rc.MarshalEnvelope(msg)
	if err != nil {
		t.Fatalf("MarshalEnvelope error: %v", err)
	}

	// 应该能用标准 JSON 解码
	var decoded RemoteMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}
	if decoded.TypeName != "TestMsg" {
		t.Fatalf("expected TestMsg, got %s", decoded.TypeName)
	}
}

func TestDefaultRemoteCodecUnmarshalSingle(t *testing.T) {
	rc := DefaultRemoteCodec()

	msg := &RemoteMessage{
		Type:     MessageTypeUser,
		TypeName: "TestMsg",
		Message:  "hello",
	}
	data, _ := json.Marshal(msg)

	isBatch, _, single, err := rc.UnmarshalEnvelope(data)
	if err != nil {
		t.Fatalf("UnmarshalEnvelope error: %v", err)
	}
	if isBatch {
		t.Fatal("expected single message, got batch")
	}
	if single.TypeName != "TestMsg" {
		t.Fatalf("expected TestMsg, got %s", single.TypeName)
	}
}

func TestDefaultRemoteCodecUnmarshalBatch(t *testing.T) {
	rc := DefaultRemoteCodec()

	batch := &RemoteMessageBatch{
		Messages: []*RemoteMessage{
			{Type: MessageTypeUser, TypeName: "A"},
			{Type: MessageTypeSystem, TypeName: "B"},
		},
	}
	data, _ := json.Marshal(batch)

	isBatch, batchMsg, _, err := rc.UnmarshalEnvelope(data)
	if err != nil {
		t.Fatalf("UnmarshalEnvelope error: %v", err)
	}
	if !isBatch {
		t.Fatal("expected batch, got single")
	}
	if len(batchMsg.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(batchMsg.Messages))
	}
}

func TestDefaultRemoteCodecMarshalPayload(t *testing.T) {
	rc := DefaultRemoteCodec()

	payload := map[string]interface{}{"name": "test", "val": float64(42)}
	data, err := rc.MarshalPayload(payload)
	if err != nil {
		t.Fatalf("MarshalPayload error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}
	if decoded["name"] != "test" {
		t.Fatalf("expected name=test, got %v", decoded["name"])
	}
}

func TestDefaultRemoteCodecUnmarshalPayload(t *testing.T) {
	rc := DefaultRemoteCodec()
	registry := NewTypeRegistry()
	registry.Register(&testMsg{})
	typeName, _ := registry.GetTypeName(&testMsg{})

	data, _ := json.Marshal(&testMsg{Name: "hello", Val: 42})
	result, err := rc.UnmarshalPayload(typeName, data, registry)
	if err != nil {
		t.Fatalf("UnmarshalPayload error: %v", err)
	}

	msg, ok := result.(*testMsg)
	if !ok {
		t.Fatalf("expected *testMsg, got %T", result)
	}
	if msg.Name != "hello" || msg.Val != 42 {
		t.Fatalf("unexpected msg: %+v", msg)
	}
}

func TestRemoteCodecWithCustomCodec(t *testing.T) {
	// 使用一个简单的 mock codec 测试非 JSON 路径
	mock := &mockCodec{
		encodeFunc: func(msg interface{}) ([]byte, error) {
			return json.Marshal(msg)
		},
		decodeFunc: func(data []byte) (interface{}, error) {
			var msg RemoteMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				return nil, err
			}
			return &msg, nil
		},
	}

	rc := NewRemoteCodec(mock, "mock")
	if rc.CodecType() != "mock" {
		t.Fatalf("expected mock, got %s", rc.CodecType())
	}
	if rc.InnerCodec() != mock {
		t.Fatal("InnerCodec should return the mock codec")
	}

	msg := &RemoteMessage{Type: MessageTypeUser, TypeName: "Test"}
	data, err := rc.MarshalEnvelope(msg)
	if err != nil {
		t.Fatalf("MarshalEnvelope error: %v", err)
	}

	isBatch, _, single, err := rc.UnmarshalEnvelope(data)
	if err != nil {
		t.Fatalf("UnmarshalEnvelope error: %v", err)
	}
	if isBatch {
		t.Fatal("expected single")
	}
	if single.TypeName != "Test" {
		t.Fatalf("expected Test, got %s", single.TypeName)
	}
}

func TestRemoteCodecUnmarshalInvalidData(t *testing.T) {
	rc := DefaultRemoteCodec()

	_, _, _, err := rc.UnmarshalEnvelope([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid data")
	}
}

// mockCodec 用于测试的 mock Codec 实现
type mockCodec struct {
	encodeFunc func(msg interface{}) ([]byte, error)
	decodeFunc func(data []byte) (interface{}, error)
}

func (m *mockCodec) Encode(msg interface{}) ([]byte, error) {
	return m.encodeFunc(msg)
}

func (m *mockCodec) Decode(data []byte) (interface{}, error) {
	return m.decodeFunc(data)
}
