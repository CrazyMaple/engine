package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// ---- POST /api/profiler/cpu ----

func (h *handlers) handleProfilerCPU(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	profiler := h.config.Profiler
	if profiler == nil {
		writeError(w, http.StatusServiceUnavailable, "profiler not configured")
		return
	}

	var req struct {
		DurationSeconds int    `json:"duration_seconds"`
		TraceID         string `json:"trace_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	duration := time.Duration(req.DurationSeconds) * time.Second
	if duration <= 0 {
		duration = 10 * time.Second
	}

	result, err := profiler.StartCPUProfile(duration, req.TraceID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{
		"id":       result.ID,
		"type":     result.TypeName,
		"size":     result.Size,
		"duration": result.Duration.String(),
	})
}

// ---- POST /api/profiler/heap ----

func (h *handlers) handleProfilerHeap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	profiler := h.config.Profiler
	if profiler == nil {
		writeError(w, http.StatusServiceUnavailable, "profiler not configured")
		return
	}

	var req struct {
		TraceID string `json:"trace_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	result := profiler.CaptureHeapProfile(req.TraceID)
	writeJSON(w, map[string]interface{}{
		"id":   result.ID,
		"type": result.TypeName,
		"size": result.Size,
	})
}

// ---- POST /api/profiler/goroutine ----

func (h *handlers) handleProfilerGoroutine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	profiler := h.config.Profiler
	if profiler == nil {
		writeError(w, http.StatusServiceUnavailable, "profiler not configured")
		return
	}

	var req struct {
		TraceID string `json:"trace_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	result := profiler.CaptureGoroutineProfile(req.TraceID)
	writeJSON(w, map[string]interface{}{
		"id":   result.ID,
		"type": result.TypeName,
		"size": result.Size,
	})
}

// ---- POST /api/profiler/block ----

func (h *handlers) handleProfilerBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	profiler := h.config.Profiler
	if profiler == nil {
		writeError(w, http.StatusServiceUnavailable, "profiler not configured")
		return
	}

	var req struct {
		TraceID string `json:"trace_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	result := profiler.CaptureBlockProfile(req.TraceID)
	writeJSON(w, map[string]interface{}{
		"id":       result.ID,
		"type":     result.TypeName,
		"size":     result.Size,
		"duration": result.Duration.String(),
	})
}

// ---- GET /api/profiler/list ----

func (h *handlers) handleProfilerList(w http.ResponseWriter, r *http.Request) {
	profiler := h.config.Profiler
	if profiler == nil {
		writeError(w, http.StatusServiceUnavailable, "profiler not configured")
		return
	}
	writeJSON(w, profiler.Store().List())
}

// ---- GET /api/profiler/get?id=X ----

func (h *handlers) handleProfilerGet(w http.ResponseWriter, r *http.Request) {
	profiler := h.config.Profiler
	if profiler == nil {
		writeError(w, http.StatusServiceUnavailable, "profiler not configured")
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id parameter")
		return
	}

	result := profiler.Store().Get(id)
	if result == nil {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+id+".pprof")
	w.Header().Set("Content-Length", strconv.Itoa(len(result.Data)))
	w.Write(result.Data)
}

// ---- GET /api/profiler/diff?a=X&b=Y ----

func (h *handlers) handleProfilerDiff(w http.ResponseWriter, r *http.Request) {
	profiler := h.config.Profiler
	if profiler == nil {
		writeError(w, http.StatusServiceUnavailable, "profiler not configured")
		return
	}

	idA := r.URL.Query().Get("a")
	idB := r.URL.Query().Get("b")
	if idA == "" || idB == "" {
		writeError(w, http.StatusBadRequest, "missing a or b parameter")
		return
	}

	a := profiler.Store().Get(idA)
	b := profiler.Store().Get(idB)
	if a == nil || b == nil {
		writeError(w, http.StatusNotFound, "one or both profiles not found")
		return
	}

	writeJSON(w, map[string]interface{}{
		"a": map[string]interface{}{
			"id": a.ID, "type": a.TypeName, "size": a.Size,
			"timestamp": a.Timestamp, "trigger": a.Trigger,
		},
		"b": map[string]interface{}{
			"id": b.ID, "type": b.TypeName, "size": b.Size,
			"timestamp": b.Timestamp, "trigger": b.Trigger,
		},
		"size_diff_bytes": b.Size - a.Size,
		"time_diff":       b.Timestamp.Sub(a.Timestamp).String(),
		"hint":            "download both profiles and use: go tool pprof -diff_base=a.pprof b.pprof",
	})
}

// ---- GET /api/profiler/actors ----

func (h *handlers) handleProfilerActors(w http.ResponseWriter, r *http.Request) {
	ap := h.config.ActorProfiler
	if ap == nil {
		writeError(w, http.StatusServiceUnavailable, "actor profiler not configured")
		return
	}
	writeJSON(w, map[string]interface{}{
		"enabled":       ap.IsEnabled(),
		"bucket_labels": BucketLabelsJSON(),
		"actors":        ap.Stats(),
	})
}

// ---- POST /api/profiler/actors/enable ----

func (h *handlers) handleProfilerActorsEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	ap := h.config.ActorProfiler
	if ap == nil {
		writeError(w, http.StatusServiceUnavailable, "actor profiler not configured")
		return
	}
	ap.Enable()
	writeJSON(w, map[string]string{"status": "enabled"})
}

