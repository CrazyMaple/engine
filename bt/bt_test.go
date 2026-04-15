package bt

import (
	"testing"
)

func TestSequence_AllSuccess(t *testing.T) {
	callOrder := []string{}
	tree := NewTree(
		Sequence(
			Action("A", func(bb *Blackboard) Status { callOrder = append(callOrder, "A"); return Success }),
			Action("B", func(bb *Blackboard) Status { callOrder = append(callOrder, "B"); return Success }),
			Action("C", func(bb *Blackboard) Status { callOrder = append(callOrder, "C"); return Success }),
		),
	)

	status := tree.Tick()
	if status != Success {
		t.Errorf("expected Success, got %v", status)
	}
	if len(callOrder) != 3 {
		t.Errorf("expected 3 calls, got %d", len(callOrder))
	}
}

func TestSequence_FailEarly(t *testing.T) {
	callOrder := []string{}
	tree := NewTree(
		Sequence(
			Action("A", func(bb *Blackboard) Status { callOrder = append(callOrder, "A"); return Success }),
			Action("B", func(bb *Blackboard) Status { callOrder = append(callOrder, "B"); return Failure }),
			Action("C", func(bb *Blackboard) Status { callOrder = append(callOrder, "C"); return Success }),
		),
	)

	status := tree.Tick()
	if status != Failure {
		t.Errorf("expected Failure, got %v", status)
	}
	if len(callOrder) != 2 {
		t.Errorf("expected 2 calls, got %d: %v", len(callOrder), callOrder)
	}
}

func TestSelector_FirstSuccess(t *testing.T) {
	callOrder := []string{}
	tree := NewTree(
		Selector(
			Action("A", func(bb *Blackboard) Status { callOrder = append(callOrder, "A"); return Failure }),
			Action("B", func(bb *Blackboard) Status { callOrder = append(callOrder, "B"); return Success }),
			Action("C", func(bb *Blackboard) Status { callOrder = append(callOrder, "C"); return Success }),
		),
	)

	status := tree.Tick()
	if status != Success {
		t.Errorf("expected Success, got %v", status)
	}
	if len(callOrder) != 2 {
		t.Errorf("expected 2 calls, got %d", len(callOrder))
	}
}

func TestSelector_AllFail(t *testing.T) {
	tree := NewTree(
		Selector(
			Action("A", func(bb *Blackboard) Status { return Failure }),
			Action("B", func(bb *Blackboard) Status { return Failure }),
		),
	)

	status := tree.Tick()
	if status != Failure {
		t.Errorf("expected Failure, got %v", status)
	}
}

func TestSequence_Running(t *testing.T) {
	callCount := 0
	tree := NewTree(
		Sequence(
			Action("A", func(bb *Blackboard) Status { return Success }),
			Action("B", func(bb *Blackboard) Status {
				callCount++
				if callCount < 3 {
					return Running
				}
				return Success
			}),
			Action("C", func(bb *Blackboard) Status { return Success }),
		),
	)

	// 前两次 Tick 应该 Running
	for i := 0; i < 2; i++ {
		status := tree.Tick()
		if status != Running {
			t.Errorf("tick %d: expected Running, got %v", i, status)
		}
	}
	// 第三次应该 Success
	status := tree.Tick()
	if status != Success {
		t.Errorf("expected Success, got %v", status)
	}
}

func TestCondition(t *testing.T) {
	tree := NewTree(
		Sequence(
			Condition("HasTarget", func(bb *Blackboard) bool {
				return bb.Has("target")
			}),
			Action("Attack", func(bb *Blackboard) Status { return Success }),
		),
	)

	// 没有 target，应该 Failure
	status := tree.Tick()
	if status != Failure {
		t.Errorf("expected Failure, got %v", status)
	}

	// 设置 target
	tree.Blackboard().Set("target", "enemy1")
	status = tree.Tick()
	if status != Success {
		t.Errorf("expected Success, got %v", status)
	}
}

func TestInverter(t *testing.T) {
	tree := NewTree(Inverter(Action("Fail", func(bb *Blackboard) Status { return Failure })))
	if tree.Tick() != Success {
		t.Error("inverter should turn Failure into Success")
	}

	tree2 := NewTree(Inverter(Action("Ok", func(bb *Blackboard) Status { return Success })))
	if tree2.Tick() != Failure {
		t.Error("inverter should turn Success into Failure")
	}
}

func TestRepeater(t *testing.T) {
	count := 0
	tree := NewTree(Repeater(3, Action("Count", func(bb *Blackboard) Status {
		count++
		return Success
	})))

	// 每次 Tick 执行一次子节点，前两次返回 Running
	tree.Tick() // count=1, Running
	tree.Tick() // count=2, Running
	status := tree.Tick()
	if status != Success {
		t.Errorf("expected Success after 3 repeats, got %v", status)
	}
	if count != 3 {
		t.Errorf("expected 3 executions, got %d", count)
	}
}

