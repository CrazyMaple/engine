package log

import "strings"

// Redacted 包装一个值，标记为日志中需要脱敏的字段
type Redacted struct {
	Value interface{} // 原始值（非日志场景可使用）
}

// String 返回脱敏后的字符串表示
func (r Redacted) String() string {
	return "***REDACTED***"
}

// Redact 将一个值标记为需要在日志中脱敏
func Redact(v interface{}) Redacted {
	return Redacted{Value: v}
}

// RedactKeys 默认需要脱敏的键名集合
// key 以小写比对（不区分大小写）
var RedactKeys = map[string]bool{
	"password":   true,
	"token":      true,
	"secret":     true,
	"api_key":    true,
	"apikey":     true,
	"credential": true,
	"auth":       true,
}

const redactedPlaceholder = "***REDACTED***"

// RedactingLogger 脱敏日志包装器
// 自动对 RedactKeys 中的键或 Redacted 类型的值进行遮蔽
type RedactingLogger struct {
	inner Logger
}

// NewRedactingLogger 创建脱敏日志包装器
func NewRedactingLogger(inner Logger) *RedactingLogger {
	return &RedactingLogger{inner: inner}
}

func (l *RedactingLogger) Debug(msg string, kvs ...interface{}) {
	l.inner.Debug(msg, redactKVs(kvs)...)
}

func (l *RedactingLogger) Info(msg string, kvs ...interface{}) {
	l.inner.Info(msg, redactKVs(kvs)...)
}

func (l *RedactingLogger) Warn(msg string, kvs ...interface{}) {
	l.inner.Warn(msg, redactKVs(kvs)...)
}

func (l *RedactingLogger) Error(msg string, kvs ...interface{}) {
	l.inner.Error(msg, redactKVs(kvs)...)
}

func (l *RedactingLogger) With(kvs ...interface{}) Logger {
	return &RedactingLogger{inner: l.inner.With(redactKVs(kvs)...)}
}

// redactKVs 对 kvs 中需要脱敏的值进行替换
func redactKVs(kvs []interface{}) []interface{} {
	if len(kvs) == 0 {
		return kvs
	}
	result := make([]interface{}, len(kvs))
	copy(result, kvs)

	for i := 0; i+1 < len(result); i += 2 {
		key, ok := result[i].(string)
		if !ok {
			continue
		}
		// 检查值是否为 Redacted 类型
		if _, isRedacted := result[i+1].(Redacted); isRedacted {
			result[i+1] = redactedPlaceholder
			continue
		}
		// 检查键名是否在脱敏列表中
		if RedactKeys[strings.ToLower(key)] {
			result[i+1] = redactedPlaceholder
		}
	}
	return result
}
