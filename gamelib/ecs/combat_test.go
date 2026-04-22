package ecs

import (
	"testing"
	"time"
)

func TestDamagePipelineBasic(t *testing.T) {
	attacker := NewEntity("attacker")
	attacker.Add(&Attack{Damage: 100, CritRate: 0, CritMulti: 2, HitRate: 1.0})

	defender := NewEntity("defender")
	defender.Add(&Defense{Armor: 100, DodgeRate: 0})
	defender.Add(&Health{Current: 1000, Max: 1000})

	pipeline := NewDamagePipeline()
	ctx := pipeline.Calculate(attacker, defender)

	if !ctx.IsHit {
		t.Error("expected hit")
	}
	if ctx.IsCrit {
		t.Error("expected no crit")
	}
	// 100 * (100 / (100 + 100)) = 50
	if ctx.FinalDamage != 50 {
		t.Errorf("expected 50 damage, got %.2f", ctx.FinalDamage)
	}
	if len(ctx.Stages) != 4 {
		t.Errorf("expected 4 stages, got %d", len(ctx.Stages))
	}
}

func TestDamagePipelineCrit(t *testing.T) {
	attacker := NewEntity("a")
	attacker.Add(&Attack{Damage: 100, CritRate: 1.0, CritMulti: 2.5, HitRate: 1.0})

	defender := NewEntity("d")
	defender.Add(&Defense{Armor: 0, DodgeRate: 0})

	pipeline := &DamagePipeline{
		stages: []DamageStage{
			&HitCheckStage{RandFunc: func() float32 { return 0.0 }},
			&CritCheckStage{RandFunc: func() float32 { return 0.0 }}, // always crit
			&ArmorReductionStage{},
			&FinalDamageStage{},
		},
	}

	ctx := pipeline.Calculate(attacker, defender)
	if !ctx.IsCrit {
		t.Error("expected crit")
	}
	// 100 * 2.5 = 250, no armor
	if ctx.FinalDamage != 250 {
		t.Errorf("expected 250, got %.2f", ctx.FinalDamage)
	}
}

func TestDamagePipelineDodge(t *testing.T) {
	attacker := NewEntity("a")
	attacker.Add(&Attack{Damage: 100, HitRate: 0.5})

	defender := NewEntity("d")
	defender.Add(&Defense{DodgeRate: 0.3})

	pipeline := &DamagePipeline{
		stages: []DamageStage{
			&HitCheckStage{RandFunc: func() float32 { return 0.9 }}, // miss
			&CritCheckStage{},
			&ArmorReductionStage{},
			&FinalDamageStage{},
		},
	}

	ctx := pipeline.Calculate(attacker, defender)
	if ctx.IsHit {
		t.Error("expected miss")
	}
	if !ctx.IsDodged {
		t.Error("expected dodge")
	}
	if ctx.FinalDamage != 0 {
		t.Error("expected 0 damage on dodge")
	}
}

func TestDamagePipelineCustomStage(t *testing.T) {
	attacker := NewEntity("a")
	attacker.Add(&Attack{Damage: 100, HitRate: 1.0})

	defender := NewEntity("d")
	defender.Add(&Defense{Armor: 0})

	pipeline := NewDamagePipeline()
	// 添加自定义阶段：固定伤害加成
	pipeline.AddStage(&flatBonusStage{bonus: 50})

	ctx := pipeline.Calculate(attacker, defender)
	if ctx.FinalDamage != 150 {
		t.Errorf("expected 150 with bonus, got %.2f", ctx.FinalDamage)
	}
}

type flatBonusStage struct {
	bonus float32
}

func (s *flatBonusStage) Name() string { return "FlatBonus" }
func (s *flatBonusStage) Process(ctx *DamageContext) {
	ctx.BaseDamage += s.bonus
}

func TestBuffDOT(t *testing.T) {
	world := NewWorld()
	e := NewEntity("target")
	e.Add(&Health{Current: 100, Max: 100})
	e.Add(&Buff{
		Effects: []BuffEffect{
			{
				ID: "poison", Type: BuffDOT, Value: 10, // 10 DPS
				Duration: 5 * time.Second, StartTime: time.Now(),
			},
		},
	})
	world.Add(e)

	bs := &BuffSystem{now: time.Now()}
	// 模拟 1 秒
	bs.Update(world, time.Second)

	h := e.GetHealth()
	if h.Current != 90 {
		t.Errorf("expected 90 health after 1s DOT, got %d", h.Current)
	}
}

