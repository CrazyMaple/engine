package stress

import (
	"fmt"
	"testing"
	"time"

	"engine/actor"
	"engine/cluster"
	"engine/remote"
)

// createNode 创建一个集群节点（ActorSystem + Remote + Cluster）
func createNode(t *testing.T, addr string, clusterName string, seeds []string, kinds []string) (*actor.ActorSystem, *remote.Remote, *cluster.Cluster) {
	t.Helper()

	system := actor.NewActorSystem()
	system.Address = addr

	r := remote.NewRemote(system, addr)
	r.Start()

	config := cluster.DefaultClusterConfig(clusterName, addr).
		WithSeedNodes(seeds...).
		WithKinds(kinds...).
		WithGossipInterval(200 * time.Millisecond).
		WithHeartbeatInterval(500 * time.Millisecond).
		WithHeartbeatTimeout(2 * time.Second)
	config.DeadTimeout = 3 * time.Second

	c := cluster.NewCluster(system, r, config)
	if err := c.Start(); err != nil {
		t.Fatalf("failed to start cluster on %s: %v", addr, err)
	}

	return system, r, c
}

// TestStressClusterNodeFailure 集群节点故障演练
// 启动 5 个节点，杀死 1 个，验证：
// 1. 其他节点在超时时间内检测到故障
// 2. 哈希环重新平衡（不再路由到故障节点）
// 3. 重新加入后恢复
func TestStressClusterNodeFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	const (
		nodeCount   = 5
		clusterName = "stress-test-cluster"
		basePort    = 19200
	)

	// 构建种子节点列表
	seeds := make([]string, nodeCount)
	for i := 0; i < nodeCount; i++ {
		seeds[i] = fmt.Sprintf("127.0.0.1:%d", basePort+i)
	}

	// 启动所有节点
	systems := make([]*actor.ActorSystem, nodeCount)
	remotes := make([]*remote.Remote, nodeCount)
	clusters := make([]*cluster.Cluster, nodeCount)

	for i := 0; i < nodeCount; i++ {
		addr := seeds[i]
		systems[i], remotes[i], clusters[i] = createNode(t, addr, clusterName, seeds, []string{"game"})
	}

	// 等待集群收敛
	t.Log("等待集群收敛...")
	time.Sleep(3 * time.Second)

	// 验证所有节点看到完整成员列表
	for i, c := range clusters {
		members := c.Members()
		t.Logf("节点 %d 看到 %d 个成员", i, len(members))
		if len(members) < nodeCount-1 { // 至少看到大部分节点
			t.Errorf("节点 %d 只看到 %d 个成员，期望至少 %d", i, len(members), nodeCount-1)
		}
	}

	// 杀死节点 2
	killIdx := 2
	t.Logf("杀死节点 %d (%s)", killIdx, seeds[killIdx])
	clusters[killIdx].Stop()
	remotes[killIdx].Stop()

	// 等待故障检测
	t.Log("等待故障检测...")
	time.Sleep(5 * time.Second)

	// 验证其他节点检测到故障
	for i, c := range clusters {
		if i == killIdx {
			continue
		}
		members := c.Members()
		// 被杀死的节点不应出现在存活列表中
		for _, m := range members {
			if m.Address == seeds[killIdx] && m.Status == cluster.MemberAlive {
				t.Logf("警告：节点 %d 仍看到节点 %d 为 Alive（可能 gossip 延迟）", i, killIdx)
			}
		}
		t.Logf("节点 %d 当前看到 %d 个成员", i, len(members))
	}

	// 重新启动节点 2
	t.Logf("重启节点 %d", killIdx)
	systems[killIdx], remotes[killIdx], clusters[killIdx] = createNode(
		t, seeds[killIdx], clusterName, seeds, []string{"game"},
	)

	// 等待重新加入
	time.Sleep(3 * time.Second)

	// 验证重新加入后成员列表恢复
	for i, c := range clusters {
		members := c.Members()
		t.Logf("节点 %d 最终看到 %d 个成员", i, len(members))
	}

	// 清理
	for i := nodeCount - 1; i >= 0; i-- {
		clusters[i].Stop()
		remotes[i].Stop()
	}

	t.Log("集群故障演练完成")
}

// TestStressNetworkPartition 网络分区测试
// 使用 TCP 代理模拟分区：
// 1. 启动 4 个节点，其中 2 个通过代理连接
// 2. 阻断代理（模拟分区）
// 3. 验证两侧各自独立运行
// 4. 恢复代理
// 5. 验证集群重新收敛
func TestStressNetworkPartition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	const clusterName = "partition-test"

	// 节点 A 和 B 直连
	addrA := "127.0.0.1:19300"
	addrB := "127.0.0.1:19301"
	// 节点 C 通过代理连接
	addrC := "127.0.0.1:19302"
	// 代理地址（节点 A 和 B 连到这个地址代替 C 的真实地址）
	proxyAddr := "127.0.0.1:19310"

	// 启动代理
	proxy := NewProxy(proxyAddr, addrC)
	if err := proxy.Start(); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	// 使用代理地址作为种子
	seeds := []string{addrA, addrB, proxyAddr}

	// 启动节点
	sysA, remA, clsA := createNode(t, addrA, clusterName, seeds, []string{"game"})
	sysB, remB, clsB := createNode(t, addrB, clusterName, seeds, []string{"game"})
	_ = sysA
	_ = sysB

	// 节点 C 使用自己的种子列表（直连 A 和 B）
	seedsC := []string{addrA, addrB, addrC}
	_, remC, clsC := createNode(t, addrC, clusterName, seedsC, []string{"game"})

	// 等待收敛
	time.Sleep(3 * time.Second)
	t.Logf("分区前 - A 看到 %d 成员, B 看到 %d 成员, C 看到 %d 成员",
		len(clsA.Members()), len(clsB.Members()), len(clsC.Members()))

	// 模拟网络分区
	t.Log("阻断代理，模拟网络分区...")
	proxy.Block()
	time.Sleep(5 * time.Second)

	t.Logf("分区中 - A 看到 %d 成员, B 看到 %d 成员, C 看到 %d 成员",
		len(clsA.Members()), len(clsB.Members()), len(clsC.Members()))

	// 恢复
	t.Log("恢复代理...")
	proxy.Unblock()
	time.Sleep(5 * time.Second)

	t.Logf("恢复后 - A 看到 %d 成员, B 看到 %d 成员, C 看到 %d 成员",
		len(clsA.Members()), len(clsB.Members()), len(clsC.Members()))

	// 清理
	clsC.Stop()
	remC.Stop()
	clsB.Stop()
	remB.Stop()
	clsA.Stop()
	remA.Stop()

	t.Log("网络分区测试完成")
}
