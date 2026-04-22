package skill

import (
	"fmt"
	"time"
)

// BuffCategory Buff 分类
type BuffCategory int

const (
	BuffCategoryBuff   BuffCategory = iota // 增益
	BuffCategoryDebuff                     // 减益
)

// StackPolicy 叠加策略
type StackPolicy int

const (
	StackReplace    StackPolicy = iota // 替换（刷新持续时间）
	StackAdd                           // 叠加层数
	StackIndependent                   // 独立实例
	StackReject                        // 拒绝（已有时不可再施加）
)

// BuffDef Buff 定义（配表数据）
type BuffDef struct {
	ID          string       // Buff ID
	Name        string       // Buff 名称
	Category    BuffCategory // 分类（增益/减益）
	Duration    time.Duration // 持续时间（0=永久）
	MaxStack    int          // 最大叠加层数
	StackPolicy StackPolicy  // 叠加策略
	TickInterval time.Duration // Tick 间隔（DOT/HOT，0=不 Tick）
	Priority    int          // 优先级（互斥判定用，高优先级替换低优先级）
	MutexGroup  string       // 互斥组（同组内只保留一个）
	Tags        []string     // 标签
	// 效果参数
	Modifiers   []AttributeModifier // 属性修改列表
	TickDamage  float32             // 每次 Tick 伤害（DOT）
	TickHeal    float32             // 每次 Tick 治疗（HOT）
}

// AttributeModifier 属性修改器
type AttributeModifier struct {
	Attribute string  // 属性名（如 "attack","defense","speed"）
	AddValue  float32 // 加法增量
	MulValue  float32 // 乘法增量（百分比，如 0.1 = +10%）
}

// BuffInstance Buff 运行时实例
type BuffInstance struct {
	Def        *BuffDef
	StackCount int       // 当前叠加层数
	StartTime  time.Time // 施加时间
	ExpireTime time.Time // 到期时间（Duration=0 时为零值表永久）
	LastTick   time.Time // 上次 Tick 时间
	SourceID   string    // 施加者 ID
}

// IsExpired 检查 Buff 是否过期
func (b *BuffInstance) IsExpired(now time.Time) bool {
	if b.Def.Duration == 0 {
		return false // 永久 Buff
	}
	return now.After(b.ExpireTime) || now.Equal(b.ExpireTime)
}

// ShouldTick 检查是否到了 Tick 时间
func (b *BuffInstance) ShouldTick(now time.Time) bool {
	if b.Def.TickInterval <= 0 {
		return false
	}
	return now.Sub(b.LastTick) >= b.Def.TickInterval
}

// Refresh 刷新持续时间
func (b *BuffInstance) Refresh(now time.Time) {
	b.StartTime = now
	if b.Def.Duration > 0 {
		b.ExpireTime = now.Add(b.Def.Duration)
	}
}

// BuffManager 实体的 Buff 管理器
type BuffManager struct {
	OwnerID string
	Buffs   []*BuffInstance
}

// NewBuffManager 创建 Buff 管理器
func NewBuffManager(ownerID string) *BuffManager {
	return &BuffManager{
		OwnerID: ownerID,
		Buffs:   make([]*BuffInstance, 0),
	}
}

// Apply 施加 Buff
func (m *BuffManager) Apply(def *BuffDef, sourceID string, now time.Time) error {
	// 互斥组检查
	if def.MutexGroup != "" {
		for i, existing := range m.Buffs {
			if existing.Def.MutexGroup == def.MutexGroup {
				if def.Priority > existing.Def.Priority {
					// 高优先级替换
					m.Buffs = append(m.Buffs[:i], m.Buffs[i+1:]...)
					break
				} else if def.Priority == existing.Def.Priority {
					// 同优先级按策略处理
					break
				} else {
					return fmt.Errorf("buff %s blocked by higher priority buff %s in mutex group %s",
						def.ID, existing.Def.ID, def.MutexGroup)
				}
			}
		}
	}

	// 查找已有同 ID Buff
	existing := m.findBuff(def.ID)
	if existing != nil {
		switch def.StackPolicy {
		case StackReplace:
			existing.Refresh(now)
			return nil
		case StackAdd:
			if existing.StackCount < def.MaxStack {
				existing.StackCount++
				existing.Refresh(now)
			}
			return nil
		case StackReject:
			return fmt.Errorf("buff %s already active", def.ID)
		case StackIndependent:
			// 继续添加新实例
		}
	}

	// 创建新实例
	inst := &BuffInstance{
		Def:        def,
		StackCount: 1,
		StartTime:  now,
		LastTick:   now,
		SourceID:   sourceID,
	}
	if def.Duration > 0 {
		inst.ExpireTime = now.Add(def.Duration)
	}

	m.Buffs = append(m.Buffs, inst)
	return nil
}

