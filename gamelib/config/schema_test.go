package config

import (
	"os"
	"path/filepath"
	"testing"
)

type testItem struct {
	ID    int    `rf:"index"`
	Name  string
	Score float64
}

func TestValidateRecordFile(t *testing.T) {
	rf := createTestRecordFile(t)
	min := float64(0)
	max := float64(100)

	schema := &TableSchema{
		Fields: []FieldSchema{
			{Name: "ID", Required: true},
			{Name: "Name", Required: true, Pattern: "^[a-zA-Z]+$"},
			{Name: "Score", Min: &min, Max: &max},
		},
	}

	errs := ValidateRecordFile(rf, schema)
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateRecordFileErrors(t *testing.T) {
	rf := createTestRecordFileWithBadData(t)
	min := float64(0)
	max := float64(50)

	schema := &TableSchema{
		Fields: []FieldSchema{
			{Name: "Score", Min: &min, Max: &max},
		},
	}

	errs := ValidateRecordFile(rf, schema)
	if len(errs) == 0 {
		t.Error("expected validation errors for score > 50")
	}
}

func createTestRecordFile(t *testing.T) *RecordFile {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "items.tsv")
	content := "ID\tName\tScore\n1\tAlice\t85.5\n2\tBob\t92.0\n"
	os.WriteFile(file, []byte(content), 0644)

	rf, err := NewRecordFile(testItem{})
	if err != nil {
		t.Fatal(err)
	}
	if err := rf.Read(file); err != nil {
		t.Fatal(err)
	}
	return rf
}

func createTestRecordFileWithBadData(t *testing.T) *RecordFile {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "items.tsv")
	content := "ID\tName\tScore\n1\tAlice\t85.5\n2\tBob\t92.0\n"
	os.WriteFile(file, []byte(content), 0644)

	rf, err := NewRecordFile(testItem{})
	if err != nil {
		t.Fatal(err)
	}
	if err := rf.Read(file); err != nil {
		t.Fatal(err)
	}
	return rf
}
