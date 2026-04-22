package config

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
)

// Index 索引映射
type Index map[interface{}]interface{}

// RecordFile 配置表加载器，从 CSV/TSV 文件读取结构化配置数据
// 通过反射将每行映射为 Go 结构体实例
// 支持 `index` tag 创建快速查找索引
type RecordFile struct {
	Comma      rune // 字段分隔符，默认 tab
	Comment    rune // 注释字符，默认 #
	typeRecord reflect.Type
	records    []interface{}
	indexes    []Index
}

// NewRecordFile 创建配置表加载器
// st 必须是结构体实例（非指针），如 MyConfig{}
func NewRecordFile(st interface{}) (*RecordFile, error) {
	typeRecord := reflect.TypeOf(st)
	if typeRecord == nil || typeRecord.Kind() != reflect.Struct {
		return nil, errors.New("st must be a struct")
	}

	for i := 0; i < typeRecord.NumField(); i++ {
		f := typeRecord.Field(i)
		kind := f.Type.Kind()

		switch kind {
		case reflect.Bool,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64,
			reflect.String,
			reflect.Struct, reflect.Array, reflect.Slice, reflect.Map:
			// OK
		default:
			return nil, fmt.Errorf("field %s: unsupported type %v", f.Name, kind)
		}

		if f.Tag.Get("rf") == "index" {
			switch kind {
			case reflect.Struct, reflect.Slice, reflect.Map:
				return nil, fmt.Errorf("field %s: cannot index %v type", f.Name, kind)
			}
		}
	}

	return &RecordFile{typeRecord: typeRecord}, nil
}

// Read 从文件加载配置数据
// 第一行为表头（跳过），后续行为数据
func (rf *RecordFile) Read(name string) error {
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()

	comma := rf.Comma
	if comma == 0 {
		comma = '\t'
	}
	comment := rf.Comment
	if comment == 0 {
		comment = '#'
	}

	reader := csv.NewReader(file)
	reader.Comma = comma
	reader.Comment = comment
	lines, err := reader.ReadAll()
	if err != nil {
		return err
	}

	if len(lines) < 1 {
		rf.records = nil
		rf.indexes = nil
		return nil
	}

	typeRecord := rf.typeRecord
	records := make([]interface{}, 0, len(lines)-1)

	// 统计索引字段数
	indexes := make([]Index, 0)
	for i := 0; i < typeRecord.NumField(); i++ {
		if typeRecord.Field(i).Tag.Get("rf") == "index" {
			indexes = append(indexes, make(Index))
		}
	}

	for n := 1; n < len(lines); n++ {
		line := lines[n]
		if len(line) != typeRecord.NumField() {
			return fmt.Errorf("line %d: field count mismatch, got %d, expect %d",
				n+1, len(line), typeRecord.NumField())
		}

		value := reflect.New(typeRecord)
		record := value.Elem()
		iIndex := 0

		for i := 0; i < typeRecord.NumField(); i++ {
			f := typeRecord.Field(i)
			strField := line[i]
			field := record.Field(i)
			if !field.CanSet() {
				continue
			}

			if err := setField(field, f.Type.Kind(), strField); err != nil {
				return fmt.Errorf("line %d, field %s: %v", n+1, f.Name, err)
			}

			if f.Tag.Get("rf") == "index" {
				index := indexes[iIndex]
				iIndex++
				key := field.Interface()
				if _, ok := index[key]; ok {
					return fmt.Errorf("line %d, field %s: duplicate index key %v", n+1, f.Name, key)
				}
				index[key] = value.Interface()
			}
		}

		records = append(records, value.Interface())
	}

	rf.records = records
	rf.indexes = indexes
	return nil
}

func setField(field reflect.Value, kind reflect.Kind, str string) error {
	switch {
	case kind == reflect.Bool:
		v, err := strconv.ParseBool(str)
		if err != nil {
			return err
		}
		field.SetBool(v)
	case kind >= reflect.Int && kind <= reflect.Int64:
		v, err := strconv.ParseInt(str, 0, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetInt(v)
	case kind >= reflect.Uint && kind <= reflect.Uint64:
		v, err := strconv.ParseUint(str, 0, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetUint(v)
	case kind == reflect.Float32 || kind == reflect.Float64:
		v, err := strconv.ParseFloat(str, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetFloat(v)
	case kind == reflect.String:
		field.SetString(str)
	case kind == reflect.Struct || kind == reflect.Array || kind == reflect.Slice || kind == reflect.Map:
		return json.Unmarshal([]byte(str), field.Addr().Interface())
	}
	return nil
}

// Record 获取第 i 条记录
func (rf *RecordFile) Record(i int) interface{} {
	return rf.records[i]
}

// NumRecord 记录总数
func (rf *RecordFile) NumRecord() int {
	return len(rf.records)
}

// Indexes 获取第 i 个索引
func (rf *RecordFile) Indexes(i int) Index {
	if i >= len(rf.indexes) {
		return nil
	}
	return rf.indexes[i]
}

// Index 通过第一个索引快速查找
func (rf *RecordFile) Index(key interface{}) interface{} {
	idx := rf.Indexes(0)
	if idx == nil {
		return nil
	}
	return idx[key]
}

// ReadFromRows 从二维字符串切片加载数据
// headerRow 指定表头行号（从 0 开始），表头行及之前的行被跳过，之后为数据行
// 可被 ExcelReader 等外部数据源复用
func (rf *RecordFile) ReadFromRows(rows [][]string, headerRow int) error {
	if len(rows) <= headerRow+1 {
		rf.records = nil
		rf.indexes = nil
		return nil
	}

	typeRecord := rf.typeRecord
	numFields := typeRecord.NumField()

	indexes := make([]Index, 0)
	for i := 0; i < numFields; i++ {
		if typeRecord.Field(i).Tag.Get("rf") == "index" {
			indexes = append(indexes, make(Index))
		}
	}

	records := make([]interface{}, 0, len(rows)-headerRow-1)

	for n := headerRow + 1; n < len(rows); n++ {
		line := rows[n]

		// 跳过空行
		if isEmptyRow(line) {
			continue
		}

		// 补齐不足的字段为空字符串（Excel 尾部空列可能被裁剪）
		for len(line) < numFields {
			line = append(line, "")
		}

		if len(line) > numFields {
			return fmt.Errorf("row %d: field count mismatch, got %d, expect %d",
				n+1, len(line), numFields)
		}

		value := reflect.New(typeRecord)
		record := value.Elem()
		iIndex := 0

		for i := 0; i < numFields; i++ {
			f := typeRecord.Field(i)
			strField := line[i]
			field := record.Field(i)
			if !field.CanSet() {
				continue
			}

			if err := setField(field, f.Type.Kind(), strField); err != nil {
				return fmt.Errorf("row %d, field %s: %v", n+1, f.Name, err)
			}

			if f.Tag.Get("rf") == "index" {
				index := indexes[iIndex]
				iIndex++
				key := field.Interface()
				if _, ok := index[key]; ok {
					return fmt.Errorf("row %d, field %s: duplicate index key %v", n+1, f.Name, key)
				}
				index[key] = value.Interface()
			}
		}

		records = append(records, value.Interface())
	}

	rf.records = records
	rf.indexes = indexes
	return nil
}

// isEmptyRow 判断一行是否全为空
func isEmptyRow(row []string) bool {
	for _, cell := range row {
		if cell != "" {
			return false
		}
	}
	return true
}
