package cluster

// Provider 集群成员发现接口（最小集），见 ADR v1.13-001 §2.1
//
// engine/cluster 不再内嵌任何具体 discovery 实现；具体 provider
// （consul / etcd / k8s / gossip）由 gamelib/cluster 提供，按本接口注入。
type Provider interface {
	// Start 初始化 provider 并开始持续推送成员变更。
	//
	//   clusterName 用于命名空间隔离（不同集群的成员互不干扰）。
	//   self        本节点的 Member 元信息（地址、节点 ID、Tag）。
	//   onChange    成员变更回调，由 provider 在自有 goroutine 中调用；
	//               每次回调都必须传入"当前集群全部活跃成员"的全量快照。
	//
	// 返回 error 即视为 provider 启动失败：Cluster.Start() 必须 fail-fast，
	// 不允许隐式 fallback 到旧 seed gossip 路径。
	Start(clusterName string, self *Member, onChange func([]*Member)) error

	// Stop 停止 provider 并释放底层资源（连接、watch、ticker 等）。
	// Stop 可被多次调用，第二次起返回 nil 即可（幂等）。
	Stop() error
}

// RegistrableProvider 可选扩展接口：表达 provider 支持显式注册 / 注销节点。
// consul / etcd / k8s 等动态 provider 实现；StaticProvider / FakeProvider 不实现。
type RegistrableProvider interface {
	Provider
	Register(self *Member) error
	Deregister(self *Member) error
}

// ListableProvider 可选扩展接口：表达 provider 支持主动拉取一次完整成员清单。
// 用于诊断 / CLI / 首次冷启动；不参与运行期推送。
type ListableProvider interface {
	Provider
	GetMembers() ([]*Member, error)
}