// Remove 移除指定 Buff
func (m *BuffManager) Remove(buffID string) bool {
	for i, b := range m.Buffs {
		if b.Def.ID == buffID {
			m.Buffs = append(m.Buffs[:i], m.Buffs[i+1:]...)
			return true
		}
	}
	return false
}

// RemoveByCategory 移除指定分类的所有 Buff（如净化移除所有 Debuff）
func (m *BuffManager) RemoveByCategory(category BuffCategory) int {
	count := 0
	active := m.Buffs[:0]
	for _, b := range m.Buffs {
		if b.Def.Category == category {
			count++
		} else {
			active = append(active, b)
		}
	}
	m.Buffs = active
	return count
}

// RemoveByTag 移除具有指定标签的所有 Buff
func (m *BuffManager) RemoveByTag(tag string) int {
	count := 0
	active := m.Buffs[:0]
	for _, b := range m.Buffs {
		if hasTag(b.Def.Tags, tag) {
			count++
		} else {
			active = append(active, b)
		}
	}
	m.Buffs = active
	return count
}

// Tick 处理 Buff 时间流逝：执行 DOT/HOT Tick + 清理过期 Buff
// 返回每次 Tick 的结果列表
func (m *BuffManager) Tick(now time.Time) []BuffTickResult {
	var results []BuffTickResult

	// 处理 Tick 效果
	for _, b := range m.Buffs {
		if b.IsExpired(now) {
			continue
		}
		if b.ShouldTick(now) {
			b.LastTick = now
			if b.Def.TickDamage > 0 {
				results = append(results, BuffTickResult{
					BuffID:   b.Def.ID,
					OwnerID:  m.OwnerID,
					SourceID: b.SourceID,
					Damage:   b.Def.TickDamage * float32(b.StackCount),
				})
			}
			if b.Def.TickHeal > 0 {
				results = append(results, BuffTickResult{
					BuffID:   b.Def.ID,
					OwnerID:  m.OwnerID,
					SourceID: b.SourceID,
					Heal:     b.Def.TickHeal * float32(b.StackCount),
				})
			}
		}
	}

	// 清理过期 Buff
	active := m.Buffs[:0]
	for _, b := range m.Buffs {
		if !b.IsExpired(now) {
			active = append(active, b)
		}
	}
	m.Buffs = active

	return results
}

// GetModifiers 汇总所有活跃 Buff 的属性修改
func (m *BuffManager) GetModifiers(now time.Time) map[string]AttributeModifier {
	merged := make(map[string]AttributeModifier)
	for _, b := range m.Buffs {
		if b.IsExpired(now) {
			continue
		}
		for _, mod := range b.Def.Modifiers {
			existing := merged[mod.Attribute]
			existing.Attribute = mod.Attribute
			existing.AddValue += mod.AddValue * float32(b.StackCount)
			existing.MulValue += mod.MulValue * float32(b.StackCount)
			merged[mod.Attribute] = existing
		}
	}
	return merged
}

// HasBuff 检查是否有指定 Buff
func (m *BuffManager) HasBuff(buffID string) bool {
	return m.findBuff(buffID) != nil
}

// HasTag 检查是否有任何带指定标签的 Buff
func (m *BuffManager) HasTag(tag string) bool {
	for _, b := range m.Buffs {
		if hasTag(b.Def.Tags, tag) {
			return true
		}
	}
	return false
}

// ActiveCount 返回活跃 Buff 数量
func (m *BuffManager) ActiveCount() int {
	return len(m.Buffs)
}

// BuffTickResult Buff Tick 结果
type BuffTickResult struct {
	BuffID   string
	OwnerID  string
	SourceID string
	Damage   float32
	Heal     float32
}

func (m *BuffManager) findBuff(buffID string) *BuffInstance {
	for _, b := range m.Buffs {
		if b.Def.ID == buffID {
			return b
		}
	}
	return nil
}

func hasTag(tags []string, target string) bool {
	for _, t := range tags {
		if t == target {
			return true
		}
	}
	return false
}
