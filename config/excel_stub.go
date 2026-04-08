//go:build !xlsx

package config

import "fmt"

// ExcelReader Excel 配置表读取器（需要 -tags xlsx 构建标签）
type ExcelReader struct {
	SheetName string // 目标 sheet 名（默认使用第一个 sheet）
	HeaderRow int    // 表头所在行号（默认 0）
}

// NewExcelReader 创建 Excel 读取器
func NewExcelReader() *ExcelReader {
	return &ExcelReader{}
}

// Read 从 Excel 文件读取数据到 RecordFile
func (r *ExcelReader) Read(filename string, rf *RecordFile) error {
	return fmt.Errorf("xlsx support not compiled: build with -tags xlsx")
}
