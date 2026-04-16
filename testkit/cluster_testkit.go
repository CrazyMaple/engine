// Package testkit 提供多节点集成测试辅助工具
//
// 支持自动启动 N 个本地节点并组成集群，简化 Actor 分布式场景测试。
// 测试结束后自动清理所有资源。
package testkit

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"

	"engine/actor"
	"engine/remote"
)

// ClusterTestKit 多节点集群测试工具
// 自动启动 N 个本地 Actor 节点 + Remote 层，测试结束后自动关闭
type ClusterTestKit struct {
	t     testing.TB
	mu    sync.Mutex
	nodes []*TestNode
}

// TestNode 测试集群中的一个节点
type TestNode struct {
	System  *actor.ActorSystem
	Remote  *remote.Remote
	Address string
}

// NewClusterTestKit 创建集群测试工具
func NewClusterTestKit(t testing.TB) *ClusterTestKit {
	tk := &ClusterTestKit{t: t}
	t.Cleanup(func() { tk.Shutdown() })
	return tk
}

// StartNodes 启动 N 个本地节点
// 每个节点使用随机可用端口，避免测试间冲突
func (tk *ClusterTestKit) StartNodes(count int) []*TestNode {
	tk.mu.Lock()
	defer tk.mu.Unlock()

	nodes := make([]*TestNode, count)
	for i := 0; i < count; i++ {
		port := randomPort()
		addr := fmt.Sprintf("127.0.0.1:%d", port)

		system := actor.NewActorSystem()
		system.Address = addr

		r := remote.NewRemote(system, addr)
		r.Start()

		node := &TestNode{
			System:  system,
			Remote:  r,
			Address: addr,
		}
		nodes[i] = node
		tk.nodes = append(tk.nodes, node)
	}
	return nodes
}

// StartNode 启动单个节点
func (tk *ClusterTestKit) StartNode() *TestNode {
	return tk.StartNodes(1)[0]
}

// SpawnOnNode 在指定节点上创建 Actor
func (tk *ClusterTestKit) SpawnOnNode(node *TestNode, props *actor.Props) *actor.PID {
	return node.System.Root.Spawn(props)
}

// SpawnNamedOnNode 在指定节点上创建命名 Actor
func (tk *ClusterTestKit) SpawnNamedOnNode(node *TestNode, props *actor.Props, name string) *actor.PID {
	return node.System.Root.SpawnNamed(props, name)
}

// SendCross 跨节点发送消息
func (tk *ClusterTestKit) SendCross(fromNode *TestNode, toPID *actor.PID, msg interface{}) {
	fromNode.System.Root.Send(toPID, msg)
}

// WaitForNodes 等待所有节点的 Remote 层就绪
func (tk *ClusterTestKit) WaitForNodes(timeout time.Duration) bool {
	tk.mu.Lock()
	nodes := make([]*TestNode, len(tk.nodes))
	copy(nodes, tk.nodes)
	tk.mu.Unlock()

	deadline := time.Now().Add(timeout)
	for _, node := range nodes {
		for time.Now().Before(deadline) {
			conn, err := net.DialTimeout("tcp", node.Address, 100*time.Millisecond)
			if err == nil {
				conn.Close()
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return true
}

// Node 获取第 i 个节点
func (tk *ClusterTestKit) Node(i int) *TestNode {
	tk.mu.Lock()
	defer tk.mu.Unlock()
	if i < 0 || i >= len(tk.nodes) {
		tk.t.Fatalf("ClusterTestKit: node index %d out of range (total %d)", i, len(tk.nodes))
	}
	return tk.nodes[i]
}

// NodeCount 返回节点数量
func (tk *ClusterTestKit) NodeCount() int {
	tk.mu.Lock()
	defer tk.mu.Unlock()
	return len(tk.nodes)
}

// Shutdown 关闭所有节点
func (tk *ClusterTestKit) Shutdown() {
	tk.mu.Lock()
	nodes := tk.nodes
	tk.nodes = nil
	tk.mu.Unlock()

	for _, node := range nodes {
		node.Remote.Stop()
	}
}

// randomPort 获取一个随机可用端口
func randomPort() int {
	// 尝试监听 :0 让操作系统分配端口
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		// 回退到随机端口
		return 30000 + rand.Intn(20000)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}
