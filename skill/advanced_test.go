package skill

import (
	"math"
	"testing"
	"time"
)

func TestCondition_Evaluate(t *testing.T) {
	ctx := &TriggerContext{
		Results:     []EffectResult{{Applied: true}, {Applied: false}},
		Crit:        true,
		TargetHPPct: 0.2,
		TargetBuffs: map[string]bool{"wet": true},
		CasterBuffs: map[string]bool{"blessed": true},
		SkillDef:    &SkillDef{Tags: []string{"ice"}},
	}

	cases := []struct {
		name string
		cd   Condition
		want bool
	}{
		{"always", Condition{Type: CondAlways}, true},
		{"hit", Condition{Type: CondHit}, true},
		{"crit", Condition{Type: CondCrit}, true},
		{"hp below 30%", Condition{Type: CondHPBelow, Op: OpLt, Value: 0.3}, true},
		{"hp above 50%", Condition{Type: CondHPAbove, Op: OpGt, Value: 0.5}, false},
		{"has wet", Condition{Type: CondHasBuff, BuffID: "wet"}, true},
		{"lacks frozen", Condition{Type: CondLacksBuff, BuffID: "frozen"}, true},
		{"caster blessed", Condition{Type: CondCasterHasBuff, BuffID: "blessed"}, true},
		{"tag ice", Condition{Type: CondTag, BuffID: "ice"}, true},
		{"hit count >= 1", Condition{Type: CondHitCount, Op: OpGe, Value: 1}, true},
		{"negate hit", Condition{Type: CondHit, Negate: true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cd.Evaluate(ctx); got != c.want {
				t.Errorf("want %v, got %v", c.want, got)
			}
		})
	}
}

func TestTrigger_OnceSemantics(t *testing.T) {
	trg := &Trigger{
		Name: "burst",
		Conditions: []Condition{
			{Type: CondHit},
		},
		ChainSkillID: "burst_follow",
		Once:         true,
	}
	ctx := &TriggerContext{Results: []EffectResult{{Applied: true}}}
	if !trg.Match(ctx) {
		t.Fatal("first match should succeed")
	}
	trg.Fire()
	if trg.Match(ctx) {
		t.Fatal("Once trigger should not match again before Reset")
	}
	trg.Reset()
	if !trg.Match(ctx) {
		t.Fatal("after reset should match")
	}
}

func TestChainPlan_Walk(t *testing.T) {
	plan := &ChainPlan{
		Root: &ChainNode{
			ID:      "root",
			SkillID: "s1",
			Delay:   10 * time.Millisecond,
			Next: []*ChainNode{
				{
					ID:      "A",
					SkillID: "s2",
					Delay:   5 * time.Millisecond,
				},
				{
					ID:        "B",
					SkillID:   "s3",
					Delay:     20 * time.Millisecond,
					Condition: &Condition{Type: CondAlways, Negate: true},
				},
			},
		},
	}
	ctx := &TriggerContext{TargetID: "foo"}
	t0 := time.Unix(1000, 0)
	actions := plan.Walk(ctx, t0)
	if len(actions) != 2 {
		t.Fatalf("want 2 actions (B blocked), got %d", len(actions))
	}
	if !actions[0].ExecuteAt.Equal(t0.Add(10 * time.Millisecond)) {
		t.Errorf("root delay wrong: %v", actions[0].ExecuteAt)
	}
	if !actions[1].ExecuteAt.Equal(t0.Add(15 * time.Millisecond)) {
		t.Errorf("A cumulative delay wrong: %v", actions[1].ExecuteAt)
	}
}

func TestChainScheduler_PopDue(t *testing.T) {
	sch := NewChainScheduler()
	t0 := time.Unix(2000, 0)
	sch.Push(
		ChainAction{SkillID: "b", ExecuteAt: t0.Add(30 * time.Millisecond)},
		ChainAction{SkillID: "a", ExecuteAt: t0.Add(10 * time.Millisecond)},
		ChainAction{SkillID: "c", ExecuteAt: t0.Add(20 * time.Millisecond)},
	)
	due := sch.PopDue(t0.Add(20 * time.Millisecond))
	if len(due) != 2 {
		t.Fatalf("want 2 due actions, got %d", len(due))
	}
	if due[0].SkillID != "a" || due[1].SkillID != "c" {
		t.Errorf("order wrong: %+v", due)
	}
	if sch.Pending() != 1 {
		t.Errorf("one action should remain, got %d", sch.Pending())
	}
	due = sch.PopDue(t0.Add(1 * time.Second))
	if len(due) != 1 || due[0].SkillID != "b" {
		t.Errorf("last action mismatch: %+v", due)
	}
}

