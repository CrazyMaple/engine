package actor

import (
	"sync"
	"sync/atomic"
)

// PIDCache 高频 PID → Process 查找缓存
// 设计目标：消除高频 Send 路径的 ProcessRegistry RWMutex 争用
//
// 机制：
//  1. sync.Map 实现 lock-free 只读命中
//  2. ProcessRegistry 写入时失效对应 entry
//  3. 对本地 PID 的 pid.p 字段缓存是快路径，PIDCache 作为次级回退
type PIDCache struct {
	entries sync.Map // map[string]*pidCacheEntry，key = address + "/" + id

	// 统计
	hits   int64
	misses int64
}

type pidCacheEntry struct {
	process Process
	version uint64 // 版本号，用于检测过期
}

// globalPIDCache 全局 PID 缓存实例
var globalPIDCache = &PIDCache{}

// GlobalPIDCache 返回全局 PID 缓存
func GlobalPIDCache() *PIDCache {
	return globalPIDCache
}

// cacheKey 构造缓存 key（避免分配：对于本地 PID 直接使用 id）
func cacheKey(pid *PID) string {
	if pid.Address == "" {
		return pid.Id
	}
	return pid.Address + "/" + pid.Id
}

// Get 从缓存中获取 Process
func (c *PIDCache) Get(pid *PID) (Process, bool) {
	if pid == nil {
		return nil, false
	}
	// 快路径：pid.p 直接缓存命中（零开销）
	if pid.p != nil {
		atomic.AddInt64(&c.hits, 1)
		return pid.p, true
	}

	// 次级缓存：sync.Map 查找
	if v, ok := c.entries.Load(cacheKey(pid)); ok {
		entry := v.(*pidCacheEntry)
		atomic.AddInt64(&c.hits, 1)
		return entry.process, true
	}

	atomic.AddInt64(&c.misses, 1)
	return nil, false
}

// Set 将 PID → Process 写入缓存
func (c *PIDCache) Set(pid *PID, process Process) {
	if pid == nil || process == nil {
		return
	}
	// 直接缓存到 PID 内部
	pid.p = process
	// 同时缓存到 sync.Map（用于相同 id 不同 PID 实例场景）
	c.entries.Store(cacheKey(pid), &pidCacheEntry{
		process: process,
	})
}

// Invalidate 失效缓存中某个 PID 的记录
func (c *PIDCache) Invalidate(pid *PID) {
	if pid == nil {
		return
	}
	pid.p = nil
	c.entries.Delete(cacheKey(pid))
}

// InvalidateAll 清空缓存（用于测试或系统重置）
func (c *PIDCache) InvalidateAll() {
	c.entries = sync.Map{}
	atomic.StoreInt64(&c.hits, 0)
	atomic.StoreInt64(&c.misses, 0)
}

// Stats 返回缓存统计
func (c *PIDCache) Stats() (hits, misses int64) {
	return atomic.LoadInt64(&c.hits), atomic.LoadInt64(&c.misses)
}

// HitRate 返回缓存命中率 [0,1]
func (c *PIDCache) HitRate() float64 {
	h := atomic.LoadInt64(&c.hits)
	m := atomic.LoadInt64(&c.misses)
	total := h + m
	if total == 0 {
		return 0
	}
	return float64(h) / float64(total)
}
