package operator

import (
	"fmt"
	"sync"
	"time"

	"engine/cluster"
	"engine/log"
)

// NodeInfo Operator 视角的节点信息
type NodeInfo struct {
	Address     string
	Version     string
	Ready       bool
	Connections int64
	ActorCount  int
	CPUPercent  float64
	LastSeen    time.Time
}

// ClusterSource 集群状态数据源接口（解耦 cluster 包直接依赖）
type ClusterSource interface {
	// Members 返回当前集群成员列表
	Members() []ClusterMemberInfo
	// IsHealthy 检查指定节点是否健康
	IsHealthy(addr string) bool
}

// ClusterMemberInfo 集群成员信息
type ClusterMemberInfo struct {
	Address string
	Kinds   []string
	Alive   bool
}

// Controller EngineCluster CRD 控制器
//
// v1.13 收缩后只保留：状态采集、事件发出、扩缩候选挑选；
// 滚动升级与 Actor 迁移已不再由 engine/cluster 提供，调用方需通过外部
// 协调器（gamelib 侧或上层运维系统）独立完成。
type Controller struct {
	mu sync.Mutex

	desired *EngineCluster
	nodes   map[string]*NodeInfo

	cluster *cluster.Cluster
	scaler  *Scaler
	source  ClusterSource

	reconcileInterval time.Duration
	stopCh            chan struct{}
	stopped           bool

	onEvent func(event ControllerEvent)
}

// ControllerEvent 控制器事件
type ControllerEvent struct {
	Type    string // "ScaleUp", "ScaleDown", "UpgradeStart", "UpgradeComplete", "MigrationTriggered"
	Message string
	Time    time.Time
}

// ControllerConfig 控制器配置
type ControllerConfig struct {
	Cluster           *cluster.Cluster
	Source            ClusterSource
	ReconcileInterval time.Duration
}

// NewController 创建控制器
func NewController(cfg ControllerConfig) *Controller {
	interval := cfg.ReconcileInterval
	if interval == 0 {
		interval = 10 * time.Second
	}
	return &Controller{
		nodes:             make(map[string]*NodeInfo),
		cluster:           cfg.Cluster,
		source:            cfg.Source,
		reconcileInterval: interval,
		stopCh:            make(chan struct{}),
	}
}

// SetScaler 关联自动扩缩容器
func (c *Controller) SetScaler(s *Scaler) {
	c.mu.Lock()
	c.scaler = s
	c.mu.Unlock()
}

// SetEventHandler 设置事件回调
func (c *Controller) SetEventHandler(fn func(ControllerEvent)) {
	c.mu.Lock()
	c.onEvent = fn
	c.mu.Unlock()
}

// Apply 应用 CRD 变更（模拟 Watch 收到新 CRD）
func (c *Controller) Apply(ec *EngineCluster) {
	c.mu.Lock()
	c.desired = ec
	c.mu.Unlock()

	c.Reconcile()
}

// Start 启动 Reconcile 循环
func (c *Controller) Start() {
	go c.reconcileLoop()
}

// Stop 停止控制器
func (c *Controller) Stop() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	c.mu.Unlock()
	close(c.stopCh)
}

// Reconcile 执行一次协调——对比期望状态与实际状态，执行动作
func (c *Controller) Reconcile() {
	c.mu.Lock()
	desired := c.desired
	c.mu.Unlock()

	if desired == nil {
		return
	}

	c.syncNodeStates()
	c.reconcileVersion(desired)
	c.reconcileReplicas(desired)
	c.updateStatus(desired)
}

// Status 返回当前 CRD 状态
func (c *Controller) Status() *EngineClusterStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.desired == nil {
		return nil
	}
	status := c.desired.Status
	return &status
}

// Nodes 返回当前节点状态表
func (c *Controller) Nodes() map[string]*NodeInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string]*NodeInfo, len(c.nodes))
	for k, v := range c.nodes {
		cp := *v
		result[k] = &cp
	}
	return result
}

func (c *Controller) reconcileLoop() {
	ticker := time.NewTicker(c.reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.Reconcile()
		}
	}
}

// syncNodeStates 从集群源同步节点状态
func (c *Controller) syncNodeStates() {
	if c.source == nil {
		return
	}

	members := c.source.Members()
	c.mu.Lock()
	defer c.mu.Unlock()

	seen := make(map[string]bool, len(members))
	for _, m := range members {
		seen[m.Address] = true
		node, exists := c.nodes[m.Address]
		if !exists {
			node = &NodeInfo{Address: m.Address}
			c.nodes[m.Address] = node
		}
		node.Ready = m.Alive && c.source.IsHealthy(m.Address)
		node.LastSeen = time.Now()
	}

	for addr := range c.nodes {
		if !seen[addr] {
			delete(c.nodes, addr)
		}
	}
}

