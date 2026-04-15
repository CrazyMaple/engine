package cluster

import (
	"fmt"
	"sync"
	"time"

	"engine/actor"
	"engine/log"
)

// NodeStatus 节点升级状态
type NodeStatus int

const (
	NodeNormal    NodeStatus = iota // 正常运行
	NodeDraining                    // 排空中（不接受新请求）
	NodeDrained                     // 已排空
	NodeUpgrading                   // 升级中
	NodeCanary                      // 金丝雀验证中
)

func (s NodeStatus) String() string {
	switch s {
	case NodeNormal:
		return "Normal"
	case NodeDraining:
		return "Draining"
	case NodeDrained:
		return "Drained"
	case NodeUpgrading:
		return "Upgrading"
	case NodeCanary:
		return "Canary"
	default:
		return fmt.Sprintf("Unknown(%d)", int(s))
	}
}

// UpgradeState 升级全局状态
type UpgradeState int

const (
	UpgradeIdle       UpgradeState = iota // 无升级
	UpgradeInProgress                     // 升级进行中
	UpgradeCanary                         // 金丝雀验证
	UpgradeRollingBack                    // 回滚中
	UpgradeCompleted                      // 升级完成
)

func (s UpgradeState) String() string {
	switch s {
	case UpgradeIdle:
		return "Idle"
	case UpgradeInProgress:
		return "InProgress"
	case UpgradeCanary:
		return "Canary"
	case UpgradeRollingBack:
		return "RollingBack"
	case UpgradeCompleted:
		return "Completed"
	default:
		return fmt.Sprintf("Unknown(%d)", int(s))
	}
}

// VersionInfo 节点版本信息
type VersionInfo struct {
	Version         string // 语义化版本号，如 "1.8.0"
	ProtocolVersion int    // 协议版本号（整数，用于兼容性检查）
	BuildHash       string // 构建哈希（可选）
}

// IsCompatibleWith 检查版本兼容性（向前兼容 N-1 版本）
func (v *VersionInfo) IsCompatibleWith(other *VersionInfo) bool {
	if v == nil || other == nil {
		return true // 无版本信息视为兼容
	}
	diff := v.ProtocolVersion - other.ProtocolVersion
	if diff < 0 {
		diff = -diff
	}
	return diff <= 1 // 允许协议版本差 1
}

// UpgradeConfig 滚动升级配置
type UpgradeConfig struct {
	// DrainTimeout 节点排空超时
	DrainTimeout time.Duration
	// CanaryDuration 金丝雀验证持续时间
	CanaryDuration time.Duration
	// CanaryNodes 金丝雀节点数量
	CanaryNodes int
	// HealthCheckInterval 升级过程中的健康检查间隔
	HealthCheckInterval time.Duration
	// HealthCheckThreshold 连续健康检查失败次数触发回滚
	HealthCheckThreshold int
	// MinHealthyNodes 集群最少健康节点数
	MinHealthyNodes int
	// VersionInfo 本节点版本
	VersionInfo *VersionInfo
}

// DefaultUpgradeConfig 默认升级配置
func DefaultUpgradeConfig() *UpgradeConfig {
	return &UpgradeConfig{
		DrainTimeout:         30 * time.Second,
		CanaryDuration:       60 * time.Second,
		CanaryNodes:          1,
		HealthCheckInterval:  5 * time.Second,
		HealthCheckThreshold: 3,
		MinHealthyNodes:      1,
	}
}

// --- 升级协议消息 ---

// DrainRequest 排空请求
type DrainRequest struct {
	NodeAddr string
	Timeout  time.Duration
}

// DrainResponse 排空响应
type DrainResponse struct {
	NodeAddr string
	Success  bool
	Error    string
}

// UpgradeStatusBroadcast 升级状态广播（通过 Gossip 同步）
type UpgradeStatusBroadcast struct {
	CoordinatorAddr string
	State           UpgradeState
	TargetVersion   string
	Progress        float64 // 0.0 ~ 1.0
	Nodes           []*NodeUpgradeStatus
}

