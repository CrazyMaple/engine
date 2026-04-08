package config

import (
	"fmt"
	"reflect"
	"strings"
)

// ConfigDiff 两个配置表版本之间的差异
type ConfigDiff struct {
	Added   []int        // 新增行号（在 new 中的索引）
	Removed []int        // 删除行号（在 old 中的索引）
	Changed []ChangedRow // 变更行
}

// ChangedRow 单行变更详情
type ChangedRow struct {
	OldRow  int                 // old 中的行索引
	NewRow  int                 // new 中的行索引
	Key     interface{}         // 匹配键值
	Changes map[string][2]string // 字段名 -> [旧值, 新值]
}

// DiffRecordFiles 比较两个 RecordFile 的差异
// 使用第一个 index 字段作为主键匹配行
func DiffRecordFiles(old, new *RecordFile) (*ConfigDiff, error) {
	if old == nil || new == nil {
		return nil, fmt.Errorf("both RecordFiles must be non-nil")
	}
	if old.typeRecord != new.typeRecord {
		return nil, fmt.Errorf("RecordFiles have different record types")
	}

	// 查找第一个 index 字段
	indexField := -1
	for i := 0; i < old.typeRecord.NumField(); i++ {
		if old.typeRecord.Field(i).Tag.Get("rf") == "index" {
			indexField = i
			break
		}
	}
	if indexField < 0 {
		return nil, fmt.Errorf("no index field found for diffing")
	}

	// 构建 old 的 key -> (index, record) 映射
	oldMap := make(map[string]int) // key -> old index
	for i := 0; i < old.NumRecord(); i++ {
		key := getFieldString(old.Record(i), indexField)
		oldMap[key] = i
	}

	// 构建 new 的映射
	newMap := make(map[string]int)
	for i := 0; i < new.NumRecord(); i++ {
		key := getFieldString(new.Record(i), indexField)
		newMap[key] = i
	}

	diff := &ConfigDiff{}

	// 检查新增和变更
	for key, newIdx := range newMap {
		oldIdx, exists := oldMap[key]
		if !exists {
			diff.Added = append(diff.Added, newIdx)
			continue
		}
		// 检查变更
		changes := compareRecords(old.Record(oldIdx), new.Record(newIdx), old.typeRecord)
		if len(changes) > 0 {
			diff.Changed = append(diff.Changed, ChangedRow{
				OldRow:  oldIdx,
				NewRow:  newIdx,
				Key:     key,
				Changes: changes,
			})
		}
	}

	// 检查删除
	for key, oldIdx := range oldMap {
		if _, exists := newMap[key]; !exists {
			diff.Removed = append(diff.Removed, oldIdx)
		}
	}

	return diff, nil
}

func getFieldString(record interface{}, fieldIdx int) string {
	v := reflect.ValueOf(record)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	return fmt.Sprintf("%v", v.Field(fieldIdx).Interface())
}

func compareRecords(oldRec, newRec interface{}, t reflect.Type) map[string][2]string {
	ov := reflect.ValueOf(oldRec)
	nv := reflect.ValueOf(newRec)
	if ov.Kind() == reflect.Ptr {
		ov = ov.Elem()
	}
	if nv.Kind() == reflect.Ptr {
		nv = nv.Elem()
	}

	changes := make(map[string][2]string)
	for i := 0; i < t.NumField(); i++ {
		oldStr := fmt.Sprintf("%v", ov.Field(i).Interface())
		newStr := fmt.Sprintf("%v", nv.Field(i).Interface())
		if oldStr != newStr {
			changes[t.Field(i).Name] = [2]string{oldStr, newStr}
		}
	}
	return changes
}

// FormatDiff 生成人类可读的差异报告
func FormatDiff(diff *ConfigDiff) string {
	if diff == nil {
		return "no diff"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== Config Diff: %d added, %d removed, %d changed ===\n",
		len(diff.Added), len(diff.Removed), len(diff.Changed)))

	for _, idx := range diff.Added {
		sb.WriteString(fmt.Sprintf("+ row %d (new)\n", idx+1))
	}
	for _, idx := range diff.Removed {
		sb.WriteString(fmt.Sprintf("- row %d (removed)\n", idx+1))
	}
	for _, ch := range diff.Changed {
		sb.WriteString(fmt.Sprintf("~ row %d (key=%v):\n", ch.NewRow+1, ch.Key))
		for field, vals := range ch.Changes {
			sb.WriteString(fmt.Sprintf("    %s: %q -> %q\n", field, vals[0], vals[1]))
		}
	}

	return sb.String()
}
