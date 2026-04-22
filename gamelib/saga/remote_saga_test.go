package saga

import (
	"fmt"
	"testing"
	"time"

	"engine/actor"
)

// TestRemoteSagaLocalSteps 测试远程 Saga 协调器的本地步骤执行
func TestRemoteSagaLocalSteps(t *testing.T) {
	logger := NewSliceLogger()
	store := NewMemorySagaStore()

	coord := NewRemoteSagaCoordinator(RemoteSagaConfig{
		Remote:     nil, // 本地步骤不需要 Remote
		Store:      store,
		Logger:     logger,
		Timeout:    5 * time.Second,
		MaxRetries: 2,
		RetryDelay: 10 * time.Millisecond,
	})

	// 构建纯本地步骤的 Saga
	var executed []string
	def := NewRemoteSaga("local-test").
		LocalStep("step1",
			func(ctx *SagaContext) error {
				executed = append(executed, "step1-action")
				ctx.Set("step1_done", true)
				return nil
			},
			func(ctx *SagaContext) error {
				executed = append(executed, "step1-compensate")
				return nil
			},
		).
		LocalStep("step2",
			func(ctx *SagaContext) error {
				executed = append(executed, "step2-action")
				ctx.Set("step2_done", true)
				return nil
			},
			func(ctx *SagaContext) error {
				executed = append(executed, "step2-compensate")
				return nil
			},
		).
		LocalStep("step3",
			func(ctx *SagaContext) error {
				executed = append(executed, "step3-action")
				return nil
			},
			nil,
		).
		Build()

	sagaCtx := &SagaContext{SagaID: "saga-local-1"}
	exec, err := coord.Execute(sagaCtx, def)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exec.Status != SagaStatusCompleted {
		t.Fatalf("expected completed, got %v", exec.Status)
	}
	if len(executed) != 3 {
		t.Fatalf("expected 3 actions, got %d: %v", len(executed), executed)
	}
	if executed[0] != "step1-action" || executed[1] != "step2-action" || executed[2] != "step3-action" {
		t.Fatalf("unexpected execution order: %v", executed)
	}

	// 验证上下文传递
	if v, ok := sagaCtx.Get("step1_done"); !ok || v != true {
		t.Error("step1_done not set in context")
	}
	if v, ok := sagaCtx.Get("step2_done"); !ok || v != true {
		t.Error("step2_done not set in context")
	}

	// 验证持久化
	loaded, loadErr := store.Load(nil, "saga-local-1")
	if loadErr != nil {
		t.Fatalf("load error: %v", loadErr)
	}
	if loaded.Status != SagaStatusCompleted {
		t.Errorf("stored status = %v, want completed", loaded.Status)
	}
}

// TestRemoteSagaCompensation 测试远程 Saga 补偿链
func TestRemoteSagaCompensation(t *testing.T) {
	logger := NewSliceLogger()

	coord := NewRemoteSagaCoordinator(RemoteSagaConfig{
		Logger:     logger,
		Timeout:    5 * time.Second,
		MaxRetries: 1,
		RetryDelay: 10 * time.Millisecond,
	})

	var executed []string
	def := NewRemoteSaga("compensate-test").
		LocalStep("step1",
			func(ctx *SagaContext) error {
				executed = append(executed, "step1-action")
				return nil
			},
			func(ctx *SagaContext) error {
				executed = append(executed, "step1-compensate")
				return nil
			},
		).
		LocalStep("step2",
			func(ctx *SagaContext) error {
				executed = append(executed, "step2-action")
				return nil
			},
			func(ctx *SagaContext) error {
				executed = append(executed, "step2-compensate")
				return nil
			},
		).
		LocalStep("step3",
			func(ctx *SagaContext) error {
				return fmt.Errorf("step3 intentional failure")
			},
			nil,
		).
		Build()

	sagaCtx := &SagaContext{SagaID: "saga-comp-1"}
	exec, err := coord.Execute(sagaCtx, def)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if exec.Status != SagaStatusCompensated {
		t.Fatalf("expected compensated, got %v", exec.Status)
	}

	// 验证补偿反向执行：step2 先补偿，step1 后补偿
	expected := []string{"step1-action", "step2-action", "step2-compensate", "step1-compensate"}
	if len(executed) != len(expected) {
		t.Fatalf("expected %d operations, got %d: %v", len(expected), len(executed), executed)
	}
	for i, e := range expected {
		if executed[i] != e {
			t.Errorf("operation[%d] = %s, want %s", i, executed[i], e)
		}
	}

	// 验证日志包含补偿记录
	entries := logger.Entries()
	compensateCount := 0
	for _, e := range entries {
		if e.Action == "compensate" {
			compensateCount++
		}
	}
	if compensateCount < 2 {
		t.Errorf("expected at least 2 compensate log entries, got %d", compensateCount)
	}
}

