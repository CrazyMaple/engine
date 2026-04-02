//go:build ignore

package main

import (
	"fmt"
	"time"

	"engine/actor"
)

// ===== AllForOneStrategy 监管策略示例 =====
// 演示：当一个子 Actor 失败时，全部兄弟 Actor 一起重启

// WorkerActor 工作 Actor
type WorkerActor struct {
	id    int
	count int
}

func (w *WorkerActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		fmt.Printf("  Worker-%d started (restart count: %d)\n", w.id, w.count)
		w.count++

	case *actor.Restarting:
		fmt.Printf("  Worker-%d restarting\n", w.id)

	case string:
		if msg == "crash" {
			fmt.Printf("  Worker-%d crashing!\n", w.id)
			panic("simulated failure")
		}
		fmt.Printf("  Worker-%d received: %s\n", w.id, msg)

	case *actor.Stopping:
		fmt.Printf("  Worker-%d stopping\n", w.id)
	}
}

// SupervisorActor 监管 Actor
type SupervisorActor struct {
	workers []*actor.PID
}

func (s *SupervisorActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		fmt.Println("Supervisor started, spawning 3 workers...")

		// 使用 AllForOneStrategy：一个失败全部重启，最多重启 5 次 / 10 秒
		for i := 0; i < 3; i++ {
			idx := i
			props := actor.PropsFromProducer(func() actor.Actor {
				return &WorkerActor{id: idx}
			})
			pid := ctx.SpawnNamed(props, fmt.Sprintf("worker-%d", i))
			s.workers = append(s.workers, pid)
		}

	case string:
		if msg == "crash-worker-1" && len(s.workers) > 1 {
			// 让 worker-1 崩溃，观察所有 worker 是否都重启
			ctx.Send(s.workers[1], "crash")
		}
	}
}

func RunSupervisionExample() {
	fmt.Println("===== AllForOneStrategy Example =====")
	fmt.Println()

	system := actor.DefaultSystem()

	// 使用 AllForOneStrategy 监管策略
	strategy := actor.NewAllForOneStrategy(5, 10*time.Second, actor.DefaultDecider)

	props := actor.PropsFromProducer(func() actor.Actor {
		return &SupervisorActor{}
	}).WithSupervisor(strategy)

	supervisorPID := system.Root.Spawn(props)
	time.Sleep(100 * time.Millisecond)

	// 让 worker-1 崩溃 => 所有 worker 重启
	fmt.Println("\n--- Crashing Worker-1 (all workers should restart) ---")
	system.Root.Send(supervisorPID, "crash-worker-1")
	time.Sleep(200 * time.Millisecond)

	fmt.Println("\n===== Example Complete =====")
}
