package ecs

import "time"

// --- 战斗/技能相关组件 ---

// Attack 攻击属性组件
type Attack struct {
	Damage    float32 // 基础伤害
	CritRate  float32 // 暴击率 (0-1)
	CritMulti float32 // 暴击倍率
	HitRate   float32 // 命中率 (0-1)
}

func (a *Attack) ComponentType() string { return "Attack" }

// Defense 防御属性组件
type Defense struct {
	Armor      float32 // 护甲
	Resistance float32 // 抗性
	DodgeRate  float32 // 闪避率 (0-1)
}

func (d *Defense) ComponentType() string { return "Defense" }

// Buff 增益/减益效果组件
type Buff struct {
	Effects []BuffEffect
}

func (b *Buff) ComponentType() string { return "Buff" }

// AddEffect 添加效果
func (b *Buff) AddEffect(effect BuffEffect) {
	b.Effects = append(b.Effects, effect)
}

// RemoveExpired 移除过期效果，返回被移除的效果
func (b *Buff) RemoveExpired(now time.Time) []BuffEffect {
	var expired []BuffEffect
	active := b.Effects[:0]
	for _, e := range b.Effects {
		if e.Duration > 0 && now.After(e.StartTime.Add(e.Duration)) {
			expired = append(expired, e)
		} else {
			active = append(active, e)
		}
	}
	b.Effects = active
	return expired
}

// BuffEffect 单个 Buff 效果
type BuffEffect struct {
	ID        string
	Type      BuffType
	Value     float32       // 效果数值
	Duration  time.Duration // 持续时间，0 表示永久
	StartTime time.Time
	StackCount int          // 叠加层数
	MaxStack   int          // 最大叠加层数
}

// BuffType Buff 类型
type BuffType int

const (
	BuffDamageUp    BuffType = iota // 伤害提升
	BuffDamageDown                  // 伤害降低
	BuffDefenseUp                   // 防御提升
	BuffDefenseDown                 // 防御降低
	BuffSpeedUp                     // 速度提升
	BuffSpeedDown                   // 速度降低
	BuffDOT                         // 持续伤害 (Damage Over Time)
	BuffHOT                         // 持续治疗 (Heal Over Time)
	BuffStun                        // 眩晕
	BuffSilence                     // 沉默
)

// SkillState 技能状态组件
type SkillState struct {
	Skills []Skill
}

func (s *SkillState) ComponentType() string { return "SkillState" }

// GetSkill 按 ID 获取技能
func (s *SkillState) GetSkill(id string) *Skill {
	for i := range s.Skills {
		if s.Skills[i].ID == id {
			return &s.Skills[i]
		}
	}
	return nil
}

// Skill 技能定义
type Skill struct {
	ID         string
	Name       string
	Cooldown   time.Duration // 冷却时间
	LastUsed   time.Time     // 上次使用时间
	CastTime   time.Duration // 前摇
	BackSwing  time.Duration // 后摇
	Phase      SkillPhase    // 当前阶段
	PhaseStart time.Time     // 阶段开始时间
}

// IsReady 技能是否就绪
func (s *Skill) IsReady(now time.Time) bool {
	if s.Phase != SkillPhaseIdle {
		return false
	}
	return now.Sub(s.LastUsed) >= s.Cooldown
}

// SkillPhase 技能阶段
type SkillPhase int

const (
	SkillPhaseIdle      SkillPhase = iota // 空闲
	SkillPhaseCasting                     // 前摇
	SkillPhaseActive                      // 释放（生效点）
	SkillPhaseBackSwing                   // 后摇
	SkillPhaseCooldown                    // 冷却
)

// --- 伤害计算管线 ---

// DamageContext 伤害计算上下文
type DamageContext struct {
	Attacker   *Entity
	Defender   *Entity
	BaseDamage float32
	IsCrit     bool
	IsHit      bool
	IsDodged   bool
	FinalDamage float32
	Stages     []string // 记录经过的管线阶段
}

// DamageStage 伤害计算阶段
type DamageStage interface {
	Name() string
	Process(ctx *DamageContext)
}

// DamagePipeline 伤害计算管线
type DamagePipeline struct {
	stages []DamageStage
}

// NewDamagePipeline 创建默认伤害管线：命中→暴击→减伤→最终
func NewDamagePipeline() *DamagePipeline {
	return &DamagePipeline{
		stages: []DamageStage{
			&HitCheckStage{},
			&CritCheckStage{},
			&ArmorReductionStage{},
			&FinalDamageStage{},
		},
	}
}

// AddStage 追加自定义阶段
func (p *DamagePipeline) AddStage(stage DamageStage) {
	// 插入到 FinalDamageStage 之前
	if len(p.stages) > 0 {
		last := p.stages[len(p.stages)-1]
		if _, ok := last.(*FinalDamageStage); ok {
			p.stages = append(p.stages[:len(p.stages)-1], stage, last)
			return
		}
	}
	p.stages = append(p.stages, stage)
}

// Calculate 执行伤害计算
func (p *DamagePipeline) Calculate(attacker, defender *Entity) *DamageContext {
	atk, _ := attacker.Get("Attack")
	attack := atk.(*Attack)

	ctx := &DamageContext{
		Attacker:   attacker,
		Defender:   defender,
		BaseDamage: attack.Damage,
		IsHit:      true,
	}

	for _, stage := range p.stages {
		stage.Process(ctx)
		ctx.Stages = append(ctx.Stages, stage.Name())
		if !ctx.IsHit {
			break // 未命中，后续阶段跳过
		}
	}

	return ctx
}

// --- 内置伤害阶段 ---

