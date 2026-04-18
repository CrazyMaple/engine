package ecs

import (
	"time"

	"engine/skill"
)

// BuffTickSystem 每帧驱动所有 BuffComponent 的 Tick
// 职责：
// 1. 执行 DOT/HOT Tick（将结果写入 BuffComponent.LastTickResults）
// 2. 清理过期 Buff
// 3. 可选：通过 ApplyDamage/ApplyHeal 回调把伤害/治疗应用到实体 Health 组件
type BuffTickSystem struct {
	priority int
	// Now 时间源（可注入），nil 时使用 time.Now
	Now func() time.Time
	// ApplyDamage 伤害落地回调（通常扣减 Health 组件），可选
	ApplyDamage func(ownerID string, damage float32)
	// ApplyHeal 治疗落地回调（通常增加 Health 组件），可选
	ApplyHeal func(ownerID string, heal float32)
}

// NewBuffTickSystem 创建 Buff 系统
func NewBuffTickSystem(priority int) *BuffTickSystem {
	return &BuffTickSystem{priority: priority}
}

func (s *BuffTickSystem) Name() string  { return "BuffTickSystem" }
func (s *BuffTickSystem) Priority() int { return s.priority }

// CanParallel BuffTickSystem 修改 Buff 列表并可能写 Health，串行执行
func (s *BuffTickSystem) CanParallel() bool { return false }

// Update 每帧 Tick 所有 BuffComponent
func (s *BuffTickSystem) Update(world *World, deltaTime time.Duration) {
	now := s.now()

	for _, e := range world.Query(BuffComponentType) {
		comp, _ := e.Get(BuffComponentType)
		bc := comp.(*BuffComponent)
		if bc.Manager == nil {
			continue
		}

		results := bc.Manager.Tick(now)
		bc.LastTickResults = results

		// 将 DOT/HOT 应用到 Health 组件（通过回调，避免强耦合）
		s.applyTickResults(e, results)
	}
}

func (s *BuffTickSystem) applyTickResults(owner *Entity, results []skill.BuffTickResult) {
	if len(results) == 0 {
		return
	}
	health := owner.GetHealth()
	for _, r := range results {
		if r.Damage > 0 {
			if s.ApplyDamage != nil {
				s.ApplyDamage(r.OwnerID, r.Damage)
			} else if health != nil {
				// 默认行为：直接扣减 Health（整数化）
				health.Current -= int(r.Damage)
				if health.Current < 0 {
					health.Current = 0
				}
			}
		}
		if r.Heal > 0 {
			if s.ApplyHeal != nil {
				s.ApplyHeal(r.OwnerID, r.Heal)
			} else if health != nil {
				health.Current += int(r.Heal)
				if health.Current > health.Max {
					health.Current = health.Max
				}
			}
		}
	}
}

func (s *BuffTickSystem) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
