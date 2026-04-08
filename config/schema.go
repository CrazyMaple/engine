package config

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// FieldSchema 配置字段校验规则
type FieldSchema struct {
	Name       string   // 字段名
	Type       string   // 期望类型: "int", "float", "string", "bool"
	Required   bool     // 是否必填（非空）
	Min        *float64 // 最小值（数值字段）
	Max        *float64 // 最大值（数值字段）
	Pattern    string   // 正则表达式（字符串字段）
	ForeignKey string   // 外键引用: "filename.fieldName"
}

// TableSchema 配置表校验 Schema
type TableSchema struct {
	Fields []FieldSchema
}

// ValidationError 单个字段校验失败
type ValidationError struct {
	Row   int    // 数据行号（从 1 开始）
	Field string // 字段名
	Value string // 实际值
	Rule  string // 校验规则名
	Msg   string // 错误描述
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("row %d, field %s: %s (value=%q, rule=%s)", e.Row, e.Field, e.Msg, e.Value, e.Rule)
}

// ValidateRecordFile 校验 RecordFile 中的所有记录
func ValidateRecordFile(rf *RecordFile, schema *TableSchema) []ValidationError {
	if rf == nil || schema == nil {
		return nil
	}

	var errs []ValidationError

	for i := 0; i < rf.NumRecord(); i++ {
		rec := rf.Record(i)
		v := reflect.ValueOf(rec)
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			continue
		}

		for _, fs := range schema.Fields {
			field := v.FieldByName(fs.Name)
			if !field.IsValid() {
				continue
			}

			strVal := fmt.Sprintf("%v", field.Interface())
			row := i + 1

			// Required 检查
			if fs.Required && strVal == "" {
				errs = append(errs, ValidationError{Row: row, Field: fs.Name, Value: strVal, Rule: "required", Msg: "field is required"})
				continue
			}

			// 类型+范围检查
			if fs.Min != nil || fs.Max != nil {
				fVal, err := toFloat64(field)
				if err == nil {
					if fs.Min != nil && fVal < *fs.Min {
						errs = append(errs, ValidationError{Row: row, Field: fs.Name, Value: strVal, Rule: "min", Msg: fmt.Sprintf("value %v < min %v", fVal, *fs.Min)})
					}
					if fs.Max != nil && fVal > *fs.Max {
						errs = append(errs, ValidationError{Row: row, Field: fs.Name, Value: strVal, Rule: "max", Msg: fmt.Sprintf("value %v > max %v", fVal, *fs.Max)})
					}
				}
			}

			// Pattern 检查（字符串字段）
			if fs.Pattern != "" && field.Kind() == reflect.String {
				matched, err := regexp.MatchString(fs.Pattern, field.String())
				if err == nil && !matched {
					errs = append(errs, ValidationError{Row: row, Field: fs.Name, Value: strVal, Rule: "pattern", Msg: fmt.Sprintf("value does not match pattern %q", fs.Pattern)})
				}
			}
		}
	}

	return errs
}

// ValidateForeignKeys 校验外键引用
// schemas key = filename, 在 mgr 中查找对应的 RecordFile
func ValidateForeignKeys(mgr *Manager, schemas map[string]*TableSchema) []ValidationError {
	if mgr == nil || schemas == nil {
		return nil
	}

	var errs []ValidationError

	for filename, schema := range schemas {
		entry := mgr.Get(filename)
		if entry == nil || entry.RecordFile == nil {
			continue
		}
		rf := entry.RecordFile

		for _, fs := range schema.Fields {
			if fs.ForeignKey == "" {
				continue
			}

			parts := strings.SplitN(fs.ForeignKey, ".", 2)
			if len(parts) != 2 {
				continue
			}
			refFile, refField := parts[0], parts[1]

			refEntry := mgr.Get(refFile)
			if refEntry == nil || refEntry.RecordFile == nil {
				continue
			}

			// 收集引用表中的合法值集合
			validValues := collectFieldValues(refEntry.RecordFile, refField)

			for i := 0; i < rf.NumRecord(); i++ {
				rec := rf.Record(i)
				v := reflect.ValueOf(rec)
				if v.Kind() == reflect.Ptr {
					v = v.Elem()
				}
				field := v.FieldByName(fs.Name)
				if !field.IsValid() {
					continue
				}
				key := fmt.Sprintf("%v", field.Interface())
				if _, ok := validValues[key]; !ok {
					errs = append(errs, ValidationError{
						Row:   i + 1,
						Field: fs.Name,
						Value: key,
						Rule:  "foreign_key",
						Msg:   fmt.Sprintf("value %q not found in %s.%s", key, refFile, refField),
					})
				}
			}
		}
	}

	return errs
}

func collectFieldValues(rf *RecordFile, fieldName string) map[string]bool {
	values := make(map[string]bool)
	for i := 0; i < rf.NumRecord(); i++ {
		rec := rf.Record(i)
		v := reflect.ValueOf(rec)
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}
		field := v.FieldByName(fieldName)
		if field.IsValid() {
			values[fmt.Sprintf("%v", field.Interface())] = true
		}
	}
	return values
}

func toFloat64(v reflect.Value) (float64, error) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint()), nil
	case reflect.Float32, reflect.Float64:
		return v.Float(), nil
	case reflect.String:
		return strconv.ParseFloat(v.String(), 64)
	default:
		return 0, fmt.Errorf("cannot convert %v to float64", v.Kind())
	}
}
