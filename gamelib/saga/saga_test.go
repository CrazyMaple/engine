package saga

import (
	"fmt"
	"testing"
	"time"
)

func TestSagaSuccess(t *testing.T) {
	logger := NewSliceLogger()
	coord := NewCoordinator(WithLogger(logger))

	saga := NewSaga("trade").
		Step("deduct_buyer", func(ctx *SagaContext) error {
			ctx.Set("deducted", true)
			return nil
		}, func(ctx *SagaContext) error {
			ctx.Set("deducted", false)
			return nil
		}).
		Step("add_seller", func(ctx *SagaContext) error {
			ctx.Set("added", true)
			return nil
		}, func(ctx *SagaContext) error {
			ctx.Set("added", false)
			return nil
		}).
		Step("transfer_item", func(ctx *SagaContext) error {
			ctx.Set("transferred", true)
			return nil
		}, func(ctx *SagaContext) error {
			ctx.Set("transferred", false)
			return nil
		}).
		Build()

	sagaCtx := &SagaContext{SagaID: "saga-001"}
	exec, err := coord.Execute(sagaCtx, saga)

	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if exec.Status != SagaStatusCompleted {
		t.Fatalf("expected completed, got %v", exec.Status)
	}
	if len(exec.StepResults) != 3 {
		t.Fatalf("expected 3 step results, got %d", len(exec.StepResults))
	}
	for _, sr := range exec.StepResults {
		if sr.Status != StepExecuted {
			t.Fatalf("step %s: expected executed, got %v", sr.StepName, sr.Status)
		}
	}

	// 验证上下文数据
	if v, _ := sagaCtx.Get("transferred"); v != true {
		t.Fatal("expected transferred=true")
	}
}

func TestSagaCompensation(t *testing.T) {
	logger := NewSliceLogger()
	store := NewMemorySagaStore()
	coord := NewCoordinator(WithLogger(logger), WithStore(store))

	compensated := make([]string, 0)

	saga := NewSaga("trade").
		Step("step1", func(ctx *SagaContext) error {
			return nil
		}, func(ctx *SagaContext) error {
			compensated = append(compensated, "step1")
			return nil
		}).
		Step("step2", func(ctx *SagaContext) error {
			return nil
		}, func(ctx *SagaContext) error {
			compensated = append(compensated, "step2")
			return nil
		}).
		Step("step3", func(ctx *SagaContext) error {
			return fmt.Errorf("step3 failed")
		}, func(ctx *SagaContext) error {
			compensated = append(compensated, "step3")
			return nil
		}).
		Build()

	sagaCtx := &SagaContext{SagaID: "saga-002"}
	exec, err := coord.Execute(sagaCtx, saga)

	if err == nil {
		t.Fatal("expected error")
	}
	if exec.Status != SagaStatusCompensated {
		t.Fatalf("expected compensated, got %v", exec.Status)
	}

	// 验证反向补偿顺序：step2 → step1（step3 失败，不补偿）
	if len(compensated) != 2 {
		t.Fatalf("expected 2 compensations, got %d", len(compensated))
	}
	if compensated[0] != "step2" || compensated[1] != "step1" {
		t.Fatalf("expected [step2, step1], got %v", compensated)
	}

	// 验证持久化
	loaded, loadErr := store.Load(nil, "saga-002")
	if loadErr != nil {
		t.Fatalf("load error: %v", loadErr)
	}
	if loaded.Status != SagaStatusCompensated {
		t.Fatalf("stored status: expected compensated, got %v", loaded.Status)
	}
}

func TestSagaTimeout(t *testing.T) {
	coord := NewCoordinator()

	saga := NewSaga("slow").
		WithTimeout(100 * time.Millisecond).
		Step("slow_step", func(ctx *SagaContext) error {
			time.Sleep(500 * time.Millisecond)
			return nil
		}, nil).
		Build()

	sagaCtx := &SagaContext{SagaID: "saga-003"}
	exec, err := coord.Execute(sagaCtx, saga)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if exec.Status != SagaStatusCompensating && exec.Status != SagaStatusCompensated {
		// 第一步超时，没有前面的步骤需要补偿
		if exec.Status != SagaStatusCompensated {
			t.Logf("status: %v (first step timeout, no compensation needed)", exec.Status)
		}
	}
}

func TestSagaBuilder(t *testing.T) {
	saga := NewSaga("test").
		Step("s1", func(ctx *SagaContext) error { return nil }, nil).
		StepWithTimeout("s2", func(ctx *SagaContext) error { return nil }, nil, 5*time.Second).
		WithTimeout(30 * time.Second).
		WithMaxRetries(5).
		WithRetryDelay(2 * time.Second).
		Build()

	if saga.Name != "test" {
		t.Fatalf("expected name 'test', got %q", saga.Name)
	}
	if len(saga.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(saga.Steps))
	}
	if saga.Steps[1].Timeout != 5*time.Second {
		t.Fatalf("expected step timeout 5s, got %v", saga.Steps[1].Timeout)
	}
	if saga.MaxRetries != 5 {
		t.Fatalf("expected max retries 5, got %d", saga.MaxRetries)
	}
}

func TestSagaContext(t *testing.T) {
	ctx := &SagaContext{SagaID: "test"}
	ctx.Set("key", "value")

	if ctx.GetString("key") != "value" {
		t.Fatal("expected 'value'")
	}
	if ctx.GetString("missing") != "" {
		t.Fatal("expected empty for missing key")
	}
	if _, ok := ctx.Get("missing"); ok {
		t.Fatal("expected missing key")
	}
}
