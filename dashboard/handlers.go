package dashboard

import (
	"encoding/json"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"engine/actor"
	"engine/log"
)

type handlers struct {
	config    Config
	startTime time.Time
}

func init() {
	// 用于计算系统运行时长
}

// ---- 响应辅助函数 ----

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ---- GET /api/system ----

type systemInfo struct {
	Address    string `json:"address"`
	ActorCount int    `json:"actor_count"`
	Uptime     string `json:"uptime"`
	GoVersion  string `json:"go_version"`
}

func (h *handlers) handleSystem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	info := systemInfo{
		Address:    h.config.System.Address,
		ActorCount: h.config.System.ProcessRegistry.Count(),
	}
	writeJSON(w, info)
}

// ---- GET /api/actors ----

type actorInfo struct {
	PID      string   `json:"pid"`
	Children []string `json:"children,omitempty"`
}

func (h *handlers) handleActors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ids := h.config.System.ProcessRegistry.GetAllIDs()
	sort.Strings(ids)

	actors := make([]actorInfo, 0, len(ids))
	for _, id := range ids {
		info := actorInfo{PID: id}

		// 尝试获取子节点信息
		proc, ok := h.config.System.ProcessRegistry.GetByID(id)
		if ok {
			if cell, ok := proc.(interface{ Children() []*actor.PID }); ok {
				children := cell.Children()
				childIDs := make([]string, len(children))
				for i, c := range children {
					childIDs[i] = c.Id
				}
				info.Children = childIDs
			}
		}
		actors = append(actors, info)
	}

	writeJSON(w, actors)
}

// ---- GET /api/actors/{id}/children ----

func (h *handlers) handleActorChildren(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 解析 URL: /api/actors/{id}/children
	path := strings.TrimPrefix(r.URL.Path, "/api/actors/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "actor id required")
		return
	}
	actorID := parts[0]

	proc, ok := h.config.System.ProcessRegistry.GetByID(actorID)
	if !ok {
		writeError(w, http.StatusNotFound, "actor not found")
		return
	}

	result := map[string]interface{}{
		"pid": actorID,
	}

	if cell, ok := proc.(interface{ Children() []*actor.PID }); ok {
		children := cell.Children()
		childIDs := make([]string, len(children))
		for i, c := range children {
			childIDs[i] = c.Id
		}
		result["children"] = childIDs
	} else {
		result["children"] = []string{}
	}

	writeJSON(w, result)
}

// ---- GET /api/cluster ----

type clusterInfo struct {
	Name    string       `json:"name"`
	Self    *memberInfo  `json:"self"`
	Members []memberInfo `json:"members"`
	Kinds   []string     `json:"kinds"`
}

type memberInfo struct {
	Address  string `json:"address"`
	Id       string `json:"id"`
	Status   string `json:"status"`
	Kinds    []string `json:"kinds,omitempty"`
	LastSeen string `json:"last_seen,omitempty"`
}

func (h *handlers) handleCluster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if h.config.Cluster == nil {
		writeError(w, http.StatusNotFound, "cluster not configured")
		return
	}

	self := h.config.Cluster.Self()
	members := h.config.Cluster.Members()

	info := clusterInfo{
		Name: h.config.Cluster.Config().ClusterName,
		Self: &memberInfo{
			Address: self.Address,
			Id:      self.Id,
			Status:  self.Status.String(),
			Kinds:   self.Kinds,
		},
		Members: make([]memberInfo, 0, len(members)),
		Kinds:   h.config.Cluster.Config().Kinds,
	}

	for _, m := range members {
		info.Members = append(info.Members, memberInfo{
			Address:  m.Address,
			Id:       m.Id,
			Status:   m.Status.String(),
			Kinds:    m.Kinds,
			LastSeen: m.LastSeen.Format(time.RFC3339),
		})
	}

	writeJSON(w, info)
}

// ---- GET /api/cluster/members ----

func (h *handlers) handleClusterMembers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if h.config.Cluster == nil {
		writeError(w, http.StatusNotFound, "cluster not configured")
		return
	}

	members := h.config.Cluster.Members()
	infos := make([]memberInfo, 0, len(members))
	for _, m := range members {
		infos = append(infos, memberInfo{
			Address:  m.Address,
			Id:       m.Id,
			Status:   m.Status.String(),
			Kinds:    m.Kinds,
			LastSeen: m.LastSeen.Format(time.RFC3339),
		})
	}

	writeJSON(w, infos)
}

// ---- GET /api/metrics ----

func (h *handlers) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if h.config.Metrics == nil {
		writeError(w, http.StatusNotFound, "metrics not configured")
		return
	}

	snap := h.config.Metrics.Snapshot()
	writeJSON(w, snap)
}

// ---- GET /api/metrics/prometheus ----

func (h *handlers) handleMetricsPrometheus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if h.config.MetricsRegistry != nil {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		h.config.MetricsRegistry.WritePrometheus(w)
		return
	}

	if h.config.Metrics == nil {
		writeError(w, http.StatusNotFound, "metrics not configured")
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	h.config.Metrics.WritePrometheus(w)
}

