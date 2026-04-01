package log

import (
	"fmt"
	"log"
	"os"
)

var logger = log.New(os.Stdout, "", log.LstdFlags)

// Debug 调试日志
func Debug(format string, v ...interface{}) {
	logger.Printf("[DEBUG] "+format, v...)
}

// Info 信息日志
func Info(format string, v ...interface{}) {
	logger.Printf("[INFO] "+format, v...)
}

// Warn 警告日志
func Warn(format string, v ...interface{}) {
	logger.Printf("[WARN] "+format, v...)
}

// Error 错误日志
func Error(format string, v ...interface{}) {
	logger.Printf("[ERROR] "+format, v...)
}

// Fatal 致命错误
func Fatal(format string, v ...interface{}) {
	logger.Printf("[FATAL] "+format, v...)
	os.Exit(1)
}

// Print 打印
func Print(v ...interface{}) {
	fmt.Println(v...)
}
