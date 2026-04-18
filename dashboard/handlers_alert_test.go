package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleAlertRulesCRUD(t *testing.T) {
	am := NewAlertManager(8)
	h := &handlers{config: Config{AlertManager: am}}

	// POST 创建规则
	body, _ := json.Marshal(AlertRule{
		ID: "r1", Name: "r1", Metric: "cpu", Op: OpGreater, Threshold: 0.8, Enabled: true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/alerts/rules", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.handleAlertRules(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	// GET 列表
	req = httptest.NewRequest(http.MethodGet, "/api/alerts/rules", nil)
	w = httptest.NewRecorder()
	h.handleAlertRules(w, req)
	var rules []AlertRule
	_ = json.Unmarshal(w.Body.Bytes(), &rules)
	if len(rules) != 1 {
		t.Fatalf("want 1 rule, got %d", len(rules))
	}

	// DELETE
	req = httptest.NewRequest(http.MethodDelete, "/api/alerts/rules?id=r1", nil)
	w = httptest.NewRecorder()
	h.handleAlertRules(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete failed: %d", w.Code)
	}
	if len(am.Rules()) != 0 {
		t.Fatal("rule not deleted")
	}
}

func TestHandleAlertSilenceAndAck(t *testing.T) {
	am := NewAlertManager(4)
	_ = am.SetRule(AlertRule{ID: "r1", Name: "r1", Metric: "m", Op: OpGreater, Threshold: 1, Enabled: true})
	am.Submit(MetricSample{Metric: "m", Value: 100})
	active := am.Active()
	if len(active) != 1 {
		t.Fatal("setup: alert should fire")
	}

	h := &handlers{config: Config{AlertManager: am}}

	// Ack
	body, _ := json.Marshal(map[string]string{"event_id": active[0].ID, "by": "ops"})
	req := httptest.NewRequest(http.MethodPost, "/api/alerts/ack", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.handleAlertAck(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ack failed: %d body=%s", w.Code, w.Body.String())
	}

	// Silence
	body, _ = json.Marshal(map[string]interface{}{"rule_id": "r1", "duration_seconds": 60})
	req = httptest.NewRequest(http.MethodPost, "/api/alerts/silence", bytes.NewReader(body))
	w = httptest.NewRecorder()
	h.handleAlertSilence(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("silence failed: %d", w.Code)
	}
}
