package dashboard

import (
	"net/http"
	"sort"
	"time"

	"engine/log"
	"engine/middleware"
)

// 追踪查询 API（增强版）：
//   - GET /api/trace/chain?trace_id=xxx   返回完整调用链（Span + 关联日志，按时间排序）
//   - GET /api/trace/active?limit=N       返回最近活跃的 TraceID 列表
//
// 数据来源：
//   - middleware.InMemorySpanExporter 采集的 Span（通过 Config.SpanExporter 注入）
//   - 可选的 log.RingBufferSink（通过 Config.LogRingBuffer 注入）用于关联日志

type traceChainSpan struct {
	SpanID        string                 `json:"span_id"`
	ParentSpanID  string                 `json:"parent_span_id,omitempty"`
	OperationName string                 `json:"operation_name"`
	Kind          int                    `json:"kind"`
	StartTime     time.Time              `json:"start_time"`
	EndTime       time.Time              `json:"end_time"`
	DurationMs    float64                `json:"duration_ms"`
	Status        int                    `json:"status"`
	StatusDesc    string                 `json:"status_desc,omitempty"`
	Attributes    map[string]interface{} `json:"attributes,omitempty"`
	Node          string                 `json:"node,omitempty"`
	ActorPID      string                 `json:"actor_pid,omitempty"`
}

type traceChainLog struct {
	Time   time.Time `json:"time"`
	Level  string    `json:"level"`
	NodeID string    `json:"node_id,omitempty"`
	Actor  string    `json:"actor,omitempty"`
	Msg    string    `json:"msg"`
}

type traceChainResponse struct {
	TraceID    string           `json:"trace_id"`
	Spans      []traceChainSpan `json:"spans"`
	Logs       []traceChainLog  `json:"logs,omitempty"`
	SpanCount  int              `json:"span_count"`
	LogCount   int              `json:"log_count"`
	TotalDurMs float64          `json:"total_duration_ms"`
	StartTime  time.Time        `json:"start_time,omitempty"`
	EndTime    time.Time        `json:"end_time,omitempty"`
	Nodes      []string         `json:"nodes,omitempty"`
}

// ---- GET /api/trace/chain?trace_id=xxx ----
func (h *handlers) handleTraceChain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	if h.config.SpanExporter == nil {
		writeError(w, http.StatusNotFound, "span exporter not configured")
		return
	}
	traceID := r.URL.Query().Get("trace_id")
	if traceID == "" {
		writeError(w, http.StatusBadRequest, "trace_id required")
		return
	}
	spans := filterSpans(h.config.SpanExporter.GetSpans(), traceID)
	resp := traceChainResponse{
		TraceID:   traceID,
		Spans:     make([]traceChainSpan, 0, len(spans)),
		SpanCount: len(spans),
	}
	if len(spans) > 0 {
		sort.Slice(spans, func(i, j int) bool { return spans[i].StartTime.Before(spans[j].StartTime) })
		resp.StartTime = spans[0].StartTime
		end := spans[0].EndTime
		nodes := map[string]struct{}{}
		for _, s := range spans {
			if s.EndTime.After(end) {
				end = s.EndTime
			}
			node, actorPID := extractAttrString(s.Attributes, "node"), extractAttrString(s.Attributes, "actor.pid")
			if node != "" {
				nodes[node] = struct{}{}
			}
			resp.Spans = append(resp.Spans, traceChainSpan{
				SpanID:        s.SpanID,
				ParentSpanID:  s.ParentSpanID,
				OperationName: s.OperationName,
				Kind:          int(s.Kind),
				StartTime:     s.StartTime,
				EndTime:       s.EndTime,
				DurationMs:    float64(s.EndTime.Sub(s.StartTime).Microseconds()) / 1000.0,
				Status:        int(s.Status),
				StatusDesc:    s.StatusDesc,
				Attributes:    s.Attributes,
				Node:          node,
				ActorPID:      actorPID,
			})
		}
		resp.EndTime = end
		resp.TotalDurMs = float64(end.Sub(resp.StartTime).Microseconds()) / 1000.0
		for n := range nodes {
			resp.Nodes = append(resp.Nodes, n)
		}
		sort.Strings(resp.Nodes)
	}

	if h.config.LogRingBuffer != nil {
		logs := h.config.LogRingBuffer.Query(log.QueryFilter{TraceID: traceID})
		resp.Logs = make([]traceChainLog, 0, len(logs))
		for _, e := range logs {
			resp.Logs = append(resp.Logs, traceChainLog{
				Time:   e.Time,
				Level:  e.Level.String(),
				NodeID: e.NodeID,
				Actor:  e.Actor,
				Msg:    e.Msg,
			})
		}
		resp.LogCount = len(resp.Logs)
	}

	writeJSON(w, resp)
}

// ---- GET /api/trace/active?limit=N ----
// 返回最近 N 条活跃 TraceID 摘要（SpanCount / 起止时间 / 节点集合）
func (h *handlers) handleTraceActive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	if h.config.SpanExporter == nil {
		writeError(w, http.StatusNotFound, "span exporter not configured")
		return
	}
	limit := parseIntDefault(r.URL.Query().Get("limit"), 50)
	if limit <= 0 {
		limit = 50
	}

	spans := h.config.SpanExporter.GetSpans()
	type agg struct {
		TraceID   string
		SpanCount int
		Start     time.Time
		End       time.Time
		Nodes     map[string]struct{}
	}
	byTrace := map[string]*agg{}
	for _, s := range spans {
		a, ok := byTrace[s.TraceID]
		if !ok {
			a = &agg{TraceID: s.TraceID, Start: s.StartTime, End: s.EndTime, Nodes: map[string]struct{}{}}
			byTrace[s.TraceID] = a
		}
		a.SpanCount++
		if s.StartTime.Before(a.Start) {
			a.Start = s.StartTime
		}
		if s.EndTime.After(a.End) {
			a.End = s.EndTime
		}
		if n := extractAttrString(s.Attributes, "node"); n != "" {
			a.Nodes[n] = struct{}{}
		}
	}
	list := make([]map[string]interface{}, 0, len(byTrace))
	for _, a := range byTrace {
		nodes := make([]string, 0, len(a.Nodes))
		for n := range a.Nodes {
			nodes = append(nodes, n)
		}
		sort.Strings(nodes)
		list = append(list, map[string]interface{}{
			"trace_id":          a.TraceID,
			"span_count":        a.SpanCount,
			"start_time":        a.Start,
			"end_time":          a.End,
			"total_duration_ms": float64(a.End.Sub(a.Start).Microseconds()) / 1000.0,
			"nodes":             nodes,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		ti, _ := list[i]["end_time"].(time.Time)
		tj, _ := list[j]["end_time"].(time.Time)
		return ti.After(tj)
	})
	if len(list) > limit {
		list = list[:limit]
	}
	writeJSON(w, map[string]interface{}{
		"count":  len(list),
		"traces": list,
	})
}

func filterSpans(spans []middleware.ExportSpanData, traceID string) []middleware.ExportSpanData {
	out := make([]middleware.ExportSpanData, 0, 8)
	for _, s := range spans {
		if s.TraceID == traceID {
			out = append(out, s)
		}
	}
	return out
}

func extractAttrString(attrs map[string]interface{}, key string) string {
	if attrs == nil {
		return ""
	}
	if v, ok := attrs[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	return n
}
