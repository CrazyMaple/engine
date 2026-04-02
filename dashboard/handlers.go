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

// ---- GET / (Dashboard 首页) ----

func (h *handlers) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}
