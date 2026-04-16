package quest

import (
	"testing"
	"time"
)

func TestQuestInstance_Progress(t *testing.T) {
	def := &QuestDef{
		ID:   "q1",
		Name: "消灭怪物",
		Steps: []StepDef{
			{ID: "s1", EventType: "kill_monster", TargetID: "goblin", Required: 5},
			{ID: "s2", EventType: "collect_item", TargetID: "goblin_ear", Required: 3},
		},
		Rewards: []RewardDef{
			{Type: "exp", Count: 100},
			{Type: "gold", Count: 50},
		},
	}

	now := time.Now()
	q := NewQuestInstance(def, "player1", now)

	if q.Status != QuestActive {
		t.Error("should be active")
	}
	if q.Progress() != 0 {
		t.Errorf("expected 0%% progress, got %d", q.Progress())
	}

	// 击杀 3 只哥布林
	changed := q.UpdateProgress("kill_monster", "goblin", 3)
	if !changed {
		t.Error("should report change")
	}
	if q.Progress() != 37 { // 3/8 = 37%
		t.Errorf("expected 37%% progress, got %d", q.Progress())
	}

	// 不匹配的事件
	changed = q.UpdateProgress("kill_monster", "dragon", 1)
	if changed {
		t.Error("should not match different target")
	}

	// 完成击杀
	q.UpdateProgress("kill_monster", "goblin", 5) // 超出也截断到 required
	if q.Steps[0].Current != 5 {
		t.Errorf("expected 5, got %d", q.Steps[0].Current)
	}

	// 收集道具
	q.UpdateProgress("collect_item", "goblin_ear", 3)
	if q.Status != QuestCompleted {
		t.Error("should be completed when all steps done")
	}

	// 领奖
	rewards, err := q.ClaimRewards()
	if err != nil {
		t.Fatal(err)
	}
	if len(rewards) != 2 {
		t.Errorf("expected 2 rewards, got %d", len(rewards))
	}
	if q.Status != QuestRewarded {
		t.Error("should be rewarded")
	}
}

func TestQuestInstance_TimeLimit(t *testing.T) {
	def := &QuestDef{
		ID:        "q2",
		TimeLimit: 1 * time.Hour,
		Steps:     []StepDef{{ID: "s1", EventType: "reach", Required: 1}},
	}

	now := time.Now()
	q := NewQuestInstance(def, "player1", now)

	if q.IsExpired(now.Add(30 * time.Minute)) {
		t.Error("should not be expired at 30min")
	}
	if !q.IsExpired(now.Add(2 * time.Hour)) {
		t.Error("should be expired at 2hr")
	}
}

func TestQuestTracker(t *testing.T) {
	registry := NewQuestRegistry()
	registry.Register(&QuestDef{
		ID:   "q1",
		Name: "新手任务",
		Steps: []StepDef{
			{ID: "s1", EventType: "kill_monster", Required: 3},
		},
		Rewards: []RewardDef{{Type: "exp", Count: 50}},
	})
	registry.Register(&QuestDef{
		ID:        "q2",
		Name:      "进阶任务",
		PrereqIDs: []string{"q1"},
		Steps: []StepDef{
			{ID: "s1", EventType: "kill_boss", Required: 1},
		},
	})

	tracker := NewQuestTracker("player1", registry)
	now := time.Now()

	// 接取任务
	if err := tracker.Accept("q1", now); err != nil {
		t.Fatal(err)
	}
	if tracker.ActiveCount() != 1 {
		t.Error("expected 1 active quest")
	}

	// 不能接取有前置的任务
	if err := tracker.Accept("q2", now); err == nil {
		t.Error("should not accept quest with unmet prerequisite")
	}

	// 处理事件
	updates := tracker.HandleEvent(GameEvent{
		Type:     "kill_monster",
		Count:    3,
		PlayerID: "player1",
	})
	if len(updates) != 1 {
		t.Errorf("expected 1 update, got %d", len(updates))
	}
	if updates[0].Status != QuestCompleted {
		t.Error("quest should be completed")
	}

	// 领奖
	rewards, err := tracker.ClaimRewards("q1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rewards) != 1 {
		t.Errorf("expected 1 reward, got %d", len(rewards))
	}
	if !tracker.IsCompleted("q1") {
		t.Error("q1 should be completed")
	}

	// 现在可以接取 q2
	if err := tracker.Accept("q2", now); err != nil {
		t.Errorf("should accept q2 after q1 completed: %v", err)
	}

	// 不能重复接取
	if err := tracker.Accept("q1", now); err == nil {
		t.Error("should not re-accept completed quest")
	}
}

