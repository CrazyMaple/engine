package log

import (
	"bytes"
	"strings"
	"testing"
)

type testLogger struct {
	buf  bytes.Buffer
	kvs  []interface{}
}

func (l *testLogger) Debug(msg string, kvs ...interface{}) { l.kvs = kvs }
func (l *testLogger) Info(msg string, kvs ...interface{})  { l.kvs = kvs }
func (l *testLogger) Warn(msg string, kvs ...interface{})  { l.kvs = kvs }
func (l *testLogger) Error(msg string, kvs ...interface{}) { l.kvs = kvs }
func (l *testLogger) With(kvs ...interface{}) Logger       { return l }

func TestRedactingLoggerAutoRedact(t *testing.T) {
	inner := &testLogger{}
	rl := NewRedactingLogger(inner)

	rl.Info("login", "user", "alice", "password", "secret123")

	if len(inner.kvs) != 4 {
		t.Fatalf("expected 4 kvs, got %d", len(inner.kvs))
	}
	// user 不应被脱敏
	if inner.kvs[1] != "alice" {
		t.Errorf("user value should not be redacted, got %v", inner.kvs[1])
	}
	// password 应被脱敏
	if inner.kvs[3] != redactedPlaceholder {
		t.Errorf("password should be redacted, got %v", inner.kvs[3])
	}
}

func TestRedactingLoggerExplicitRedact(t *testing.T) {
	inner := &testLogger{}
	rl := NewRedactingLogger(inner)

	rl.Info("data", "custom_field", Redact("sensitive"))

	if inner.kvs[1] != redactedPlaceholder {
		t.Errorf("Redacted value should be masked, got %v", inner.kvs[1])
	}
}

func TestRedactedString(t *testing.T) {
	r := Redact("my-secret")
	s := r.String()
	if !strings.Contains(s, "REDACTED") {
		t.Errorf("Redacted.String() = %q, want contains REDACTED", s)
	}
	// 原始值仍可访问
	if r.Value != "my-secret" {
		t.Errorf("Redacted.Value = %v, want my-secret", r.Value)
	}
}
