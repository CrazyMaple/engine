package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"engine/actor"
	"gamelib/middleware"
)

func setupTestHandlers() *handlers {
	system := actor.NewActorSystem()
	metrics := middleware.NewMetrics()
	tracker := NewHotActorTracker()

	// 创建一个测试 Actor
	props := actor.PropsFromFunc(func(ctx actor.Context) {})
	system.Root.SpawnNamed(props, "test-actor-1")

	// 记录一些热点数据
	tracker.Record("test-actor-1", 5*time.Millisecond)
	tracker.Record("test-actor-1", 3*time.Millisecond)
	tracker.Record("test-actor-2", 10*time.Millisecond)

	return &handlers{
		config: Config{
			System:     system,
			Metrics:    metrics,
			HotTracker: tracker,
		},
	}
}

func TestHandleSystem(t *testing.T) {
	h := setupTestHandlers()

	req := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w := httptest.NewRecorder()
	h.handleSystem(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var info systemInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.ActorCount < 1 {
		t.Fatalf("expected at least 1 actor, got %d", info.ActorCount)
	}
}

func TestHandleActors(t *testing.T) {
	h := setupTestHandlers()

	req := httptest.NewRequest(http.MethodGet, "/api/actors", nil)
	w := httptest.NewRecorder()
	h.handleActors(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var actors []actorInfo
	if err := json.Unmarshal(w.Body.Bytes(), &actors); err != nil {
		t.Fatal(err)
	}
	if len(actors) < 1 {
		t.Fatal("expected at least 1 actor")
	}
}

func TestHandleHotActors(t *testing.T) {
	h := setupTestHandlers()

	req := httptest.NewRequest(http.MethodGet, "/api/hotactors?n=10", nil)
	w := httptest.NewRecorder()
	h.handleHotActors(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var stats []*ActorStats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 {
		t.Fatalf("expected 2 hot actors, got %d", len(stats))
	}
	// 应按消息量排序，test-actor-1 有 2 条
	if stats[0].PID != "test-actor-1" {
		t.Fatalf("expected test-actor-1 as top, got %s", stats[0].PID)
	}
	if stats[0].MsgCount != 2 {
		t.Fatalf("expected msg count 2, got %d", stats[0].MsgCount)
	}
}

func TestHandleMetrics(t *testing.T) {
	h := setupTestHandlers()

	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	w := httptest.NewRecorder()
	h.handleMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleMetricsPrometheus(t *testing.T) {
	h := setupTestHandlers()

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/prometheus", nil)
	w := httptest.NewRecorder()
	h.handleMetricsPrometheus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	contentType := w.Header().Get("Content-Type")
	if contentType != "text/plain; version=0.0.4; charset=utf-8" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
}

func TestHandleClusterNotConfigured(t *testing.T) {
	h := setupTestHandlers()

	req := httptest.NewRequest(http.MethodGet, "/api/cluster", nil)
	w := httptest.NewRecorder()
	h.handleCluster(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when cluster not configured, got %d", w.Code)
	}
}

func TestHandleMethodNotAllowed(t *testing.T) {
	h := setupTestHandlers()

	endpoints := []string{"/api/system", "/api/actors", "/api/cluster", "/api/metrics", "/api/hotactors"}
	for _, ep := range endpoints {
		req := httptest.NewRequest(http.MethodPost, ep, nil)
		w := httptest.NewRecorder()

		switch ep {
		case "/api/system":
			h.handleSystem(w, req)
		case "/api/actors":
			h.handleActors(w, req)
		case "/api/cluster":
			h.handleCluster(w, req)
		case "/api/metrics":
			h.handleMetrics(w, req)
		case "/api/hotactors":
			h.handleHotActors(w, req)
		}

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: expected 405, got %d", ep, w.Code)
		}
	}
}

func TestHandleIndex(t *testing.T) {
	h := setupTestHandlers()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.handleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatal("expected HTML content type")
	}
	if w.Body.Len() < 100 {
		t.Fatal("HTML body too short")
	}
}

func TestHotActorTracker(t *testing.T) {
	tracker := NewHotActorTracker()

	for i := 0; i < 100; i++ {
		tracker.Record("actor-a", time.Millisecond)
	}
	for i := 0; i < 50; i++ {
		tracker.Record("actor-b", 2*time.Millisecond)
	}
	for i := 0; i < 200; i++ {
		tracker.Record("actor-c", 500*time.Microsecond)
	}

	top := tracker.TopN(2)
	if len(top) != 2 {
		t.Fatalf("expected 2, got %d", len(top))
	}
	if top[0].PID != "actor-c" {
		t.Fatalf("expected actor-c as top, got %s", top[0].PID)
	}
	if top[0].MsgCount != 200 {
		t.Fatalf("expected 200, got %d", top[0].MsgCount)
	}

	// Test reset
	tracker.Reset()
	top = tracker.TopN(10)
	if len(top) != 0 {
		t.Fatal("expected empty after reset")
	}
}

func TestDashboardStartStop(t *testing.T) {
	system := actor.NewActorSystem()
	d := New(Config{
		Addr:   fmt.Sprintf("127.0.0.1:%d", 19100+time.Now().UnixNano()%100),
		System: system,
	})

	if err := d.Start(); err != nil {
		t.Fatal(err)
	}

	// 等待 HTTP 服务就绪
	time.Sleep(50 * time.Millisecond)

	if err := d.Stop(); err != nil {
		t.Fatal(err)
	}
}
