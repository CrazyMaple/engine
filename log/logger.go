package log

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// Level 日志级别
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// String 返回级别名称
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return fmt.Sprintf("LEVEL(%d)", int(l))
	}
}

// ParseLevel 从字符串解析日志级别
func ParseLevel(s string) (Level, error) {
	switch s {
	case "debug", "DEBUG":
		return LevelDebug, nil
	case "info", "INFO":
		return LevelInfo, nil
	case "warn", "WARN", "warning", "WARNING":
		return LevelWarn, nil
	case "error", "ERROR":
		return LevelError, nil
	default:
		return LevelDebug, fmt.Errorf("unknown log level: %q", s)
	}
}

// Logger 结构化日志接口
type Logger interface {
	// Debug 调试日志
	Debug(msg string, kvs ...interface{})
	// Info 信息日志
	Info(msg string, kvs ...interface{})
	// Warn 警告日志
	Warn(msg string, kvs ...interface{})
	// Error 错误日志
	Error(msg string, kvs ...interface{})
	// With 返回附带预设字段的子 Logger
	With(kvs ...interface{}) Logger
}

// 全局状态
var (
	globalLevel  atomic.Int32
	loggerMu     sync.RWMutex
	activeLogger Logger
)

func init() {
	globalLevel.Store(int32(LevelDebug))
}

// SetLevel 设置全局日志级别（运行时可动态调整）
func SetLevel(level Level) {
	globalLevel.Store(int32(level))
}

// GetLevel 获取当前全局日志级别
func GetLevel() Level {
	return Level(globalLevel.Load())
}

// SetLogger 设置全局 Logger 实现
func SetLogger(l Logger) {
	loggerMu.Lock()
	activeLogger = l
	loggerMu.Unlock()
}

// GetLogger 获取当前全局 Logger
func GetLogger() Logger {
	loggerMu.RLock()
	l := activeLogger
	loggerMu.RUnlock()
	return l
}

// L 获取当前全局 Logger 的快捷方式
func L() Logger {
	return GetLogger()
}

// Enabled 检查指定级别是否启用（用于避免无用的参数构造）
func Enabled(level Level) bool {
	return level >= Level(globalLevel.Load())
}