// NodeUpgradeStatus 单节点升级状态
type NodeUpgradeStatus struct {
	Address string
	Status  NodeStatus
	Version string
}

// VersionCheckRequest 版本兼容性检查请求
type VersionCheckRequest struct {
	Version *VersionInfo
}

// VersionCheckResponse 版本兼容性检查响应
type VersionCheckResponse struct {
	Compatible bool
	LocalVer   *VersionInfo
	Error      string
}

// --- 升级事件 ---

// UpgradeStartedEvent 升级开始事件
type UpgradeStartedEvent struct {
	TargetVersion string
	TotalNodes    int
}

// UpgradeProgressEvent 升级进度事件
type UpgradeProgressEvent struct {
	NodeAddr    string
	Status      NodeStatus
	CompletedN  int
	TotalN      int
}

// UpgradeCompletedEvent 升级完成事件
type UpgradeCompletedEvent struct {
	Version  string
	Duration time.Duration
}

// UpgradeRollbackEvent 升级回滚事件
type UpgradeRollbackEvent struct {
	Reason  string
	NodeAddr string
}

// NodeDrainedEvent 节点排空完成事件
type NodeDrainedEvent struct {
	NodeAddr string
}

// --- 升级协调器 ---

// RollingUpgradeCoordinator 滚动升级协调器
type RollingUpgradeCoordinator struct {
	cluster          *Cluster
	config           *UpgradeConfig
	state            UpgradeState
	coordPID         *actor.PID
	nodeStatus       map[string]*NodeUpgradeStatus
	drainStatus      map[string]NodeStatus // 本节点维护的排空状态
	migrationManager *MigrationManager     // 可选：迁移管理器（排空时迁移有状态 Actor）
	mu               sync.RWMutex
}

// NewRollingUpgradeCoordinator 创建滚动升级协调器
func NewRollingUpgradeCoordinator(c *Cluster, config *UpgradeConfig) *RollingUpgradeCoordinator {
	if config == nil {
		config = DefaultUpgradeConfig()
	}
	return &RollingUpgradeCoordinator{
		cluster:     c,
		config:      config,
		state:       UpgradeIdle,
		nodeStatus:  make(map[string]*NodeUpgradeStatus),
		drainStatus: make(map[string]NodeStatus),
	}
}

// WithMigrationManager 关联迁移管理器（排空时自动迁移有状态 Actor）
func (ruc *RollingUpgradeCoordinator) WithMigrationManager(mm *MigrationManager) *RollingUpgradeCoordinator {
	ruc.migrationManager = mm
	return ruc
}

// ActiveActorCount 获取本节点活跃 Actor 数量（不含系统 Actor）
func (ruc *RollingUpgradeCoordinator) ActiveActorCount() int {
	all := ruc.cluster.System().ProcessRegistry.GetAll()
	count := 0
	for id := range all {
		// 排除系统内部 Actor（以 "cluster/" 或 "$" 开头的是系统级）
		if len(id) > 0 && id[0] != '$' {
			count++
		}
	}
	return count
}

// Start 启动升级协调器
func (ruc *RollingUpgradeCoordinator) Start() {
	props := actor.PropsFromProducer(func() actor.Actor {
		return &upgradeCoordActor{coordinator: ruc}
	})
	ruc.coordPID = ruc.cluster.System().Root.SpawnNamed(props, "cluster/upgrade")

	log.Info("RollingUpgradeCoordinator started on %s", ruc.cluster.Self().Address)
}

// Stop 停止升级协调器
func (ruc *RollingUpgradeCoordinator) Stop() {
	if ruc.coordPID != nil {
		ruc.cluster.System().Root.Stop(ruc.coordPID)
	}
}

// State 获取当前升级状态
func (ruc *RollingUpgradeCoordinator) State() UpgradeState {
	ruc.mu.RLock()
	defer ruc.mu.RUnlock()
	return ruc.state
}

