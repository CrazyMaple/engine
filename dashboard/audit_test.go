package dashboard

import (
	"testing"
)

func TestAuditLog_RecordAndRecent(t *testing.T) {
	al := NewAuditLog()

	al.Record("config_reload", "game.json", "dashboard", "admin", "127.0.0.1")
	al.Record("log_level_change", "info", "api", "operator", "10.0.0.1")

	entries := al.Recent(10)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// 最近的在前
	if entries[0].Action != "log_level_change" {
		t.Errorf("expected most recent first, got %s", entries[0].Action)
	}
	if entries[1].Action != "config_reload" {
		t.Errorf("expected second entry, got %s", entries[1].Action)
	}
}

func TestAuditLog_CircularBuffer(t *testing.T) {
	al := &AuditLog{
		entries: make([]AuditEntry, 3),
		maxSize: 3,
	}

	al.Record("a1", "d1", "s1", "", "")
	al.Record("a2", "d2", "s2", "", "")
	al.Record("a3", "d3", "s3", "", "")
	al.Record("a4", "d4", "s4", "", "") // 覆盖 a1

	entries := al.Recent(10)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Action != "a4" {
		t.Errorf("expected a4, got %s", entries[0].Action)
	}
}

func TestAuditLog_RecentEmpty(t *testing.T) {
	al := NewAuditLog()

	if entries := al.Recent(5); entries != nil {
		t.Errorf("expected nil for empty log, got %v", entries)
	}
	if entries := al.Recent(0); entries != nil {
		t.Errorf("expected nil for n=0, got %v", entries)
	}
}
