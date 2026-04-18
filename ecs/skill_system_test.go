package ecs

import (
	"testing"
	"time"

	"engine/skill"
)

func newTestSkillRegistry() *skill.SkillRegistry {
	reg := skill.NewSkillRegistry()
	reg.Register(&skill.SkillDef{
		ID:         "fireball",
		Name:       "火球",
		Cooldown:   2 * time.Second,
		TargetType: skill.TargetSingle,
		Range:      30,
		Effects:    []string{"fire_dmg"},
	})
	reg.Register(&skill.SkillDef{
		ID:         "nova",
		Name:       "新星",
		Cooldown:   5 * time.Second,
		TargetType: skill.TargetAOE,
		AOERadius:  10,
		Effects:    []string{"fire_dmg"},
	})
	return reg
}

func newTestPipeline() *skill.EffectPipeline {
	p := skill.NewEffectPipeline()
	p.RegisterEffect(&skill.EffectDef{
		ID:     "fire_dmg",
		Type:   skill.EffectDamage,
		Value:  50,
		Chance: 1,
	})
	return p
}

func TestSkillSystem_CastSingleTarget(t *testing.T) {
	reg := newTestSkillRegistry()
	pipeline := newTestPipeline()

	caster := skill.NewSkillCaster("hero", reg)
	if err := caster.LearnSkill("fireball"); err != nil {
		t.Fatal(err)
	}

	world := NewWorld()
	hero := NewEntity("hero")
	hero.Add(&Position{X: 0, Y: 0})
	sc := NewSkillComponent(caster)
	sc.EnqueueCast(SkillCastRequest{SkillID: "fireball", TargetID: "mob1"})
	hero.Add(sc)
	world.Add(hero)

	now := time.Unix(1000, 0)
	sys := NewSkillCastSystem(100, pipeline)
	sys.Now = func() time.Time { return now }
	sys.Update(world, 16*time.Millisecond)

	if len(sc.PendingCasts) != 0 {
		t.Errorf("pending should be drained, got %d", len(sc.PendingCasts))
	}
	if len(sc.LastResults) != 1 {
		t.Fatalf("want 1 result, got %d", len(sc.LastResults))
	}
	r := sc.LastResults[0]
	if !r.Applied || r.Damage != 50 || r.TargetID != "mob1" {
		t.Errorf("unexpected result: %+v", r)
	}
}

func TestSkillSystem_CooldownBlocks(t *testing.T) {
	reg := newTestSkillRegistry()
	pipeline := newTestPipeline()
	caster := skill.NewSkillCaster("hero", reg)
	caster.LearnSkill("fireball")

	world := NewWorld()
	hero := NewEntity("hero")
	sc := NewSkillComponent(caster)
	hero.Add(sc)
	world.Add(hero)

	now := time.Unix(1000, 0)
	sys := NewSkillCastSystem(100, pipeline)
	sys.Now = func() time.Time { return now }

	// 第一次释放成功
	sc.EnqueueCast(SkillCastRequest{SkillID: "fireball", TargetID: "mob1"})
	sys.Update(world, 0)
	if len(sc.LastResults) == 0 || !sc.LastResults[0].Applied {
		t.Fatalf("first cast should apply: %+v", sc.LastResults)
	}

	// 立即第二次应被冷却拒绝
	sc.EnqueueCast(SkillCastRequest{SkillID: "fireball", TargetID: "mob1"})
	sys.Update(world, 0)
	if len(sc.LastResults) != 1 {
		t.Fatalf("want 1 result (blocked), got %d", len(sc.LastResults))
	}
	if sc.LastResults[0].Applied {
		t.Errorf("second cast should be blocked, got: %+v", sc.LastResults[0])
	}
}

