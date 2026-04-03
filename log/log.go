package log

import (
	"fmt"
	"os"
)

func init() {
	activeLogger = NewTextLogger(os.Stdout)
}

// Debug 调试日志（向后兼容）
func Debug(format string, v ...interface{}) {
	if !Enabled(LevelDebug) {
		return
	}
	GetLogger().Debug(fmt.Sprintf(format, v...))
}

// Info 信息日志（向后兼容）
func Info(format string, v ...interface{}) {
	if !Enabled(LevelInfo) {
		return
	}
	GetLogger().Info(fmt.Sprintf(format, v...))
}

// Warn 警告日志（向后兼容）
func Warn(format string, v ...interface{}) {
	if !Enabled(LevelWarn) {
		return
	}
	GetLogger().Warn(fmt.Sprintf(format, v...))
}

// Error 错误日志（向后兼容）
func Error(format string, v ...interface{}) {
	if !Enabled(LevelError) {
		return
	}
	GetLogger().Error(fmt.Sprintf(format, v...))
}

// Fatal 致命错误（向后兼容）
func Fatal(format string, v ...interface{}) {
	GetLogger().Error(fmt.Sprintf(format, v...))
	os.Exit(1)
}

// Print 打印（向后兼容）
func Print(v ...interface{}) {
	fmt.Println(v...)
}
