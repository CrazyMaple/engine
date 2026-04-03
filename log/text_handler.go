package log

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// textLogger 文本格式日志实现，输出人类可读的格式
// 格式：2024/01/01 12:00:00 [INFO] msg key=val key2=val2
type textLogger struct {
	w      io.Writer
	mu     sync.Mutex
	prefix []interface{} // 预设的 key-value 字段
}

// NewTextLogger 创建文本格式 Logger
func NewTextLogger(w io.Writer) Logger {
	return &textLogger{w: w}
}

func (l *textLogger) Debug(msg string, kvs ...interface{}) {
	if !Enabled(LevelDebug) {
		return
	}
	l.log(LevelDebug, msg, kvs)
}

func (l *textLogger) Info(msg string, kvs ...interface{}) {
	if !Enabled(LevelInfo) {
		return
	}
	l.log(LevelInfo, msg, kvs)
}

func (l *textLogger) Warn(msg string, kvs ...interface{}) {
	if !Enabled(LevelWarn) {
		return
	}
	l.log(LevelWarn, msg, kvs)
}

func (l *textLogger) Error(msg string, kvs ...interface{}) {
	if !Enabled(LevelError) {
		return
	}
	l.log(LevelError, msg, kvs)
}

func (l *textLogger) With(kvs ...interface{}) Logger {
	merged := make([]interface{}, 0, len(l.prefix)+len(kvs))
	merged = append(merged, l.prefix...)
	merged = append(merged, kvs...)
	return &textLogger{w: l.w, prefix: merged}
}

func (l *textLogger) log(level Level, msg string, kvs []interface{}) {
	now := time.Now().Format("2006/01/02 15:04:05")

	buf := make([]byte, 0, 256)
	buf = append(buf, now...)
	buf = append(buf, " ["...)
	buf = append(buf, level.String()...)
	buf = append(buf, "] "...)
	buf = append(buf, msg...)

	// 写入预设字段
	buf = appendKVs(buf, l.prefix)
	// 写入本次字段
	buf = appendKVs(buf, kvs)

	buf = append(buf, '\n')

	l.mu.Lock()
	l.w.Write(buf)
	l.mu.Unlock()
}

func appendKVs(buf []byte, kvs []interface{}) []byte {
	for i := 0; i+1 < len(kvs); i += 2 {
		buf = append(buf, ' ')
		buf = append(buf, fmt.Sprint(kvs[i])...)
		buf = append(buf, '=')
		buf = append(buf, fmt.Sprint(kvs[i+1])...)
	}
	return buf
}
