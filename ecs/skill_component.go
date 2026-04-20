package ecs

import (
	"engine/skill"
)

// SkillComponent 技能组件：持有实体的技能栏和冷却状态
type SkillComponent struct {
	Caster *skill.SkillCaster
	// PendingCasts 待处理的释放请求（事件驱动，SkillSystem 每帧消费）
	PendingCasts []SkillCastRequest
	// LastResults 最近一次释放产生的效果结果（供上层读取）
	LastResults []skill.EffectResult
	// ActiveSession 当前正在推进的阶段化技能会话（nil = 无）
	ActiveSession *skill.CastSession
	// ChainScheduler 链式派生动作调度器
	ChainScheduler *skill.ChainScheduler
}

func (c *SkillComponent) ComponentType() string { return "Skill" }

// NewSkillComponent 创建技能组件
func NewSkillComponent(caster *skill.SkillCaster) *SkillComponent {
	return &SkillComponent{
		Caster:         caster,
		PendingCasts:   make([]SkillCastRequest, 0),
		ChainScheduler: skill.NewChainScheduler(),
	}
}

// EnqueueCast 入队一次技能释放请求
func (c *SkillComponent) EnqueueCast(req SkillCastRequest) {
	c.PendingCasts = append(c.PendingCasts, req)
}

// SkillCastRequest 技能释放请求
type SkillCastRequest struct {
	SkillID  string
	TargetID string
	// AOETargets 预计算的 AOE 目标列表（可选，留空则由 SkillSystem 根据位置查询）
	AOETargets []string
}

// BuffComponent Buff 组件：持有实体上活跃的所有 Buff
// 组件类型为 "BuffAura"，避开旧 ecs/combat.go 中占用的 "Buff"
type BuffComponent struct {
	Manager *skill.BuffManager
	// LastTickResults 最近一次 Tick 产生的 DOT/HOT 结果
	LastTickResults []skill.BuffTickResult
}

// BuffComponentType 新 BuffComponent 在实体上注册的类型字符串
const BuffComponentType = "BuffAura"

func (c *BuffComponent) ComponentType() string { return BuffComponentType }

// NewBuffComponent 创建 Buff 组件
func NewBuffComponent(ownerID string) *BuffComponent {
	return &BuffComponent{
		Manager: skill.NewBuffManager(ownerID),
	}
}
