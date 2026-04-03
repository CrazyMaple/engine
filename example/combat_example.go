//go:build ignore

package main

import (
	"engine/ecs"
	"fmt"
	"time"
)

func getAtk(e *ecs.Entity) *ecs.Attack {
	c, _ := e.Get("Attack")
	return c.(*ecs.Attack)
}

func getDef(e *ecs.Entity) *ecs.Defense {
	c, _ := e.Get("Defense")
	return c.(*ecs.Defense)
}

// combat_example 演示 ECS 战斗/技能框架
// 运行方式：go run example/combat_example.go
func main() {
	fmt.Println("=== ECS 战斗框架示例 ===")
	fmt.Println()

	// --- 1. 创建 ECS 世界和系统 ---
	world := ecs.NewWorld()
	systems := ecs.NewSystemGroup()
	systems.Add(&ecs.BuffSystem{}, &ecs.SkillSystem{})

	// --- 2. 创建战士实体 ---
	warrior := ecs.NewEntity("warrior")
	warrior.Add(&ecs.Position{X: 10, Y: 10})
	warrior.Add(&ecs.Health{Current: 500, Max: 500})
	warrior.Add(&ecs.Attack{Damage: 80, CritRate: 0.3, CritMulti: 2.0, HitRate: 0.95})
	warrior.Add(&ecs.Defense{Armor: 50, DodgeRate: 0.1})
	warrior.Add(&ecs.SkillState{Skills: []ecs.Skill{
		{
			ID: "heavy_slash", Name: "重斩",
			Cooldown: 3 * time.Second, CastTime: 500 * time.Millisecond, BackSwing: 200 * time.Millisecond,
			Phase: ecs.SkillPhaseIdle,
		},
	}})
	warrior.Add(&ecs.Buff{})
	world.Add(warrior)

	// --- 3. 创建法师实体 ---
	mage := ecs.NewEntity("mage")
	mage.Add(&ecs.Position{X: 20, Y: 10})
	mage.Add(&ecs.Health{Current: 300, Max: 300})
	mage.Add(&ecs.Attack{Damage: 120, CritRate: 0.2, CritMulti: 2.5, HitRate: 0.9})
	mage.Add(&ecs.Defense{Armor: 20, DodgeRate: 0.15})
	mage.Add(&ecs.SkillState{Skills: []ecs.Skill{
		{
			ID: "fireball", Name: "火球术",
			Cooldown: 2 * time.Second, CastTime: 800 * time.Millisecond, BackSwing: 100 * time.Millisecond,
			Phase: ecs.SkillPhaseIdle,
		},
	}})
	mage.Add(&ecs.Buff{})
	world.Add(mage)

	fmt.Printf("战士: HP=%d/%d, ATK=%.0f, DEF=%.0f\n",
		warrior.GetHealth().Current, warrior.GetHealth().Max,
		getAtk(warrior).Damage,
		getDef(warrior).Armor)

	fmt.Printf("法师: HP=%d/%d, ATK=%.0f, DEF=%.0f\n",
		mage.GetHealth().Current, mage.GetHealth().Max,
		getAtk(mage).Damage,
		getDef(mage).Armor)
	fmt.Println()

	// --- 4. 伤害计算管线演示 ---
	fmt.Println("--- 伤害计算 ---")
	pipeline := ecs.NewDamagePipeline()

	// 战士攻击法师
	dmgCtx := pipeline.Calculate(warrior, mage)
	fmt.Printf("战士 → 法师: 命中=%v, 暴击=%v, 伤害=%.1f (阶段: %v)\n",
		dmgCtx.IsHit, dmgCtx.IsCrit, dmgCtx.FinalDamage, dmgCtx.Stages)

	// 应用伤害
	mageHP := mage.GetHealth()
	mageHP.Current -= int(dmgCtx.FinalDamage)
	fmt.Printf("法师 HP: %d/%d\n", mageHP.Current, mageHP.Max)
	fmt.Println()

	// --- 5. Buff/DOT 演示 ---
	fmt.Println("--- Buff 系统 ---")
	mageBuff, _ := mage.Get("Buff")
	mageBuff.(*ecs.Buff).AddEffect(ecs.BuffEffect{
		ID: "burning", Type: ecs.BuffDOT, Value: 15,
		Duration: 3 * time.Second, StartTime: time.Now(),
	})
	fmt.Println("法师被施加燃烧 DOT (15 DPS, 3秒)")

	// 模拟 3 帧
	for i := 0; i < 3; i++ {
		systems.Update(world, time.Second)
		fmt.Printf("  第%d秒: 法师 HP=%d\n", i+1, mage.GetHealth().Current)
	}
	fmt.Println()

	// --- 6. 技能时间线演示 ---
	fmt.Println("--- 技能时间线 ---")
	now := time.Now()
	ss, _ := warrior.Get("SkillState")
	skill := ss.(*ecs.SkillState).GetSkill("heavy_slash")

	ok := ecs.CastSkill(skill, now)
	fmt.Printf("战士释放重斩: %v (阶段: 前摇)\n", ok)

	// 模拟技能推进
	phases := []string{"空闲", "前摇", "释放", "后摇", "冷却"}
	for i := 0; i < 8; i++ {
		systems.Update(world, 500*time.Millisecond)
		fmt.Printf("  +%dms: 阶段=%s\n", (i+1)*500, phases[skill.Phase])
	}
	fmt.Println()

	// --- 7. 帧驱动器演示 ---
	fmt.Println("--- 帧驱动器 (20 FPS) ---")
	ticker := ecs.NewTicker(world, systems, 20)
	for i := 0; i < 5; i++ {
		ticker.TickOnce(50 * time.Millisecond)
	}
	fmt.Printf("执行了 %d 帧\n", ticker.FrameCount())
	fmt.Println()

	fmt.Println("=== 示例结束 ===")
}
