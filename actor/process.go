package actor

import "sync"

// Process 是Actor进程的抽象接口
type Process interface {
	SendUserMessage(pid *PID, message interface{})
	SendSystemMessage(pid *PID, message interface{})
	Stop(pid *PID)
}

// ProcessRegistry 管理本地Actor进程
type ProcessRegistry struct {
	localActors    map[string]Process
	remoteProc     Process // 远程进程代理
	federatedProc  Process // 联邦进程代理（跨集群）
	mu             sync.RWMutex
}

// NewProcessRegistry 创建进程注册表
func NewProcessRegistry() *ProcessRegistry {
	return &ProcessRegistry{
		localActors: make(map[string]Process),
	}
}

// SetRemoteProcess 设置远程进程代理
func (pr *ProcessRegistry) SetRemoteProcess(process Process) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.remoteProc = process
}

// SetFederatedProcess 设置联邦进程代理（用于跨集群消息路由）
func (pr *ProcessRegistry) SetFederatedProcess(process Process) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.federatedProc = process
}

// Add 注册进程
func (pr *ProcessRegistry) Add(pid *PID, process Process) {
	pr.mu.Lock()
	pr.localActors[pid.Id] = process
	pid.p = process
	pr.mu.Unlock()
	// 同步写入全局 PID 缓存
	globalPIDCache.Set(pid, process)
}

// Remove 移除进程
func (pr *ProcessRegistry) Remove(pid *PID) {
	pr.mu.Lock()
	delete(pr.localActors, pid.Id)
	pr.mu.Unlock()
	// 失效 PID 缓存
	globalPIDCache.Invalidate(pid)
}

// Get 获取进程
func (pr *ProcessRegistry) Get(pid *PID) (Process, bool) {
	// 如果是联邦 PID（cluster:// 开头），返回联邦进程代理
	if !pid.IsLocal() && len(pid.Address) > 10 && pid.Address[:10] == "cluster://" {
		pr.mu.RLock()
		fp := pr.federatedProc
		pr.mu.RUnlock()
		if fp != nil {
			return fp, true
		}
		return nil, false
	}

	// 如果是远程PID，返回远程进程代理
	if !pid.IsLocal() {
		pr.mu.RLock()
		rp := pr.remoteProc
		pr.mu.RUnlock()
		if rp != nil {
			return rp, true
		}
		return nil, false
	}

	// 本地 PID 快路径：pid.p 直接缓存命中（零开销）
	if pid.p != nil {
		return pid.p, true
	}

	// 次级缓存：全局 PIDCache 查找（sync.Map，无锁）
	if p, ok := globalPIDCache.Get(pid); ok {
		return p, true
	}

	// 最后回退到 RWMutex 保护的 map 查找
	pr.mu.RLock()
	p, ok := pr.localActors[pid.Id]
	pr.mu.RUnlock()
	if ok {
		// 回填到缓存
		globalPIDCache.Set(pid, p)
	}
	return p, ok
}

// GetByID 通过ID获取进程
func (pr *ProcessRegistry) GetByID(id string) (Process, bool) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	p, ok := pr.localActors[id]
	return p, ok
}

// GetAll 获取所有已注册的进程（返回浅拷贝）
func (pr *ProcessRegistry) GetAll() map[string]Process {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	result := make(map[string]Process, len(pr.localActors))
	for k, v := range pr.localActors {
		result[k] = v
	}
	return result
}

// Count 返回已注册的进程数量
func (pr *ProcessRegistry) Count() int {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return len(pr.localActors)
}

// GetAllIDs 获取所有已注册的进程 ID 列表
func (pr *ProcessRegistry) GetAllIDs() []string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	ids := make([]string, 0, len(pr.localActors))
	for id := range pr.localActors {
		ids = append(ids, id)
	}
	return ids
}