// IsDraining 检查本节点是否正在排空
func (ruc *RollingUpgradeCoordinator) IsDraining() bool {
	ruc.mu.RLock()
	defer ruc.mu.RUnlock()
	status, ok := ruc.drainStatus[ruc.cluster.Self().Address]
	return ok && (status == NodeDraining || status == NodeDrained)
}

// DrainNode 排空指定节点
func (ruc *RollingUpgradeCoordinator) DrainNode(nodeAddr string) error {
	timeout := ruc.config.DrainTimeout
	future := ruc.cluster.System().Root.RequestFuture(ruc.coordPID, &DrainRequest{
		NodeAddr: nodeAddr,
		Timeout:  timeout,
	}, timeout+5*time.Second)

	result, err := future.Wait()
	if err != nil {
		return fmt.Errorf("drain request failed: %w", err)
	}

	resp, ok := result.(*DrainResponse)
	if !ok {
		return fmt.Errorf("unexpected response type: %T", result)
	}

	if !resp.Success {
		return fmt.Errorf("drain failed: %s", resp.Error)
	}

	return nil
}

// CheckVersionCompatibility 检查节点版本兼容性
func (ruc *RollingUpgradeCoordinator) CheckVersionCompatibility(targetAddr string) (bool, error) {
	if ruc.config.VersionInfo == nil {
		return true, nil
	}

	target := actor.NewPID(targetAddr, "cluster/upgrade")
	future := ruc.cluster.System().Root.RequestFuture(target, &VersionCheckRequest{
		Version: ruc.config.VersionInfo,
	}, 5*time.Second)

	result, err := future.Wait()
	if err != nil {
		return false, fmt.Errorf("version check failed: %w", err)
	}

	resp, ok := result.(*VersionCheckResponse)
	if !ok {
		return false, fmt.Errorf("unexpected response type: %T", result)
	}

	return resp.Compatible, nil
}

// StartRollingUpgrade 启动滚动升级流程
// nodes 为要升级的节点地址列表（按顺序逐个升级）
func (ruc *RollingUpgradeCoordinator) StartRollingUpgrade(targetVersion string, nodes []string) error {
	ruc.mu.Lock()
	if ruc.state != UpgradeIdle && ruc.state != UpgradeCompleted {
		ruc.mu.Unlock()
		return fmt.Errorf("upgrade already in progress (state: %s)", ruc.state)
	}

	// 检查最少健康节点数
	alive := ruc.cluster.Members()
	if len(alive) < ruc.config.MinHealthyNodes+1 {
		ruc.mu.Unlock()
		return fmt.Errorf("not enough healthy nodes: have %d, need at least %d+1",
			len(alive), ruc.config.MinHealthyNodes)
	}

	ruc.state = UpgradeInProgress
	ruc.mu.Unlock()

	// 发布升级开始事件
	ruc.cluster.System().EventStream.Publish(&UpgradeStartedEvent{
		TargetVersion: targetVersion,
		TotalNodes:    len(nodes),
	})

	// 在后台逐个升级节点
	go ruc.executeRollingUpgrade(targetVersion, nodes)

	return nil
}

