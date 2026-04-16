package canary

import (
	"testing"
)

func TestABTestManager_CreateExperiment(t *testing.T) {
	m := NewABTestManager()

	exp := Experiment{
		ID:   "exp1",
		Name: "Button Color Test",
		Variants: []Variant{
			{Name: "control", Weight: 50},
			{Name: "treatment", Weight: 50},
		},
	}

	if err := m.CreateExperiment(exp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := m.GetExperiment("exp1")
	if got == nil {
		t.Fatal("expected experiment")
	}
	if got.Status != ExperimentDraft {
		t.Errorf("expected draft status, got %s", got.Status)
	}
}

func TestABTestManager_ValidationErrors(t *testing.T) {
	m := NewABTestManager()

	// 无 ID
	err := m.CreateExperiment(Experiment{
		Variants: []Variant{{Name: "a", Weight: 50}, {Name: "b", Weight: 50}},
	})
	if err == nil {
		t.Error("expected error for empty ID")
	}

	// 少于 2 个变体
	err = m.CreateExperiment(Experiment{
		ID:       "x",
		Variants: []Variant{{Name: "a", Weight: 100}},
	})
	if err == nil {
		t.Error("expected error for < 2 variants")
	}

	// 权重不等于 100
	err = m.CreateExperiment(Experiment{
		ID:       "x",
		Variants: []Variant{{Name: "a", Weight: 60}, {Name: "b", Weight: 60}},
	})
	if err == nil {
		t.Error("expected error for weights != 100")
	}
}

func TestABTestManager_Lifecycle(t *testing.T) {
	m := NewABTestManager()
	m.CreateExperiment(Experiment{
		ID:       "exp1",
		Variants: []Variant{{Name: "a", Weight: 50}, {Name: "b", Weight: 50}},
	})

	// draft -> running
	if err := m.StartExperiment("exp1"); err != nil {
		t.Fatalf("start error: %v", err)
	}
	if m.GetExperiment("exp1").Status != ExperimentRunning {
		t.Error("expected running")
	}

	// running -> paused
	if err := m.PauseExperiment("exp1"); err != nil {
		t.Fatalf("pause error: %v", err)
	}
	if m.GetExperiment("exp1").Status != ExperimentPaused {
		t.Error("expected paused")
	}

	// paused -> running
	if err := m.StartExperiment("exp1"); err != nil {
		t.Fatalf("restart error: %v", err)
	}

	// running -> completed
	if err := m.CompleteExperiment("exp1"); err != nil {
		t.Fatalf("complete error: %v", err)
	}
	if m.GetExperiment("exp1").Status != ExperimentCompleted {
		t.Error("expected completed")
	}
	if m.GetExperiment("exp1").EndTime == nil {
		t.Error("expected endTime to be set")
	}
}

func TestABTestManager_InvalidTransitions(t *testing.T) {
	m := NewABTestManager()
	m.CreateExperiment(Experiment{
		ID:       "exp1",
		Variants: []Variant{{Name: "a", Weight: 50}, {Name: "b", Weight: 50}},
	})

	// 不能暂停 draft
	if err := m.PauseExperiment("exp1"); err == nil {
		t.Error("expected error pausing draft")
	}

	// 不能启动不存在的
	if err := m.StartExperiment("nonexistent"); err == nil {
		t.Error("expected error for nonexistent")
	}
}

func TestABTestManager_Assign(t *testing.T) {
	m := NewABTestManager()
	m.CreateExperiment(Experiment{
		ID: "exp1",
		Variants: []Variant{
			{Name: "control", Weight: 50},
			{Name: "treatment", Weight: 50},
		},
	})
	m.StartExperiment("exp1")

	// 分配应返回结果
	results := m.Assign("user1", map[string]string{"user_id": "user1"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ExperimentID != "exp1" {
		t.Errorf("expected exp1, got %s", results[0].ExperimentID)
	}

	// 确定性：同一用户同一变体
	results2 := m.Assign("user1", map[string]string{"user_id": "user1"})
	if results[0].VariantName != results2[0].VariantName {
		t.Error("expected same variant for same user")
	}
}

func TestABTestManager_AssignWithTargetRules(t *testing.T) {
	m := NewABTestManager()
	m.CreateExperiment(Experiment{
		ID: "cn-only",
		Variants: []Variant{
			{Name: "control", Weight: 50},
			{Name: "treatment", Weight: 50},
		},
		TargetRules: []ConditionGroup{
			{
				Logic:      "or",
				Conditions: []Condition{{Field: "region", Operator: "eq", Value: "cn"}},
			},
		},
	})
	m.StartExperiment("cn-only")

	// cn 用户参与
	results := m.Assign("user1", map[string]string{"user_id": "user1", "region": "cn"})
	if len(results) != 1 {
		t.Errorf("expected 1 result for cn user, got %d", len(results))
	}

	// us 用户不参与
	results = m.Assign("user2", map[string]string{"user_id": "user2", "region": "us"})
	if len(results) != 0 {
		t.Errorf("expected 0 results for us user, got %d", len(results))
	}
}

func TestABTestManager_PausedNotAssigned(t *testing.T) {
	m := NewABTestManager()
	m.CreateExperiment(Experiment{
		ID:       "exp1",
		Variants: []Variant{{Name: "a", Weight: 50}, {Name: "b", Weight: 50}},
	})
	m.StartExperiment("exp1")
	m.PauseExperiment("exp1")

	results := m.Assign("user1", nil)
	if len(results) != 0 {
		t.Errorf("expected no assignment for paused experiment, got %d", len(results))
	}
}

func TestABTestManager_Stats(t *testing.T) {
	m := NewABTestManager()
	m.CreateExperiment(Experiment{
		ID: "exp1",
		Variants: []Variant{
			{Name: "control", Weight: 50},
			{Name: "treatment", Weight: 50},
		},
	})
	m.StartExperiment("exp1")

	for i := 0; i < 100; i++ {
		m.Assign("user"+intToTestStr(i), nil)
	}

	stats := m.ExperimentStats("exp1")
	if stats == nil {
		t.Fatal("expected stats")
	}
	total := stats["control"] + stats["treatment"]
	if total != 100 {
		t.Errorf("expected 100 total assignments, got %d", total)
	}

	// 两个变体都应有分配
	if stats["control"] == 0 {
		t.Error("expected some control assignments")
	}
	if stats["treatment"] == 0 {
		t.Error("expected some treatment assignments")
	}
}

func TestABTestManager_Delete(t *testing.T) {
	m := NewABTestManager()
	m.CreateExperiment(Experiment{
		ID:       "exp1",
		Variants: []Variant{{Name: "a", Weight: 50}, {Name: "b", Weight: 50}},
	})

	m.DeleteExperiment("exp1")

	if m.GetExperiment("exp1") != nil {
		t.Error("expected nil after delete")
	}
}

func TestABTestManager_ListExperiments(t *testing.T) {
	m := NewABTestManager()
	m.CreateExperiment(Experiment{
		ID:       "exp1",
		Name:     "First",
		Variants: []Variant{{Name: "a", Weight: 50}, {Name: "b", Weight: 50}},
	})
	m.CreateExperiment(Experiment{
		ID:       "exp2",
		Name:     "Second",
		Variants: []Variant{{Name: "a", Weight: 50}, {Name: "b", Weight: 50}},
	})

	list := m.ListExperiments()
	if len(list) != 2 {
		t.Errorf("expected 2 experiments, got %d", len(list))
	}
}

func TestABTestManager_MultipleExperiments(t *testing.T) {
	m := NewABTestManager()
	m.CreateExperiment(Experiment{
		ID:       "exp1",
		Variants: []Variant{{Name: "a", Weight: 50}, {Name: "b", Weight: 50}},
	})
	m.CreateExperiment(Experiment{
		ID:       "exp2",
		Variants: []Variant{{Name: "x", Weight: 70}, {Name: "y", Weight: 30}},
	})
	m.StartExperiment("exp1")
	m.StartExperiment("exp2")

	results := m.Assign("user1", nil)
	if len(results) != 2 {
		t.Errorf("expected 2 results (one per experiment), got %d", len(results))
	}
}

// 辅助函数：避免依赖 strconv
func intToTestStr(n int) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 10)
	for n > 0 {
		b = append(b, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}
