package codegen

import "fmt"

// CompatibilityReport 兼容性报告
type CompatibilityReport struct {
	Compatible      bool     // 是否向后兼容
	BreakingChanges []string // 不兼容变更描述
	Warnings        []string // 兼容但需注意的变更
	Additions       []string // 新增项
}

// CheckCompatibility 检查两组消息定义之间的兼容性
// 规则：新增字段/消息 → 兼容(警告)；删除消息/类型变更 → 不兼容
func CheckCompatibility(old, new []MessageDef) *CompatibilityReport {
	report := &CompatibilityReport{Compatible: true}

	oldManifest := GenerateManifest(old, 1)
	newManifest := GenerateManifest(new, 2)
	diff := CompareManifests(oldManifest, newManifest)

	// 新增消息 — 兼容
	for _, name := range diff.AddedMessages {
		report.Additions = append(report.Additions, fmt.Sprintf("new message: %s", name))
	}

	// 删除消息 — 不兼容
	for _, name := range diff.RemovedMessages {
		report.Compatible = false
		report.BreakingChanges = append(report.BreakingChanges,
			fmt.Sprintf("removed message: %s", name))
	}

	for _, change := range diff.ChangedMessages {
		// 新增字段 — 兼容(警告)
		for _, f := range change.AddedFields {
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("%s: new field %s (%s)", change.Name, f.Name, f.Type))
		}
		// 删除字段 — 兼容(警告)
		for _, f := range change.RemovedFields {
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("%s: removed field %s (%s)", change.Name, f.Name, f.Type))
		}
		// 类型变更 — 不兼容
		for _, f := range change.ChangedFields {
			report.Compatible = false
			report.BreakingChanges = append(report.BreakingChanges,
				fmt.Sprintf("%s: field %s type changed from %s to %s",
					change.Name, f.Name, f.OldType, f.NewType))
		}
	}

	return report
}

// UpdateManifest 更新版本清单文件：加载现有清单，合并新消息定义，保存
func UpdateManifest(manifestPath string, msgs []MessageDef, newVersion int) error {
	existing, err := LoadManifest(manifestPath)
	if err != nil {
		// 文件不存在时创建新清单
		manifest := GenerateManifest(msgs, newVersion)
		return SaveManifest(manifestPath, manifest)
	}

	// 合并：保留旧消息的 AddedIn，更新字段快照
	existingMap := make(map[string]*MessageVersion)
	for i := range existing.Messages {
		existingMap[existing.Messages[i].Name] = &existing.Messages[i]
	}

	manifest := &VersionManifest{
		ProtocolVersion: newVersion,
		Messages:        make([]MessageVersion, 0, len(msgs)),
	}

	for _, msg := range msgs {
		mv := MessageVersion{
			Name:    msg.Name,
			Fields:  msg.Fields,
			AddedIn: newVersion,
		}
		if old, exists := existingMap[msg.Name]; exists {
			mv.AddedIn = old.AddedIn
			mv.Version = old.Version + 1
		} else {
			mv.Version = 1
		}
		manifest.Messages = append(manifest.Messages, mv)
	}

	return SaveManifest(manifestPath, manifest)
}
