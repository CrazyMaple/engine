package dashboard

import (
	"encoding/json"
	"net/http"

	"engine/cluster/canary"
)

// ---- GET /api/canary/status ----

func (h *handlers) handleCanaryStatus(w http.ResponseWriter, r *http.Request) {
	ce := h.config.CanaryEngine
	if ce == nil {
		writeError(w, http.StatusServiceUnavailable, "canary engine not configured")
		return
	}
	result := ce.Status()
	result["rules_detail"] = ce.Rules()
	writeJSON(w, result)
}

// ---- POST /api/canary/rules ----

func (h *handlers) handleCanaryRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	ce := h.config.CanaryEngine
	if ce == nil {
		writeError(w, http.StatusServiceUnavailable, "canary engine not configured")
		return
	}

	var rules []canary.Rule
	if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ce.SetRules(rules)
	writeJSON(w, map[string]interface{}{"status": "ok", "rules": len(rules)})
}

// ---- POST /api/canary/weights ----

func (h *handlers) handleCanaryWeights(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	ce := h.config.CanaryEngine
	if ce == nil {
		writeError(w, http.StatusServiceUnavailable, "canary engine not configured")
		return
	}

	var weights map[string]int
	if err := json.NewDecoder(r.Body).Decode(&weights); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := ce.SetWeights(weights); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"status": "ok", "weights": weights})
}

// ---- POST /api/canary/promote ----

func (h *handlers) handleCanaryPromote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	ce := h.config.CanaryEngine
	if ce == nil {
		writeError(w, http.StatusServiceUnavailable, "canary engine not configured")
		return
	}

	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Version == "" {
		writeError(w, http.StatusBadRequest, "version is required")
		return
	}
	ce.Promote(req.Version)

	if h.config.AuditLog != nil {
		h.config.AuditLog.Record("canary_promote", req.Version, "dashboard", "", r.RemoteAddr)
	}
	writeJSON(w, map[string]string{"status": "promoted", "version": req.Version})
}

// ---- POST /api/canary/rollback ----

func (h *handlers) handleCanaryRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	ce := h.config.CanaryEngine
	if ce == nil {
		writeError(w, http.StatusServiceUnavailable, "canary engine not configured")
		return
	}

	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Version == "" {
		writeError(w, http.StatusBadRequest, "version is required")
		return
	}
	ce.Rollback(req.Version)

	if h.config.AuditLog != nil {
		h.config.AuditLog.Record("canary_rollback", req.Version, "dashboard", "", r.RemoteAddr)
	}
	writeJSON(w, map[string]string{"status": "rolled_back", "version": req.Version})
}

// ---- GET /api/canary/compare ----

func (h *handlers) handleCanaryCompare(w http.ResponseWriter, r *http.Request) {
	comp := h.config.CanaryComparator
	if comp == nil {
		writeError(w, http.StatusServiceUnavailable, "canary comparator not configured")
		return
	}

	baseline := r.URL.Query().Get("baseline")
	canaryV := r.URL.Query().Get("canary")
	if baseline == "" || canaryV == "" {
		writeError(w, http.StatusBadRequest, "baseline and canary parameters required")
		return
	}

	// 先采集最新指标
	comp.Collect(baseline)
	comp.Collect(canaryV)

	report := comp.Compare(baseline, canaryV)
	writeJSON(w, report)
}