// ---- GET /api/hotactors ----

func (h *handlers) handleHotActors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if h.config.HotTracker == nil {
		writeError(w, http.StatusNotFound, "hot actor tracker not configured")
		return
	}

	n := 20 // 默认返回前 20
	if nStr := r.URL.Query().Get("n"); nStr != "" {
		if parsed, err := strconv.Atoi(nStr); err == nil && parsed > 0 {
			n = parsed
		}
	}

	stats := h.config.HotTracker.TopN(n)
	writeJSON(w, stats)
}

// ---- GET /api/runtime ----

type runtimeInfo struct {
	GoVersion    string `json:"go_version"`
	NumGoroutine int    `json:"num_goroutine"`
	NumCPU       int    `json:"num_cpu"`
	// 内存
	AllocMB      float64 `json:"alloc_mb"`
	TotalAllocMB float64 `json:"total_alloc_mb"`
	SysMB        float64 `json:"sys_mb"`
	HeapAllocMB  float64 `json:"heap_alloc_mb"`
	HeapInuseMB  float64 `json:"heap_inuse_mb"`
	StackInuseMB float64 `json:"stack_inuse_mb"`
	// GC
	NumGC        uint32  `json:"num_gc"`
	GCPauseMs    float64 `json:"gc_pause_ms"`     // 最近一次 GC 暂停时间
	GCPauseTotMs float64 `json:"gc_pause_tot_ms"` // GC 暂停总时间
	GCCPUPercent float64 `json:"gc_cpu_percent"`
}

func (h *handlers) handleRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	toMB := func(b uint64) float64 { return float64(b) / 1024 / 1024 }

	var lastPause float64
	if mem.NumGC > 0 {
		lastPause = float64(mem.PauseNs[(mem.NumGC+255)%256]) / 1e6
	}

	info := runtimeInfo{
		GoVersion:    runtime.Version(),
		NumGoroutine: runtime.NumGoroutine(),
		NumCPU:       runtime.NumCPU(),
		AllocMB:      toMB(mem.Alloc),
		TotalAllocMB: toMB(mem.TotalAlloc),
		SysMB:        toMB(mem.Sys),
		HeapAllocMB:  toMB(mem.HeapAlloc),
		HeapInuseMB:  toMB(mem.HeapInuse),
		StackInuseMB: toMB(mem.StackInuse),
		NumGC:        mem.NumGC,
		GCPauseMs:    lastPause,
		GCPauseTotMs: float64(mem.PauseTotalNs) / 1e6,
		GCCPUPercent: mem.GCCPUFraction * 100,
	}
	writeJSON(w, info)
}

// ---- GET /api/actors/topology ----

type actorNode struct {
	PID      string       `json:"pid"`
	Children []*actorNode `json:"children,omitempty"`
}

func (h *handlers) handleActorTopology(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 收集所有 actor 及其子节点
	ids := h.config.System.ProcessRegistry.GetAllIDs()
	childrenMap := make(map[string][]string)  // pid -> children ids
	hasParent := make(map[string]bool)

	for _, id := range ids {
		proc, ok := h.config.System.ProcessRegistry.GetByID(id)
		if !ok {
			continue
		}
		if cell, ok := proc.(interface{ Children() []*actor.PID }); ok {
			children := cell.Children()
			for _, c := range children {
				childrenMap[id] = append(childrenMap[id], c.Id)
				hasParent[c.Id] = true
			}
		}
	}

	// 构建树：根节点是没有父节点的 actor
	var buildNode func(id string) *actorNode
	buildNode = func(id string) *actorNode {
		node := &actorNode{PID: id}
		if kids, ok := childrenMap[id]; ok {
			sort.Strings(kids)
			for _, kid := range kids {
				node.Children = append(node.Children, buildNode(kid))
			}
		}
		return node
	}

	sort.Strings(ids)
	roots := make([]*actorNode, 0)
	for _, id := range ids {
		if !hasParent[id] {
			roots = append(roots, buildNode(id))
		}
	}

	writeJSON(w, roots)
}

// ---- GET /api/traces?trace_id=xxx ----

func (h *handlers) handleTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if h.config.TraceStore == nil {
		writeError(w, http.StatusNotFound, "trace store not configured")
		return
	}

	traceID := r.URL.Query().Get("trace_id")
	if traceID != "" {
		records := h.config.TraceStore.QueryByTraceID(traceID)
		writeJSON(w, records)
		return
	}

	// 无 trace_id 时返回最近记录
	n := 50
	if nStr := r.URL.Query().Get("n"); nStr != "" {
		if parsed, err := strconv.Atoi(nStr); err == nil && parsed > 0 {
			n = parsed
		}
	}
	records := h.config.TraceStore.Recent(n)
	writeJSON(w, records)
}

// ---- GET /api/metrics/history ----

func (h *handlers) handleMetricsHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.config.MetricsHistory == nil {
		writeError(w, http.StatusNotFound, "metrics history not configured")
		return
	}
	writeJSON(w, h.config.MetricsHistory.GetHistory())
}

