package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"engine/actor"
)

func TestExportReportJSON(t *testing.T) {
	sys := actor.NewActorSystem()
	h := &handlers{config: Config{System: sys}}

	report := h.ExportReport()
	if report.GeneratedAt == "" {
		t.Error("GeneratedAt empty")
	}
	if report.Runtime.GoVersion == "" {
		t.Error("GoVersion empty")
	}

	// JSON 可序列化
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if len(data) == 0 {
		t.Error("empty report JSON")
	}
}

func TestHandleReportJSON(t *testing.T) {
	sys := actor.NewActorSystem()
	h := &handlers{config: Config{System: sys}}

	req := httptest.NewRequest(http.MethodGet, "/api/report", nil)
	w := httptest.NewRecorder()

	h.handleReportJSON(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var report RuntimeReport
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Runtime.NumCPU == 0 {
		t.Error("NumCPU = 0")
	}
}

func TestHandleReportCSV(t *testing.T) {
	sys := actor.NewActorSystem()
	h := &handlers{config: Config{System: sys}}

	req := httptest.NewRequest(http.MethodGet, "/api/report.csv", nil)
	w := httptest.NewRecorder()

	h.handleReportCSV(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}
	body := w.Body.String()
	if body == "" {
		t.Error("empty CSV")
	}
}

func TestHandleHeatmap(t *testing.T) {
	sys := actor.NewActorSystem()
	h := &handlers{config: Config{System: sys}}

	req := httptest.NewRequest(http.MethodGet, "/api/actors/heatmap", nil)
	w := httptest.NewRecorder()

	h.handleHeatmap(w, req)

	// 没配置 HotTracker 应返回 404
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestLivePushServerCreateStop(t *testing.T) {
	sys := actor.NewActorSystem()
	cfg := Config{System: sys}

	lps := NewLivePushServer(cfg, LivePushConfig{})
	lps.Start()

	if lps.ClientCount() != 0 {
		t.Errorf("clients = %d, want 0", lps.ClientCount())
	}

	lps.Stop()
}

func TestFillRuntimeInfo(t *testing.T) {
	h := &handlers{}
	var info runtimeInfo
	h.fillRuntimeInfo(&info)

	if info.GoVersion == "" {
		t.Error("GoVersion empty")
	}
	if info.NumCPU == 0 {
		t.Error("NumCPU = 0")
	}
	if info.SysMB <= 0 {
		t.Error("SysMB <= 0")
	}
}

func TestParseTopics(t *testing.T) {
	topics := parseTopics("runtime,metrics, cluster")
	if len(topics) != 3 {
		t.Errorf("topics = %v, want 3 items", topics)
	}
	if topics[2] != "cluster" {
		t.Errorf("topics[2] = %q, want cluster", topics[2])
	}

	empty := parseTopics("")
	if len(empty) != 0 {
		t.Error("empty string should give nil topics")
	}
}
