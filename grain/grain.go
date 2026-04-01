package grain

import (
	"time"

	"engine/actor"
	"engine/cluster"
)

// GrainClient 虚拟 Actor 客户端，提供便捷的 Grain 操作 API
type GrainClient struct {
	cluster *cluster.Cluster
	lookup  IdentityLookup
}

// NewGrainClient 创建 Grain 客户端
func NewGrainClient(c *cluster.Cluster, lookup IdentityLookup) *GrainClient {
	return &GrainClient{
		cluster: c,
		lookup:  lookup,
	}
}

// Get 获取 Grain 的 PID，不存在则自动激活
func (gc *GrainClient) Get(kind, identity string) (*actor.PID, error) {
	return gc.lookup.Get(&GrainIdentity{Kind: kind, Identity: identity})
}

// Send 向 Grain 发送消息（fire-and-forget）
func (gc *GrainClient) Send(kind, identity string, message interface{}) error {
	pid, err := gc.Get(kind, identity)
	if err != nil {
		return err
	}
	gc.cluster.System().Root.Send(pid, message)
	return nil
}

// Request 向 Grain 发送请求并等待响应
func (gc *GrainClient) Request(kind, identity string, message interface{}, timeout time.Duration) (interface{}, error) {
	pid, err := gc.Get(kind, identity)
	if err != nil {
		return nil, err
	}
	future := gc.cluster.System().Root.RequestFuture(pid, message, timeout)
	return future.Wait()
}

// Remove 从缓存中移除 Grain（通常在 Grain 去激活时调用）
func (gc *GrainClient) Remove(kind, identity string) {
	gc.lookup.Remove(&GrainIdentity{Kind: kind, Identity: identity})
}

// Lookup 返回底层的 IdentityLookup
func (gc *GrainClient) Lookup() IdentityLookup {
	return gc.lookup
}
