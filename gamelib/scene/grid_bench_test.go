package scene

import (
	"fmt"
	"math/rand"
	"testing"
)

func setupGrid(entityCount int) *Grid {
	g := NewGrid(GridConfig{
		Width:    1000,
		Height:   1000,
		CellSize: 50,
	})

	for i := 0; i < entityCount; i++ {
		g.Add(&GridEntity{
			ID: fmt.Sprintf("entity-%d", i),
			X:  rand.Float32() * 1000,
			Y:  rand.Float32() * 1000,
		})
	}
	return g
}

func BenchmarkGridAdd(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("entities_%d", count), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				g := NewGrid(GridConfig{Width: 1000, Height: 1000, CellSize: 50})
				for i := 0; i < count; i++ {
					g.Add(&GridEntity{
						ID: fmt.Sprintf("e-%d", i),
						X:  rand.Float32() * 1000,
						Y:  rand.Float32() * 1000,
					})
				}
			}
		})
	}
}

func BenchmarkGridMove(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("entities_%d", count), func(b *testing.B) {
			g := setupGrid(count)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				id := fmt.Sprintf("entity-%d", i%count)
				g.Move(id, rand.Float32()*1000, rand.Float32()*1000)
			}
		})
	}
}

func BenchmarkGridGetAOI(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("entities_%d", count), func(b *testing.B) {
			g := setupGrid(count)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				id := fmt.Sprintf("entity-%d", i%count)
				g.GetAOI(id)
			}
		})
	}
}

func BenchmarkGridRemove(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprintf("entities_%d", count), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				b.StopTimer()
				g := setupGrid(count)
				b.StartTimer()
				for i := 0; i < count; i++ {
					g.Remove(fmt.Sprintf("entity-%d", i))
				}
			}
		})
	}
}
