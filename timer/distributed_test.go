package timer

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestDistributedScheduler_RegisterAndExecute(t *testing.T) {
	disp := NewDispatcher(100)
	store := NewMemoryTaskStore()
	var executed int32

	sched := NewDistributedScheduler(DistributedSchedulerConfig{
		Dispatcher: disp,
		Store:      store,
		NodeID:     "node-1",
		IsLeader:   func() bool { return true },
	})

	err := sched.Register(&TaskDef{
		ID:       "task-1",
		Name:     "test task",
		CronExpr: "* * * * *",
		Handler: func() error {
			atomic.AddInt32(&executed, 1)
			return nil
		},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	tasks := sched.ListTasks()
	if len(tasks) != 1 {
		t.Errorf("tasks = %d, want 1", len(tasks))
	}

	sched.Stop()
}

func TestDistributedScheduler_NonLeaderSkips(t *testing.T) {
	disp := NewDispatcher(100)
	store := NewMemoryTaskStore()
	var executed int32

	sched := NewDistributedScheduler(DistributedSchedulerConfig{
		Dispatcher: disp,
		Store:      store,
		NodeID:     "node-2",
		IsLeader:   func() bool { return false },
	})

	task := &TaskDef{
		ID:       "task-2",
		Name:     "non-leader task",
		CronExpr: "* * * * *",
		Handler: func() error {
			atomic.AddInt32(&executed, 1)
			return nil
		},
	}

	// 直接调用 executeTask 模拟
	sched.executeTask(task)

	if atomic.LoadInt32(&executed) != 0 {
		t.Error("non-leader should not execute")
	}

	sched.Stop()
}

func TestDistributedScheduler_TaskLogging(t *testing.T) {
	disp := NewDispatcher(100)
	store := NewMemoryTaskStore()

	sched := NewDistributedScheduler(DistributedSchedulerConfig{
		Dispatcher: disp,
		Store:      store,
		NodeID:     "node-1",
		IsLeader:   func() bool { return true },
	})

	// 成功任务
	sched.executeTask(&TaskDef{
		ID:      "t1",
		Name:    "success task",
		Handler: func() error { return nil },
		Timeout: 5 * time.Second,
	})

	logs, _ := store.RecentLogs("t1", 10)
	if len(logs) != 1 {
		t.Fatalf("logs = %d", len(logs))
	}
	if logs[0].Status != TaskSuccess {
		t.Errorf("status = %s, want success", logs[0].Status)
	}

	// 失败任务
	sched.executeTask(&TaskDef{
		ID:      "t2",
		Name:    "fail task",
		Handler: func() error { return &testErr{} },
		Timeout: 5 * time.Second,
	})

	logs, _ = store.RecentLogs("t2", 10)
	if len(logs) != 1 {
		t.Fatalf("t2 logs = %d", len(logs))
	}
	if logs[0].Status != TaskFailed {
		t.Errorf("t2 status = %s, want failed", logs[0].Status)
	}

	sched.Stop()
}

func TestDistributedScheduler_TaskTimeout(t *testing.T) {
	disp := NewDispatcher(100)
	store := NewMemoryTaskStore()

	sched := NewDistributedScheduler(DistributedSchedulerConfig{
		Dispatcher: disp,
		Store:      store,
		NodeID:     "node-1",
		IsLeader:   func() bool { return true },
	})

	sched.executeTask(&TaskDef{
		ID:   "t3",
		Name: "timeout task",
		Handler: func() error {
			time.Sleep(200 * time.Millisecond)
			return nil
		},
		Timeout: 50 * time.Millisecond,
	})

	logs, _ := store.RecentLogs("t3", 10)
	if len(logs) != 1 {
		t.Fatalf("t3 logs = %d", len(logs))
	}
	if logs[0].Status != TaskTimeout {
		t.Errorf("t3 status = %s, want timeout", logs[0].Status)
	}

	sched.Stop()
}

func TestDistributedScheduler_Unregister(t *testing.T) {
	disp := NewDispatcher(100)
	store := NewMemoryTaskStore()

	sched := NewDistributedScheduler(DistributedSchedulerConfig{
		Dispatcher: disp,
		Store:      store,
		NodeID:     "node-1",
	})

	_ = sched.Register(&TaskDef{
		ID:       "t4",
		Name:     "task to remove",
		CronExpr: "* * * * *",
		Handler:  func() error { return nil },
	})

	sched.Unregister("t4")

	if len(sched.ListTasks()) != 0 {
		t.Error("task not removed")
	}

	sched.Stop()
}

func TestMemoryTaskStore_PersistAndLoad(t *testing.T) {
	store := NewMemoryTaskStore()

	_ = store.SaveTask(&TaskDef{ID: "a", Name: "task-a", CronExpr: "*/5 * * * *"})
	_ = store.SaveTask(&TaskDef{ID: "b", Name: "task-b", CronExpr: "0 * * * *"})

	tasks, _ := store.LoadTasks()
	if len(tasks) != 2 {
		t.Errorf("loaded %d tasks", len(tasks))
	}

	_ = store.DeleteTask("a")
	tasks, _ = store.LoadTasks()
	if len(tasks) != 1 {
		t.Errorf("after delete: %d tasks", len(tasks))
	}
}

func TestMemoryTaskStore_Logs(t *testing.T) {
	store := NewMemoryTaskStore()

	for i := 0; i < 5; i++ {
		_ = store.AppendLog(&TaskLog{
			TaskID:   "t1",
			TaskName: "test",
			Status:   TaskSuccess,
		})
	}

	logs, _ := store.RecentLogs("t1", 3)
	if len(logs) != 3 {
		t.Errorf("recent logs = %d, want 3", len(logs))
	}

	// 全部日志
	all, _ := store.RecentLogs("", 100)
	if len(all) != 5 {
		t.Errorf("all logs = %d, want 5", len(all))
	}
}

type testErr struct{}

func (e *testErr) Error() string { return "test error" }
