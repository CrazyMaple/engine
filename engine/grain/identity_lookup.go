package grain

import (
	"fmt"
	"sync"
	"time"

	"engine/actor"
	"engine/cluster"
	"engine/log"
	"engine/remote"
)

// IdentityLookup Grain 身份定位接口
type IdentityLookup interface {
	// Get 获取 Grain 的 PID，不存在则激活
	Get(identity *GrainIdentity) (*actor.PID, error)
	// Remove 移除 Grain 缓存（去激活时调用）
	Remove(identity *GrainIdentity)
	// Setup 初始化
	Setup(c *cluster.Cluster, kinds *KindRegistry)
	// Shutdown 关闭
	Shutdown()
}

// DistHashIdentityLookup 基于一致性哈希的 Grain 定位
type DistHashIdentityLookup struct {
	cluster      *cluster.Cluster
	kinds        *KindRegistry
	pidCache     map[string]*actor.PID // identity string -> PID
	placementPID *actor.PID            // 本地 PlacementActor PID
	mu           sync.RWMutex
}

// NewDistHashIdentityLookup 创建基于一致性哈希的定位器
func NewDistHashIdentityLookup() *DistHashIdentityLookup {
	return &DistHashIdentityLookup{
		pidCache: make(map[string]*actor.PID),
	}
}

// Setup 初始化定位器
func (l *DistHashIdentityLookup) Setup(c *cluster.Cluster, kinds *KindRegistry) {
	l.cluster = c
	l.kinds = kinds

	// 注册远程消息类型
	remote.RegisterType(&ActivateRequest{})
	remote.RegisterType(&ActivateResponse{})
	remote.RegisterType(&DeactivateRequest{})
	remote.RegisterType(&GrainInit{})
	remote.RegisterType(&GrainIdentity{})

	// 创建本地 PlacementActor
	props := actor.PropsFromProducer(func() actor.Actor {
		return newPlacementActor(kinds)
	})
	l.placementPID = c.System().Root.SpawnNamed(props, "grain/placement")

	// 监听拓扑变更，清空缓存
	c.System().EventStream.Subscribe(func(event interface{}) {
		if _, ok := event.(*cluster.ClusterTopologyEvent); ok {
			l.clearCache()
		}
	})
}

// Shutdown 关闭定位器
func (l *DistHashIdentityLookup) Shutdown() {
	if l.placementPID != nil {
		l.cluster.System().Root.Stop(l.placementPID)
	}
}

// Get 获取 Grain 的 PID
func (l *DistHashIdentityLookup) Get(identity *GrainIdentity) (*actor.PID, error) {
	key := identity.String()

	// 检查缓存
	l.mu.RLock()
	if pid, ok := l.pidCache[key]; ok {
		l.mu.RUnlock()
		return pid, nil
	}
	l.mu.RUnlock()

	// 通过一致性哈希确定归属节点
	member := l.cluster.GetMemberForIdentity(identity.Identity, identity.Kind)
	if member == nil {
		return nil, fmt.Errorf("no member available for kind: %s", identity.Kind)
	}

	// 向归属节点的 PlacementActor 发送激活请求
	var pid *actor.PID
	var err error

	if member.Address == l.cluster.Self().Address {
		// 本地激活
		pid, err = l.activateLocal(identity)
	} else {
		// 远程激活
		pid, err = l.activateRemote(identity, member)
	}

	if err != nil {
		return nil, err
	}

	// 缓存 PID
	l.mu.Lock()
	l.pidCache[key] = pid
	l.mu.Unlock()

	return pid, nil
}

// Remove 移除 Grain 缓存
func (l *DistHashIdentityLookup) Remove(identity *GrainIdentity) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.pidCache, identity.String())
}

// activateLocal 本地激活
func (l *DistHashIdentityLookup) activateLocal(identity *GrainIdentity) (*actor.PID, error) {
	future := l.cluster.System().Root.RequestFuture(
		l.placementPID,
		&ActivateRequest{Identity: identity},
		5*time.Second,
	)

	result, err := future.Wait()
	if err != nil {
		return nil, fmt.Errorf("activate %s: %w", identity.String(), err)
	}

	resp, ok := result.(*ActivateResponse)
	if !ok {
		return nil, fmt.Errorf("activate %s: unexpected response type", identity.String())
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("activate %s: %s", identity.String(), resp.Error)
	}

	return resp.PID, nil
}

// activateRemote 远程激活
func (l *DistHashIdentityLookup) activateRemote(identity *GrainIdentity, member *cluster.Member) (*actor.PID, error) {
	target := actor.NewPID(member.Address, "grain/placement")

	future := l.cluster.System().Root.RequestFuture(
		target,
		&ActivateRequest{Identity: identity},
		5*time.Second,
	)

	result, err := future.Wait()
	if err != nil {
		return nil, fmt.Errorf("remote activate %s on %s: %w", identity.String(), member.Address, err)
	}

	resp, ok := result.(*ActivateResponse)
	if !ok {
		return nil, fmt.Errorf("remote activate %s: unexpected response type", identity.String())
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("remote activate %s: %s", identity.String(), resp.Error)
	}

	return resp.PID, nil
}

// clearCache 清空 PID 缓存
func (l *DistHashIdentityLookup) clearCache() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pidCache = make(map[string]*actor.PID)
	log.Debug("IdentityLookup: cache cleared due to topology change")
}
