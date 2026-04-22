package scene

import (
	"testing"
)

func newTestGrid() *Grid {
	return NewGrid(GridConfig{
		Width:    1000,
		Height:   1000,
		CellSize: 100,
	})
}

func TestGridAddRemove(t *testing.T) {
	g := newTestGrid()

	g.Add(&GridEntity{ID: "e1", X: 50, Y: 50})
	g.Add(&GridEntity{ID: "e2", X: 150, Y: 150})

	if g.EntityCount() != 2 {
		t.Errorf("expected 2, got %d", g.EntityCount())
	}

	e := g.Remove("e1")
	if e == nil || e.ID != "e1" {
		t.Error("should return removed entity")
	}
	if g.EntityCount() != 1 {
		t.Errorf("expected 1, got %d", g.EntityCount())
	}
}

func TestGridPosToCell(t *testing.T) {
	g := newTestGrid()

	cx, cy := g.posToCell(50, 50)
	if cx != 0 || cy != 0 {
		t.Errorf("expected (0,0), got (%d,%d)", cx, cy)
	}

	cx, cy = g.posToCell(150, 250)
	if cx != 1 || cy != 2 {
		t.Errorf("expected (1,2), got (%d,%d)", cx, cy)
	}

	// 越界
	cx, cy = g.posToCell(-10, 2000)
	if cx != 0 {
		t.Errorf("expected clamped x=0, got %d", cx)
	}
	if cy != g.rows-1 {
		t.Errorf("expected clamped y=%d, got %d", g.rows-1, cy)
	}
}

func TestGridAOI(t *testing.T) {
	g := newTestGrid()

	// 放在同一格子
	g.Add(&GridEntity{ID: "a", X: 50, Y: 50})
	g.Add(&GridEntity{ID: "b", X: 60, Y: 60})

	// 放在相邻格子
	g.Add(&GridEntity{ID: "c", X: 150, Y: 50})

	// 放在远处
	g.Add(&GridEntity{ID: "d", X: 500, Y: 500})

	aoi := g.GetAOI("a")
	ids := make(map[string]bool)
	for _, e := range aoi {
		ids[e.ID] = true
	}

	if !ids["b"] {
		t.Error("b should be in a's AOI (same cell)")
	}
	if !ids["c"] {
		t.Error("c should be in a's AOI (adjacent cell)")
	}
	if ids["d"] {
		t.Error("d should NOT be in a's AOI (far away)")
	}
}

func TestGridMoveSameCell(t *testing.T) {
	g := newTestGrid()
	g.Add(&GridEntity{ID: "e1", X: 50, Y: 50})

	entered, left := g.Move("e1", 60, 60) // 仍在 (0,0) 格子
	if len(entered) != 0 || len(left) != 0 {
		t.Error("same cell move should have no AOI changes")
	}
}

func TestGridMoveCrossCell(t *testing.T) {
	g := newTestGrid()

	// a 在 (0,0) 格子，b 在 (0,0)，c 在 (2,0)
	g.Add(&GridEntity{ID: "a", X: 50, Y: 50})
	g.Add(&GridEntity{ID: "b", X: 60, Y: 60})
	g.Add(&GridEntity{ID: "c", X: 250, Y: 50})

	// b 不在 c 的 AOI，c 不在 b 的 AOI
	// a 移动到 (2,0) 格子附近
	entered, left := g.Move("a", 250, 50)

	enteredIDs := make(map[string]bool)
	for _, e := range entered {
		enteredIDs[e.ID] = true
	}
	leftIDs := make(map[string]bool)
	for _, e := range left {
		leftIDs[e.ID] = true
	}

	// c 应该进入 a 的视野（如果 c 不在旧 AOI 中）
	// b 可能离开 a 的视野（如果 b 不在新 AOI 中）
	// 具体取决于格子距离
	// (0,0) → (2,0)：旧 AOI 是 (0,0)(1,0)(0,1)(1,1)，新 AOI 是 (1,0)(2,0)(3,0)(1,1)(2,1)(3,1)
	// b 在 (0,0)，不在新 AOI → left
	// c 在 (2,0)，不在旧 AOI → entered
	if !enteredIDs["c"] {
		t.Error("c should have entered AOI")
	}
	if !leftIDs["b"] {
		t.Error("b should have left AOI")
	}
}

func TestGridGetNeighborCells(t *testing.T) {
	g := newTestGrid()

	// 中心格子应有 9 个邻居
	cells := g.GetNeighborCells(5, 5)
	if len(cells) != 9 {
		t.Errorf("center cell should have 9 neighbors, got %d", len(cells))
	}

	// 角落格子应有 4 个邻居
	cells = g.GetNeighborCells(0, 0)
	if len(cells) != 4 {
		t.Errorf("corner cell should have 4 neighbors, got %d", len(cells))
	}

	// 边缘格子应有 6 个邻居
	cells = g.GetNeighborCells(5, 0)
	if len(cells) != 6 {
		t.Errorf("edge cell should have 6 neighbors, got %d", len(cells))
	}
}
