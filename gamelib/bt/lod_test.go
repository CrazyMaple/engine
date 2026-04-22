package bt

import (
	"testing"
)

func TestLODManager_Register(t *testing.T) {
	m := NewLODManager(DefaultLODConfig())
	tree := NewTree(Action("noop", func(bb *Blackboard) Status { return Success }))

	m.Register("npc1", tree)
	if m.Count() != 1 {
		t.Errorf("expected 1, got %d", m.Count())
	}

	m.Unregister("npc1")
	if m.Count() != 0 {
		t.Errorf("expected 0, got %d", m.Count())
	}
}

func TestLODManager_CalculateLevel(t *testing.T) {
	cfg := LODConfig{
		NearRange: 100,
		MidRange:  300,
		FarRange:  600,
	}
	m := NewLODManager(cfg)
	tree := NewTree(Action("noop", func(bb *Blackboard) Status { return Success }))
	m.Register("npc1", tree)

	observers := []struct{ X, Y float64 }{{0, 0}}

	tests := []struct {
		x, y     float64
		expected LODLevel
	}{
		{50, 50, LODFull},     // ~70 < 100
		{200, 200, LODHalf},   // ~283 < 300
		{400, 400, LODQuarter}, // ~566 < 600
		{1000, 1000, LODPaused}, // ~1414 > 600
	}

	for _, tt := range tests {
		m.UpdateLOD("npc1", tt.x, tt.y, observers)
		got := m.GetLevel("npc1")
		if got != tt.expected {
			t.Errorf("pos(%v,%v): expected LOD %d, got %d", tt.x, tt.y, tt.expected, got)
		}
	}
}

func TestLODManager_ShouldTick(t *testing.T) {
	m := NewLODManager(DefaultLODConfig())
	tree := NewTree(Action("noop", func(bb *Blackboard) Status { return Success }))
	m.Register("npc1", tree)

	// LODFull: every frame
	m.SetLevel("npc1", LODFull)
	for i := 0; i < 5; i++ {
		if !m.ShouldTick("npc1") {
			t.Errorf("LODFull should tick every frame, failed at %d", i)
		}
	}

	// LODHalf: every 2 frames
	m.SetLevel("npc1", LODHalf)
	results := []bool{m.ShouldTick("npc1"), m.ShouldTick("npc1"), m.ShouldTick("npc1"), m.ShouldTick("npc1")}
	// accumulator resets at entry, so: 1→false(acc=1), 2→true(acc=0), 1→false, 2→true
	expected := []bool{false, true, false, true}
	for i, e := range expected {
		if results[i] != e {
			t.Errorf("LODHalf frame %d: expected %v, got %v", i, e, results[i])
		}
	}

	// LODPaused: never
	m.SetLevel("npc1", LODPaused)
	for i := 0; i < 10; i++ {
		if m.ShouldTick("npc1") {
			t.Error("LODPaused should never tick")
		}
	}

	// Unknown entity
	if m.ShouldTick("unknown") {
		t.Error("unknown entity should not tick")
	}
}

func TestLODManager_Stats(t *testing.T) {
	m := NewLODManager(DefaultLODConfig())
	for _, id := range []string{"a", "b", "c"} {
		tree := NewTree(Action("noop", func(bb *Blackboard) Status { return Success }))
		m.Register(id, tree)
	}

	m.SetLevel("a", LODFull)
	m.SetLevel("b", LODHalf)
	m.SetLevel("c", LODFull)

	stats := m.Stats()
	if stats[LODFull] != 2 {
		t.Errorf("expected 2 LODFull, got %d", stats[LODFull])
	}
	if stats[LODHalf] != 1 {
		t.Errorf("expected 1 LODHalf, got %d", stats[LODHalf])
	}
}

func TestLODManager_NoObservers(t *testing.T) {
	m := NewLODManager(DefaultLODConfig())
	tree := NewTree(Action("noop", func(bb *Blackboard) Status { return Success }))
	m.Register("npc1", tree)

	// 无观察者时应保持 LODFull
	m.UpdateLOD("npc1", 1000, 1000, nil)
	if m.GetLevel("npc1") != LODFull {
		t.Error("expected LODFull with no observers")
	}
}
