package ecs

import (
	"time"

	"gamelib/skill"
)

// SkillCastSystem 技能释放系统
// 事件驱动：消费 SkillComponent.PendingCasts 队列，驱动技能冷却判定、阶段推进、效果派生。
// 效果结果写入 SkillComponent.LastResults，由上层（战斗系统/网络同步）消费。
type SkillCastSystem struct {
	priority int
	pipeline *skill.EffectPipeline
	// Now 时间源（可注入以便测试），nil 时使用 time.Now
	Now func() time.Time
	// TargetLocator 按 TargetID 查询实体位置（AOE 使用），可选
	TargetLocator func(id string) (x, y float32, ok bool)
	// AOECandidates 返回可能成为 AOE 目标的实体 ID 集合（供 FindTargetsInRadius 使用），可选
	AOECandidates func() []skill.TargetPosition
	// CritResolver 暴击判定（触发器 CondCrit 使用），nil 时恒为 false
	CritResolver func(casterID, targetID string) bool
	// HPResolver 血量百分比解析（CondHPBelow/Above 使用），nil 时视为 1.0
	HPResolver func(entityID string) float64
	// BuffResolver 实体 Buff 集合（CondHasBuff/LacksBuff 使用），nil 时视为空集
	BuffResolver func(entityID string) map[string]bool
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

// Update 消费全部待释放请求 + 推进阶段化会话 + 执行到期链式动作
func (s *SkillCastSystem) Update(world *World, deltaTime time.Duration) {
	now := s.now()

	for _, e := range world.Query("Skill") {
		comp, _ := e.Get("Skill")
		sc := comp.(*SkillComponent)

		// 一次处理完全部请求（避免请求滞留）
		sc.LastResults = sc.LastResults[:0]

		for _, req := range sc.PendingCasts {
			results := s.processCast(e, sc, req, now)
			sc.LastResults = append(sc.LastResults, results...)
		}
		sc.PendingCasts = sc.PendingCasts[:0]

		// 推进阶段化会话
		if sc.ActiveSession != nil {
			results := s.advanceSession(e, sc, now)
			sc.LastResults = append(sc.LastResults, results...)
		}

		// 执行到期的链式派生动作
		if sc.ChainScheduler != nil && sc.ChainScheduler.Pending() > 0 {
			due := sc.ChainScheduler.PopDue(now)
			for _, act := range due {
				results := s.executeChainAction(e, sc, act, now)
				sc.LastResults = append(sc.LastResults, results...)
			}
		}
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

	// 若技能含阶段化定义，走会话驱动；否则按原逻辑一次性执行
	if def.Phased != nil && len(def.Phased.Phases) > 0 {
		session := skill.NewCastSession(def, def.Phased, sc.Caster.OwnerID, req.TargetID, req.AOETargets, now)
		// 每次新会话开始前重置技能上全部 triggers 的 Once 状态
		resetTriggers(def, session)
		sc.ActiveSession = session
		// 立即推进一次，让零时长阶段（瞬发）能在当帧完成
		return s.advanceSession(caster, sc, now)
	}

	ctx := s.buildEffectContext(caster, sc, def, req)
	results, execErr := s.pipeline.Execute(def.Effects, ctx)
	if execErr != nil {
		results = append(results, skill.EffectResult{
			EffectID: req.SkillID,
			TargetID: req.TargetID,
			Applied:  false,
			Reason:   execErr.Error(),
		})
	}
	// 技能级触发器：一次释放完后统一求值
	s.fireTriggers(def.Triggers, def, sc, req.TargetID, results, now)
	// 技能级 Chain DAG：全量展开
	if def.Chain != nil {
		tctx := s.buildTriggerContext(def, sc, req.TargetID, results)
		actions := def.Chain.Walk(tctx, now)
		if sc.ChainScheduler != nil {
			sc.ChainScheduler.Push(actions...)
		}
	}
	return results
}

// advanceSession 推进一个阶段化会话
func (s *SkillCastSystem) advanceSession(caster *Entity, sc *SkillComponent, now time.Time) []skill.EffectResult {
	session := sc.ActiveSession
	if session == nil || session.IsDone() {
		sc.ActiveSession = nil
		return nil
	}

	var aggregated []skill.EffectResult

	// 在一次 Update 里，如果前一阶段已结束、时间又足够，连续推进多个瞬发阶段
	for session.State == skill.PhaseStateRunning {
		phase := session.ActivePhase()
		if phase == nil {
			session.State = skill.PhaseStateCompleted
			break
		}

		req := SkillCastRequest{
			SkillID:    session.SkillID,
			TargetID:   session.TargetID,
			AOETargets: session.AOETargets,
		}

		// Channel 阶段：按 TickInterval 周期执行 Effects
		if phase.Kind == skill.PhaseChannel && phase.TickInterval > 0 {
			for session.LastTick.Add(phase.TickInterval).Before(now) || session.LastTick.Add(phase.TickInterval).Equal(now) {
				session.LastTick = session.LastTick.Add(phase.TickInterval)
				if session.LastTick.After(session.PhaseStart.Add(phase.Duration)) {
					break
				}
				ctx := s.buildEffectContext(caster, sc, session.Skill, req)
				res, _ := s.pipeline.Execute(phase.Effects, ctx)
				aggregated = append(aggregated, res...)
				session.Results = append(session.Results, res...)
			}
		}

		// 阶段是否完成
		elapsed := session.PhaseElapsed(now)
		if elapsed < phase.Duration {
			break // 等待下一帧
		}

		// 非 Channel：阶段在结束时执行一次 Effects
		if phase.Kind != skill.PhaseChannel && len(phase.Effects) > 0 {
			ctx := s.buildEffectContext(caster, sc, session.Skill, req)
			res, _ := s.pipeline.Execute(phase.Effects, ctx)
			aggregated = append(aggregated, res...)
			session.Results = append(session.Results, res...)
		}

		// 阶段触发器求值
		if len(phase.Triggers) > 0 {
			s.fireTriggers(phase.Triggers, session.Skill, sc, session.TargetID, session.Results, now)
		}

		// 进入下一阶段
		session.CurrentPhase++
		if session.CurrentPhase >= len(session.Phased.Phases) {
			session.State = skill.PhaseStateCompleted
			// 技能级 triggers 汇总（整技能结束）
			if len(session.Skill.Triggers) > 0 {
				s.fireTriggers(session.Skill.Triggers, session.Skill, sc, session.TargetID, session.Results, now)
			}
			// Chain DAG
			if session.Skill.Chain != nil && sc.ChainScheduler != nil {
				tctx := s.buildTriggerContext(session.Skill, sc, session.TargetID, session.Results)
				sc.ChainScheduler.Push(session.Skill.Chain.Walk(tctx, now)...)
			}
			break
		}
		session.PhaseStart = now
		session.LastTick = now
	}

	if session.IsDone() {
		sc.ActiveSession = nil
	}
	return aggregated
}

// buildEffectContext 构造效果执行上下文
func (s *SkillCastSystem) buildEffectContext(caster *Entity, sc *SkillComponent, def *skill.SkillDef, req SkillCastRequest) *skill.EffectContext {
	ctx := &skill.EffectContext{
		CasterID:  sc.Caster.OwnerID,
		TargetID:  req.TargetID,
		TargetIDs: req.AOETargets,
		SkillDef:  def,
	}
	if pos := caster.GetPosition(); pos != nil {
		ctx.CasterPos = [2]float32{pos.X, pos.Y}
	}
	if req.TargetID != "" && s.TargetLocator != nil {
		if x, y, ok := s.TargetLocator(req.TargetID); ok {
			ctx.TargetPos = [2]float32{x, y}
		}
	}
	if def.TargetType == skill.TargetAOE && len(ctx.TargetIDs) == 0 && s.AOECandidates != nil {
		candidates := s.AOECandidates()
		center := ctx.TargetPos
		if req.TargetID == "" {
			center = ctx.CasterPos
		}
		ctx.TargetIDs = skill.FindTargetsInRadius(center[0], center[1], def.AOERadius, candidates)
	}
	return ctx
}

// buildTriggerContext 构造触发器求值上下文
func (s *SkillCastSystem) buildTriggerContext(def *skill.SkillDef, sc *SkillComponent, targetID string, results []skill.EffectResult) *skill.TriggerContext {
	ctx := &skill.TriggerContext{
		CasterID: sc.Caster.OwnerID,
		TargetID: targetID,
		SkillDef: def,
		Results:  results,
	}
	if s.CritResolver != nil {
		ctx.Crit = s.CritResolver(sc.Caster.OwnerID, targetID)
	}
	if s.HPResolver != nil {
		ctx.CasterHPPct = s.HPResolver(sc.Caster.OwnerID)
		if targetID != "" {
			ctx.TargetHPPct = s.HPResolver(targetID)
		}
	}
	if s.BuffResolver != nil {
		ctx.CasterBuffs = s.BuffResolver(sc.Caster.OwnerID)
		if targetID != "" {
			ctx.TargetBuffs = s.BuffResolver(targetID)
		}
	}
	return ctx
}

// fireTriggers 批量求值触发器并产出链式动作
func (s *SkillCastSystem) fireTriggers(triggers []*skill.Trigger, def *skill.SkillDef, sc *SkillComponent, targetID string, results []skill.EffectResult, now time.Time) {
	if len(triggers) == 0 || sc.ChainScheduler == nil {
		return
	}
	ctx := s.buildTriggerContext(def, sc, targetID, results)
	for _, trg := range triggers {
		if trg == nil || !trg.Match(ctx) {
			continue
		}
		trg.Fire()
		delay := time.Duration(trg.ChainDelayMS) * time.Millisecond
		action := skill.ChainAction{
			SkillID:   trg.ChainSkillID,
			TargetID:  targetID,
			BuffID:    trg.ApplyBuff,
			Effects:   trg.ExtraEffects,
			ExecuteAt: now.Add(delay),
			Source:    trg.Name,
		}
		if action.SkillID == "" && action.BuffID == "" && len(action.Effects) == 0 {
			continue
		}
		sc.ChainScheduler.Push(action)
	}
}

// executeChainAction 执行一条链式派生动作
// - SkillID 非空：入队到 PendingCasts（下一帧触发常规释放流程）
// - Effects 非空：立即走 EffectPipeline
// - BuffID 非空：组装成 EffectApplyBuff 结果（由上层 Buff 系统消费）
func (s *SkillCastSystem) executeChainAction(caster *Entity, sc *SkillComponent, act skill.ChainAction, now time.Time) []skill.EffectResult {
	var results []skill.EffectResult

	if act.SkillID != "" {
		sc.PendingCasts = append(sc.PendingCasts, SkillCastRequest{
			SkillID:  act.SkillID,
			TargetID: act.TargetID,
		})
	}

	if len(act.Effects) > 0 {
		ctx := &skill.EffectContext{
			CasterID: sc.Caster.OwnerID,
			TargetID: act.TargetID,
		}
		if pos := caster.GetPosition(); pos != nil {
			ctx.CasterPos = [2]float32{pos.X, pos.Y}
		}
		res, err := s.pipeline.Execute(act.Effects, ctx)
		if err == nil {
			results = append(results, res...)
		}
	}

	if act.BuffID != "" {
		results = append(results, skill.EffectResult{
			EffectID: act.Source,
			Type:     skill.EffectApplyBuff,
			TargetID: act.TargetID,
			BuffID:   act.BuffID,
			Applied:  true,
		})
	}

	return results
}

func resetTriggers(def *skill.SkillDef, _ *skill.CastSession) {
	for _, trg := range def.Triggers {
		if trg != nil {
			trg.Reset()
		}
	}
	if def.Phased != nil {
		for i := range def.Phased.Phases {
			for _, trg := range def.Phased.Phases[i].Triggers {
				if trg != nil {
					trg.Reset()
				}
			}
		}
	}
}

func (s *SkillCastSystem) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
