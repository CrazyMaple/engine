package skill

import (
	"testing"
	"time"
)

func TestCooldownManager(t *testing.T) {
	cm := NewCooldownManager()
	now := time.Now()

	// 初始无冷却
	if cm.IsOnCooldown("skill1", now) {
		t.Error("should not be on cooldown initially")
	}

	// 开始冷却
	cm.StartCooldown("skill1", 5*time.Second, now)
	if !cm.IsOnCooldown("skill1", now.Add(2*time.Second)) {
		t.Error("should be on cooldown at 2s")
	}
	if cm.IsOnCooldown("skill1", now.Add(6*time.Second)) {
		t.Error("should not be on cooldown at 6s")
	}

	// 剩余时间
	rem := cm.Remaining("skill1", now.Add(3*time.Second))
	if rem < 1*time.Second || rem > 3*time.Second {
		t.Errorf("expected ~2s remaining, got %v", rem)
	}

	// 全局 CD
	cm.StartGlobalCD(1*time.Second, now)
	if !cm.IsGlobalCD(now.Add(500 * time.Millisecond)) {
		t.Error("should be in global CD")
	}
	if cm.IsGlobalCD(now.Add(2 * time.Second)) {
		t.Error("global CD should have expired")
	}

	// 清除冷却
	cm.ClearCooldown("skill1")
	if cm.IsOnCooldown("skill1", now) {
		t.Error("cooldown should be cleared")
	}

	// 缩短冷却
	cm.StartCooldown("skill2", 10*time.Second, now)
	cm.ReduceCooldown("skill2", 5*time.Second)
	if cm.IsOnCooldown("skill2", now.Add(6*time.Second)) {
		t.Error("reduced cooldown should have expired at 6s")
	}
}

func TestSkillCaster(t *testing.T) {
	registry := NewSkillRegistry()
	registry.Register(&SkillDef{
		ID:       "fireball",
		Name:     "火球术",
		Cooldown: 3 * time.Second,
		GlobalCD: 500 * time.Millisecond,
		Effects:  []string{"damage_fire"},
	})
	registry.Register(&SkillDef{
		ID:       "heal",
		Name:     "治疗术",
		Cooldown: 5 * time.Second,
	})

	caster := NewSkillCaster("player1", registry)

	// 学习技能
	if err := caster.LearnSkill("fireball"); err != nil {
		t.Fatal(err)
	}
	if err := caster.LearnSkill("heal"); err != nil {
		t.Fatal(err)
	}

	now := time.Now()

	// 释放火球
	def, err := caster.CastSkill("fireball", now)
	if err != nil {
		t.Fatal(err)
	}
	if def.ID != "fireball" {
		t.Errorf("expected fireball, got %s", def.ID)
	}

	// 冷却中不能再放
	_, err = caster.CastSkill("fireball", now.Add(1*time.Second))
	if err == nil {
		t.Error("should not cast during cooldown")
	}

	// 全局 CD 影响其他技能
	_, err = caster.CastSkill("heal", now.Add(100*time.Millisecond))
	if err == nil {
		t.Error("should not cast during global CD")
	}

	// 全局 CD 过后可以释放其他技能
	_, err = caster.CastSkill("heal", now.Add(1*time.Second))
	if err != nil {
		t.Errorf("heal should be castable after global CD: %v", err)
	}

	// 冷却结束可以再放
	_, err = caster.CastSkill("fireball", now.Add(4*time.Second))
	if err != nil {
		t.Errorf("fireball should be castable after cooldown: %v", err)
	}

	// 遗忘技能
	caster.ForgetSkill("fireball")
	_, err = caster.CastSkill("fireball", now.Add(10*time.Second))
	if err == nil {
		t.Error("should not cast forgotten skill")
	}

	// 学习不存在的技能
	if err := caster.LearnSkill("nonexistent"); err == nil {
		t.Error("should error for nonexistent skill")
	}
}

func TestSkillRegistry(t *testing.T) {
	r := NewSkillRegistry()
	r.Register(&SkillDef{ID: "s1", Name: "Skill 1"})
	r.Register(&SkillDef{ID: "s2", Name: "Skill 2"})

	if _, ok := r.Get("s1"); !ok {
		t.Error("s1 should exist")
	}
	if _, ok := r.Get("s3"); ok {
		t.Error("s3 should not exist")
	}
	if len(r.All()) != 2 {
		t.Errorf("expected 2 skills, got %d", len(r.All()))
	}
}

func TestBuffManager(t *testing.T) {
	bm := NewBuffManager("player1")
	now := time.Now()

	// 施加 Buff
	atkBuff := &BuffDef{
		ID:          "atk_up",
		Name:        "攻击提升",
		Category:    BuffCategoryBuff,
		Duration:    10 * time.Second,
		MaxStack:    3,
		StackPolicy: StackAdd,
		Modifiers: []AttributeModifier{
			{Attribute: "attack", AddValue: 10},
		},
	}

	if err := bm.Apply(atkBuff, "source1", now); err != nil {
		t.Fatal(err)
	}
	if !bm.HasBuff("atk_up") {
		t.Error("should have atk_up buff")
	}

	// 叠加
	bm.Apply(atkBuff, "source1", now)
	if bm.ActiveCount() != 1 {
		t.Error("StackAdd should not create new instance")
	}

	// 属性修改
	mods := bm.GetModifiers(now)
	if mod, ok := mods["attack"]; !ok || mod.AddValue != 20 { // 2 stacks * 10
		t.Errorf("expected attack +20, got %+v", mods)
	}

	// DOT Buff
	dotBuff := &BuffDef{
		ID:           "poison",
		Category:     BuffCategoryDebuff,
		Duration:     5 * time.Second,
		MaxStack:     1,
		StackPolicy:  StackReplace,
		TickInterval: 1 * time.Second,
		TickDamage:   10,
	}
	bm.Apply(dotBuff, "enemy1", now)

	results := bm.Tick(now.Add(1500 * time.Millisecond))
	hasDot := false
	for _, r := range results {
		if r.BuffID == "poison" && r.Damage > 0 {
			hasDot = true
		}
	}
	if !hasDot {
		t.Error("poison DOT should have ticked")
	}

	// 过期清理
	results = bm.Tick(now.Add(6 * time.Second))
	if bm.HasBuff("poison") {
		t.Error("poison should have expired")
	}

	// 移除 Debuff
	bm.Apply(dotBuff, "enemy1", now.Add(6*time.Second))
	removed := bm.RemoveByCategory(BuffCategoryDebuff)
	if removed != 1 {
		t.Errorf("expected 1 debuff removed, got %d", removed)
	}
}

