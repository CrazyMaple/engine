package log

import (
	"bytes"
	"strings"
	"testing"
)

func TestLogLevels(t *testing.T) {
	var buf bytes.Buffer
	old := GetLogger()
	oldLevel := GetLevel()
	defer func() {
		SetLogger(old)
		SetLevel(oldLevel)
	}()

	SetLogger(NewTextLogger(&buf))
	SetLevel(LevelDebug)

	tests := []struct {
		name   string
		fn     func(string, ...interface{})
		format string
		args   []interface{}
		prefix string
	}{
		{"Debug", Debug, "test %d", []interface{}{1}, "[DEBUG]"},
		{"Info", Info, "test %d", []interface{}{2}, "[INFO]"},
		{"Warn", Warn, "test %d", []interface{}{3}, "[WARN]"},
		{"Error", Error, "test %d", []interface{}{4}, "[ERROR]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			tt.fn(tt.format, tt.args...)
			output := buf.String()

			if !strings.Contains(output, tt.prefix) {
				t.Errorf("output %q should contain prefix %q", output, tt.prefix)
			}
		})
	}
}
