package quest

import (
	"testing"
	"time"
)

func TestPrerequisite_AllOf(t *testing.T) {
	pre := AllOf(
		RequireQuest("q1"),
		RequireLevel(10),
	)
	ctx := &PrereqContext{
		QuestDone: func(id string) bool { return id == "q1" },
		Level:     10,
	}
	if !pre.Evaluate(ctx) {
		t.Error("AND with both met should pass")
	}
	ctx.Level = 5
	if pre.Evaluate(ctx) {
		t.Error("AND should fail when level below")
	}
}

func TestPrerequisite_AnyOf(t *testing.T) {
	pre := AnyOf(
		RequireLevel(100),
		RequireFlag("vip"),
	)
	ctx := &PrereqContext{Flags: map[string]bool{"vip": true}, Level: 5}
	if !pre.Evaluate(ctx) {
		t.Error("OR should pass when vip flag set")
	}
	ctx.Flags["vip"] = false
	if pre.Evaluate(ctx) {
		t.Error("OR should fail when neither met")
	}
}

func TestPrerequisite_NoneOf(t *testing.T) {
	pre := NoneOf(RequireFlag("banned"))
	ctx := &PrereqContext{Flags: map[string]bool{}}
	if !pre.Evaluate(ctx) {
		t.Error("NoneOf should pass when no bad flag set")
	}
	ctx.Flags["banned"] = true
	if pre.Evaluate(ctx) {
		t.Error("NoneOf should fail when banned flag set")
	}
}

func TestPrerequisite_Reputation(t *testing.T) {
	pre := RequireReputation("guild", 100)
	ctx := &PrereqContext{Reputation: map[string]int{"guild": 120}}
	if !pre.Evaluate(ctx) {
		t.Error("reputation 120 should satisfy ≥100")
	}
	ctx.Reputation["guild"] = 50
	if pre.Evaluate(ctx) {
		t.Error("reputation 50 should fail ≥100")
	}
}

func TestPrerequisite_Negate(t *testing.T) {
	pre := RequireFlag("dead")
	pre.Negate = true
	ctx := &PrereqContext{Flags: map[string]bool{"dead": false}}
	if !pre.Evaluate(ctx) {
		t.Error("negated false flag should pass")
	}
	ctx.Flags["dead"] = true
	if pre.Evaluate(ctx) {
		t.Error("negated true flag should fail")
	}
}

func TestPrerequisite_Nested(t *testing.T) {
	// (level>=10 AND (quest1 OR quest2)) AND NOT(banned)
	pre := AllOf(
		RequireLevel(10),
		AnyOf(RequireQuest("q1"), RequireQuest("q2")),
		NoneOf(RequireFlag("banned")),
	)
	ctx := &PrereqContext{
		QuestDone:  func(id string) bool { return id == "q2" },
		Level:      15,
		Flags:      map[string]bool{},
		Reputation: map[string]int{},
	}
	if !pre.Evaluate(ctx) {
		t.Error("nested should pass")
	}
	ctx.Flags["banned"] = true
	if pre.Evaluate(ctx) {
		t.Error("nested should fail when banned")
	}
}

func TestTracker_PrereqAccept(t *testing.T) {
	reg := NewQuestRegistry()
	reg.Register(&QuestDef{
		ID:    "locked",
		Name:  "Locked",
		Type:  QuestTypeMain,
		Steps: []StepDef{{ID: "s1", EventType: "ping", Required: 1}},
		Prereq: AllOf(
			RequireLevel(20),
			RequireFlag("elite"),
		),
	})

	tr := NewQuestTracker("p1", reg)
	now := time.Unix(1000, 0)

	// 未满足条件
	if err := tr.Accept("locked", now); err == nil {
		t.Error("should reject when prereq unmet")
	}

	tr.SetPlayerLevel(20)
	tr.SetFlag("elite", true)
	if err := tr.Accept("locked", now); err != nil {
		t.Errorf("should accept when prereq met: %v", err)
	}
}

func TestTracker_LevelGate(t *testing.T) {
	reg := NewQuestRegistry()
	reg.Register(&QuestDef{
		ID:    "hard",
		Type:  QuestTypeMain,
		Level: 30,
		Steps: []StepDef{{ID: "s1", EventType: "ping", Required: 1}},
	})
	tr := NewQuestTracker("p1", reg)
	tr.SetPlayerLevel(10)
	if err := tr.Accept("hard", time.Now()); err == nil {
		t.Error("level 10 should not accept level-30 quest")
	}
	tr.SetPlayerLevel(30)
	if err := tr.Accept("hard", time.Now()); err != nil {
		t.Errorf("level 30 should accept: %v", err)
	}
}

func TestQuestInstance_ChooseBranch(t *testing.T) {
	def := &QuestDef{
		ID:   "fork",
		Type: QuestTypeMain,
		Steps: []StepDef{
			{ID: "talk_npc", EventType: "talk", TargetID: "npc1", Required: 1},
		},
		Branches: &BranchDef{
			ChoicePoint: "talk_npc",
			Paths: map[string][]StepDef{
				"good": {{ID: "good_step", EventType: "help_villager", Required: 3}},
				"evil": {{ID: "evil_step", EventType: "raid_village", Required: 1}},
			},
			AutoCompleteOnPick: true,
		},
	}
	now := time.Unix(2000, 0)
	inst := NewQuestInstance(def, "p1", now)
	if inst.Branch == nil {
		t.Fatal("branch state should be initialized")
	}

	// 选择 good
	if !inst.ChooseBranch("good") {
		t.Fatal("choose good should succeed")
	}
	if inst.ChosenBranch() != "good" {
		t.Errorf("want good, got %s", inst.ChosenBranch())
	}
	// 选择点应已完成
	if !inst.Steps[0].Done {
		t.Error("choice point should be auto-completed")
	}
	// 新步骤已展开
	if len(inst.Steps) != 2 || inst.Steps[1].StepID != "good_step" {
		t.Errorf("branch steps not expanded: %+v", inst.Steps)
	}
	// 不能二次选择
	if inst.ChooseBranch("evil") {
		t.Error("second choice should fail")
	}
	// 推进分支步骤
	inst.UpdateProgress("help_villager", "", 3)
	if inst.Status != QuestCompleted {
		t.Errorf("want completed, got %v", inst.Status)
	}
}