// executeRollingUpgrade 执行滚动升级（后台 goroutine）
func (ruc *RollingUpgradeCoordinator) executeRollingUpgrade(targetVersion string, nodes []string) {
	startTime := time.Now()
	canaryCount := ruc.config.CanaryNodes
	if canaryCount > len(nodes) {
		canaryCount = len(nodes)
	}

	for i, nodeAddr := range nodes {
		// 检查是否正在回滚
		ruc.mu.RLock()
		if ruc.state == UpgradeRollingBack {
			ruc.mu.RUnlock()
			return
		}
		ruc.mu.RUnlock()

		// 金丝雀阶段：前 N 个节点升级后进行验证
		if i == canaryCount && canaryCount > 0 {
			ruc.mu.Lock()
			ruc.state = UpgradeCanary
			ruc.mu.Unlock()

			log.Info("Canary phase: waiting %v for validation", ruc.config.CanaryDuration)
			if !ruc.canaryValidation() {
				ruc.triggerRollback("canary validation failed")
				return
			}

			ruc.mu.Lock()
			ruc.state = UpgradeInProgress
			ruc.mu.Unlock()
		}

		// 检查集群健康
		alive := ruc.cluster.Members()
		if len(alive) < ruc.config.MinHealthyNodes {
			ruc.triggerRollback(fmt.Sprintf("too few healthy nodes: %d", len(alive)))
			return
		}

		// Step 1: 排空节点
		if err := ruc.drainNodeInternal(nodeAddr); err != nil {
			ruc.triggerRollback(fmt.Sprintf("drain failed for %s: %v", nodeAddr, err))
			return
		}

		// Step 2: 发布进度事件
		ruc.mu.Lock()
		ruc.nodeStatus[nodeAddr] = &NodeUpgradeStatus{
			Address: nodeAddr,
			Status:  NodeUpgrading,
			Version: targetVersion,
		}
		ruc.mu.Unlock()

		ruc.cluster.System().EventStream.Publish(&UpgradeProgressEvent{
			NodeAddr:   nodeAddr,
			Status:     NodeUpgrading,
			CompletedN: i + 1,
			TotalN:     len(nodes),
		})

		// 广播升级状态
		ruc.broadcastUpgradeStatus(targetVersion, float64(i+1)/float64(len(nodes)))

		log.Info("Upgrade: node %s marked as upgrading (%d/%d)",
			nodeAddr, i+1, len(nodes))
	}

	// 全部完成
	ruc.mu.Lock()
	ruc.state = UpgradeCompleted
	ruc.mu.Unlock()

	ruc.cluster.System().EventStream.Publish(&UpgradeCompletedEvent{
		Version:  targetVersion,
		Duration: time.Since(startTime),
	})

	log.Info("Rolling upgrade completed: version %s, took %v",
		targetVersion, time.Since(startTime))
}

// drainNodeInternal 内部排空节点实现
func (ruc *RollingUpgradeCoordinator) drainNodeInternal(nodeAddr string) error {
	ruc.mu.Lock()
	ruc.drainStatus[nodeAddr] = NodeDraining
	ruc.mu.Unlock()

	log.Info("Draining node: %s", nodeAddr)

	// 如果有迁移管理器且是本节点，先迁移有状态 Actor
	if nodeAddr == ruc.cluster.Self().Address && ruc.migrationManager != nil {
		ruc.migrateActorsBeforeDrain(nodeAddr)
	}

	// 向目标节点发送排空请求
	if nodeAddr == ruc.cluster.Self().Address {
		// 本节点排空：标记状态，等待存量请求完成
		ruc.waitForDrain(nodeAddr)
	} else {
		// 远程节点排空
		target := actor.NewPID(nodeAddr, "cluster/upgrade")
		future := ruc.cluster.System().Root.RequestFuture(target, &DrainRequest{
			NodeAddr: nodeAddr,
			Timeout:  ruc.config.DrainTimeout,
		}, ruc.config.DrainTimeout+5*time.Second)

		result, err := future.Wait()
		if err != nil {
			return fmt.Errorf("drain timeout: %w", err)
		}

		resp, ok := result.(*DrainResponse)
		if !ok {
			return fmt.Errorf("unexpected drain response: %T", result)
		}
		if !resp.Success {
			return fmt.Errorf("drain error: %s", resp.Error)
		}
	}

	ruc.mu.Lock()
	ruc.drainStatus[nodeAddr] = NodeDrained
	ruc.mu.Unlock()

	ruc.cluster.System().EventStream.Publish(&NodeDrainedEvent{
		NodeAddr: nodeAddr,
	})

	log.Info("Node drained: %s", nodeAddr)
	return nil
}

