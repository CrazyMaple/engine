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

// ---- GET/POST /api/canary/advanced_rules ----

func (h *handlers) handleCanaryAdvancedRules(w http.ResponseWriter, r *http.Request) {
	re := h.config.CanaryRuleEngine
	if re == nil {
		writeError(w, http.StatusServiceUnavailable, "rule engine not configured")
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, re.AdvancedRules())
	case http.MethodPost:
		var rules []canary.AdvancedRule
		if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		re.SetAdvancedRules(rules)
		writeJSON(w, map[string]interface{}{"status": "ok", "rules": len(rules)})
	default:
		writeError(w, http.StatusMethodNotAllowed, "GET or POST only")
	}
}

// ---- GET /api/canary/rule_hits ----

func (h *handlers) handleCanaryRuleHits(w http.ResponseWriter, r *http.Request) {
	re := h.config.CanaryRuleEngine
	if re == nil {
		writeError(w, http.StatusServiceUnavailable, "rule engine not configured")
		return
	}
	writeJSON(w, re.HitCounts())
}

// ---- GET/POST /api/ab/experiments ----

func (h *handlers) handleABExperiments(w http.ResponseWriter, r *http.Request) {
	abm := h.config.ABTestManager
	if abm == nil {
		writeError(w, http.StatusServiceUnavailable, "ab test manager not configured")
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, abm.ListExperiments())
	case http.MethodPost:
		var exp canary.Experiment
		if err := json.NewDecoder(r.Body).Decode(&exp); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := abm.CreateExperiment(exp); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, map[string]string{"status": "created", "id": exp.ID})
	default:
		writeError(w, http.StatusMethodNotAllowed, "GET or POST only")
	}
}

// ---- GET/POST/DELETE /api/ab/experiment?id=xxx ----

func (h *handlers) handleABExperiment(w http.ResponseWriter, r *http.Request) {
	abm := h.config.ABTestManager
	if abm == nil {
		writeError(w, http.StatusServiceUnavailable, "ab test manager not configured")
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id parameter required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		exp := abm.GetExperiment(id)
		if exp == nil {
			writeError(w, http.StatusNotFound, "experiment not found")
			return
		}
		writeJSON(w, exp)
	case http.MethodPost:
		// 状态变更：action=start|pause|complete
		action := r.URL.Query().Get("action")
		var err error
		switch action {
		case "start":
			err = abm.StartExperiment(id)
		case "pause":
			err = abm.PauseExperiment(id)
		case "complete":
			err = abm.CompleteExperiment(id)
		default:
			writeError(w, http.StatusBadRequest, "action must be start, pause, or complete")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if h.config.AuditLog != nil {
			h.config.AuditLog.Record("ab_experiment_"+action, id, "dashboard", "", r.RemoteAddr)
		}
		writeJSON(w, map[string]string{"status": action, "id": id})
	case http.MethodDelete:
		abm.DeleteExperiment(id)
		writeJSON(w, map[string]string{"status": "deleted", "id": id})
	default:
		writeError(w, http.StatusMethodNotAllowed, "GET, POST, or DELETE only")
	}
}

// ---- GET /api/ab/assign?user_id=xxx ----

func (h *handlers) handleABAssign(w http.ResponseWriter, r *http.Request) {
	abm := h.config.ABTestManager
	if abm == nil {
		writeError(w, http.StatusServiceUnavailable, "ab test manager not configured")
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id parameter required")
		return
	}

	// 从 query 参数构建 labels
	labels := make(map[string]string)
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			labels[k] = v[0]
		}
	}

	results := abm.Assign(userID, labels)
	writeJSON(w, results)
}

// ---- GET /api/ab/stats?id=xxx ----

func (h *handlers) handleABStats(w http.ResponseWriter, r *http.Request) {
	abm := h.config.ABTestManager
	if abm == nil {
		writeError(w, http.StatusServiceUnavailable, "ab test manager not configured")
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id parameter required")
		return
	}

	stats := abm.ExperimentStats(id)
	if stats == nil {
		writeError(w, http.StatusNotFound, "experiment not found")
		return
	}
	writeJSON(w, stats)
}
