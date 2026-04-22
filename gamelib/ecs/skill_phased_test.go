package ecs

import (
	"testing"
	"time"

	"gamelib/skill"
)

// TestSkillSystem_PhasedFlow: 验证多阶段技能按时间推进、阶段效果顺序执行
func TestSkillSystem_PhasedFlow(t *testing.T) {
	reg := skill.NewSkillRegistry()
	reg.Register(&skill.SkillDef{
		ID:         "nova_phased",
		TargetType: skill.TargetSingle,
		Cooldown:   5 * time.Second,
		Phased: &skill.PhasedSkill{
			Phases: []skill.PhaseDef{
				{Kind: skill.PhasePreCast, Duration: 300 * time.Millisecond, Effects: nil},
				{Kind: skill.PhaseCast, Duration: 0, Effects: []string{"fire_dmg"}},
				{Kind: skill.PhaseBackswing, Duration: 200 * time.Millisecond, Effects: nil},
			},
		},
	})

	pipeline := skill.NewEffectPipeline()
	pipeline.RegisterEffect(&skill.EffectDef{ID: "fire_dmg", Type: skill.EffectDamage, Value: 40, Chance: 1})

	caster := skill.NewSkillCaster("hero", reg)
	caster.LearnSkill("nova_phased")

	world := NewWorld()
	hero := NewEntity("hero")
	hero.Add(&Position{X: 0, Y: 0})
	sc := NewSkillComponent(caster)
	hero.Add(sc)
	world.Add(hero)

	now := time.Unix(3000, 0)
	sys := NewSkillCastSystem(100, pipeline)
	sys.Now = func() time.Time { return now }

	sc.EnqueueCast(SkillCastRequest{SkillID: "nova_phased", TargetID: "mob1"})
	sys.Update(world, 0)

	// 0ms：前摇未结束，不应有 Cast 阶段效果
	if len(sc.LastResults) != 0 {
		t.Fatalf("t=0 no effect expected, got %+v", sc.LastResults)
	}
	if sc.ActiveSession == nil {
		t.Fatal("session should be active after enqueue")
	}

	// 300ms：前摇结束，Cast 阶段瞬发执行伤害
	now = now.Add(300 * time.Millisecond)
	sys.Update(world, 0)
	if len(sc.LastResults) != 1 || sc.LastResults[0].Damage != 40 {
		t.Fatalf("after pre_cast want 1 damage=40, got %+v", sc.LastResults)
	}
	if sc.ActiveSession == nil {
		t.Fatal("session should still be active during backswing")
	}

	// 500ms 后：后摇结束，会话完成
	now = now.Add(200 * time.Millisecond)
	sys.Update(world, 0)
	if sc.ActiveSession != nil {
		t.Fatalf("session should be completed, got state=%v", sc.ActiveSession.State)
	}
}

