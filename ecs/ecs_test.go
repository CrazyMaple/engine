package ecs

import "testing"

func TestEntityAddGet(t *testing.T) {
	e := NewEntity("player1")
	e.Add(&Position{X: 10, Y: 20, Z: 0})
	e.Add(&Health{Current: 100, Max: 100})

	pos := e.GetPosition()
	if pos == nil || pos.X != 10 || pos.Y != 20 {
		t.Errorf("expected Position{10,20,0}, got %v", pos)
	}

	hp := e.GetHealth()
	if hp == nil || hp.Current != 100 {
		t.Errorf("expected Health{100,100}, got %v", hp)
	}
}

func TestEntityRemove(t *testing.T) {
	e := NewEntity("p1")
	e.Add(&Position{X: 1, Y: 2})

	if !e.Has("Position") {
		t.Fatal("should have Position")
	}

	e.Remove("Position")
	if e.Has("Position") {
		t.Fatal("should not have Position after remove")
	}
}

func TestEntityAll(t *testing.T) {
	e := NewEntity("p1")
	e.Add(&Position{})
	e.Add(&Health{})

	all := e.All()
	if len(all) != 2 {
		t.Errorf("expected 2 components, got %d", len(all))
	}
}

func TestEntityOverwrite(t *testing.T) {
	e := NewEntity("p1")
	e.Add(&Position{X: 1})
	e.Add(&Position{X: 99})

	pos := e.GetPosition()
	if pos.X != 99 {
		t.Errorf("expected overwritten X=99, got %f", pos.X)
	}
}

func TestWorld(t *testing.T) {
	w := NewWorld()

	e1 := NewEntity("e1")
	e1.Add(&Position{X: 1})
	e1.Add(&Health{Current: 50, Max: 100})

	e2 := NewEntity("e2")
	e2.Add(&Position{X: 2})

	e3 := NewEntity("e3")
	e3.Add(&Health{Current: 100, Max: 100})

	w.Add(e1)
	w.Add(e2)
	w.Add(e3)

	if w.Count() != 3 {
		t.Errorf("expected 3 entities, got %d", w.Count())
	}

	// Query by Position
	withPos := w.Query("Position")
	if len(withPos) != 2 {
		t.Errorf("expected 2 entities with Position, got %d", len(withPos))
	}

	// Query by Health
	withHP := w.Query("Health")
	if len(withHP) != 2 {
		t.Errorf("expected 2 entities with Health, got %d", len(withHP))
	}

	// QueryMulti: Position AND Health
	both := w.QueryMulti("Position", "Health")
	if len(both) != 1 || both[0].ID != "e1" {
		t.Errorf("expected only e1 with both, got %v", both)
	}

	// Remove
	w.Remove("e2")
	if w.Count() != 2 {
		t.Errorf("expected 2 after remove, got %d", w.Count())
	}
}

func TestWorldEach(t *testing.T) {
	w := NewWorld()
	w.Add(NewEntity("a"))
	w.Add(NewEntity("b"))

	count := 0
	w.Each(func(e *Entity) { count++ })
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}
