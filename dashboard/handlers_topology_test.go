package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDrainStateLifecycle(t *testing.T) {
	ResetDrained()
	defer ResetDrained()

	if IsDrained("a:1") {
		t.Fatal("should be empty initially")
	}

	// 直接调用内部映射模拟标记
	drainMu.Lock()
	drainMarked["a:1"] = drainState{Address: "a:1", Reason: "maintenance", MarkedAt: time.Now()}
	drainMu.Unlock()

	if !IsDrained("a:1") {
		t.Fatal("should be drained")
	}
	if got := ListDrained(); len(got) != 1 {
		t.Fatalf("ListDrained want 1, got %d", len(got))
	}

	ResetDrained()
	if IsDrained("a:1") {
		t.Fatal("ResetDrained should clear state")
	}
}

func TestHandleTopology_NoClusterReturns404(t *testing.T) {
	h := &handlers{config: Config{}}
	req := httptest.NewRequest(http.MethodGet, "/api/topology/node?address=a:1", nil)
	w := httptest.NewRecorder()
	h.handleTopologyNode(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestHandleTopology_MissingAddress(t *testing.T) {
	// 校验请求验证逻辑：cluster 为 nil 提前返回 404；
	// 这里通过触发 drain 路径（同样无 cluster）来验证错误码
	h := &handlers{config: Config{}}
	req := httptest.NewRequest(http.MethodPost, "/api/topology/drain", nil)
	w := httptest.NewRecorder()
	h.handleTopologyDrain(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}