// ---- POST /api/profiler/actors/disable ----

func (h *handlers) handleProfilerActorsDisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	ap := h.config.ActorProfiler
	if ap == nil {
		writeError(w, http.StatusServiceUnavailable, "actor profiler not configured")
		return
	}
	ap.Disable()
	writeJSON(w, map[string]string{"status": "disabled"})
}

// ---- GET /api/profiler/auto/config ----
// ---- POST /api/profiler/auto/config ----

func (h *handlers) handleProfilerAutoConfig(w http.ResponseWriter, r *http.Request) {
	profiler := h.config.Profiler
	if profiler == nil {
		writeError(w, http.StatusServiceUnavailable, "profiler not configured")
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, profiler.AutoConfig())
	case http.MethodPost:
		var cfg struct {
			Enabled          *bool   `json:"enabled"`
			CPUThreshold     float64 `json:"cpu_threshold"`
			GCPauseThreshMs  int     `json:"gc_pause_threshold_ms"`
			CheckIntervalSec int     `json:"check_interval_sec"`
			ProfileDurSec    int     `json:"profile_duration_sec"`
		}
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		autoCfg := profiler.AutoConfig()
		if cfg.Enabled != nil {
			autoCfg.Enabled = *cfg.Enabled
		}
		if cfg.CPUThreshold > 0 {
			autoCfg.CPUThreshold = cfg.CPUThreshold
		}
		if cfg.GCPauseThreshMs > 0 {
			autoCfg.GCPauseThreshold = time.Duration(cfg.GCPauseThreshMs) * time.Millisecond
		}
		if cfg.CheckIntervalSec > 0 {
			autoCfg.CheckInterval = time.Duration(cfg.CheckIntervalSec) * time.Second
		}
		if cfg.ProfileDurSec > 0 {
			autoCfg.ProfileDuration = time.Duration(cfg.ProfileDurSec) * time.Second
		}
		profiler.SetAutoConfig(autoCfg)
		writeJSON(w, autoCfg)
	default:
		writeError(w, http.StatusMethodNotAllowed, "GET or POST only")
	}
}

// BucketLabelsJSON 返回直方图桶标签（供前端展示）
func BucketLabelsJSON() []string {
	// 引用 middleware 包中的 BucketLabels
	return []string{"<1ms", "1-5ms", "5-10ms", "10-50ms", "50-100ms", ">100ms"}
}