func TestTracker_ChooseBranch(t *testing.T) {
	reg := NewQuestRegistry()
	reg.Register(&QuestDef{
		ID:    "fork",
		Type:  QuestTypeSide,
		Steps: []StepDef{{ID: "start", EventType: "none", Required: 1}},
		Branches: &BranchDef{
			ChoicePoint:        "start",
			Paths:              map[string][]StepDef{"a": {{ID: "a1", EventType: "e1", Required: 1}}},
			AutoCompleteOnPick: true,
		},
	})
	tr := NewQuestTracker("p1", reg)
	_ = tr.Accept("fork", time.Unix(3000, 0))
	if err := tr.ChooseBranch("fork", "a"); err != nil {
		t.Errorf("choose a: %v", err)
	}
	if err := tr.ChooseBranch("fork", "b"); err == nil {
		t.Error("second choice should fail")
	}
}

func TestSharedQuestPool_Flow(t *testing.T) {
	reg := NewQuestRegistry()
	reg.Register(&QuestDef{
		ID:      "team_dragon",
		Name:    "Slay dragon",
		Type:    QuestTypeMain,
		Shared:  true,
		Steps:   []StepDef{{ID: "kill", EventType: "kill_dragon", Required: 3}},
		Rewards: []RewardDef{{Type: "item", ItemID: "chest", Count: 1}},
	})

	pool := NewSharedQuestPool("teamA", reg)
	if err := pool.Accept("team_dragon", []string{"p1", "p2"}, time.Unix(4000, 0)); err != nil {
		t.Fatalf("accept: %v", err)
	}

	// p1 贡献 2 次，p2 贡献 1 次
	pool.HandleEvent(GameEvent{Type: "kill_dragon", Count: 2, PlayerID: "p1"})
	pool.HandleEvent(GameEvent{Type: "kill_dragon", Count: 1, PlayerID: "p2"})

	sq, _ := pool.GetShared("team_dragon")
	if sq.Instance.Status != QuestCompleted {
		t.Fatalf("want completed, got %v", sq.Instance.Status)
	}

	r1, err := pool.ClaimRewards("team_dragon", "p1")
	if err != nil || len(r1) != 1 {
		t.Errorf("p1 claim: %v rewards=%+v", err, r1)
	}
	r2, err := pool.ClaimRewards("team_dragon", "p2")
	if err != nil || len(r2) != 1 {
		t.Errorf("p2 claim: %v rewards=%+v", err, r2)
	}
	// 重复领奖
	if _, err := pool.ClaimRewards("team_dragon", "p1"); err == nil {
		t.Error("duplicate claim should fail")
	}
	// 非成员领奖
	if _, err := pool.ClaimRewards("team_dragon", "outsider"); err == nil {
		t.Error("outsider should not claim")
	}

	if !sq.AllClaimed() {
		t.Error("all members should have claimed")
	}

	if removed := pool.Cleanup(); removed != 1 {
		t.Errorf("want 1 cleaned, got %d", removed)
	}
}

func TestSharedQuestPool_NonMemberEventsIgnored(t *testing.T) {
	reg := NewQuestRegistry()
	reg.Register(&QuestDef{
		ID:     "team_dig",
		Type:   QuestTypeSide,
		Shared: true,
		Steps:  []StepDef{{ID: "dig", EventType: "dig", Required: 5}},
	})
	pool := NewSharedQuestPool("team", reg)
	_ = pool.Accept("team_dig", []string{"p1"}, time.Now())

	// p2 不是成员，进度不应推进
	pool.HandleEvent(GameEvent{Type: "dig", Count: 10, PlayerID: "p2"})
	sq, _ := pool.GetShared("team_dig")
	if sq.Instance.Steps[0].Current != 0 {
		t.Errorf("non-member event should not progress, got %d", sq.Instance.Steps[0].Current)
	}
}

func TestSharedQuestPool_RejectNonShared(t *testing.T) {
	reg := NewQuestRegistry()
	reg.Register(&QuestDef{
		ID:     "solo",
		Type:   QuestTypeSide,
		Shared: false,
		Steps:  []StepDef{{ID: "s", EventType: "e", Required: 1}},
	})
	pool := NewSharedQuestPool("team", reg)
	if err := pool.Accept("solo", []string{"p1"}, time.Now()); err == nil {
		t.Error("should reject non-shared quest")
	}
}

func TestSharedQuestPool_AddRemoveMember(t *testing.T) {
	reg := NewQuestRegistry()
	reg.Register(&QuestDef{
		ID:     "team_walk",
		Shared: true,
		Steps:  []StepDef{{ID: "w", EventType: "walk", Required: 1}},
	})
	pool := NewSharedQuestPool("team", reg)
	_ = pool.Accept("team_walk", []string{"p1"}, time.Now())

	pool.AddMember("p2")
	pool.HandleEvent(GameEvent{Type: "walk", Count: 1, PlayerID: "p2"})
	sq, _ := pool.GetShared("team_walk")
	if sq.Instance.Status != QuestCompleted {
		t.Error("p2 event should progress")
	}

	pool.RemoveMember("p2")
	if _, ok := sq.Members["p2"]; ok {
		t.Error("p2 should be removed")
	}
}