// reconcileVersion 检查是否需要版本升级
//
// v1.13 起 controller 不再驱动具体的滚动升级流程；只发出事件并维护 CRD 状态字段，
// 由上层运维系统接收事件后实际执行节点替换。
func (c *Controller) reconcileVersion(desired *EngineCluster) {
	c.mu.Lock()
	currentVersion := desired.Status.CurrentVersion
	targetVersion := desired.Spec.Version
	c.mu.Unlock()

	if currentVersion == targetVersion || targetVersion == "" {
		return
	}

	log.Info("operator: version change detected %s -> %s", currentVersion, targetVersion)

	c.mu.Lock()
	nodes := make([]string, 0, len(c.nodes))
	for addr, info := range c.nodes {
		if info.Version != targetVersion {
			nodes = append(nodes, addr)
		}
	}
	strategy := desired.Spec.UpgradeStrategy
	c.mu.Unlock()

	if len(nodes) == 0 {
		c.mu.Lock()
		desired.Status.CurrentVersion = targetVersion
		desired.Status.Phase = PhaseRunning
		desired.Status.TargetVersion = ""
		c.mu.Unlock()
		return
	}

	c.mu.Lock()
	desired.Status.Phase = PhaseUpgrading
	desired.Status.TargetVersion = targetVersion
	c.mu.Unlock()

	c.emitEvent("UpgradeStart", fmt.Sprintf("rolling upgrade to %s, %d nodes", targetVersion, len(nodes)))

	if strategy.MigrateActors {
		c.emitEvent("MigrationTriggered", "migrating actors before upgrade")
	}

	// 没有内置协调器：标记完成；具体的节点替换由外部驱动
	c.mu.Lock()
	desired.Status.CurrentVersion = targetVersion
	desired.Status.Phase = PhaseRunning
	desired.Status.TargetVersion = ""
	c.mu.Unlock()
	c.emitEvent("UpgradeComplete", fmt.Sprintf("upgraded to %s (no coordinator)", targetVersion))
}

// reconcileReplicas 检查是否需要扩缩容
func (c *Controller) reconcileReplicas(desired *EngineCluster) {
	c.mu.Lock()
	currentCount := len(c.nodes)
	desiredCount := desired.Spec.Replicas
	c.mu.Unlock()

	if currentCount == desiredCount || desiredCount <= 0 {
		return
	}

	if currentCount < desiredCount {
		c.mu.Lock()
		desired.Status.Phase = PhaseScaling
		c.mu.Unlock()
		c.emitEvent("ScaleUp", fmt.Sprintf("scaling from %d to %d", currentCount, desiredCount))
		return
	}

	c.mu.Lock()
	desired.Status.Phase = PhaseScaling
	migrate := desired.Spec.UpgradeStrategy.MigrateActors
	c.mu.Unlock()

	removeCount := currentCount - desiredCount
	candidates := c.selectScaleDownCandidates(removeCount)

	if migrate {
		for _, addr := range candidates {
			c.emitEvent("MigrationTriggered", fmt.Sprintf("migrating actors from %s before scale-down", addr))
		}
	}

	c.emitEvent("ScaleDown", fmt.Sprintf("scaling from %d to %d, removing %v", currentCount, desiredCount, candidates))
}

// selectScaleDownCandidates 选择缩容候选节点（Actor 数最少的优先）
func (c *Controller) selectScaleDownCandidates(count int) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	type nodeScore struct {
		addr  string
		score int64
	}

	scores := make([]nodeScore, 0, len(c.nodes))
	for addr, info := range c.nodes {
		scores = append(scores, nodeScore{
			addr:  addr,
			score: info.Connections + int64(info.ActorCount),
		})
	}

	for i := 0; i < len(scores); i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[j].score < scores[i].score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	result := make([]string, 0, count)
	for i := 0; i < count && i < len(scores); i++ {
		result = append(result, scores[i].addr)
	}
	return result
}

// updateStatus 更新 CRD status 字段
func (c *Controller) updateStatus(desired *EngineCluster) {
	c.mu.Lock()
	defer c.mu.Unlock()

	readyCount := 0
	for _, info := range c.nodes {
		if info.Ready {
			readyCount++
		}
	}
	desired.Status.ReadyReplicas = readyCount

	if desired.Status.Phase != PhaseUpgrading && desired.Status.Phase != PhaseFailed {
		if readyCount >= desired.Spec.Replicas {
			desired.Status.Phase = PhaseRunning
		} else if readyCount == 0 {
			desired.Status.Phase = PhasePending
		}
	}
}

func (c *Controller) emitEvent(eventType, message string) {
	c.mu.Lock()
	fn := c.onEvent
	c.mu.Unlock()

	if fn != nil {
		fn(ControllerEvent{
			Type:    eventType,
			Message: message,
			Time:    time.Now(),
		})
	}
}
