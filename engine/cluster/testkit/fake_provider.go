// Package testkit 提供 engine/cluster 测试与 gamelib 各 Provider 实现的契约测试基础设施。
package testkit

import (
	"sync"

	"engine/cluster"
)

// FakeProvider in-memory Provider，仅依赖标准库 + engine/cluster。
// 用于 engine cluster 的核心测试（Start/Stop、成员上下线、Singleton、ConsistentHash、
// ClusterTopologyEvent）以及 gamelib 各动态 provider 的契约测试。
type FakeProvider struct {
	mu       sync.Mutex
	started  bool
	self     *Member
	onChange func([]*cluster.Member)
}

// Member 重导出以便测试代码无需双 import。
type Member = cluster.Member

// NewFakeProvider 创建一个空白 FakeProvider。
func NewFakeProvider() *FakeProvider {
	return &FakeProvider{}
}

// Start 实现 cluster.Provider。
// 注意：不会自动把 self 推送出去——调用方按需通过 PushMembers 模拟拓扑变化。
func (f *FakeProvider) Start(_ string, self *Member, onChange func([]*cluster.Member)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = true
	f.self = self
	f.onChange = onChange
	return nil
}

// Stop 实现 cluster.Provider；幂等。
func (f *FakeProvider) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = false
	f.onChange = nil
	return nil
}

// PushMembers 模拟服务发现层推送一次成员快照。
// 测试代码通过此方法触发 Cluster 的成员变更处理（含 ClusterTopologyEvent 发布）。
func (f *FakeProvider) PushMembers(members []*cluster.Member) {
	f.mu.Lock()
	cb := f.onChange
	f.mu.Unlock()
	if cb != nil {
		cb(members)
	}
}

// Self 返回 Start 时记录的本节点信息（用于测试断言）。
func (f *FakeProvider) Self() *Member {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.self
}

// Started 返回是否已启动（用于测试断言）。
func (f *FakeProvider) Started() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.started
}
