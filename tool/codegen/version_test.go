package codegen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateManifest(t *testing.T) {
	msgs := []MessageDef{
		{Name: "LoginRequest", ID: 1001, Fields: []FieldDef{{Name: "Username", Type: "string"}}},
		{Name: "LoginResponse", ID: 1002, Fields: []FieldDef{{Name: "Success", Type: "bool"}}},
	}

	m := GenerateManifest(msgs, 1)
	if m.ProtocolVersion != 1 {
		t.Errorf("expected version 1, got %d", m.ProtocolVersion)
	}
	if len(m.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(m.Messages))
	}
	if m.Messages[0].Name != "LoginRequest" {
		t.Errorf("expected LoginRequest, got %s", m.Messages[0].Name)
	}
	if m.Messages[0].AddedIn != 1 {
		t.Errorf("expected AddedIn 1, got %d", m.Messages[0].AddedIn)
	}
}

func TestCompareManifestsAddMessage(t *testing.T) {
	old := &VersionManifest{
		ProtocolVersion: 1,
		Messages: []MessageVersion{
			{Name: "LoginRequest", Fields: []FieldDef{{Name: "Username", Type: "string"}}},
		},
	}
	new := &VersionManifest{
		ProtocolVersion: 2,
		Messages: []MessageVersion{
			{Name: "LoginRequest", Fields: []FieldDef{{Name: "Username", Type: "string"}}},
			{Name: "ChatMessage", Fields: []FieldDef{{Name: "Content", Type: "string"}}},
		},
	}

	diff := CompareManifests(old, new)
	if len(diff.AddedMessages) != 1 || diff.AddedMessages[0] != "ChatMessage" {
		t.Errorf("expected ChatMessage added, got %v", diff.AddedMessages)
	}
	if len(diff.RemovedMessages) != 0 {
		t.Errorf("expected no removed, got %v", diff.RemovedMessages)
	}
}

func TestCompareManifestsRemoveMessage(t *testing.T) {
	old := &VersionManifest{
		ProtocolVersion: 1,
		Messages: []MessageVersion{
			{Name: "LoginRequest", Fields: []FieldDef{{Name: "Username", Type: "string"}}},
			{Name: "OldMessage", Fields: []FieldDef{{Name: "X", Type: "int"}}},
		},
	}
	new := &VersionManifest{
		ProtocolVersion: 2,
		Messages: []MessageVersion{
			{Name: "LoginRequest", Fields: []FieldDef{{Name: "Username", Type: "string"}}},
		},
	}

	diff := CompareManifests(old, new)
	if len(diff.RemovedMessages) != 1 || diff.RemovedMessages[0] != "OldMessage" {
		t.Errorf("expected OldMessage removed, got %v", diff.RemovedMessages)
	}
}

func TestCompareManifestsFieldChange(t *testing.T) {
	old := &VersionManifest{
		ProtocolVersion: 1,
		Messages: []MessageVersion{
			{Name: "Msg", Fields: []FieldDef{
				{Name: "A", Type: "string"},
				{Name: "B", Type: "int"},
			}},
		},
	}
	new := &VersionManifest{
		ProtocolVersion: 2,
		Messages: []MessageVersion{
			{Name: "Msg", Fields: []FieldDef{
				{Name: "A", Type: "string"},
				{Name: "B", Type: "int64"}, // type changed
				{Name: "C", Type: "bool"},  // new field
			}},
		},
	}

	diff := CompareManifests(old, new)
	if len(diff.ChangedMessages) != 1 {
		t.Fatalf("expected 1 changed message, got %d", len(diff.ChangedMessages))
	}
	change := diff.ChangedMessages[0]
	if len(change.AddedFields) != 1 || change.AddedFields[0].Name != "C" {
		t.Errorf("expected field C added, got %v", change.AddedFields)
	}
	if len(change.ChangedFields) != 1 || change.ChangedFields[0].Name != "B" {
		t.Errorf("expected field B type changed, got %v", change.ChangedFields)
	}
}