func TestTargetQuery_Circle(t *testing.T) {
	q := &TargetQuery{Shape: ShapeCircle, CenterX: 0, CenterY: 0, Radius: 10}
	in := q.Select([]TargetPosition{
		{ID: "in", X: 3, Y: 4},  // dist=5
		{ID: "out", X: 20, Y: 0},
	})
	if len(in) != 1 || in[0] != "in" {
		t.Errorf("want [in], got %v", in)
	}
}

func TestTargetQuery_Sector(t *testing.T) {
	// 朝向 X 正方向，张角 90 度
	q := &TargetQuery{
		Shape:    ShapeSector,
		CenterX:  0,
		CenterY:  0,
		Radius:   20,
		DirAngle: 0,
		FOV:      float32(math.Pi / 2),
	}
	hits := q.Select([]TargetPosition{
		{ID: "front", X: 10, Y: 0},   // 正前方
		{ID: "side", X: 0, Y: 10},    // 侧方 90 度恰好在边界
		{ID: "back", X: -10, Y: 0},   // 正后方
		{ID: "diag", X: 5, Y: 5},     // 45 度前方
		{ID: "far", X: 50, Y: 0},     // 太远
	})
	m := map[string]bool{}
	for _, id := range hits {
		m[id] = true
	}
	if !m["front"] || !m["diag"] {
		t.Errorf("front/diag should be inside sector: %v", hits)
	}
	if m["back"] || m["far"] {
		t.Errorf("back/far should be outside: %v", hits)
	}
}

func TestTargetQuery_Rect(t *testing.T) {
	q := &TargetQuery{
		Shape:    ShapeRect,
		CenterX:  0,
		CenterY:  0,
		DirAngle: 0,
		Length:   20,
		Width:    4,
	}
	hits := q.Select([]TargetPosition{
		{ID: "in", X: 10, Y: 1},
		{ID: "boundary", X: 20, Y: -2},
		{ID: "behind", X: -1, Y: 0},
		{ID: "too-wide", X: 10, Y: 5},
	})
	m := map[string]bool{}
	for _, id := range hits {
		m[id] = true
	}
	if !m["in"] || !m["boundary"] {
		t.Errorf("want in+boundary, got %v", hits)
	}
	if m["behind"] || m["too-wide"] {
		t.Errorf("behind/too-wide should be excluded: %v", hits)
	}
}

func TestTargetQuery_MaxCount(t *testing.T) {
	q := &TargetQuery{Shape: ShapeCircle, Radius: 50, MaxCount: 2}
	hits := q.Select([]TargetPosition{
		{ID: "a", X: 1, Y: 0},
		{ID: "b", X: 2, Y: 0},
		{ID: "c", X: 3, Y: 0},
	})
	if len(hits) != 2 {
		t.Fatalf("want 2 hits, got %d", len(hits))
	}
	// 按距离升序，a 最近
	if hits[0] != "a" || hits[1] != "b" {
		t.Errorf("want [a,b], got %v", hits)
	}
}

func TestTargetQueue(t *testing.T) {
	q := NewTargetQueue(ByDistance(0, 0))
	q.Add(TargetPosition{ID: "far", X: 10, Y: 0})
	q.Add(TargetPosition{ID: "near", X: 1, Y: 0})
	q.Add(TargetPosition{ID: "mid", X: 5, Y: 0})

	p, ok := q.Pop()
	if !ok || p.ID != "near" {
		t.Errorf("expected near first, got %s", p.ID)
	}
	p, _ = q.Pop()
	if p.ID != "mid" {
		t.Errorf("expected mid second, got %s", p.ID)
	}
}

func TestPhasedSkill_TotalDuration(t *testing.T) {
	s := &PhasedSkill{Phases: []PhaseDef{
		{Duration: 100 * time.Millisecond},
		{Duration: 50 * time.Millisecond},
		{Duration: 200 * time.Millisecond},
	}}
	if got := s.TotalDuration(); got != 350*time.Millisecond {
		t.Errorf("want 350ms, got %v", got)
	}
}
