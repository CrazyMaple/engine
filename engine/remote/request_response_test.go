package remote

import (
	"testing"
	"time"

	"engine/actor"
)

func TestRemoteFutureRegistry_RegisterAndComplete(t *testing.T) {
	reg := NewRemoteFutureRegistry()

	future := actor.NewFuture(5 * time.Second)
	futurePID := actor.GeneratePID()
	future.SetPID(futurePID)

	// 注册 Future 进程
	futureProc := &actor.FutureProcess_Export{Future: future}
	actor.DefaultSystem().ProcessRegistry.Add(futurePID, futureProc)

	// 注册到远程 Future 注册表
	requestID := reg.Register(future, futurePID)

	if requestID == "" {
		t.Fatal("expected non-empty request ID")
	}

	if reg.PendingCount() != 1 {
		t.Fatalf("expected 1 pending, got %d", reg.PendingCount())
	}

	// 完成 Future
	ok := reg.Complete(requestID, "hello-response", "")
	if !ok {
		t.Fatal("expected Complete to return true")
	}

	if reg.PendingCount() != 0 {
		t.Fatalf("expected 0 pending after complete, got %d", reg.PendingCount())
	}

	// 验证 Future 收到结果
	result, err := future.Wait()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello-response" {
		t.Fatalf("expected 'hello-response', got %v", result)
	}
}

func TestRemoteFutureRegistry_CompleteWithError(t *testing.T) {
	reg := NewRemoteFutureRegistry()

	future := actor.NewFuture(5 * time.Second)
	futurePID := actor.GeneratePID()
	future.SetPID(futurePID)

	futureProc := &actor.FutureProcess_Export{Future: future}
	actor.DefaultSystem().ProcessRegistry.Add(futurePID, futureProc)

	requestID := reg.Register(future, futurePID)
	reg.Complete(requestID, nil, "something went wrong")

	_, err := future.Wait()
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "remote request: something went wrong" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestRemoteFutureRegistry_CompleteMissing(t *testing.T) {
	reg := NewRemoteFutureRegistry()

	ok := reg.Complete("nonexistent-id", "data", "")
	if ok {
		t.Fatal("expected false for missing request ID")
	}
}

func TestRemoteRequestMessage_TypeRegistration(t *testing.T) {
	// Verify that RemoteRequestMessage and RemoteResponseMessage are registered
	_, ok := defaultTypeRegistry.GetTypeName(&RemoteRequestMessage{})
	if !ok {
		t.Error("RemoteRequestMessage not registered in TypeRegistry")
	}

	_, ok = defaultTypeRegistry.GetTypeName(&RemoteResponseMessage{})
	if !ok {
		t.Error("RemoteResponseMessage not registered in TypeRegistry")
	}
}

func TestGenerateRequestID_Unique(t *testing.T) {
	ids := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id := generateRequestID()
		if ids[id] {
			t.Fatalf("duplicate ID generated: %s", id)
		}
		ids[id] = true
	}
}
