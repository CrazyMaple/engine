package ecs

import (
	"testing"
	"time"

	"engine/bt"
)

func TestAIComponent_ShouldTick(t *testing.T) {
	tree := bt.NewTree(bt.Action("noop", func(bb *bt.Blackboard) bt.Status {
		return bt.Success
	}))
	ai := NewAIComponent(tree, "test_tree")

	// LOD 0 = 每帧 Tick
	ai.LODLevel = 0
	for i := 0; i < 5; i++ {
		if !ai.ShouldTick() {
			t.Errorf("LOD 0: should tick every frame, failed at frame %d", i)
		}
	}

	// LOD 1 = 每 2 帧
	ai.LODLevel = 1
	ai.TickAccumulator = 0
	results := make([]bool, 4)
	for i := range results {
		results[i] = ai.ShouldTick()
	}
	// 应该是 false, true, false, true
	if results[0] || !results[1] || results[2] || !results[3] {
		t.Errorf("LOD 1: expected [false,true,false,true], got %v", results)
	}

	// LOD 3 = 暂停
	ai.LODLevel = 3
	ai.TickAccumulator = 0
	for i := 0; i < 10; i++ {
		if ai.ShouldTick() {
			t.Error("LOD 3: should never tick")
		}
	}

	// Disabled
	ai.LODLevel = 0
	ai.Enabled = false
	ai.TickAccumulator = 0
	if ai.ShouldTick() {
		t.Error("disabled AI should not tick")
	}
}

func TestAISystem_Update(t *testing.T) {
	tickCount := 0
	tree := bt.NewTree(bt.Action("count", func(bb *bt.Blackboard) bt.Status {
		tickCount++
		return bt.Success
	}))

	world := NewWorld()
	e := NewEntity("npc1")
	e.Add(&Position{X: 10, Y: 10})
	e.Add(&Health{Current: 100, Max: 100})
	ai := NewAIComponent(tree, "counter")
	e.Add(ai)
	world.Add(e)

	sys := NewAISystem(10)
	sys.LODEnabled = false // 关闭 LOD 以测试基础功能

	dt := 16 * time.Millisecond
	sys.Update(world, dt)

	if tickCount != 1 {
		t.Errorf("expected 1 tick, got %d", tickCount)
	}
	if ai.TickCount != 1 {
		t.Errorf("expected TickCount=1, got %d", ai.TickCount)
	}
	if ai.LastStatus != bt.Success {
		t.Errorf("expected Success, got %s", ai.LastStatus)
	}

	// 验证黑板数据
	bb := tree.Blackboard()
	if id, _ := bb.GetString("entity_id"); id != "npc1" {
		t.Errorf("expected entity_id=npc1, got %s", id)
	}
}

func TestAISystem_LOD(t *testing.T) {
	tickCount := 0
	tree := bt.NewTree(bt.Action("count", func(bb *bt.Blackboard) bt.Status {
		tickCount++
		return bt.Success
	}))

	world := NewWorld()
	e := NewEntity("far_npc")
	e.Add(&Position{X: 1000, Y: 1000}) // 远距
	e.Add(NewAIComponent(tree, "counter"))
	world.Add(e)

	sys := NewAISystem(10)
	sys.LODEnabled = true
	sys.LODNearRange = 100
	sys.LODMidRange = 300
	sys.LODFarRange = 600
	sys.AddObserver(0, 0) // 观察者在原点

	dt := 16 * time.Millisecond

	// 距离 ~1414 > FarRange(600)，LOD=3(暂停)
	for i := 0; i < 10; i++ {
		sys.Update(world, dt)
	}
	if tickCount != 0 {
		t.Errorf("far NPC should not tick, got %d ticks", tickCount)
	}

	// 移到近距
	pos := e.GetPosition()
	pos.X = 50
	pos.Y = 50
	tickCount = 0
	comp, _ := e.Get("AI")
	ai := comp.(*AIComponent)
	ai.TickAccumulator = 0

	for i := 0; i < 5; i++ {
		sys.Update(world, dt)
	}
	if tickCount != 5 {
		t.Errorf("near NPC should tick every frame, got %d ticks in 5 frames", tickCount)
	}
}

func TestAISystem_MultipleEntities(t *testing.T) {
	ticks := map[string]int{}

	makeTree := func(id string) *bt.Tree {
		return bt.NewTree(bt.Action("count_"+id, func(bb *bt.Blackboard) bt.Status {
			eid, _ := bb.GetString("entity_id")
			ticks[eid]++
			return bt.Success
		}))
	}

	world := NewWorld()
	for _, id := range []string{"npc1", "npc2", "npc3"} {
		e := NewEntity(id)
		e.Add(&Position{X: 10, Y: 10})
		e.Add(NewAIComponent(makeTree(id), "tree_"+id))
		world.Add(e)
	}

	sys := NewAISystem(10)
	sys.LODEnabled = false
	dt := 16 * time.Millisecond

	sys.Update(world, dt)

	for _, id := range []string{"npc1", "npc2", "npc3"} {
		if ticks[id] != 1 {
			t.Errorf("entity %s: expected 1 tick, got %d", id, ticks[id])
		}
	}
}
