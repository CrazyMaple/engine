package log

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestLevelString(t *testing.T) {
	tests := []struct {
		level Level
		want  string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("Level(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  Level
		err   bool
	}{
		{"debug", LevelDebug, false},
		{"DEBUG", LevelDebug, false},
		{"info", LevelInfo, false},
		{"warn", LevelWarn, false},
		{"warning", LevelWarn, false},
		{"error", LevelError, false},
		{"unknown", LevelDebug, true},
	}
	for _, tt := range tests {
		got, err := ParseLevel(tt.input)
		if (err != nil) != tt.err {
			t.Errorf("ParseLevel(%q) error = %v, wantErr %v", tt.input, err, tt.err)
			continue
		}
		if !tt.err && got != tt.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	old := GetLogger()
	oldLevel := GetLevel()
	defer func() {
		SetLogger(old)
		SetLevel(oldLevel)
	}()

	SetLogger(NewTextLogger(&buf))
	SetLevel(LevelWarn)

	Debug("should not appear")
	Info("should not appear either")
	Warn("warning message")
	Error("error message")

	output := buf.String()
	if strings.Contains(output, "should not appear") {
		t.Error("Debug/Info messages should be filtered out at Warn level")
	}
	if !strings.Contains(output, "warning message") {
		t.Error("Warn message should appear")
	}
	if !strings.Contains(output, "error message") {
		t.Error("Error message should appear")
	}
}

func TestTextLoggerFormat(t *testing.T) {
	var buf bytes.Buffer
	old := GetLogger()
	oldLevel := GetLevel()
	defer func() {
		SetLogger(old)
		SetLevel(oldLevel)
	}()

	SetLogger(NewTextLogger(&buf))
	SetLevel(LevelDebug)

	L().Info("hello world", "key1", "val1", "key2", 42)

	output := buf.String()
	if !strings.Contains(output, "[INFO]") {
		t.Error("should contain [INFO] prefix")
	}
	if !strings.Contains(output, "hello world") {
		t.Error("should contain message")
	}
	if !strings.Contains(output, "key1=val1") {
		t.Error("should contain key1=val1")
	}
	if !strings.Contains(output, "key2=42") {
		t.Error("should contain key2=42")
	}
}

func TestJSONLoggerFormat(t *testing.T) {
	var buf bytes.Buffer
	old := GetLogger()
	oldLevel := GetLevel()
	defer func() {
		SetLogger(old)
		SetLevel(oldLevel)
	}()

	SetLogger(NewJSONLogger(&buf))
	SetLevel(LevelDebug)

	L().Info("json test", "component", "actor", "count", 5)

	var m map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("failed to parse JSON output: %v, output: %s", err, buf.String())
	}

	if m["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", m["level"])
	}
	if m["msg"] != "json test" {
		t.Errorf("msg = %v, want 'json test'", m["msg"])
	}
	if m["component"] != "actor" {
		t.Errorf("component = %v, want 'actor'", m["component"])
	}
	if m["time"] == nil {
		t.Error("time field should be present")
	}
}

func TestLoggerWith(t *testing.T) {
	var buf bytes.Buffer
	old := GetLogger()
	oldLevel := GetLevel()
	defer func() {
		SetLogger(old)
		SetLevel(oldLevel)
	}()

	SetLogger(NewTextLogger(&buf))
	SetLevel(LevelDebug)

	child := L().With("module", "remote")
	child.Info("connected", "addr", "127.0.0.1:8080")

	output := buf.String()
	if !strings.Contains(output, "module=remote") {
		t.Error("should contain preset field module=remote")
	}
	if !strings.Contains(output, "addr=127.0.0.1:8080") {
		t.Error("should contain call-site field addr=127.0.0.1:8080")
	}
}

func TestBackwardCompatibility(t *testing.T) {
	var buf bytes.Buffer
	old := GetLogger()
	oldLevel := GetLevel()
	defer func() {
		SetLogger(old)
		SetLevel(oldLevel)
	}()

	SetLogger(NewTextLogger(&buf))
	SetLevel(LevelDebug)

	// 使用旧风格的 Printf 格式调用
	Debug("test %d", 1)
	Info("test %d", 2)
	Warn("test %d", 3)
	Error("test %d", 4)

	output := buf.String()
	if !strings.Contains(output, "[DEBUG] test 1") {
		t.Errorf("Debug backward compat failed, output: %s", output)
	}
	if !strings.Contains(output, "[INFO] test 2") {
		t.Errorf("Info backward compat failed, output: %s", output)
	}
	if !strings.Contains(output, "[WARN] test 3") {
		t.Errorf("Warn backward compat failed, output: %s", output)
	}
	if !strings.Contains(output, "[ERROR] test 4") {
		t.Errorf("Error backward compat failed, output: %s", output)
	}
}

func TestPrint(t *testing.T) {
	// Print uses fmt.Println, just verify no panic
	Print("hello", "world")
}