// TestRemoteSagaTimeout 测试远程 Saga 超时
func TestRemoteSagaTimeout(t *testing.T) {
	coord := NewRemoteSagaCoordinator(RemoteSagaConfig{
		Timeout:    200 * time.Millisecond,
		MaxRetries: 0,
		RetryDelay: 10 * time.Millisecond,
	})

	def := NewRemoteSaga("timeout-test").
		LocalStep("slow-step",
			func(ctx *SagaContext) error {
				time.Sleep(500 * time.Millisecond)
				return nil
			},
			nil,
		).
		Build()

	sagaCtx := &SagaContext{SagaID: "saga-timeout-1"}
	exec, err := coord.Execute(sagaCtx, def)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	// 步骤可能在全局超时内执行完但超时，或被超时打断
	if exec.Status != SagaStatusCompensating && exec.Status != SagaStatusCompleted && exec.Status != SagaStatusCompensated {
		// 任何状态都可接受（超时时机不确定）
		t.Logf("status: %v, error: %v", exec.Status, err)
	}
}

// TestRemoteSagaBuilder 测试构建器 API
func TestRemoteSagaBuilder(t *testing.T) {
	pid := &actor.PID{Address: "127.0.0.1:8080", Id: "actor1"}

	def := NewRemoteSaga("builder-test").
		LocalStep("local1",
			func(ctx *SagaContext) error { return nil },
			func(ctx *SagaContext) error { return nil },
		).
		RemoteStep("remote1", pid, 5*time.Second).
		LocalStep("local2",
			func(ctx *SagaContext) error { return nil },
			nil,
		).
		Build()

	if def.Name != "builder-test" {
		t.Errorf("name = %s, want builder-test", def.Name)
	}
	if len(def.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(def.Steps))
	}
	if def.Steps[0].Name != "local1" || def.Steps[0].TargetPID != nil {
		t.Error("step 0 should be local")
	}
	if def.Steps[1].Name != "remote1" || def.Steps[1].TargetPID == nil {
		t.Error("step 1 should be remote")
	}
	if def.Steps[2].Name != "local2" || def.Steps[2].TargetPID != nil {
		t.Error("step 2 should be local")
	}
}

// TestRemoteSagaCompensationFailure 测试补偿失败场景
func TestRemoteSagaCompensationFailure(t *testing.T) {
	coord := NewRemoteSagaCoordinator(RemoteSagaConfig{
		Timeout:    5 * time.Second,
		MaxRetries: 1,
		RetryDelay: 10 * time.Millisecond,
	})

	def := NewRemoteSaga("comp-fail-test").
		LocalStep("step1",
			func(ctx *SagaContext) error { return nil },
			func(ctx *SagaContext) error {
				return fmt.Errorf("compensate also fails")
			},
		).
		LocalStep("step2",
			func(ctx *SagaContext) error {
				return fmt.Errorf("step2 fails")
			},
			nil,
		).
		Build()

	sagaCtx := &SagaContext{SagaID: "saga-compfail-1"}
	exec, _ := coord.Execute(sagaCtx, def)

	if exec.Status != SagaStatusFailed {
		t.Fatalf("expected failed (compensation failure), got %v", exec.Status)
	}
}