// HitCheckStage 命中判定
type HitCheckStage struct {
	// RandFunc 可选的随机函数，返回 0-1 之间的值（用于测试注入）
	RandFunc func() float32
}

func (s *HitCheckStage) Name() string { return "HitCheck" }
func (s *HitCheckStage) Process(ctx *DamageContext) {
	atk, _ := ctx.Attacker.Get("Attack")
	def, _ := ctx.Defender.Get("Defense")
	if atk == nil || def == nil {
		return
	}
	attack := atk.(*Attack)
	defense := def.(*Defense)

	// 命中率 vs 闪避率
	hitChance := attack.HitRate - defense.DodgeRate
	if hitChance < 0.05 {
		hitChance = 0.05 // 最低 5% 命中
	}

	var roll float32
	if s.RandFunc != nil {
		roll = s.RandFunc()
	} else {
		roll = 0.5 // 默认命中（确定性行为，实际使用时注入随机）
	}

	if roll > hitChance {
		ctx.IsHit = false
		ctx.IsDodged = true
		ctx.FinalDamage = 0
	}
}

// CritCheckStage 暴击判定
type CritCheckStage struct {
	RandFunc func() float32
}

func (s *CritCheckStage) Name() string { return "CritCheck" }
func (s *CritCheckStage) Process(ctx *DamageContext) {
	atk, _ := ctx.Attacker.Get("Attack")
	if atk == nil {
		return
	}
	attack := atk.(*Attack)

	var roll float32
	if s.RandFunc != nil {
		roll = s.RandFunc()
	} else {
		roll = 0.5
	}

	if roll < attack.CritRate {
		ctx.IsCrit = true
		ctx.BaseDamage *= attack.CritMulti
	}
}

// ArmorReductionStage 护甲减伤
type ArmorReductionStage struct{}

func (s *ArmorReductionStage) Name() string { return "ArmorReduction" }
func (s *ArmorReductionStage) Process(ctx *DamageContext) {
	def, _ := ctx.Defender.Get("Defense")
	if def == nil {
		return
	}
	defense := def.(*Defense)

	// 护甲减伤公式：damage * (100 / (100 + armor))
	if defense.Armor > 0 {
		reduction := 100.0 / (100.0 + defense.Armor)
		ctx.BaseDamage *= reduction
	}
}

// FinalDamageStage 最终伤害确定
type FinalDamageStage struct{}

func (s *FinalDamageStage) Name() string { return "FinalDamage" }
func (s *FinalDamageStage) Process(ctx *DamageContext) {
	ctx.FinalDamage = ctx.BaseDamage
	if ctx.FinalDamage < 0 {
		ctx.FinalDamage = 0
	}
}

// --- Buff/DOT/HOT 处理系统 ---

// BuffSystem Buff 效果处理系统
type BuffSystem struct {
	now time.Time // 当前时间，外部注入
}

func (s *BuffSystem) Name() string     { return "BuffSystem" }
func (s *BuffSystem) Priority() int    { return 5 }
func (s *BuffSystem) Update(w *World, dt time.Duration) {
	s.now = s.now.Add(dt)
	entities := w.Query("Buff")
	for _, e := range entities {
		bc, _ := e.Get("Buff")
		buff := bc.(*Buff)

		// 处理 DOT/HOT
		health := e.GetHealth()
		for _, ef := range buff.Effects {
			if health == nil {
				continue
			}
			switch ef.Type {
			case BuffDOT:
				health.Current -= int(ef.Value * float32(dt.Seconds()))
				if health.Current < 0 {
					health.Current = 0
				}
			case BuffHOT:
				health.Current += int(ef.Value * float32(dt.Seconds()))
				if health.Current > health.Max {
					health.Current = health.Max
				}
			}
		}

		// 清理过期 buff
		buff.RemoveExpired(s.now)
	}
}

// SkillSystem 技能时间线处理系统
type SkillSystem struct {
	now time.Time
}

func (s *SkillSystem) Name() string     { return "SkillSystem" }
func (s *SkillSystem) Priority() int    { return 15 }
func (s *SkillSystem) Update(w *World, dt time.Duration) {
	s.now = s.now.Add(dt)
	entities := w.Query("SkillState")
	for _, e := range entities {
		sc, _ := e.Get("SkillState")
		state := sc.(*SkillState)

		for i := range state.Skills {
			skill := &state.Skills[i]
			s.advanceSkillPhase(skill)
		}
	}
}

// advanceSkillPhase 推进技能阶段
func (s *SkillSystem) advanceSkillPhase(skill *Skill) {
	elapsed := s.now.Sub(skill.PhaseStart)

	switch skill.Phase {
	case SkillPhaseCasting:
		if elapsed >= skill.CastTime {
			skill.Phase = SkillPhaseActive
			skill.PhaseStart = s.now
		}
	case SkillPhaseActive:
		// Active 阶段立即转为 BackSwing
		skill.Phase = SkillPhaseBackSwing
		skill.PhaseStart = s.now
	case SkillPhaseBackSwing:
		if elapsed >= skill.BackSwing {
			skill.Phase = SkillPhaseCooldown
			skill.PhaseStart = s.now
			skill.LastUsed = s.now
		}
	case SkillPhaseCooldown:
		if elapsed >= skill.Cooldown {
			skill.Phase = SkillPhaseIdle
		}
	}
}

// CastSkill 发起技能释放
func CastSkill(skill *Skill, now time.Time) bool {
	if !skill.IsReady(now) {
		return false
	}
	skill.Phase = SkillPhaseCasting
	skill.PhaseStart = now
	return true
}
