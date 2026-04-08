package scene

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestAOIInterface(t *testing.T) {
	configs := []struct {
		name string
		algo AOIAlgorithm
	}{
		{"Grid", AOIGrid},
		{"CrossLink", AOICrossLink},
		{"Lighthouse", AOILighthouse},
	}

	for _, cfg := range configs {
		t.Run(cfg.name, func(t *testing.T) {
			aoi := NewAOI(AOIConfig{
				Width:     100,
				Height:    100,
				ViewRange: 10,
				Algorithm: cfg.algo,
				CellSize:  10,
			})
			testAOIBasic(t, aoi)
		})
	}
}

func testAOIBasic(t *testing.T, aoi AOI) {
	// Add entities
	e1 := NewGridEntity("e1", 5, 5, nil, nil)
	e2 := NewGridEntity("e2", 8, 8, nil, nil)
	e3 := NewGridEntity("e3", 50, 50, nil, nil) // far away
	aoi.Add(e1)
	aoi.Add(e2)
	aoi.Add(e3)

	if aoi.EntityCount() != 3 {
		t.Fatalf("expected 3 entities, got %d", aoi.EntityCount())
	}

	// e1 should see e2 but not e3
	nearby := aoi.GetNearby("e1")
	hasE2, hasE3 := false, false
	for _, e := range nearby {
		if e.ID == "e2" {
			hasE2 = true
		}
		if e.ID == "e3" {
			hasE3 = true
		}
	}
	if !hasE2 {
		t.Error("e1 should see e2")
	}
	if hasE3 {
		t.Error("e1 should not see e3")
	}

	// Move e3 close to e1
	entered, _ := aoi.Move("e3", 6, 6)
	foundE1 := false
	for _, e := range entered {
		if e.ID == "e1" || e.ID == "e2" {
			foundE1 = true
		}
	}
	if !foundE1 {
		t.Error("moving e3 close should trigger enter events")
	}

	// Remove
	removed := aoi.Remove("e2")
	if removed == nil || removed.ID != "e2" {
		t.Error("should remove e2")
	}
	if aoi.EntityCount() != 2 {
		t.Errorf("expected 2 entities after remove, got %d", aoi.EntityCount())
	}
}

func TestCrossLinkedListAOIEdgeCases(t *testing.T) {
	aoi := NewCrossLinkedListAOI(100, 100, 10)

	// Move non-existent entity
	entered, left := aoi.Move("nope", 10, 10)
	if entered != nil || left != nil {
		t.Error("move non-existent should return nil")
	}

	// Remove non-existent
	e := aoi.Remove("nope")
	if e != nil {
		t.Error("remove non-existent should return nil")
	}

	// Get non-existent
	if aoi.Get("nope") != nil {
		t.Error("get non-existent should return nil")
	}

	// Duplicate add
	aoi.Add(NewGridEntity("dup", 5, 5, nil, nil))
	aoi.Add(NewGridEntity("dup", 50, 50, nil, nil)) // should be ignored
	if aoi.EntityCount() != 1 {
		t.Error("duplicate add should be ignored")
	}
	if e := aoi.Get("dup"); e.X != 5 {
		t.Error("duplicate add should keep original position")
	}
}

func TestLighthouseAOIEdgeCases(t *testing.T) {
	aoi := NewLighthouseAOI(100, 100, 10, 10)

	// Boundary positions
	aoi.Add(NewGridEntity("corner", 0, 0, nil, nil))
	aoi.Add(NewGridEntity("far", 99, 99, nil, nil))

	nearby := aoi.GetNearby("corner")
	for _, e := range nearby {
		if e.ID == "far" {
			t.Error("corner should not see far entity")
		}
	}

	// Move to far corner
	entered, left := aoi.Move("corner", 95, 95)
	hasFar := false
	for _, e := range entered {
		if e.ID == "far" {
			hasFar = true
		}
	}
	if !hasFar {
		t.Error("moving corner close to far should trigger enter")
	}
	_ = left
}

// 基准测试：对比三种 AOI 算法在不同实体密度下的性能

func benchmarkAOIAdd(b *testing.B, aoi AOI, count int) {
	entities := make([]*GridEntity, count)
	for i := 0; i < count; i++ {
		entities[i] = NewGridEntity(
			fmt.Sprintf("e%d", i),
			rand.Float32()*1000,
			rand.Float32()*1000,
			nil, nil,
		)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, e := range entities {
			aoi.Add(e)
		}
	}
}

func benchmarkAOIMove(b *testing.B, aoi AOI) {
	ids := make([]string, 0)
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("e%d", i)
		aoi.Add(NewGridEntity(id, rand.Float32()*1000, rand.Float32()*1000, nil, nil))
		ids = append(ids, id)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := ids[i%len(ids)]
		aoi.Move(id, rand.Float32()*1000, rand.Float32()*1000)
	}
}

func benchmarkAOIGetNearby(b *testing.B, aoi AOI) {
	ids := make([]string, 0)
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("e%d", i)
		aoi.Add(NewGridEntity(id, rand.Float32()*1000, rand.Float32()*1000, nil, nil))
		ids = append(ids, id)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		aoi.GetNearby(ids[i%len(ids)])
	}
}

func BenchmarkGrid_Add500(b *testing.B) {
	aoi := NewGridAOI(1000, 1000, 50)
	benchmarkAOIAdd(b, aoi, 500)
}

func BenchmarkCrossLink_Add500(b *testing.B) {
	aoi := NewCrossLinkedListAOI(1000, 1000, 50)
	benchmarkAOIAdd(b, aoi, 500)
}

func BenchmarkLighthouse_Add500(b *testing.B) {
	aoi := NewLighthouseAOI(1000, 1000, 50, 50)
	benchmarkAOIAdd(b, aoi, 500)
}

func BenchmarkGrid_Move(b *testing.B) {
	aoi := NewGridAOI(1000, 1000, 50)
	benchmarkAOIMove(b, aoi)
}

func BenchmarkCrossLink_Move(b *testing.B) {
	aoi := NewCrossLinkedListAOI(1000, 1000, 50)
	benchmarkAOIMove(b, aoi)
}

func BenchmarkLighthouse_Move(b *testing.B) {
	aoi := NewLighthouseAOI(1000, 1000, 50, 50)
	benchmarkAOIMove(b, aoi)
}

func BenchmarkGrid_GetNearby(b *testing.B) {
	aoi := NewGridAOI(1000, 1000, 50)
	benchmarkAOIGetNearby(b, aoi)
}

func BenchmarkCrossLink_GetNearby(b *testing.B) {
	aoi := NewCrossLinkedListAOI(1000, 1000, 50)
	benchmarkAOIGetNearby(b, aoi)
}

func BenchmarkLighthouse_GetNearby(b *testing.B) {
	aoi := NewLighthouseAOI(1000, 1000, 50, 50)
	benchmarkAOIGetNearby(b, aoi)
}
