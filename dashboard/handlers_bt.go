package dashboard

import (
	"net/http"
	"sync"
	"time"

	"engine/bt"
)

// BTDebugRegistry 行为树调试注册表
// 用于追踪运行中行为树的执行状态，供 Dashboard 可视化
type BTDebugRegistry struct {
	mu      sync.RWMutex
	trees   map[string]*BTDebugInfo // entityID → 调试信息
	maxHist int                     // 执行历史最大记录数
}

// BTDebugInfo 单个行为树的调试信息
type BTDebugInfo struct {
	EntityID    string            `json:"entity_id"`
	TreeID      string            `json:"tree_id"`
	CurrentNode string            `json:"current_node"`
	LastStatus  string            `json:"last_status"`
	LODLevel    int               `json:"lod_level"`
	TickCount   uint64            `json:"tick_count"`
	LastTickAt  time.Time         `json:"last_tick_at"`
	History     []BTExecutionStep `json:"history"`
}

// BTExecutionStep 行为树单步执行记录
type BTExecutionStep struct {
	Timestamp time.Time `json:"timestamp"`
	NodeName  string    `json:"node_name"`
	Status    string    `json:"status"`
}

// NewBTDebugRegistry 创建调试注册表
func NewBTDebugRegistry(maxHistory int) *BTDebugRegistry {
	if maxHistory <= 0 {
		maxHistory = 50
	}
	return &BTDebugRegistry{
		trees:   make(map[string]*BTDebugInfo),
		maxHist: maxHistory,
	}
}

// Register 注册行为树供调试
func (r *BTDebugRegistry) Register(entityID, treeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trees[entityID] = &BTDebugInfo{
		EntityID: entityID,
		TreeID:   treeID,
		History:  make([]BTExecutionStep, 0, r.maxHist),
	}
}

// Unregister 注销行为树
func (r *BTDebugRegistry) Unregister(entityID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.trees, entityID)
}

// RecordTick 记录一次 Tick 执行结果
func (r *BTDebugRegistry) RecordTick(entityID string, nodeName string, status bt.Status, lodLevel int, tickCount uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	info, ok := r.trees[entityID]
	if !ok {
		return
	}

	now := time.Now()
	info.CurrentNode = nodeName
	info.LastStatus = status.String()
	info.LODLevel = lodLevel
	info.TickCount = tickCount
	info.LastTickAt = now

	step := BTExecutionStep{
		Timestamp: now,
		NodeName:  nodeName,
		Status:    status.String(),
	}

	if len(info.History) >= r.maxHist {
		// 环形移除最早的
		copy(info.History, info.History[1:])
		info.History[len(info.History)-1] = step
	} else {
		info.History = append(info.History, step)
	}
}

// GetAll 获取所有调试信息
func (r *BTDebugRegistry) GetAll() []BTDebugInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]BTDebugInfo, 0, len(r.trees))
	for _, info := range r.trees {
		cp := *info
		cp.History = make([]BTExecutionStep, len(info.History))
		copy(cp.History, info.History)
		result = append(result, cp)
	}
	return result
}

// Get 获取单个实体的调试信息
func (r *BTDebugRegistry) Get(entityID string) (*BTDebugInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, ok := r.trees[entityID]
	if !ok {
		return nil, false
	}
	cp := *info
	cp.History = make([]BTExecutionStep, len(info.History))
	copy(cp.History, info.History)
	return &cp, true
}

// Count 返回注册的行为树数量
func (r *BTDebugRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.trees)
}

// --- Dashboard HTTP Handlers ---

// btSummaryResponse 行为树列表响应
type btSummaryResponse struct {
	Total int           `json:"total"`
	Trees []BTDebugInfo `json:"trees"`
}

// handleBTList GET /api/bt/list — 列出所有注册的行为树调试信息
func (h *handlers) handleBTList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if h.config.BTDebugRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "bt debug registry not configured")
		return
	}

	all := h.config.BTDebugRegistry.GetAll()
	writeJSON(w, btSummaryResponse{
		Total: len(all),
		Trees: all,
	})
}

// handleBTDetail GET /api/bt/detail?entity_id=xxx — 获取单个行为树详情+执行历史
func (h *handlers) handleBTDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if h.config.BTDebugRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "bt debug registry not configured")
		return
	}

	entityID := r.URL.Query().Get("entity_id")
	if entityID == "" {
		writeError(w, http.StatusBadRequest, "entity_id parameter required")
		return
	}

	info, ok := h.config.BTDebugRegistry.Get(entityID)
	if !ok {
		writeError(w, http.StatusNotFound, "entity not found")
		return
	}

	writeJSON(w, info)
}

// handleBTStats GET /api/bt/stats — 行为树统计（总数、各 LOD 级别分布）
func (h *handlers) handleBTStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if h.config.BTDebugRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "bt debug registry not configured")
		return
	}

	all := h.config.BTDebugRegistry.GetAll()
	lodDist := map[int]int{}
	statusDist := map[string]int{}
	for _, info := range all {
		lodDist[info.LODLevel]++
		statusDist[info.LastStatus]++
	}

	writeJSON(w, map[string]interface{}{
		"total":               len(all),
		"lod_distribution":    lodDist,
		"status_distribution": statusDist,
	})
}
