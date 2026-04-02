package codec

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	engerr "engine/errors"
)

// 测试用消息类型
type TestLogin struct {
	Type     string `json:"type"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type TestMove struct {
	Type string  `json:"type"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

// === JSONCodec 测试 ===

func TestJSONCodecRegister(t *testing.T) {
	c := NewJSONCodec()
	c.Register(&TestLogin{})

	// 注册的类型名应为 "TestLogin"
	if _, ok := c.msgInfo["TestLogin"]; !ok {
		t.Fatal("TestLogin should be registered")
	}
}

func TestJSONCodecRegisterPanicOnNonPointer(t *testing.T) {
	c := NewJSONCodec()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register non-pointer should panic")
		}
	}()
	c.Register(TestLogin{})
}

func TestJSONCodecEncode(t *testing.T) {
	c := NewJSONCodec()
	msg := &TestLogin{Type: "TestLogin", Username: "alice", Password: "123"}

	data, err := c.Encode(msg)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("encoded data is not valid JSON: %v", err)
	}
	if m["username"] != "alice" {
		t.Fatalf("expected username=alice, got %v", m["username"])
	}
}

func TestJSONCodecDecodeSuccess(t *testing.T) {
	c := NewJSONCodec()
	c.Register(&TestLogin{})

	input := `{"type":"TestLogin","username":"bob","password":"456"}`
	msg, err := c.Decode([]byte(input))
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	login, ok := msg.(*TestLogin)
	if !ok {
		t.Fatalf("expected *TestLogin, got %T", msg)
	}
	if login.Username != "bob" {
		t.Fatalf("expected username=bob, got %s", login.Username)
	}
}

func TestJSONCodecDecodeUnregisteredType(t *testing.T) {
	c := NewJSONCodec()
	// 不注册 TestLogin

	input := `{"type":"TestLogin","username":"bob"}`
	_, err := c.Decode([]byte(input))
	if err == nil {
		t.Fatal("expected error for unregistered type")
	}
	var codecErr *engerr.CodecError
	if !errors.As(err, &codecErr) {
		t.Fatalf("expected CodecError, got: %T", err)
	}
	if codecErr.TypeName != "TestLogin" {
		t.Fatalf("expected TypeName=TestLogin, got %s", codecErr.TypeName)
	}
}

func TestJSONCodecDecodeInvalidJSON(t *testing.T) {
	c := NewJSONCodec()
	_, err := c.Decode([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestJSONCodecDecodeMissingType(t *testing.T) {
	c := NewJSONCodec()
	input := `{"username":"bob"}`
	_, err := c.Decode([]byte(input))
	if err == nil {
		t.Fatal("expected error for missing type field")
	}
	var codecErr *engerr.CodecError
	if !errors.As(err, &codecErr) {
		t.Fatalf("expected CodecError, got: %T", err)
	}
}

func TestJSONCodecMultipleTypes(t *testing.T) {
	c := NewJSONCodec()
	c.Register(&TestLogin{})
	c.Register(&TestMove{})

	// 解码 TestMove
	input := `{"type":"TestMove","x":1.5,"y":2.5}`
	msg, err := c.Decode([]byte(input))
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	move, ok := msg.(*TestMove)
	if !ok {
		t.Fatalf("expected *TestMove, got %T", msg)
	}
	if move.X != 1.5 || move.Y != 2.5 {
		t.Fatalf("expected (1.5, 2.5), got (%v, %v)", move.X, move.Y)
	}
}

// === SimpleProcessor 测试 ===

func TestSimpleProcessorMarshalUnmarshal(t *testing.T) {
	c := NewJSONCodec()
	c.Register(&TestLogin{})
	p := NewSimpleProcessor(c)

	msg := &TestLogin{Type: "TestLogin", Username: "alice", Password: "123"}

	// Marshal
	data, err := p.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(data))
	}

	// Unmarshal
	decoded, err := p.Unmarshal(data[0])
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	login, ok := decoded.(*TestLogin)
	if !ok {
		t.Fatalf("expected *TestLogin, got %T", decoded)
	}
	if login.Username != "alice" {
		t.Fatalf("expected username=alice, got %s", login.Username)
	}
}

func TestSimpleProcessorRoute(t *testing.T) {
	c := NewJSONCodec()
	c.Register(&TestLogin{})
	p := NewSimpleProcessor(c)

	var routed bool
	p.Register(&TestLogin{}, func(msg *TestLogin, agent interface{}) {
		routed = true
		if msg.Username != "test" {
			panic("unexpected username")
		}
	})

	msg := &TestLogin{Type: "TestLogin", Username: "test"}
	err := p.Route(msg, "dummy-agent")
	if err != nil {
		t.Fatalf("Route error: %v", err)
	}
	if !routed {
		t.Fatal("handler should have been called")
	}
}

func TestSimpleProcessorRouteUnregistered(t *testing.T) {
	c := NewJSONCodec()
	p := NewSimpleProcessor(c)

	msg := &TestLogin{Type: "TestLogin"}
	err := p.Route(msg, nil)
	if err == nil {
		t.Fatal("expected error for unregistered handler")
	}
	if !strings.Contains(err.Error(), "handler not found") {
		t.Fatalf("expected 'handler not found' error, got: %v", err)
	}
}

func TestSimpleProcessorRegisterPanics(t *testing.T) {
	c := NewJSONCodec()
	p := NewSimpleProcessor(c)

	// 非指针消息应 panic
	t.Run("non_pointer_msg", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("should panic on non-pointer message")
			}
		}()
		p.Register(TestLogin{}, func(msg TestLogin, agent interface{}) {})
	})

	// 非函数 handler 应 panic
	t.Run("non_func_handler", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("should panic on non-function handler")
			}
		}()
		p.Register(&TestLogin{}, "not a function")
	})
}