func TestQuestTracker_Abandon(t *testing.T) {
	registry := NewQuestRegistry()
	registry.Register(&QuestDef{
		ID:    "q1",
		Steps: []StepDef{{ID: "s1", EventType: "test", Required: 1}},
	})

	tracker := NewQuestTracker("p1", registry)
	now := time.Now()
	tracker.Accept("q1", now)
	tracker.Abandon("q1")

	if tracker.ActiveCount() != 0 {
		t.Error("should have no active quests")
	}
}

func TestQuestTracker_CheckExpired(t *testing.T) {
	registry := NewQuestRegistry()
	registry.Register(&QuestDef{
		ID:        "q1",
		TimeLimit: 1 * time.Minute,
		Steps:     []StepDef{{ID: "s1", EventType: "test", Required: 1}},
	})

	tracker := NewQuestTracker("p1", registry)
	now := time.Now()
	tracker.Accept("q1", now)

	expired := tracker.CheckExpired(now.Add(2 * time.Minute))
	if len(expired) != 1 {
		t.Errorf("expected 1 expired, got %d", len(expired))
	}
	if tracker.ActiveCount() != 0 {
		t.Error("expired quest should be removed")
	}
}

func TestAchievementTracker(t *testing.T) {
	defs := []*AchievementDef{
		{
			ID:        "first_blood",
			Name:      "初次击杀",
			Category:  "combat",
			EventType: "kill_monster",
			Required:  1,
			Points:    10,
			Rewards:   []RewardDef{{Type: "gold", Count: 100}},
		},
		{
			ID:        "hunter",
			Name:      "猎人",
			Category:  "combat",
			EventType: "kill_monster",
			Required:  100,
			Points:    50,
		},
		{
			ID:        "explorer",
			Name:      "探索者",
			Category:  "exploration",
			EventType: "visit_area",
			Required:  10,
			Points:    30,
		},
	}

	tracker := NewAchievementTracker("player1", defs)

	if tracker.TotalCount() != 3 {
		t.Errorf("expected 3, got %d", tracker.TotalCount())
	}

	// 击杀事件
	updates := tracker.HandleEvent(GameEvent{
		Type:     "kill_monster",
		Count:    1,
		PlayerID: "player1",
	})

	// first_blood 应达成
	achieved := false
	for _, u := range updates {
		if u.AchievementID == "first_blood" && u.Achieved {
			achieved = true
		}
	}
	if !achieved {
		t.Error("first_blood should be achieved")
	}

	if tracker.TotalPoints() != 10 {
		t.Errorf("expected 10 points, got %d", tracker.TotalPoints())
	}
	if tracker.AchievedCount() != 1 {
		t.Errorf("expected 1 achieved, got %d", tracker.AchievedCount())
	}

	// 领取奖励
	rewards, ok := tracker.ClaimRewards("first_blood")
	if !ok || len(rewards) != 1 {
		t.Error("should claim first_blood rewards")
	}

	// 重复领取
	_, ok = tracker.ClaimRewards("first_blood")
	if ok {
		t.Error("should not re-claim")
	}

	// 按分类查询
	combat := tracker.GetByCategory("combat")
	if len(combat) != 2 {
		t.Errorf("expected 2 combat achievements, got %d", len(combat))
	}

	// hunter 未达成
	hunter := tracker.GetAchieved()
	for _, a := range hunter {
		if a.Def.ID == "hunter" {
			t.Error("hunter should not be achieved yet")
		}
	}
}

func TestAchievementTracker_OnAchieved(t *testing.T) {
	defs := []*AchievementDef{
		{
			ID:        "ach1",
			EventType: "test",
			Required:  1,
			Points:    5,
		},
	}

	callbackCalled := false
	tracker := NewAchievementTracker("p1", defs)
	tracker.SetOnAchieved(func(playerID string, ach *AchievementInstance) {
		callbackCalled = true
		if playerID != "p1" {
			t.Error("wrong player ID in callback")
		}
	})

	tracker.HandleEvent(GameEvent{Type: "test", Count: 1})
	if !callbackCalled {
		t.Error("onAchieved callback should be called")
	}
}

func TestQuestRegistry(t *testing.T) {
	r := NewQuestRegistry()
	r.Register(&QuestDef{ID: "q1", Type: QuestTypeMain})
	r.Register(&QuestDef{ID: "q2", Type: QuestTypeDaily})
	r.Register(&QuestDef{ID: "q3", Type: QuestTypeDaily})

	daily := r.GetByType(QuestTypeDaily)
	if len(daily) != 2 {
		t.Errorf("expected 2 daily quests, got %d", len(daily))
	}

	all := r.All()
	if len(all) != 3 {
		t.Errorf("expected 3 quests, got %d", len(all))
	}
}
