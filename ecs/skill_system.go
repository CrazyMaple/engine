package ecs

import (
	"time"

	"engine/skill"
)

// SkillCastSystem 技能释放系统
// 事件驱动：消费 SkillComponent.PendingCasts 队列，调用 SkillCaster 进行冷却判定并执行效果管线
// 效果结果写入 SkillComponent.LastResults，由上层（战斗系统/网络同步）消费
type SkillCastSystem struct {
	priority int
	pipeline *skill.EffectPipeline
	// Now 时间源（可注入以便测试），nil 时使用 time.Now
	Now func() time.Time
	// TargetLocator 按 TargetID 查询实体位置（AOE 使用），可选
	TargetLocator func(id string) (x, y float32, ok bool)
	// AOECandidates 返回可能成为 AOE 目标的实体 ID 集合（供 FindTargetsInRadius 使用），可选
	AOECandidates func() []skill.TargetPosition
}

// NewSkillCastSystem 创建技能系统
func NewSkillCastSystem(priority int, pipeline *skill.EffectPipeline) *SkillCastSystem {
	return &SkillCastSystem{
		priority: priority,
		pipeline: pipeline,
	}
}

func (s *SkillCastSystem) Name() string  { return "SkillCastSystem" }
func (s *SkillCastSystem) Priority() int { return s.priority }

// CanParallel SkillCastSystem 读写 Skill/Buff 组件，不宜并行
func (s *SkillCastSystem) CanParallel() bool { return false }

// Update 消费全部待释放请求
func (s *SkillCastSystem) Update(world *World, deltaTime time.Duration) {
	now := s.now()

	for _, e := range world.Query("Skill") {
		comp, _ := e.Get("Skill")
		sc := comp.(*SkillComponent)
		if len(sc.PendingCasts) == 0 {
			continue
		}

		// 一次处理完全部请求（避免请求滞留）
		sc.LastResults = sc.LastResults[:0]
		for _, req := range sc.PendingCasts {
			results := s.processCast(e, sc, req, now)
			sc.LastResults = append(sc.LastResults, results...)
		}
		sc.PendingCasts = sc.PendingCasts[:0]
	}
}

// processCast 处理单次释放请求
func (s *SkillCastSystem) processCast(caster *Entity, sc *SkillComponent, req SkillCastRequest, now time.Time) []skill.EffectResult {
	def, err := sc.Caster.CastSkill(req.SkillID, now)
	if err != nil {
		return []skill.EffectResult{{
			EffectID: req.SkillID,
			TargetID: req.TargetID,
			Applied:  false,
			Reason:   err.Error(),
		}}
	}

	ctx := &skill.EffectContext{
		CasterID:  sc.Caster.OwnerID,
		TargetID:  req.TargetID,
		TargetIDs: req.AOETargets,
		SkillDef:  def,
	}

	// 填充施法者位置 + 攻击力（如果有 Position 和 Health/Combat 组件就读）
	if pos := caster.GetPosition(); pos != nil {
		ctx.CasterPos = [2]float32{pos.X, pos.Y}
	}

	// 目标位置（用于 AOE 解析）
	if req.TargetID != "" && s.TargetLocator != nil {
		if x, y, ok := s.TargetLocator(req.TargetID); ok {
			ctx.TargetPos = [2]float32{x, y}
		}
	}

	// 如果是 AOE 技能且未预填充目标，按范围查找
	if def.TargetType == skill.TargetAOE && len(ctx.TargetIDs) == 0 && s.AOECandidates != nil {
		candidates := s.AOECandidates()
		center := ctx.TargetPos
		if def.TargetType == skill.TargetAOE && req.TargetID == "" {
			center = ctx.CasterPos
		}
		ctx.TargetIDs = skill.FindTargetsInRadius(center[0], center[1], def.AOERadius, candidates)
	}

	results, execErr := s.pipeline.Execute(def.Effects, ctx)
	if execErr != nil {
		results = append(results, skill.EffectResult{
			EffectID: req.SkillID,
			TargetID: req.TargetID,
			Applied:  false,
			Reason:   execErr.Error(),
		})
	}
	return results
}

func (s *SkillCastSystem) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
