package skill

import (
	"fmt"
	"time"
)

// TargetType 技能目标类型
type TargetType int

const (
	TargetNone   TargetType = iota // 无目标（自身释放）
	TargetSingle                   // 单体目标
	TargetAOE                      // 范围目标
	TargetSelf                     // 仅自身
)

// CostType 消耗类型
type CostType int

const (
	CostNone CostType = iota // 无消耗
	CostMP                   // 消耗 MP
	CostHP                   // 消耗 HP
	CostItem                 // 消耗道具
)

// SkillDef 技能定义（配表数据）
type SkillDef struct {
	ID          string        // 技能 ID
	Name        string        // 技能名称
	Level       int           // 技能等级
	Cooldown    time.Duration // 冷却时间
	GlobalCD    time.Duration // 全局冷却（释放后所有技能共享的短 CD）
	CastTime    time.Duration // 前摇时间
	BackSwing   time.Duration // 后摇时间
	TargetType  TargetType    // 目标类型
	Range       float32       // 施法距离
	AOERadius   float32       // AOE 半径（TargetAOE 时有效）
	CostType    CostType      // 消耗类型
	CostValue   int           // 消耗数值
	Effects     []string      // 效果链（Effect ID 列表，按顺序执行）
	Tags        []string      // 技能标签（如 "physical","magic","heal"）
	Description string        // 技能描述

	// --- 高级表达（可选，v1.11 扩展）---
	// Phased 多阶段定义，非 nil 时由阶段化管线驱动；nil 时退化为一次性执行 Effects
	Phased *PhasedSkill
	// Chain 连锁 DAG（可选），当任一阶段 Triggers 无 ChainSkillID 但技能整体需要 DAG 派生时使用
	Chain *ChainPlan
	// Triggers 释放完毕后统一求值的触发器（与 Phases 内 triggers 叠加）
	Triggers []*Trigger
}

// SkillInstance 技能运行时实例
type SkillInstance struct {
	Def       *SkillDef
	CDManager *CooldownManager
}

// CanCast 检查技能是否可释放
func (s *SkillInstance) CanCast(now time.Time) error {
	if s.CDManager.IsOnCooldown(s.Def.ID, now) {
		remaining := s.CDManager.Remaining(s.Def.ID, now)
		return fmt.Errorf("skill %s on cooldown (%.1fs remaining)", s.Def.ID, remaining.Seconds())
	}
	if s.CDManager.IsGlobalCD(now) {
		return fmt.Errorf("global cooldown active")
	}
	return nil
}

// Cast 释放技能，开始冷却
func (s *SkillInstance) Cast(now time.Time) error {
	if err := s.CanCast(now); err != nil {
		return err
	}
	s.CDManager.StartCooldown(s.Def.ID, s.Def.Cooldown, now)
	if s.Def.GlobalCD > 0 {
		s.CDManager.StartGlobalCD(s.Def.GlobalCD, now)
	}
	return nil
}

// SkillRegistry 技能定义注册表
type SkillRegistry struct {
	skills map[string]*SkillDef
}

// NewSkillRegistry 创建技能注册表
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{
		skills: make(map[string]*SkillDef),
	}
}

// Register 注册技能定义
func (r *SkillRegistry) Register(def *SkillDef) {
	r.skills[def.ID] = def
}

// Get 获取技能定义
func (r *SkillRegistry) Get(id string) (*SkillDef, bool) {
	def, ok := r.skills[id]
	return def, ok
}

// All 获取所有技能定义
func (r *SkillRegistry) All() []*SkillDef {
	result := make([]*SkillDef, 0, len(r.skills))
	for _, def := range r.skills {
		result = append(result, def)
	}
	return result
}

// SkillCaster 技能持有者
type SkillCaster struct {
	OwnerID   string
	Skills    map[string]*SkillInstance
	CDManager *CooldownManager
	Registry  *SkillRegistry
}

// NewSkillCaster 创建技能持有者
func NewSkillCaster(ownerID string, registry *SkillRegistry) *SkillCaster {
	return &SkillCaster{
		OwnerID:   ownerID,
		Skills:    make(map[string]*SkillInstance),
		CDManager: NewCooldownManager(),
		Registry:  registry,
	}
}

// LearnSkill 学习技能
func (c *SkillCaster) LearnSkill(skillID string) error {
	def, ok := c.Registry.Get(skillID)
	if !ok {
		return fmt.Errorf("skill %s not found in registry", skillID)
	}
	c.Skills[skillID] = &SkillInstance{
		Def:       def,
		CDManager: c.CDManager,
	}
	return nil
}

// ForgetSkill 遗忘技能
func (c *SkillCaster) ForgetSkill(skillID string) {
	delete(c.Skills, skillID)
	c.CDManager.ClearCooldown(skillID)
}

// CastSkill 释放技能
func (c *SkillCaster) CastSkill(skillID string, now time.Time) (*SkillDef, error) {
	inst, ok := c.Skills[skillID]
	if !ok {
		return nil, fmt.Errorf("skill %s not learned", skillID)
	}
	if err := inst.Cast(now); err != nil {
		return nil, err
	}
	return inst.Def, nil
}

// GetSkillIDs 获取已学技能 ID 列表
func (c *SkillCaster) GetSkillIDs() []string {
	ids := make([]string, 0, len(c.Skills))
	for id := range c.Skills {
		ids = append(ids, id)
	}
	return ids
}
