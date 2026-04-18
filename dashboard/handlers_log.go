package dashboard

import (
	"net/http"
	"strconv"
	"time"

	"engine/log"
)

// ---- GET /api/log/query ----
// 查询参数：trace_id / actor / node / level / msg / since / until / limit
func (h *handlers) handleLogQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	rb := h.config.LogRingBuffer
	if rb == nil {
		writeError(w, http.StatusServiceUnavailable, "log ring buffer not configured")
		return
	}

	q := r.URL.Query()
	filter := log.QueryFilter{
		TraceID:   q.Get("trace_id"),
		Actor:     q.Get("actor"),
		NodeID:    q.Get("node"),
		MsgSubstr: q.Get("msg"),
	}
	if lv := q.Get("level"); lv != "" {
		if parsed, err := log.ParseLevel(lv); err == nil {
			filter.MinLevel = parsed
		}
	}
	if since := q.Get("since"); since != "" {
		if t, err := parseTimestamp(since); err == nil {
			filter.Since = t
		}
	}
	if until := q.Get("until"); until != "" {
		if t, err := parseTimestamp(until); err == nil {
			filter.Until = t
		}
	}
	if lim := q.Get("limit"); lim != "" {
		if n, err := strconv.Atoi(lim); err == nil && n >= 0 {
			filter.Limit = n
		}
	}
	if filter.Limit == 0 {
		filter.Limit = 200
	}

	entries := rb.Query(filter)
	resp := make([]map[string]interface{}, 0, len(entries))
	for _, e := range entries {
		resp = append(resp, map[string]interface{}{
			"time":     e.Time.Format(time.RFC3339Nano),
			"level":    e.Level.String(),
			"msg":      e.Msg,
			"node_id":  e.NodeID,
			"trace_id": e.TraceID,
			"actor":    e.Actor,
			"fields":   e.Fields,
		})
	}
	writeJSON(w, map[string]interface{}{
		"count":   len(resp),
		"entries": resp,
	})
}

// ---- GET /api/log/stats ----

func (h *handlers) handleLogStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	rb := h.config.LogRingBuffer
	if rb == nil {
		writeError(w, http.StatusServiceUnavailable, "log ring buffer not configured")
		return
	}
	writeJSON(w, map[string]interface{}{
		"buffered":       rb.Len(),
		"total_received": rb.TotalReceived(),
	})
}

// parseTimestamp 支持 RFC3339 或 Unix 秒/毫秒
func parseTimestamp(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n > 1_000_000_000_000 { // 毫秒
			return time.UnixMilli(n), nil
		}
		return time.Unix(n, 0), nil
	}
	return time.Time{}, errInvalidTime
}

var errInvalidTime = newSimpleErr("invalid timestamp")

type simpleErr struct{ msg string }

func (e *simpleErr) Error() string  { return e.msg }
func newSimpleErr(s string) *simpleErr { return &simpleErr{msg: s} }
