//go:build xlsx

package config

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// ExcelReader Excel 配置表读取器
// 需要 -tags xlsx 构建标签启用
type ExcelReader struct {
	SheetName string // 目标 sheet 名（空则使用第一个 sheet）
	HeaderRow int    // 表头所在行号（从 0 开始，默认 0）
}

// NewExcelReader 创建 Excel 读取器
func NewExcelReader() *ExcelReader {
	return &ExcelReader{}
}

// Read 从 Excel 文件读取数据到 RecordFile
// 读取指定 Sheet 的所有行，表头行之后为数据行
func (r *ExcelReader) Read(filename string, rf *RecordFile) error {
	f, err := excelize.OpenFile(filename)
	if err != nil {
		return fmt.Errorf("open excel file: %w", err)
	}
	defer f.Close()

	sheetName := r.SheetName
	if sheetName == "" {
		sheetName = f.GetSheetName(0)
		if sheetName == "" {
			return fmt.Errorf("excel file has no sheets")
		}
	}

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return fmt.Errorf("read sheet %q: %w", sheetName, err)
	}

	return rf.ReadFromRows(rows, r.HeaderRow)
}

// ReadMultiSheet 从 Excel 文件读取多个 Sheet，返回 sheetName → RecordFile 映射
// rfFactory 根据 sheet 名返回对应的 RecordFile；返回 nil 表示跳过该 sheet
func (r *ExcelReader) ReadMultiSheet(filename string, rfFactory func(sheetName string) *RecordFile) (map[string]*RecordFile, error) {
	f, err := excelize.OpenFile(filename)
	if err != nil {
		return nil, fmt.Errorf("open excel file: %w", err)
	}
	defer f.Close()

	result := make(map[string]*RecordFile)
	for _, name := range f.GetSheetList() {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		rf := rfFactory(name)
		if rf == nil {
			continue
		}

		rows, err := f.GetRows(name)
		if err != nil {
			return nil, fmt.Errorf("read sheet %q: %w", name, err)
		}

		if err := rf.ReadFromRows(rows, r.HeaderRow); err != nil {
			return nil, fmt.Errorf("parse sheet %q: %w", name, err)
		}

		result[name] = rf
	}

	return result, nil
}
