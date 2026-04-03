package codegen

import (
	"encoding/json"
	"fmt"
	"os"
)

// MessageVersion 消息版本信息
type MessageVersion struct {
	Name    string     `json:"name"`
	Version int        `json:"version"`           // 该消息的 schema 版本
	Fields  []FieldDef `json:"fields"`             // 字段快照
	AddedIn int        `json:"added_in,omitempty"` // 首次添加的协议版本
}

// VersionManifest 版本清单
type VersionManifest struct {
	ProtocolVersion int              `json:"protocol_version"`
	Messages        []MessageVersion `json:"messages"`
}

// ManifestDiff 版本差异
type ManifestDiff struct {
	AddedMessages   []string        // 新增的消息
	RemovedMessages []string        // 删除的消息
	ChangedMessages []MessageChange // 变更的消息
}

// MessageChange 消息变更详情
type MessageChange struct {
	Name          string
	AddedFields   []FieldDef
	RemovedFields []FieldDef
	ChangedFields []FieldChange
}

// FieldChange 字段变更
type FieldChange struct {
	Name    string
	OldType string
	NewType string
}

// GenerateManifest 从消息定义生成版本清单
func GenerateManifest(msgs []MessageDef, version int) *VersionManifest {
	manifest := &VersionManifest{
		ProtocolVersion: version,
		Messages:        make([]MessageVersion, 0, len(msgs)),
	}
	for _, msg := range msgs {
		mv := MessageVersion{
			Name:    msg.Name,
			Version: 1,
			Fields:  msg.Fields,
			AddedIn: version,
		}
		manifest.Messages = append(manifest.Messages, mv)
	}
	return manifest
}

// CompareManifests 比较两个版本清单，返回差异
func CompareManifests(old, new *VersionManifest) *ManifestDiff {
	diff := &ManifestDiff{}

	oldMap := make(map[string]*MessageVersion, len(old.Messages))
	for i := range old.Messages {
		oldMap[old.Messages[i].Name] = &old.Messages[i]
	}
	newMap := make(map[string]*MessageVersion, len(new.Messages))
	for i := range new.Messages {
		newMap[new.Messages[i].Name] = &new.Messages[i]
	}

	// 检查新增和变更
	for name, newMsg := range newMap {
		oldMsg, exists := oldMap[name]
		if !exists {
			diff.AddedMessages = append(diff.AddedMessages, name)
			continue
		}
		if change := compareFields(name, oldMsg.Fields, newMsg.Fields); change != nil {
			diff.ChangedMessages = append(diff.ChangedMessages, *change)
		}
	}

	// 检查删除
	for name := range oldMap {
		if _, exists := newMap[name]; !exists {
			diff.RemovedMessages = append(diff.RemovedMessages, name)
		}
	}

	return diff
}

func compareFields(msgName string, oldFields, newFields []FieldDef) *MessageChange {
	oldMap := make(map[string]*FieldDef, len(oldFields))
	for i := range oldFields {
		oldMap[oldFields[i].Name] = &oldFields[i]
	}
	newMap := make(map[string]*FieldDef, len(newFields))
	for i := range newFields {
		newMap[newFields[i].Name] = &newFields[i]
	}

	var change MessageChange
	change.Name = msgName

	for name, newF := range newMap {
		oldF, exists := oldMap[name]
		if !exists {
			change.AddedFields = append(change.AddedFields, *newF)
			continue
		}
		if oldF.Type != newF.Type {
			change.ChangedFields = append(change.ChangedFields, FieldChange{
				Name:    name,
				OldType: oldF.Type,
				NewType: newF.Type,
			})
		}
	}
	for name := range oldMap {
		if _, exists := newMap[name]; !exists {
			change.RemovedFields = append(change.RemovedFields, *oldMap[name])
		}
	}

	if len(change.AddedFields) == 0 && len(change.RemovedFields) == 0 && len(change.ChangedFields) == 0 {
		return nil
	}
	return &change
}

// LoadManifest 从 JSON 文件加载版本清单
func LoadManifest(path string) (*VersionManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m VersionManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// SaveManifest 保存版本清单为 JSON 文件
func SaveManifest(path string, m *VersionManifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
