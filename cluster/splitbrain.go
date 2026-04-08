package cluster

import (
	"sync"
	"time"

	"engine/log"
)

// PartitionState 分区状态
type PartitionState int

const (
	// PartitionNormal 正常（无脑裂）
	PartitionNormal PartitionState = iota
	// PartitionSuspected 疑似脑裂（等待 StableWindow 确认）
	PartitionSuspected
	// PartitionDetected 已确认脑裂
	PartitionDetected
	// PartitionResolving 正在解决脑裂
	PartitionResolving
)

func (s PartitionState) String() string {
	switch s {
	case PartitionNormal:
		return "Normal"
	case PartitionSuspected:
		return "Suspected"
	case PartitionDetected:
		return "Detected"
	case PartitionResolving:
		return "Resolving"
	default:
		return "Unknown"
	}
}

// SplitBrainDetector 脑裂检测器
// 定期检查 Quorum，检测网络分区并调用 Resolver 决定处理策略
type SplitBrainDetector struct {
	cluster  *Cluster
	config   *SplitBrainConfig
	state    PartitionState
	suspectStart time.Time // 疑似脑裂开始时间
	mu       sync.RWMutex
	stopChan chan struct{}
	stopped  chan struct{}
}

// NewSplitBrainDetector 创建脑裂检测器
func NewSplitBrainDetector(cluster *Cluster, config *SplitBrainConfig) *SplitBrainDetector {
	if config.Resolver == nil {
		config.Resolver = &KeepMajorityResolver{}
	}
	return &SplitBrainDetector{
		cluster:  cluster,
		config:   config,
		state:    PartitionNormal,
		stopChan: make(chan struct{}),
		stopped:  make(chan struct{}),
	}
}

// Start 启动脑裂检测
func (d *SplitBrainDetector) Start() {
	go d.detectLoop()
	log.Info("Split-brain detector started (interval=%v, stableWindow=%v)",
		d.config.CheckInterval, d.config.StableWindow)
}

// Stop 停止脑裂检测
func (d *SplitBrainDetector) Stop() {
	close(d.stopChan)
	<-d.stopped
	log.Info("Split-brain detector stopped")
}

// State 获取当前分区状态
func (d *SplitBrainDetector) State() PartitionState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.state
}

func (d *SplitBrainDetector) detectLoop() {
	defer close(d.stopped)

	ticker := time.NewTicker(d.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.checkQuorum()
		case <-d.stopChan:
			return
		}
	}
}

// checkQuorum 核心检测逻辑
func (d *SplitBrainDetector) checkQuorum() {
	reachable := d.getReachableMembers()
	allKnown := d.getAllKnownMembers()

	totalKnown := len(allKnown)
	reachableCount := len(reachable)

	// 单节点或尚无其他成员，不做检测
	if totalKnown <= 1 {
		d.transitionTo(PartitionNormal)
		return
	}

	// Quorum 计算：需要超过半数
	quorum := totalKnown/2 + 1
	hasQuorum := reachableCount >= quorum

	d.mu.Lock()
	currentState := d.state

	if hasQuorum {
		// 有 Quorum，恢复正常
		if currentState != PartitionNormal {
			d.state = PartitionNormal
			d.mu.Unlock()

			log.Info("Split-brain resolved: quorum restored (%d/%d)", reachableCount, totalKnown)
			d.cluster.system.EventStream.Publish(&SplitBrainResolvedEvent{
				Decision:   DecisionKeepRunning,
				ResolvedAt: time.Now(),
			})
			return
		}
		d.mu.Unlock()
		return
	}

	// 无 Quorum
	switch currentState {
	case PartitionNormal:
		// 首次检测到：进入疑似状态
		d.state = PartitionSuspected
		d.suspectStart = time.Now()
		d.mu.Unlock()
		log.Warn("Split-brain suspected: only %d/%d members reachable (quorum=%d)",
			reachableCount, totalKnown, quorum)

	case PartitionSuspected:
		// 检查稳定窗口是否过期
		if time.Since(d.suspectStart) >= d.config.StableWindow {
			d.state = PartitionDetected
			d.mu.Unlock()

			log.Error("Split-brain confirmed after stable window (%v): %d/%d members reachable",
				d.config.StableWindow, reachableCount, totalKnown)

			// 计算不可达成员
			unreachable := d.getUnreachableMembers(reachable, allKnown)

			d.cluster.system.EventStream.Publish(&SplitBrainDetectedEvent{
				ReachableMembers:   reachable,
				UnreachableMembers: unreachable,
				DetectedAt:         time.Now(),
			})

			// 调用 Resolver
			d.resolve(reachable, allKnown)
		} else {
			d.mu.Unlock()
		}

	case PartitionDetected, PartitionResolving:
		d.mu.Unlock()
		// 已在处理中

	default:
		d.mu.Unlock()
	}
}

// resolve 调用策略解决脑裂
func (d *SplitBrainDetector) resolve(reachable, allKnown []*Member) {
	d.mu.Lock()
	d.state = PartitionResolving
	d.mu.Unlock()

	ctx := ResolverContext{
		Self:      d.cluster.self,
		Reachable: reachable,
		AllKnown:  allKnown,
	}

	decision := d.config.Resolver.Resolve(ctx)

	log.Info("Split-brain resolver decision: %s", decision)

	d.cluster.system.EventStream.Publish(&SplitBrainResolvedEvent{
		Decision:   decision,
		ResolvedAt: time.Now(),
	})

	if decision == DecisionShutdown {
		log.Warn("Split-brain resolver decided to shutdown this partition")
		// 异步执行关闭，避免死锁（checkQuorum 在 detectLoop 中运行）
		go d.cluster.Stop()
	} else {
		d.mu.Lock()
		d.state = PartitionNormal
		d.mu.Unlock()
	}
}

// getReachableMembers 获取当前可达成员（Alive 状态）
func (d *SplitBrainDetector) getReachableMembers() []*Member {
	return d.cluster.memberList.GetMembers()
}

// getAllKnownMembers 获取所有已知成员（包括 Suspect/Dead）
func (d *SplitBrainDetector) getAllKnownMembers() []*Member {
	return d.cluster.memberList.GetAllMembers()
}

// getUnreachableMembers 计算不可达成员
func (d *SplitBrainDetector) getUnreachableMembers(reachable, allKnown []*Member) []*Member {
	reachableSet := make(map[string]bool, len(reachable))
	for _, m := range reachable {
		reachableSet[m.Id] = true
	}

	var unreachable []*Member
	for _, m := range allKnown {
		if !reachableSet[m.Id] {
			unreachable = append(unreachable, m)
		}
	}
	return unreachable
}

// transitionTo 安全的状态转换
func (d *SplitBrainDetector) transitionTo(newState PartitionState) {
	d.mu.Lock()
	d.state = newState
	d.mu.Unlock()
}
