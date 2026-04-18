package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// ---- /api/alerts/rules ----
// GET 返回所有规则；POST 新增/更新规则；DELETE 按 id 查询参数删除
func (h *handlers) handleAlertRules(w http.ResponseWriter, r *http.Request) {
	am := h.config.AlertManager
	if am == nil {
		writeError(w, http.StatusServiceUnavailable, "alert manager not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, am.Rules())
	case http.MethodPost:
		var rule AlertRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := am.SetRule(rule); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, map[string]interface{}{"status": "ok", "id": rule.ID})
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id required")
			return
		}
		am.DeleteRule(id)
		writeJSON(w, map[string]interface{}{"status": "ok"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ---- GET /api/alerts/active ----
func (h *handlers) handleAlertActive(w http.ResponseWriter, r *http.Request) {
	am := h.config.AlertManager
	if am == nil {
		writeError(w, http.StatusServiceUnavailable, "alert manager not configured")
		return
	}
	writeJSON(w, am.Active())
}

// ---- GET /api/alerts/history?limit=N ----
func (h *handlers) handleAlertHistory(w http.ResponseWriter, r *http.Request) {
	am := h.config.AlertManager
	if am == nil {
		writeError(w, http.StatusServiceUnavailable, "alert manager not configured")
		return
	}
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	writeJSON(w, am.History(limit))
}

// ---- POST /api/alerts/silence ----
// body: {"rule_id":"...","duration_seconds":600}
func (h *handlers) handleAlertSilence(w http.ResponseWriter, r *http.Request) {
	am := h.config.AlertManager
	if am == nil {
		writeError(w, http.StatusServiceUnavailable, "alert manager not configured")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req struct {
		RuleID          string `json:"rule_id"`
		DurationSeconds int    `json:"duration_seconds"`
		Cancel          bool   `json:"cancel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.RuleID == "" {
		writeError(w, http.StatusBadRequest, "rule_id required")
		return
	}
	if req.Cancel {
		am.Unsilence(req.RuleID)
	} else {
		dur := time.Duration(req.DurationSeconds) * time.Second
		if dur <= 0 {
			dur = 10 * time.Minute
		}
		am.Silence(req.RuleID, time.Now().Add(dur))
	}
	writeJSON(w, map[string]interface{}{"status": "ok"})
}

// ---- POST /api/alerts/ack ----
// body: {"event_id":"...","by":"operator"}
func (h *handlers) handleAlertAck(w http.ResponseWriter, r *http.Request) {
	am := h.config.AlertManager
	if am == nil {
		writeError(w, http.StatusServiceUnavailable, "alert manager not configured")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var req struct {
		EventID string `json:"event_id"`
		By      string `json:"by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.EventID == "" {
		writeError(w, http.StatusBadRequest, "event_id required")
		return
	}
	if !am.Ack(req.EventID, req.By) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	writeJSON(w, map[string]interface{}{"status": "ok"})
}