// ---- GET /api/cluster/graph ----

type graphNode struct {
	ID      string   `json:"id"`
	Address string   `json:"address"`
	Status  string   `json:"status"`
	Kinds   []string `json:"kinds,omitempty"`
}

type graphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type clusterGraph struct {
	Nodes []graphNode `json:"nodes"`
	Edges []graphEdge `json:"edges"`
}

func (h *handlers) handleClusterGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.config.Cluster == nil {
		writeError(w, http.StatusNotFound, "cluster not configured")
		return
	}

	members := h.config.Cluster.Members()
	nodes := make([]graphNode, 0, len(members))
	edges := make([]graphEdge, 0)

	for _, m := range members {
		nodes = append(nodes, graphNode{
			ID:      m.Id,
			Address: m.Address,
			Status:  m.Status.String(),
			Kinds:   m.Kinds,
		})
	}

	// 全连接拓扑：alive 节点之间相互连接
	for i := 0; i < len(nodes); i++ {
		if nodes[i].Status != "alive" {
			continue
		}
		for j := i + 1; j < len(nodes); j++ {
			if nodes[j].Status != "alive" {
				continue
			}
			edges = append(edges, graphEdge{From: nodes[i].ID, To: nodes[j].ID})
		}
	}

	writeJSON(w, clusterGraph{Nodes: nodes, Edges: edges})
}

// ---- GET /api/actors/flamegraph ----

type flameNode struct {
	Name     string       `json:"name"`
	Value    int64        `json:"value"` // 消息数
	Children []*flameNode `json:"children,omitempty"`
}

func (h *handlers) handleFlameGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.config.HotTracker == nil {
		writeError(w, http.StatusNotFound, "hot actor tracker not configured")
		return
	}

	stats := h.config.HotTracker.TopN(0) // 获取全部
	root := &flameNode{Name: "root", Value: 0}

	for _, s := range stats {
		parts := strings.Split(s.PID, "/")
		node := root
		root.Value += s.MsgCount

		for _, part := range parts {
			found := false
			for _, child := range node.Children {
				if child.Name == part {
					child.Value += s.MsgCount
					node = child
					found = true
					break
				}
			}
			if !found {
				child := &flameNode{Name: part, Value: s.MsgCount}
				node.Children = append(node.Children, child)
				node = child
			}
		}
	}

	writeJSON(w, root)
}

// ---- GET /api/config ----

type configInfo struct {
	Filename string `json:"filename"`
	Type     string `json:"type"`
}

func (h *handlers) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.config.ConfigManager == nil {
		writeError(w, http.StatusNotFound, "config manager not configured")
		return
	}

	// 通过 ListEntries 获取已注册的配置
	entries := h.config.ConfigManager.ListEntries()
	infos := make([]configInfo, 0, len(entries))
	for _, e := range entries {
		typeName := "record_file"
		if e.Type == 1 { // EntryTypeJSON
			typeName = "json"
		}
		infos = append(infos, configInfo{Filename: e.Filename, Type: typeName})
	}
	writeJSON(w, infos)
}

// ---- POST /api/config/reload ----

func (h *handlers) handleConfigReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.config.ConfigManager == nil {
		writeError(w, http.StatusNotFound, "config manager not configured")
		return
	}

	var req struct {
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	entry := h.config.ConfigManager.Get(req.Filename)
	if entry == nil {
		writeError(w, http.StatusNotFound, "config not found: "+req.Filename)
		return
	}

	if err := h.config.ConfigManager.ReloadEntry(req.Filename); err != nil {
		writeError(w, http.StatusInternalServerError, "reload failed: "+err.Error())
		return
	}

	// 记录审计日志
	if h.config.AuditLog != nil {
		h.config.AuditLog.Record("config_reload", req.Filename, "dashboard", extractOperator(r), r.RemoteAddr)
	}

	writeJSON(w, map[string]string{"status": "ok", "filename": req.Filename})
}

// ---- GET /api/audit ----

func (h *handlers) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.config.AuditLog == nil {
		writeError(w, http.StatusNotFound, "audit log not configured")
		return
	}

	n := 50
	if nStr := r.URL.Query().Get("n"); nStr != "" {
		if parsed, err := strconv.Atoi(nStr); err == nil && parsed > 0 {
			n = parsed
		}
	}
	writeJSON(w, h.config.AuditLog.Recent(n))
}

// ---- GET/POST /api/log/level ----

func (h *handlers) handleLogLevel(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]string{"level": log.GetLevel().String()})

	case http.MethodPost:
		var req struct {
			Level string `json:"level"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		level, err := log.ParseLevel(req.Level)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		log.SetLevel(level)

		// 记录审计日志
		if h.config.AuditLog != nil {
			h.config.AuditLog.Record("log_level_change", req.Level, "dashboard", extractOperator(r), r.RemoteAddr)
		}

		writeJSON(w, map[string]string{"status": "ok", "level": level.String()})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ---- GET / (Dashboard 首页) ----

func (h *handlers) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}