// waitForDrain 等待本节点排空完成
// 持续检查活跃 Actor 数量，直到所有用户 Actor 停止或超时
func (ruc *RollingUpgradeCoordinator) waitForDrain(nodeAddr string) {
	deadline := time.After(ruc.config.DrainTimeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	initialCount := ruc.ActiveActorCount()
	log.Info("Drain waiting: %s has %d active actors", nodeAddr, initialCount)

	for {
		select {
		case <-deadline:
			remaining := ruc.ActiveActorCount()
			log.Warn("Drain timeout for %s, %d actors still active, proceeding", nodeAddr, remaining)
			return
		case <-ticker.C:
			count := ruc.ActiveActorCount()
			if count <= 0 {
				log.Info("Drain complete for %s: all actors stopped", nodeAddr)
				return
			}
			log.Debug("Drain in progress for %s: %d actors remaining", nodeAddr, count)
		}
	}
}

// canaryValidation 金丝雀验证
func (ruc *RollingUpgradeCoordinator) canaryValidation() bool {
	deadline := time.After(ruc.config.CanaryDuration)
	ticker := time.NewTicker(ruc.config.HealthCheckInterval)
	defer ticker.Stop()

	failCount := 0

	for {
		select {
		case <-deadline:
			return true // 验证通过
		case <-ticker.C:
			if !ruc.isClusterHealthy() {
				failCount++
				if failCount >= ruc.config.HealthCheckThreshold {
					return false
				}
			} else {
				failCount = 0
			}
		}
	}
}

// isClusterHealthy 检查集群是否健康
func (ruc *RollingUpgradeCoordinator) isClusterHealthy() bool {
	alive := ruc.cluster.Members()
	return len(alive) >= ruc.config.MinHealthyNodes
}

// triggerRollback 触发回滚
func (ruc *RollingUpgradeCoordinator) triggerRollback(reason string) {
	ruc.mu.Lock()
	ruc.state = UpgradeRollingBack
	ruc.mu.Unlock()

	log.Warn("Rolling upgrade rollback triggered: %s", reason)

	ruc.cluster.System().EventStream.Publish(&UpgradeRollbackEvent{
		Reason: reason,
	})

	// 将所有排空中的节点恢复为正常
	ruc.mu.Lock()
	for addr := range ruc.drainStatus {
		ruc.drainStatus[addr] = NodeNormal
	}
	ruc.state = UpgradeIdle
	ruc.mu.Unlock()
}

// broadcastUpgradeStatus 通过 Gossip 广播升级状态
func (ruc *RollingUpgradeCoordinator) broadcastUpgradeStatus(targetVersion string, progress float64) {
	ruc.mu.RLock()
	nodes := make([]*NodeUpgradeStatus, 0, len(ruc.nodeStatus))
	for _, ns := range ruc.nodeStatus {
		nodes = append(nodes, ns)
	}
	ruc.mu.RUnlock()

	broadcast := &UpgradeStatusBroadcast{
		CoordinatorAddr: ruc.cluster.Self().Address,
		State:           ruc.state,
		TargetVersion:   targetVersion,
		Progress:        progress,
		Nodes:           nodes,
	}

	// 发送到所有存活成员的升级 Actor
	members := ruc.cluster.Members()
	for _, m := range members {
		if m.Address == ruc.cluster.Self().Address {
			continue
		}
		target := actor.NewPID(m.Address, "cluster/upgrade")
		ruc.cluster.System().Root.Send(target, broadcast)
	}
}

// --- 升级协调 Actor ---

type upgradeCoordActor struct {
	coordinator *RollingUpgradeCoordinator
}

func (a *upgradeCoordActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		// 初始化

	case *DrainRequest:
		a.handleDrainRequest(ctx, msg)

	case *VersionCheckRequest:
		a.handleVersionCheck(ctx, msg)

	case *UpgradeStatusBroadcast:
		a.handleStatusBroadcast(ctx, msg)
	}
}

