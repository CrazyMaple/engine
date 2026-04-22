package log

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// jsonLogger JSON 格式日志实现，每行输出一个 JSON 对象
// 格式：{"time":"2024-01-01T12:00:00.000Z","level":"INFO","msg":"...","key":"val"}
type jsonLogger struct {
	w      io.Writer
	mu     sync.Mutex
	prefix []interface{} // 预设的 key-value 字段
}

// NewJSONLogger 创建 JSON 格式 Logger
func NewJSONLogger(w io.Writer) Logger {
	return &jsonLogger{w: w}
}

func (l *jsonLogger) Debug(msg string, kvs ...interface{}) {
	if !Enabled(LevelDebug) {
		return
	}
	l.log(LevelDebug, msg, kvs)
}

func (l *jsonLogger) Info(msg string, kvs ...interface{}) {
	if !Enabled(LevelInfo) {
		return
	}
	l.log(LevelInfo, msg, kvs)
}

func (l *jsonLogger) Warn(msg string, kvs ...interface{}) {
	if !Enabled(LevelWarn) {
		return
	}
	l.log(LevelWarn, msg, kvs)
}

func (l *jsonLogger) Error(msg string, kvs ...interface{}) {
	if !Enabled(LevelError) {
		return
	}
	l.log(LevelError, msg, kvs)
}

func (l *jsonLogger) With(kvs ...interface{}) Logger {
	merged := make([]interface{}, 0, len(l.prefix)+len(kvs))
	merged = append(merged, l.prefix...)
	merged = append(merged, kvs...)
	return &jsonLogger{w: l.w, prefix: merged}
}

func (l *jsonLogger) log(level Level, msg string, kvs []interface{}) {
	fields := make(map[string]interface{}, 3+len(l.prefix)/2+len(kvs)/2)
	fields["time"] = time.Now().Format(time.RFC3339Nano)
	fields["level"] = level.String()
	fields["msg"] = msg

	// 写入预设字段
	putKVs(fields, l.prefix)
	// 写入本次字段
	putKVs(fields, kvs)

	data, err := json.Marshal(fields)
	if err != nil {
		return
	}
	data = append(data, '\n')

	l.mu.Lock()
	l.w.Write(data)
	l.mu.Unlock()
}

func putKVs(m map[string]interface{}, kvs []interface{}) {
	for i := 0; i+1 < len(kvs); i += 2 {
		key := fmt.Sprint(kvs[i])
		m[key] = kvs[i+1]
	}
}