// TestSkillSystem_ChainTrigger: 验证触发器产生链式动作并按时间派发
func TestSkillSystem_ChainTrigger(t *testing.T) {
	reg := skill.NewSkillRegistry()
	reg.Register(&skill.SkillDef{
		ID:         "frostbolt",
		TargetType: skill.TargetSingle,
		Cooldown:   1 * time.Second,
		Effects:    []string{"ice_dmg"},
		Triggers: []*skill.Trigger{
			{
				Name:         "freeze_on_hit",
				Conditions:   []skill.Condition{{Type: skill.CondHit}},
				ApplyBuff:    "frozen",
				ChainDelayMS: 50,
			},
		},
	})
	pipeline := skill.NewEffectPipeline()
	pipeline.RegisterEffect(&skill.EffectDef{ID: "ice_dmg", Type: skill.EffectDamage, Value: 20, Chance: 1})

	caster := skill.NewSkillCaster("mage", reg)
	caster.LearnSkill("frostbolt")

	world := NewWorld()
	mage := NewEntity("mage")
	mage.Add(&Position{X: 0, Y: 0})
	sc := NewSkillComponent(caster)
	mage.Add(sc)
	world.Add(mage)

	now := time.Unix(4000, 0)
	sys := NewSkillCastSystem(100, pipeline)
	sys.Now = func() time.Time { return now }

	sc.EnqueueCast(SkillCastRequest{SkillID: "frostbolt", TargetID: "goblin"})
	sys.Update(world, 0)
	// 伤害已生效，但 Buff 延迟 50ms 不应立即派发
	if len(sc.LastResults) != 1 || sc.LastResults[0].Damage != 20 {
		t.Fatalf("first update should produce damage, got %+v", sc.LastResults)
	}
	if sc.ChainScheduler.Pending() != 1 {
		t.Fatalf("chain scheduler should have 1 pending, got %d", sc.ChainScheduler.Pending())
	}

	// 50ms 后链式 Buff 动作应该派发
	now = now.Add(50 * time.Millisecond)
	sys.Update(world, 0)
	found := false
	for _, r := range sc.LastResults {
		if r.Type == skill.EffectApplyBuff && r.BuffID == "frozen" {
			found = true
		}
	}
	if !found {
		t.Fatalf("chain buff not fired: %+v", sc.LastResults)
	}
	if sc.ChainScheduler.Pending() != 0 {
		t.Error("chain scheduler should be drained")
	}
}

// TestSkillSystem_ConditionalChain: 验证条件不满足时 Trigger 不触发
func TestSkillSystem_ConditionalChain(t *testing.T) {
	reg := skill.NewSkillRegistry()
	reg.Register(&skill.SkillDef{
		ID:         "probe",
		TargetType: skill.TargetSingle,
		Effects:    []string{"probe_dmg"},
		Triggers: []*skill.Trigger{
			{
				Name: "execute_below_30",
				Conditions: []skill.Condition{
					{Type: skill.CondHit},
					{Type: skill.CondHPBelow, Op: skill.OpLt, Value: 0.3},
				},
				ChainSkillID: "execute",
				ChainDelayMS: 500, // 留在调度器里，方便断言
			},
		},
	})
	pipeline := skill.NewEffectPipeline()
	pipeline.RegisterEffect(&skill.EffectDef{ID: "probe_dmg", Type: skill.EffectDamage, Value: 5, Chance: 1})

	caster := skill.NewSkillCaster("rogue", reg)
	caster.LearnSkill("probe")

	world := NewWorld()
	rogue := NewEntity("rogue")
	rogue.Add(&Position{X: 0, Y: 0})
	sc := NewSkillComponent(caster)
	rogue.Add(sc)
	world.Add(rogue)

	now := time.Unix(5000, 0)
	sys := NewSkillCastSystem(100, pipeline)
	sys.Now = func() time.Time { return now }
	// 目标满血（99%）→ 条件不满足，Trigger 不触发
	sys.HPResolver = func(id string) float64 {
		if id == "boss" {
			return 0.99
		}
		return 1
	}

	sc.EnqueueCast(SkillCastRequest{SkillID: "probe", TargetID: "boss"})
	sys.Update(world, 0)
	if sc.ChainScheduler.Pending() != 0 {
		t.Errorf("trigger should not fire at full HP, pending=%d", sc.ChainScheduler.Pending())
	}

	// 降血到 20% → 下一次释放触发 execute
	sys.HPResolver = func(id string) float64 {
		if id == "boss" {
			return 0.2
		}
		return 1
	}
	// 等冷却（probe 无 Cooldown 字段默认 0）再次释放
	now = now.Add(10 * time.Millisecond)
	sys.Now = func() time.Time { return now }
	sc.EnqueueCast(SkillCastRequest{SkillID: "probe", TargetID: "boss"})
	sys.Update(world, 0)
	if sc.ChainScheduler.Pending() != 1 {
		t.Errorf("trigger should fire at low HP, pending=%d", sc.ChainScheduler.Pending())
	}
}
