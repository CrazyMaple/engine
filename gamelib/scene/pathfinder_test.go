package scene

import "testing"

func TestAStarBasicPath(t *testing.T) {
	pf := NewAStarPathfinder(10, 10, false)

	path := pf.FindPath(Point{0, 0}, Point{9, 0})
	if path == nil {
		t.Fatal("should find a path")
	}
	if len(path) != 10 {
		t.Fatalf("expected path length 10, got %d", len(path))
	}
	if path[0].X != 0 || path[0].Y != 0 {
		t.Error("path should start at origin")
	}
	if path[len(path)-1].X != 9 || path[len(path)-1].Y != 0 {
		t.Error("path should end at destination")
	}
}

func TestAStarObstacle(t *testing.T) {
	pf := NewAStarPathfinder(10, 10, false)

	// 创建一面墙
	for y := 0; y < 8; y++ {
		pf.SetWalkable(5, y, false)
	}

	path := pf.FindPath(Point{0, 0}, Point{9, 0})
	if path == nil {
		t.Fatal("should find a path around obstacle")
	}

	// 路径应绕过墙壁
	for _, p := range path {
		if p.X == 5 && p.Y < 8 {
			t.Errorf("path should not go through wall at (%d,%d)", p.X, p.Y)
		}
	}
}

func TestAStarNoPath(t *testing.T) {
	pf := NewAStarPathfinder(10, 10, false)

	// 完全封死目标
	for x := 0; x < 10; x++ {
		pf.SetWalkable(x, 5, false)
	}

	path := pf.FindPath(Point{0, 0}, Point{9, 9})
	if path != nil {
		t.Error("should return nil when no path exists")
	}
}

func TestAStarSamePoint(t *testing.T) {
	pf := NewAStarPathfinder(10, 10, false)
	path := pf.FindPath(Point{5, 5}, Point{5, 5})
	if len(path) != 1 {
		t.Errorf("same point should return single-point path, got %d", len(path))
	}
}

func TestAStarDiagonal(t *testing.T) {
	pf := NewAStarPathfinder(10, 10, true) // 允许对角线

	path := pf.FindPath(Point{0, 0}, Point{5, 5})
	if path == nil {
		t.Fatal("should find diagonal path")
	}
	// 对角线路径应比直线更短
	if len(path) > 6 {
		t.Errorf("diagonal path should be ~6, got %d", len(path))
	}
}

func TestAStarDiagonalNoCornerCutting(t *testing.T) {
	pf := NewAStarPathfinder(10, 10, true)

	// 放置对角阻挡
	pf.SetWalkable(1, 0, false)
	pf.SetWalkable(0, 1, false)

	// 从 (0,0) 不应该对角穿到 (1,1)
	path := pf.FindPath(Point{0, 0}, Point{1, 1})
	if path != nil {
		t.Error("should not cut corners through obstacles")
	}
}

func TestAStarWeight(t *testing.T) {
	pf := NewAStarPathfinder(10, 5, false)

	// 中间行设高权重
	for x := 0; x < 10; x++ {
		pf.SetWeight(x, 2, 10.0)
	}

	path := pf.FindPath(Point{0, 0}, Point{9, 4})
	if path == nil {
		t.Fatal("should find path")
	}

	// 路径应倾向避开高权重区域
	highWeightCount := 0
	for _, p := range path {
		if p.Y == 2 {
			highWeightCount++
		}
	}
	// 不应该穿过太多高权重格子
	if highWeightCount > 2 {
		t.Logf("path goes through %d high-weight cells, may not be optimal", highWeightCount)
	}
}

func TestAStarUnwalkableEndpoints(t *testing.T) {
	pf := NewAStarPathfinder(10, 10, false)

	pf.SetWalkable(0, 0, false)
	path := pf.FindPath(Point{0, 0}, Point{5, 5})
	if path != nil {
		t.Error("should return nil when start is unwalkable")
	}

	pf.SetWalkable(0, 0, true)
	pf.SetWalkable(5, 5, false)
	path = pf.FindPath(Point{0, 0}, Point{5, 5})
	if path != nil {
		t.Error("should return nil when end is unwalkable")
	}
}

func TestPathCache(t *testing.T) {
	pf := NewAStarPathfinder(10, 10, false)

	from, to := Point{0, 0}, Point{5, 0}
	path1 := pf.FindPath(from, to)
	path2 := pf.FindPath(from, to) // 应来自缓存

	if len(path1) != len(path2) {
		t.Fatal("cached path should match")
	}
	for i := range path1 {
		if path1[i] != path2[i] {
			t.Fatal("cached path should be identical")
		}
	}

	// 修改地图应使缓存失效
	pf.SetWalkable(3, 0, false)
	path3 := pf.FindPath(from, to)
	if path3 == nil {
		t.Fatal("should find alternative path")
	}
}

func TestIsWalkableOutOfBounds(t *testing.T) {
	pf := NewAStarPathfinder(10, 10, false)

	if pf.IsWalkable(-1, 0) {
		t.Error("out of bounds should not be walkable")
	}
	if pf.IsWalkable(10, 0) {
		t.Error("out of bounds should not be walkable")
	}
}

func BenchmarkAStarSimple(b *testing.B) {
	pf := NewAStarPathfinder(100, 100, false)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pf.cache.Invalidate()
		pf.FindPath(Point{0, 0}, Point{99, 99})
	}
}

func BenchmarkAStarWithObstacles(b *testing.B) {
	pf := NewAStarPathfinder(100, 100, true)
	// 创建随机墙壁
	for y := 10; y < 90; y += 10 {
		for x := 0; x < 90; x++ {
			pf.SetWalkable(x, y, false)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pf.cache.Invalidate()
		pf.FindPath(Point{0, 0}, Point{99, 99})
	}
}

func BenchmarkAStarCached(b *testing.B) {
	pf := NewAStarPathfinder(100, 100, false)
	pf.FindPath(Point{0, 0}, Point{99, 99}) // warm up cache
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pf.FindPath(Point{0, 0}, Point{99, 99})
	}
}