func TestLimiter(t *testing.T) {
	count := 0
	node := Limiter(2, Action("Inc", func(bb *Blackboard) Status {
		count++
		return Success
	}))
	bb := NewBlackboard()

	node.Tick(bb) // count=1
	node.Tick(bb) // count=2
	status := node.Tick(bb)

	if status != Failure {
		t.Errorf("expected Failure after limit, got %v", status)
	}
	if count != 2 {
		t.Errorf("expected 2 executions, got %d", count)
	}
}

func TestParallel_RequireAll(t *testing.T) {
	node := Parallel(RequireAll,
		Action("A", func(bb *Blackboard) Status { return Success }),
		Action("B", func(bb *Blackboard) Status { return Success }),
	)
	bb := NewBlackboard()

	if node.Tick(bb) != Success {
		t.Error("RequireAll: all success should return Success")
	}
}

func TestParallel_RequireOne(t *testing.T) {
	node := Parallel(RequireOne,
		Action("A", func(bb *Blackboard) Status { return Failure }),
		Action("B", func(bb *Blackboard) Status { return Success }),
	)
	bb := NewBlackboard()

	if node.Tick(bb) != Success {
		t.Error("RequireOne: one success should return Success")
	}
}

func TestBlackboard(t *testing.T) {
	bb := NewBlackboard()

	bb.Set("hp", 100)
	if v, ok := bb.GetInt("hp"); !ok || v != 100 {
		t.Errorf("expected hp=100, got %v", v)
	}

	bb.Set("name", "npc1")
	if v, ok := bb.GetString("name"); !ok || v != "npc1" {
		t.Errorf("expected name=npc1, got %v", v)
	}

	if !bb.Has("hp") {
		t.Error("expected Has(hp)=true")
	}

	bb.Delete("hp")
	if bb.Has("hp") {
		t.Error("expected Has(hp)=false after delete")
	}
}

func TestNPCBehaviorTree(t *testing.T) {
	// 模拟需求文档中的 NPC 行为树示例
	attackCount := 0
	fleeCount := 0
	patrolCount := 0

	checkRange := func(bb *Blackboard) bool {
		dist, ok := bb.GetFloat64("enemy_distance")
		return ok && dist < 10.0
	}
	checkHealth := func(bb *Blackboard) bool {
		hp, ok := bb.GetInt("hp")
		return ok && hp < 20
	}
	attackEnemy := func(bb *Blackboard) Status { attackCount++; return Success }
	fleeFromEnemy := func(bb *Blackboard) Status { fleeCount++; return Success }
	patrol := func(bb *Blackboard) Status { patrolCount++; return Success }

	tree := NewTree(
		Selector(
			Sequence(
				Condition("IsEnemyInRange", checkRange),
				Action("Attack", attackEnemy),
			),
			Sequence(
				Condition("IsHealthLow", checkHealth),
				Action("Flee", fleeFromEnemy),
			),
			Action("Patrol", patrol),
		),
	)

	// 场景1：没有敌人，血量正常 → 巡逻
	tree.Blackboard().Set("enemy_distance", 50.0)
	tree.Blackboard().Set("hp", 100)
	tree.Tick()
	if patrolCount != 1 {
		t.Errorf("expected patrol, got attack=%d flee=%d patrol=%d", attackCount, fleeCount, patrolCount)
	}

	// 场景2：敌人在范围内 → 攻击
	tree.Blackboard().Set("enemy_distance", 5.0)
	tree.Tick()
	if attackCount != 1 {
		t.Errorf("expected attack, got attack=%d", attackCount)
	}

	// 场景3：血量低，敌人不在范围 → 逃跑
	tree.Blackboard().Set("enemy_distance", 50.0)
	tree.Blackboard().Set("hp", 10)
	tree.Tick()
	if fleeCount != 1 {
		t.Errorf("expected flee, got flee=%d", fleeCount)
	}
}

func TestLoadTreeFromJSON(t *testing.T) {
	reg := NewActionRegistry()
	executed := false
	reg.RegisterAction("DoSomething", func(bb *Blackboard) Status {
		executed = true
		return Success
	})
	reg.RegisterCondition("AlwaysTrue", func(bb *Blackboard) bool {
		return true
	})

	jsonData := []byte(`{
		"type": "sequence",
		"children": [
			{"type": "condition", "name": "AlwaysTrue"},
			{"type": "action", "name": "DoSomething"}
		]
	}`)

	tree, err := LoadTreeFromJSON(jsonData, reg)
	if err != nil {
		t.Fatalf("load tree: %v", err)
	}

	status := tree.Tick()
	if status != Success {
		t.Errorf("expected Success, got %v", status)
	}
	if !executed {
		t.Error("action was not executed")
	}
}
