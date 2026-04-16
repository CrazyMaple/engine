package skill

import "time"

// CooldownEntry 单个冷却条目
type CooldownEntry struct {
	SkillID  string
	ReadyAt  time.Time     // 冷却结束时间
	Duration time.Duration // 冷却总时长
}

// CooldownManager 技能冷却管理器
// 管理技能独立 CD 和全局 CD
type CooldownManager struct {
	cooldowns   map[string]*CooldownEntry // skillID → 冷却状态
	globalCDEnd time.Time                 // 全局 CD 结束时间
}

// NewCooldownManager 创建冷却管理器
func NewCooldownManager() *CooldownManager {
	return &CooldownManager{
		cooldowns: make(map[string]*CooldownEntry),
	}
}

// StartCooldown 开始技能独立冷却
func (m *CooldownManager) StartCooldown(skillID string, duration time.Duration, now time.Time) {
	m.cooldowns[skillID] = &CooldownEntry{
		SkillID:  skillID,
		ReadyAt:  now.Add(duration),
		Duration: duration,
	}
}

// StartGlobalCD 开始全局冷却
func (m *CooldownManager) StartGlobalCD(duration time.Duration, now time.Time) {
	m.globalCDEnd = now.Add(duration)
}

// IsOnCooldown 检查技能是否在冷却中
func (m *CooldownManager) IsOnCooldown(skillID string, now time.Time) bool {
	entry, ok := m.cooldowns[skillID]
	if !ok {
		return false
	}
	return now.Before(entry.ReadyAt)
}

// IsGlobalCD 检查全局冷却是否激活
func (m *CooldownManager) IsGlobalCD(now time.Time) bool {
	return now.Before(m.globalCDEnd)
}

// Remaining 返回技能剩余冷却时间
func (m *CooldownManager) Remaining(skillID string, now time.Time) time.Duration {
	entry, ok := m.cooldowns[skillID]
	if !ok {
		return 0
	}
	remaining := entry.ReadyAt.Sub(now)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// GlobalCDRemaining 返回全局 CD 剩余时间
func (m *CooldownManager) GlobalCDRemaining(now time.Time) time.Duration {
	remaining := m.globalCDEnd.Sub(now)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// ClearCooldown 清除技能冷却（如 CD 重置技能）
func (m *CooldownManager) ClearCooldown(skillID string) {
	delete(m.cooldowns, skillID)
}

// ClearAll 清除所有冷却
func (m *CooldownManager) ClearAll() {
	m.cooldowns = make(map[string]*CooldownEntry)
	m.globalCDEnd = time.Time{}
}

// ReduceCooldown 缩短技能冷却时间
func (m *CooldownManager) ReduceCooldown(skillID string, amount time.Duration) {
	entry, ok := m.cooldowns[skillID]
	if !ok {
		return
	}
	entry.ReadyAt = entry.ReadyAt.Add(-amount)
}

// GetAllCooldowns 获取所有冷却中的技能状态
func (m *CooldownManager) GetAllCooldowns(now time.Time) []CooldownEntry {
	result := make([]CooldownEntry, 0)
	for _, entry := range m.cooldowns {
		if now.Before(entry.ReadyAt) {
			result = append(result, *entry)
		}
	}
	return result
}