func TestSkillSystem_AOE(t *testing.T) {
	reg := newTestSkillRegistry()
	pipeline := newTestPipeline()
	// 改为 AOE 效果
	pipeline.RegisterEffect(&skill.EffectDef{
		ID:     "fire_dmg",
		Type:   skill.EffectDamage,
		Value:  20,
		Chance: 1,
		AOE:    true,
		Radius: 10,
	})

	caster := skill.NewSkillCaster("hero", reg)
	caster.LearnSkill("nova")

	world := NewWorld()
	hero := NewEntity("hero")
	hero.Add(&Position{X: 0, Y: 0})
	sc := NewSkillComponent(caster)
	hero.Add(sc)
	world.Add(hero)

	sys := NewSkillCastSystem(100, pipeline)
	sys.Now = func() time.Time { return time.Unix(1000, 0) }
	sys.AOECandidates = func() []skill.TargetPosition {
		return []skill.TargetPosition{
			{ID: "mob1", X: 2, Y: 2},
			{ID: "mob2", X: 20, Y: 20},
			{ID: "mob3", X: -3, Y: 4},
		}
	}

	sc.EnqueueCast(SkillCastRequest{SkillID: "nova"})
	sys.Update(world, 0)

	if len(sc.LastResults) != 2 {
		t.Fatalf("want 2 AOE hits, got %d: %+v", len(sc.LastResults), sc.LastResults)
	}
	// 命中 mob1 和 mob3
	hit := map[string]bool{}
	for _, r := range sc.LastResults {
		hit[r.TargetID] = true
	}
	if !hit["mob1"] || !hit["mob3"] {
		t.Errorf("want mob1+mob3 hit, got %v", hit)
	}
}

func TestBuffSystem_TickAndExpire(t *testing.T) {
	world := NewWorld()
	hero := NewEntity("hero")
	hero.Add(&Health{Current: 100, Max: 100})
	bc := NewBuffComponent("hero")
	hero.Add(bc)
	world.Add(hero)

	def := &skill.BuffDef{
		ID:           "burn",
		Duration:     3 * time.Second,
		TickInterval: time.Second,
		TickDamage:   10,
		MaxStack:     1,
		StackPolicy:  skill.StackReplace,
	}

	t0 := time.Unix(1000, 0)
	if err := bc.Manager.Apply(def, "caster", t0); err != nil {
		t.Fatal(err)
	}

	sys := NewBuffTickSystem(200)
	// 固定时间源
	curr := t0
	sys.Now = func() time.Time { return curr }

	// t0: 不应 Tick（LastTick == t0，间隔 0）
	sys.Update(world, 0)
	if len(bc.LastTickResults) != 0 {
		t.Errorf("t0: no tick expected, got %d", len(bc.LastTickResults))
	}

	// 推进 1s，应 Tick 一次
	curr = t0.Add(time.Second)
	sys.Update(world, 0)
	if len(bc.LastTickResults) != 1 || bc.LastTickResults[0].Damage != 10 {
		t.Errorf("1s tick: want 1 damage=10, got %+v", bc.LastTickResults)
	}
	if hero.GetHealth().Current != 90 {
		t.Errorf("health want 90, got %d", hero.GetHealth().Current)
	}

	// 推进到 4s（超过 3s 过期）
	curr = t0.Add(4 * time.Second)
	sys.Update(world, 0)
	if bc.Manager.ActiveCount() != 0 {
		t.Errorf("buff should expire, got %d active", bc.Manager.ActiveCount())
	}
}

func TestBuffSystem_HealCallback(t *testing.T) {
	world := NewWorld()
	hero := NewEntity("hero")
	hero.Add(&Health{Current: 50, Max: 100})
	bc := NewBuffComponent("hero")
	hero.Add(bc)
	world.Add(hero)

	def := &skill.BuffDef{
		ID:           "regen",
		Duration:     5 * time.Second,
		TickInterval: time.Second,
		TickHeal:     15,
		MaxStack:     1,
		StackPolicy:  skill.StackReplace,
	}
	t0 := time.Unix(2000, 0)
	bc.Manager.Apply(def, "priest", t0)

	healed := 0
	sys := NewBuffTickSystem(200)
	sys.Now = func() time.Time { return t0.Add(time.Second) }
	sys.ApplyHeal = func(ownerID string, heal float32) {
		healed += int(heal)
	}
	sys.Update(world, 0)
	if healed != 15 {
		t.Errorf("callback heal want 15, got %d", healed)
	}
	// 回调接管后默认行为不应再改动 Health
	if hero.GetHealth().Current != 50 {
		t.Errorf("health should stay 50 when callback used, got %d", hero.GetHealth().Current)
	}
}