func (a *upgradeCoordActor) handleDrainRequest(ctx actor.Context, msg *DrainRequest) {
	log.Info("Received drain request for %s", msg.NodeAddr)

	// 标记本节点为排空中
	a.coordinator.mu.Lock()
	a.coordinator.drainStatus[msg.NodeAddr] = NodeDraining
	a.coordinator.mu.Unlock()

	// 等待排空
	a.coordinator.waitForDrain(msg.NodeAddr)

	a.coordinator.mu.Lock()
	a.coordinator.drainStatus[msg.NodeAddr] = NodeDrained
	a.coordinator.mu.Unlock()

	ctx.Respond(&DrainResponse{
		NodeAddr: msg.NodeAddr,
		Success:  true,
	})
}

func (a *upgradeCoordActor) handleVersionCheck(ctx actor.Context, msg *VersionCheckRequest) {
	localVer := a.coordinator.config.VersionInfo
	compatible := true
	if localVer != nil && msg.Version != nil {
		compatible = localVer.IsCompatibleWith(msg.Version)
	}

	ctx.Respond(&VersionCheckResponse{
		Compatible: compatible,
		LocalVer:   localVer,
	})
}

func (a *upgradeCoordActor) handleStatusBroadcast(_ actor.Context, msg *UpgradeStatusBroadcast) {
	// 更新本地对升级进度的感知
	a.coordinator.mu.Lock()
	defer a.coordinator.mu.Unlock()

	for _, ns := range msg.Nodes {
		a.coordinator.nodeStatus[ns.Address] = ns
	}

	log.Debug("Upgrade status from %s: %s (%.0f%%)",
		msg.CoordinatorAddr, msg.State, msg.Progress*100)
}

// migrateActorsBeforeDrain 排空前迁移本节点上的可迁移 Actor 到其他健康节点
func (ruc *RollingUpgradeCoordinator) migrateActorsBeforeDrain(nodeAddr string) {
	mm := ruc.migrationManager

	// 选择一个健康的目标节点
	targetAddr := ruc.selectMigrationTarget(nodeAddr)
	if targetAddr == "" {
		log.Warn("No migration target available, skipping actor migration for %s", nodeAddr)
		return
	}

	// 获取所有本地 Actor，尝试迁移
	allProcs := ruc.cluster.System().ProcessRegistry.GetAll()
	migrated := 0
	for id := range allProcs {
		// 跳过系统 Actor
		if len(id) > 0 && id[0] == '$' {
			continue
		}
		// 跳过集群内部 Actor
		if len(id) > 8 && id[:8] == "cluster/" {
			continue
		}
		if len(id) > 10 && id[:10] == "singleton/" {
			continue
		}

		pid := actor.NewLocalPID(id)
		newPID, err := mm.Migrate(pid, targetAddr)
		if err != nil {
			log.Debug("Skipping migration for %s: %v", id, err)
			continue
		}
		migrated++
		log.Info("Pre-drain migration: %s -> %s (new: %s)", id, targetAddr, newPID.String())
	}

	if migrated > 0 {
		log.Info("Pre-drain migration completed: %d actors migrated from %s to %s",
			migrated, nodeAddr, targetAddr)
	}
}

// selectMigrationTarget 选择一个健康的迁移目标节点（排除正在排空的节点）
func (ruc *RollingUpgradeCoordinator) selectMigrationTarget(excludeAddr string) string {
	members := ruc.cluster.Members()

	ruc.mu.RLock()
	defer ruc.mu.RUnlock()

	for _, m := range members {
		if m.Address == excludeAddr {
			continue
		}
		if m.Status != MemberAlive {
			continue
		}
		// 排除正在排空的节点
		if status, ok := ruc.drainStatus[m.Address]; ok && status != NodeNormal {
			continue
		}
		return m.Address
	}
	return ""
}
