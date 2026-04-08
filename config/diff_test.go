package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiffRecordFiles(t *testing.T) {
	dir := t.TempDir()

	// Old version
	oldFile := filepath.Join(dir, "old.tsv")
	os.WriteFile(oldFile, []byte("ID\tName\tScore\n1\tAlice\t85.5\n2\tBob\t92.0\n3\tCarol\t70.0\n"), 0644)

	// New version: Alice changed, Bob same, Carol removed, Dave added
	newFile := filepath.Join(dir, "new.tsv")
	os.WriteFile(newFile, []byte("ID\tName\tScore\n1\tAlice\t90.0\n2\tBob\t92.0\n4\tDave\t88.0\n"), 0644)

	oldRF, _ := NewRecordFile(testItem{})
	oldRF.Read(oldFile)

	newRF, _ := NewRecordFile(testItem{})
	newRF.Read(newFile)

	diff, err := DiffRecordFiles(oldRF, newRF)
	if err != nil {
		t.Fatalf("DiffRecordFiles error: %v", err)
	}

	if len(diff.Added) != 1 {
		t.Errorf("expected 1 added, got %d", len(diff.Added))
	}
	if len(diff.Removed) != 1 {
		t.Errorf("expected 1 removed, got %d", len(diff.Removed))
	}
	if len(diff.Changed) != 1 {
		t.Errorf("expected 1 changed, got %d", len(diff.Changed))
	}

	// 验证变更内容
	if len(diff.Changed) > 0 {
		ch := diff.Changed[0]
		if ch.Key != "1" {
			t.Errorf("expected changed key=1, got %v", ch.Key)
		}
		if scoreChange, ok := ch.Changes["Score"]; !ok || scoreChange[0] != "85.5" || scoreChange[1] != "90" {
			t.Errorf("unexpected score change: %v", ch.Changes)
		}
	}

	// FormatDiff 不应 panic
	report := FormatDiff(diff)
	if report == "" {
		t.Error("expected non-empty diff report")
	}
}

func TestDiffRecordFilesNoIndex(t *testing.T) {
	type noIndexItem struct {
		Name string
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "test.tsv")
	os.WriteFile(file, []byte("Name\nAlice\n"), 0644)

	rf1, _ := NewRecordFile(noIndexItem{})
	rf1.Read(file)
	rf2, _ := NewRecordFile(noIndexItem{})
	rf2.Read(file)

	_, err := DiffRecordFiles(rf1, rf2)
	if err == nil {
		t.Error("expected error for missing index field")
	}
}