func TestBuffHOT(t *testing.T) {
	world := NewWorld()
	e := NewEntity("target")
	e.Add(&Health{Current: 50, Max: 100})
	e.Add(&Buff{
		Effects: []BuffEffect{
			{
				ID: "regen", Type: BuffHOT, Value: 20, // 20 HPS
				Duration: 10 * time.Second, StartTime: time.Now(),
			},
		},
	})
	world.Add(e)

	bs := &BuffSystem{now: time.Now()}
	bs.Update(world, time.Second)

	h := e.GetHealth()
	if h.Current != 70 {
		t.Errorf("expected 70 health after 1s HOT, got %d", h.Current)
	}
}

func TestBuffExpire(t *testing.T) {
	now := time.Now()
	b := &Buff{
		Effects: []BuffEffect{
			{ID: "short", Duration: time.Second, StartTime: now},
			{ID: "long", Duration: time.Hour, StartTime: now},
			{ID: "perm", Duration: 0, StartTime: now}, // permanent
		},
	}

	expired := b.RemoveExpired(now.Add(2 * time.Second))
	if len(expired) != 1 || expired[0].ID != "short" {
		t.Errorf("expected 'short' expired, got %v", expired)
	}
	if len(b.Effects) != 2 {
		t.Errorf("expected 2 remaining, got %d", len(b.Effects))
	}
}

func TestSkillTimeline(t *testing.T) {
	now := time.Now()
	skill := Skill{
		ID:        "fireball",
		Name:      "Fireball",
		Cooldown:  2 * time.Second,
		CastTime:  500 * time.Millisecond,
		BackSwing: 300 * time.Millisecond,
		Phase:     SkillPhaseIdle,
	}

	// 释放技能
	ok := CastSkill(&skill, now)
	if !ok {
		t.Fatal("expected cast success")
	}
	if skill.Phase != SkillPhaseCasting {
		t.Error("expected casting phase")
	}

	// 创建 SkillSystem 模拟推进
	world := NewWorld()
	e := NewEntity("caster")
	e.Add(&SkillState{Skills: []Skill{skill}})
	world.Add(e)

	ss := &SkillSystem{now: now}

	// 推进 500ms → casting 完成 → active
	ss.Update(world, 500*time.Millisecond)
	sc, _ := e.Get("SkillState")
	s := sc.(*SkillState).GetSkill("fireball")
	if s.Phase != SkillPhaseActive {
		t.Errorf("expected Active after 500ms, got %d", s.Phase)
	}

	// 再推进一帧 → active → backswing
	ss.Update(world, 10*time.Millisecond)
	if s.Phase != SkillPhaseBackSwing {
		t.Errorf("expected BackSwing, got %d", s.Phase)
	}

	// 推进 300ms → backswing 完成 → cooldown
	ss.Update(world, 300*time.Millisecond)
	if s.Phase != SkillPhaseCooldown {
		t.Errorf("expected Cooldown, got %d", s.Phase)
	}

	// 推进 2s → cooldown 完成 → idle
	ss.Update(world, 2*time.Second)
	if s.Phase != SkillPhaseIdle {
		t.Errorf("expected Idle after cooldown, got %d", s.Phase)
	}
}

func TestSkillNotReady(t *testing.T) {
	now := time.Now()
	skill := Skill{
		ID:       "slash",
		Cooldown: time.Second,
		Phase:    SkillPhaseCasting, // currently casting
	}

	ok := CastSkill(&skill, now)
	if ok {
		t.Error("should not cast while already casting")
	}
}

func TestBuffSystemWithSkillSystem(t *testing.T) {
	world := NewWorld()
	sg := NewSystemGroup()

	bs := &BuffSystem{now: time.Now()}
	ss := &SkillSystem{now: time.Now()}
	sg.Add(bs, ss)

	e := NewEntity("warrior")
	e.Add(&Health{Current: 100, Max: 100})
	e.Add(&Buff{Effects: []BuffEffect{
		{ID: "bleed", Type: BuffDOT, Value: 5, Duration: 3 * time.Second, StartTime: time.Now()},
	}})
	e.Add(&SkillState{Skills: []Skill{
		{ID: "heal", Cooldown: time.Second, CastTime: 0, BackSwing: 0, Phase: SkillPhaseIdle},
	}})
	world.Add(e)

	// 1 秒后 bleed 扣血
	sg.Update(world, time.Second)
	h := e.GetHealth()
	if h.Current != 95 {
		t.Errorf("expected 95 after 1s DOT(5dps), got %d", h.Current)
	}
}
