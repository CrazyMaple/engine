package ecs

import (
	"sync/atomic"
	"testing"
	"time"
)

// --- test systems ---

type movementSystem struct {
	updateCount int
}

func (s *movementSystem) Name() string     { return "Movement" }
func (s *movementSystem) Priority() int    { return 10 }
func (s *movementSystem) Update(w *World, dt time.Duration) {
	s.updateCount++
	entities := w.QueryMulti("Position", "Movement")
	for _, e := range entities {
		pos := e.GetPosition()
		mov, ok := e.Get("Movement")
		if pos == nil || !ok {
			continue
		}
		m := mov.(*Movement)
		seconds := float32(dt.Seconds())
		pos.X += m.VelocityX * seconds
		pos.Y += m.VelocityY * seconds
		pos.Z += m.VelocityZ * seconds
	}
}

type healthSystem struct {
	updateCount int
}

func (s *healthSystem) Name() string     { return "Health" }
func (s *healthSystem) Priority() int    { return 20 }
func (s *healthSystem) Update(w *World, dt time.Duration) {
	s.updateCount++
	entities := w.Query("Health")
	for _, e := range entities {
		h := e.GetHealth()
		if h != nil && h.Current > h.Max {
			h.Current = h.Max
		}
	}
}

type parallelCounterSystem struct {
	name    string
	counter *int64
}

func (s *parallelCounterSystem) Name() string     { return s.name }
func (s *parallelCounterSystem) Priority() int    { return 10 }
func (s *parallelCounterSystem) CanParallel() bool { return true }
func (s *parallelCounterSystem) Update(_ *World, _ time.Duration) {
	atomic.AddInt64(s.counter, 1)
}

type serialSystem struct {
	name    string
	counter *int64
}

func (s *serialSystem) Name() string     { return s.name }
func (s *serialSystem) Priority() int    { return 10 }
func (s *serialSystem) Update(_ *World, _ time.Duration) {
	atomic.AddInt64(s.counter, 1)
}

// --- tests ---

func TestSystemGroupUpdate(t *testing.T) {
	world := NewWorld()
	e := NewEntity("player")
	e.Add(&Position{X: 0, Y: 0, Z: 0})
	e.Add(&Movement{Speed: 5, VelocityX: 1, VelocityY: 2, VelocityZ: 0})
	world.Add(e)

	sg := NewSystemGroup()
	ms := &movementSystem{}
	hs := &healthSystem{}
	sg.Add(ms, hs)

	// 模拟 100ms 帧
	sg.Update(world, 100*time.Millisecond)

	pos := e.GetPosition()
	if pos.X != 0.1 || pos.Y != 0.2 {
		t.Errorf("expected (0.1, 0.2), got (%.2f, %.2f)", pos.X, pos.Y)
	}
	if ms.updateCount != 1 {
		t.Errorf("movement system should update 1 time, got %d", ms.updateCount)
	}
	if hs.updateCount != 1 {
		t.Errorf("health system should update 1 time, got %d", hs.updateCount)
	}
}

func TestSystemGroupPriority(t *testing.T) {
	sg := NewSystemGroup()

	var order []string
	sg.Add(&orderSystem{name: "C", prio: 30, order: &order})
	sg.Add(&orderSystem{name: "A", prio: 10, order: &order})
	sg.Add(&orderSystem{name: "B", prio: 20, order: &order})

	sg.Update(NewWorld(), time.Millisecond)

	if len(order) != 3 || order[0] != "A" || order[1] != "B" || order[2] != "C" {
		t.Errorf("expected order [A B C], got %v", order)
	}
}

type orderSystem struct {
	name  string
	prio  int
	order *[]string
}

func (s *orderSystem) Name() string     { return s.name }
func (s *orderSystem) Priority() int    { return s.prio }
func (s *orderSystem) Update(_ *World, _ time.Duration) {
	*s.order = append(*s.order, s.name)
}

func TestSystemGroupRemove(t *testing.T) {
	sg := NewSystemGroup()
	sg.Add(&movementSystem{}, &healthSystem{})

	if sg.Count() != 2 {
		t.Fatalf("expected 2 systems, got %d", sg.Count())
	}

	sg.Remove("Movement")
	if sg.Count() != 1 {
		t.Fatalf("expected 1 system after remove, got %d", sg.Count())
	}
}

func TestParallelUpdate(t *testing.T) {
	sg := NewSystemGroup()
	var counter int64

	sg.Add(
		&parallelCounterSystem{name: "P1", counter: &counter},
		&parallelCounterSystem{name: "P2", counter: &counter},
		&parallelCounterSystem{name: "P3", counter: &counter},
	)

	sg.UpdateParallel(NewWorld(), time.Millisecond)

	if atomic.LoadInt64(&counter) != 3 {
		t.Errorf("expected 3 parallel updates, got %d", counter)
	}
}

func TestParallelWithSerialMixed(t *testing.T) {
	sg := NewSystemGroup()
	var counter int64

	// 同优先级但混合 parallel 和 serial
	sg.Add(
		&parallelCounterSystem{name: "P1", counter: &counter},
		&parallelCounterSystem{name: "P2", counter: &counter},
		&serialSystem{name: "S1", counter: &counter},
	)

	sg.UpdateParallel(NewWorld(), time.Millisecond)

	if atomic.LoadInt64(&counter) != 3 {
		t.Errorf("expected 3 total updates, got %d", counter)
	}
}

func TestTicker(t *testing.T) {
	world := NewWorld()
	sg := NewSystemGroup()
	ms := &movementSystem{}
	sg.Add(ms)

	e := NewEntity("p1")
	e.Add(&Position{X: 0, Y: 0, Z: 0})
	e.Add(&Movement{VelocityX: 100}) // 100 单位/秒
	world.Add(e)

	ticker := NewTicker(world, sg, 20) // 20 FPS = 50ms 间隔

	// 手动 TickOnce
	ticker.TickOnce(50 * time.Millisecond)
	if ticker.FrameCount() != 1 {
		t.Errorf("expected frame count 1, got %d", ticker.FrameCount())
	}

	pos := e.GetPosition()
	if pos.X != 5.0 { // 100 * 0.05 = 5
		t.Errorf("expected X=5.0, got %.2f", pos.X)
	}
}

func TestTickerFixedStep(t *testing.T) {
	world := NewWorld()
	sg := NewSystemGroup()
	ms := &movementSystem{}
	sg.Add(ms)

	ticker := NewTicker(world, sg, 20) // 50ms interval

	// 模拟 120ms 流逝（应该执行 2 帧）
	ticker.lastTick = time.Now().Add(-120 * time.Millisecond)
	ticker.Tick()

	if ticker.FrameCount() != 2 {
		t.Errorf("expected 2 frames for 120ms at 20fps, got %d", ticker.FrameCount())
	}
}

func TestTickerParallel(t *testing.T) {
	world := NewWorld()
	sg := NewSystemGroup()
	var counter int64
	sg.Add(
		&parallelCounterSystem{name: "P1", counter: &counter},
		&parallelCounterSystem{name: "P2", counter: &counter},
	)

	ticker := NewTicker(world, sg, 10)
	ticker.SetParallel(true)
	ticker.TickOnce(100 * time.Millisecond)

	if atomic.LoadInt64(&counter) != 2 {
		t.Errorf("expected 2, got %d", counter)
	}
}