func TestCheckCompatibility(t *testing.T) {
	old := []MessageDef{
		{Name: "Msg1", Fields: []FieldDef{{Name: "A", Type: "string"}}},
		{Name: "Msg2", Fields: []FieldDef{{Name: "X", Type: "int"}}},
	}

	// 兼容变更：新增字段 + 新增消息
	newCompat := []MessageDef{
		{Name: "Msg1", Fields: []FieldDef{{Name: "A", Type: "string"}, {Name: "B", Type: "int"}}},
		{Name: "Msg2", Fields: []FieldDef{{Name: "X", Type: "int"}}},
		{Name: "Msg3", Fields: []FieldDef{{Name: "Y", Type: "bool"}}},
	}
	report := CheckCompatibility(old, newCompat)
	if !report.Compatible {
		t.Error("should be compatible")
	}
	if len(report.Additions) != 1 {
		t.Errorf("expected 1 addition, got %d", len(report.Additions))
	}
	if len(report.Warnings) != 1 {
		t.Errorf("expected 1 warning (new field), got %d", len(report.Warnings))
	}

	// 不兼容变更：类型变更
	newBreaking := []MessageDef{
		{Name: "Msg1", Fields: []FieldDef{{Name: "A", Type: "int"}}}, // string→int
		{Name: "Msg2", Fields: []FieldDef{{Name: "X", Type: "int"}}},
	}
	report2 := CheckCompatibility(old, newBreaking)
	if report2.Compatible {
		t.Error("should be incompatible due to type change")
	}
	if len(report2.BreakingChanges) != 1 {
		t.Errorf("expected 1 breaking change, got %d", len(report2.BreakingChanges))
	}

	// 不兼容变更：删除消息
	newRemoved := []MessageDef{
		{Name: "Msg1", Fields: []FieldDef{{Name: "A", Type: "string"}}},
	}
	report3 := CheckCompatibility(old, newRemoved)
	if report3.Compatible {
		t.Error("should be incompatible due to removed message")
	}
}

func TestSaveAndLoadManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	msgs := []MessageDef{
		{Name: "TestMsg", ID: 1001, Fields: []FieldDef{{Name: "Value", Type: "string"}}},
	}

	manifest := GenerateManifest(msgs, 1)
	if err := SaveManifest(path, manifest); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ProtocolVersion != 1 {
		t.Errorf("expected version 1, got %d", loaded.ProtocolVersion)
	}
	if len(loaded.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(loaded.Messages))
	}
	if loaded.Messages[0].Name != "TestMsg" {
		t.Errorf("expected TestMsg, got %s", loaded.Messages[0].Name)
	}
}

func TestUpdateManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	// 第一次创建
	msgs1 := []MessageDef{
		{Name: "Msg1", Fields: []FieldDef{{Name: "A", Type: "string"}}},
	}
	if err := UpdateManifest(path, msgs1, 1); err != nil {
		t.Fatal(err)
	}

	// 第二次更新（新增消息 + 更新字段）
	msgs2 := []MessageDef{
		{Name: "Msg1", Fields: []FieldDef{{Name: "A", Type: "string"}, {Name: "B", Type: "int"}}},
		{Name: "Msg2", Fields: []FieldDef{{Name: "X", Type: "bool"}}},
	}
	if err := UpdateManifest(path, msgs2, 2); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.ProtocolVersion != 2 {
		t.Errorf("expected version 2, got %d", m.ProtocolVersion)
	}
	if len(m.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(m.Messages))
	}
	// Msg1 应保留 AddedIn=1，Version 递增
	for _, msg := range m.Messages {
		if msg.Name == "Msg1" {
			if msg.AddedIn != 1 {
				t.Errorf("Msg1 AddedIn should be 1, got %d", msg.AddedIn)
			}
			if msg.Version != 2 {
				t.Errorf("Msg1 Version should be 2, got %d", msg.Version)
			}
		}
		if msg.Name == "Msg2" {
			if msg.AddedIn != 2 {
				t.Errorf("Msg2 AddedIn should be 2, got %d", msg.AddedIn)
			}
		}
	}

	// 清理
	os.Remove(path)
}