func TestBuffManager_MutexGroup(t *testing.T) {
	bm := NewBuffManager("player1")
	now := time.Now()

	lowPrio := &BuffDef{
		ID:          "shield_weak",
		MutexGroup:  "shield",
		Priority:    1,
		Duration:    10 * time.Second,
		StackPolicy: StackReplace,
	}
	highPrio := &BuffDef{
		ID:          "shield_strong",
		MutexGroup:  "shield",
		Priority:    5,
		Duration:    10 * time.Second,
		StackPolicy: StackReplace,
	}

	bm.Apply(lowPrio, "src", now)
	if !bm.HasBuff("shield_weak") {
		t.Error("should have weak shield")
	}

	// 高优先级替换低优先级
	bm.Apply(highPrio, "src", now)
	if bm.HasBuff("shield_weak") {
		t.Error("weak shield should be replaced")
	}
	if !bm.HasBuff("shield_strong") {
		t.Error("should have strong shield")
	}

	// 低优先级被阻挡
	err := bm.Apply(lowPrio, "src", now)
	if err == nil {
		t.Error("low priority should be blocked by high priority in same mutex group")
	}
}

func TestBuffManager_StackReject(t *testing.T) {
	bm := NewBuffManager("player1")
	now := time.Now()

	def := &BuffDef{
		ID:          "unique_buff",
		Duration:    10 * time.Second,
		StackPolicy: StackReject,
	}

	bm.Apply(def, "src", now)
	err := bm.Apply(def, "src", now)
	if err == nil {
		t.Error("StackReject should prevent re-application")
	}
}

func TestEffectPipeline(t *testing.T) {
	p := NewEffectPipeline()

	p.RegisterEffect(&EffectDef{
		ID:    "fire_damage",
		Type:  EffectDamage,
		Value: 50,
		Ratio: 1.5,
		Chance: 1.0,
	})
	p.RegisterEffect(&EffectDef{
		ID:     "burn",
		Type:   EffectApplyBuff,
		BuffID: "burning",
		Chance: 1.0,
	})

	ctx := &EffectContext{
		CasterID:  "player1",
		TargetID:  "monster1",
		CasterAtk: 100,
	}

	results, err := p.Execute([]string{"fire_damage", "burn"}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// 伤害 = 50 + 100*1.5 = 200
	if results[0].Damage != 200 {
		t.Errorf("expected damage 200, got %f", results[0].Damage)
	}
	if !results[0].Applied {
		t.Error("damage should be applied")
	}

	// Buff 施加
	if results[1].BuffID != "burning" {
		t.Errorf("expected buff burning, got %s", results[1].BuffID)
	}
}

func TestEffectPipeline_AOE(t *testing.T) {
	p := NewEffectPipeline()
	p.RegisterEffect(&EffectDef{
		ID:     "aoe_blast",
		Type:   EffectDamage,
		Value:  30,
		Chance: 1.0,
		AOE:    true,
		Radius: 50,
	})

	ctx := &EffectContext{
		CasterID:  "player1",
		TargetIDs: []string{"m1", "m2", "m3"},
	}

	results, err := p.Execute([]string{"aoe_blast"}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 AOE targets, got %d", len(results))
	}
}

func TestEffectPipeline_ChanceFail(t *testing.T) {
	p := NewEffectPipeline()
	p.RegisterEffect(&EffectDef{
		ID:     "lucky_strike",
		Type:   EffectDamage,
		Value:  100,
		Chance: 0.5,
	})

	ctx := &EffectContext{
		CasterID: "p1",
		TargetID: "m1",
		RandFunc: func() float32 { return 0.9 }, // > 0.5, 不触发
	}

	results, _ := p.Execute([]string{"lucky_strike"}, ctx)
	if results[0].Applied {
		t.Error("should not apply with high roll")
	}

	ctx.RandFunc = func() float32 { return 0.3 } // < 0.5, 触发
	results, _ = p.Execute([]string{"lucky_strike"}, ctx)
	if !results[0].Applied {
		t.Error("should apply with low roll")
	}
}

func TestFindTargetsInRadius(t *testing.T) {
	targets := []TargetPosition{
		{ID: "a", X: 10, Y: 10},
		{ID: "b", X: 50, Y: 50},
		{ID: "c", X: 200, Y: 200},
	}

	found := FindTargetsInRadius(0, 0, 100, targets)
	if len(found) != 2 {
		t.Errorf("expected 2 targets in radius 100, got %d", len(found))
	}
}
