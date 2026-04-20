package dashboard

import (
	"net/http"

	"engine/actor"
)

// --- 热点 Actor 画像 API ---
// 依赖 Config.HotProfiler（*actor.HotActorProfiler）
//   - GET /api/profile/hotactors?topn=N&only_hot=true|false
//     返回按 P99 降序的前 N 个 Actor（可选仅热点）
//   - GET /api/profile/candidates
//     返回所有热点 Actor 作为迁移候选（IsHot=true）

type hotActorsResponse struct {
	Count     int                             `json:"count"`
	Threshold int64                           `json:"hot_p99_threshold_ns"`
	Window    int                             `json:"window_size"`
	Actors    []actor.HotActorProfileSnapshot `json:"actors"`
}

func (h *handlers) handleProfileHotActors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	if h.config.HotProfiler == nil {
		writeError(w, http.StatusNotFound, "hot actor profiler not configured")
		return
	}
	topN := parseIntDefault(r.URL.Query().Get("topn"), 20)
	if topN <= 0 {
		topN = 20
	}
	onlyHot := r.URL.Query().Get("only_hot") == "true"

	snaps := h.config.HotProfiler.TopN(topN, onlyHot)
	cfg := h.config.HotProfiler.Config()
	resp := hotActorsResponse{
		Count:     len(snaps),
		Threshold: cfg.HotP99Threshold.Nanoseconds(),
		Window:    cfg.WindowSize,
		Actors:    snaps,
	}
	writeJSON(w, resp)
}

type migrationCandidatesResponse struct {
	Count      int                             `json:"count"`
	Candidates []actor.HotActorProfileSnapshot `json:"candidates"`
	Advice     []string                        `json:"advice,omitempty"`
}

func (h *handlers) handleProfileCandidates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	if h.config.HotProfiler == nil {
		writeError(w, http.StatusNotFound, "hot actor profiler not configured")
		return
	}
	candidates := h.config.HotProfiler.MigrationCandidates()
	resp := migrationCandidatesResponse{
		Count:      len(candidates),
		Candidates: candidates,
		Advice:     buildMigrationAdvice(candidates),
	}
	writeJSON(w, resp)
}

// buildMigrationAdvice 依据候选列表生成一段人类可读的迁移建议
func buildMigrationAdvice(c []actor.HotActorProfileSnapshot) []string {
	if len(c) == 0 {
		return nil
	}
	advice := make([]string, 0, len(c))
	for _, s := range c {
		var tier string
		switch {
		case s.P99Ns > 500e6:
			tier = "urgent"
		case s.P99Ns > 100e6:
			tier = "high"
		default:
			tier = "medium"
		}
		advice = append(advice, formatAdvice(s.PID, tier, s.P99Ns, s.MsgCount))
	}
	return advice
}

func formatAdvice(pid, tier string, p99Ns float64, msgCount int64) string {
	ms := p99Ns / 1e6
	return "[" + tier + "] " + pid + ": P99=" + formatFloat(ms, 1) + "ms msg_count=" + formatInt(msgCount) + " — 建议迁移到低负载节点"
}

func formatFloat(v float64, decimals int) string {
	if v < 0 {
		return "-" + formatFloat(-v, decimals)
	}
	mult := 1.0
	for i := 0; i < decimals; i++ {
		mult *= 10
	}
	rounded := int64(v*mult + 0.5)
	whole := rounded / int64(mult)
	frac := rounded % int64(mult)
	if decimals == 0 {
		return formatInt(whole)
	}
	fracStr := ""
	for i := 0; i < decimals; i++ {
		fracStr = string(rune('0'+(frac%10))) + fracStr
		frac /= 10
	}
	return formatInt(whole) + "." + fracStr
}

func formatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 20)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		return "-" + string(buf)
	}
	return string(buf)
}
