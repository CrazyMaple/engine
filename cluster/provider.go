package cluster

// ClusterProvider 集群服务发现提供者接口
// 抽象外部服务发现后端（如 Consul、etcd、K8s），
// 使集群可以从不同的发现机制获取成员信息
type ClusterProvider interface {
	// Start 启动服务发现，onChange 回调在成员变更时被调用
	Start(clusterName string, self *Member, onChange func(members []*Member)) error

	// Stop 停止服务发现
	Stop() error

	// Register 注册本节点到服务发现后端
	Register() error

	// Deregister 从服务发现后端移除本节点
	Deregister() error

	// GetMembers 获取当前已知成员列表
	GetMembers() ([]*Member, error)
}
