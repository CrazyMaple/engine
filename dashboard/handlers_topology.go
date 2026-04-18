package dashboard

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"engine/actor"
	"engine/cluster"
)

// drainState 节点排空标记，运维操作的 Dashboard 侧记忆
type drainState struct {
	Address  string    `json:"address"`
	Reason   string    `json:"reason"`
	MarkedAt time.Time `json:"marked_at"`
}

var (
	drainMu     sync.RWMutex
	drainMarked = map[string]drainState{}
)

// ---- GET /api/topology/node?address=host:port ----
// 返回节点详情（成员信息 + 排空标记）
func (h *handlers) handleTopologyNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	if h.config.Cluster == nil {
		writeError(w, http.StatusNotFound, "cluster not configured")
		return
	}
	addr := r.URL.Query().Get("address")
	if addr == "" {
		writeError(w, http.StatusBadRequest, "address required")
		return
	}
	var found *cluster.Member
	for _, m := range h.config.Cluster.Members() {
		if m.Address == addr {
			found = m
			break
		}
	}
	if found == nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	drainMu.RLock()
	mark, drain := drainMarked[addr]
	drainMu.RUnlock()

	resp := map[string]interface{}{
		"address":   found.Address,
		"id":        found.Id,
		"status":    found.Status.String(),
		"kinds":     found.Kinds,
		"seq":       found.Seq,
		"last_seen": found.LastSeen.Format(time.RFC3339Nano),
		"is_self":   found.Address == h.config.Cluster.Self().Address,
		"draining":  drain,
	}
	if drain {
		resp["drain_info"] = mark
	}
	writeJSON(w, resp)
}

// ---- POST /api/topology/migrate ----
// body: {"actor_id":"...", "target_address":"host:port"}
// 通过外部注入的 MigrationManager 执行；当前实现仅接受请求并记录指令
func (h *handlers) handleTopologyMigrate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	if h.config.Cluster == nil {
		writeError(w, http.StatusNotFound, "cluster not configured")
		return
	}
	var req struct {
		ActorID       string `json:"actor_id"`
		TargetAddress string `json:"target_address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ActorID == "" || req.TargetAddress == "" {
		writeError(w, http.StatusBadRequest, "actor_id and target_address required")
		return
	}
	// 校验目标节点存在
	var found bool
	for _, m := range h.config.Cluster.Members() {
		if m.Address == req.TargetAddress {
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusBadRequest, "target_address not in cluster")
		return
	}
	pid := actor.NewPID(h.config.Cluster.Self().Address, req.ActorID)
	resp := map[string]interface{}{
		"status":         "queued",
		"actor_id":       pid.Id,
		"target_address": req.TargetAddress,
		"note":           "migration request accepted; integrate with MigrationManager.Migrate to execute",
	}
	writeJSON(w, resp)
}

// ---- POST /api/topology/drain ----
// body: {"address":"host:port", "reason":"...", "cancel":false}
// 标记节点为排空状态（不再接受新请求）。仅维护 Dashboard 侧标记，
// 上层调用方在路由时应过滤 drainMarked 中的节点。
func (h *handlers) handleTopologyDrain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	if h.config.Cluster == nil {
		writeError(w, http.StatusNotFound, "cluster not configured")
		return
	}
	var req struct {
		Address string `json:"address"`
		Reason  string `json:"reason"`
		Cancel  bool   `json:"cancel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Address == "" {
		writeError(w, http.StatusBadRequest, "address required")
		return
	}
	drainMu.Lock()
	if req.Cancel {
		delete(drainMarked, req.Address)
	} else {
		drainMarked[req.Address] = drainState{
			Address:  req.Address,
			Reason:   req.Reason,
			MarkedAt: time.Now(),
		}
	}
	all := make([]drainState, 0, len(drainMarked))
	for _, v := range drainMarked {
		all = append(all, v)
	}
	drainMu.Unlock()

	writeJSON(w, map[string]interface{}{
		"status": "ok",
		"drains": all,
	})
}

// IsDrained 提供给路由层判断节点是否被标记排空
func IsDrained(address string) bool {
	drainMu.RLock()
	_, ok := drainMarked[address]
	drainMu.RUnlock()
	return ok
}

// ListDrained 提供给程序内部查询当前所有排空节点
func ListDrained() []drainState {
	drainMu.RLock()
	defer drainMu.RUnlock()
	out := make([]drainState, 0, len(drainMarked))
	for _, v := range drainMarked {
		out = append(out, v)
	}
	return out
}

// ResetDrained 仅用于测试：清空排空标记
func ResetDrained() {
	drainMu.Lock()
	drainMarked = map[string]drainState{}
	drainMu.Unlock()
}
