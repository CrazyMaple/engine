package cluster

import (
	"fmt"
	"sync"
	"time"

	"engine/log"
)

// DCConfig 数据中心配置
type DCConfig struct {
	// DCName 本节点所属数据中心名称，如 "us-east-1", "cn-north-1"
	DCName string

	// LocalRoutePriority 本地 DC 路由优先，true 时优先选择同 DC 节点
	LocalRoutePriority bool

	// CrossDCHeartbeatMultiplier 跨 DC 心跳间隔倍数（减少跨 IDC 流量）
	// 跨 DC 心跳间隔 = HeartbeatInterval * Multiplier
	CrossDCHeartbeatMultiplier int

	// CrossDCHeartbeatTimeout 跨 DC 心跳超时倍数
	CrossDCHeartbeatTimeoutMultiplier int

	// FailoverEnabled 是否启用 DC 故障转移
	FailoverEnabled bool

	// FailoverThreshold DC 中存活节点低于此数量触发故障转移
	FailoverThreshold int

	// FailoverCooldown 故障转移冷却时间（防止频繁切换）
	FailoverCooldown time.Duration
}

// DefaultDCConfig 默认数据中心配置
func DefaultDCConfig(dcName string) *DCConfig {
	return &DCConfig{
		DCName:                            dcName,
		LocalRoutePriority:                true,
		CrossDCHeartbeatMultiplier:        3,
		CrossDCHeartbeatTimeoutMultiplier: 3,
		FailoverEnabled:                   true,
		FailoverThreshold:                 1,
		FailoverCooldown:                  30 * time.Second,
	}
}

// RouteMode 路由模式：区分读写操作的路由策略
type RouteMode int

const (
	// RouteDefault 默认路由（本地 DC 优先）
	RouteDefault RouteMode = iota
	// RouteRead 读路由（优先本地 DC 副本，就近读取降低延迟）
	RouteRead
	// RouteWrite 写路由（始终路由到主节点，确保一致性）
	RouteWrite
)

// --- DC 感知成员扩展 ---

// DCMember 携带 DC 标签的成员信息
type DCMember struct {
	*Member
	DC string // 所属数据中心
}

// --- DC 事件 ---

// DCFailoverEvent DC 故障转移事件
type DCFailoverEvent struct {
	FailedDC  string   // 故障的 DC
	BackupDC  string   // 接管的 DC
	Timestamp time.Time
}

// DCRecoveredEvent DC 恢复事件
type DCRecoveredEvent struct {
	DC        string
	Timestamp time.Time
}

// --- 多数据中心管理器 ---

// MultiDCManager 多数据中心管理器
type MultiDCManager struct {
	cluster       *Cluster
	config        *DCConfig
	dcMembers     map[string][]*Member // dcName -> members
	dcStatus      map[string]bool      // dcName -> healthy
	failoverState map[string]string    // failedDC -> backupDC
	lastFailover  map[string]time.Time // dcName -> last failover time
	mu            sync.RWMutex
}

// NewMultiDCManager 创建多数据中心管理器
func NewMultiDCManager(c *Cluster, config *DCConfig) *MultiDCManager {
	if config == nil {
		config = DefaultDCConfig("default")
	}
	return &MultiDCManager{
		cluster:       c,
		config:        config,
		dcMembers:     make(map[string][]*Member),
		dcStatus:      make(map[string]bool),
		failoverState: make(map[string]string),
		lastFailover:  make(map[string]time.Time),
	}
}

// Start 启动多 DC 管理器
func (m *MultiDCManager) Start() {
	// 监听拓扑变更事件以更新 DC 成员分组
	m.cluster.System().EventStream.Subscribe(func(event interface{}) {
		switch event.(type) {
		case *ClusterTopologyEvent:
			m.onTopologyChange()
		case *MemberJoinedEvent:
			m.onTopologyChange()
		case *MemberDeadEvent:
			m.onTopologyChange()
		case *MemberLeftEvent:
			m.onTopologyChange()
		}
	})

	// 初始化本 DC 状态
	m.mu.Lock()
	m.dcStatus[m.config.DCName] = true
	m.mu.Unlock()

	log.Info("MultiDCManager started: dc=%s, localPriority=%v, failover=%v",
		m.config.DCName, m.config.LocalRoutePriority, m.config.FailoverEnabled)
}

// Stop 停止多 DC 管理器
func (m *MultiDCManager) Stop() {
	log.Info("MultiDCManager stopped: dc=%s", m.config.DCName)
}

// LocalDC 获取本节点的 DC 名称
func (m *MultiDCManager) LocalDC() string {
	return m.config.DCName
}

// GetMemberDC 获取指定成员的 DC 名称
func (m *MultiDCManager) GetMemberDC(member *Member) string {
	// 从 Member 的 Kinds 中提取 DC 标签
	// 约定：Kinds 中包含 "dc:<name>" 格式的标签
	for _, k := range member.Kinds {
		if len(k) > 3 && k[:3] == "dc:" {
			return k[3:]
		}
	}
	return "default"
}

// MembersInDC 获取指定 DC 的存活成员
func (m *MultiDCManager) MembersInDC(dc string) []*Member {
	m.mu.RLock()
	defer m.mu.RUnlock()
	members, ok := m.dcMembers[dc]
	if !ok {
		return nil
	}
	result := make([]*Member, len(members))
	copy(result, members)
	return result
}

// LocalMembers 获取本地 DC 的存活成员
func (m *MultiDCManager) LocalMembers() []*Member {
	return m.MembersInDC(m.config.DCName)
}

// AllDCs 获取所有已知 DC 列表
func (m *MultiDCManager) AllDCs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dcs := make([]string, 0, len(m.dcMembers))
	for dc := range m.dcMembers {
		dcs = append(dcs, dc)
	}
	return dcs
}

// IsDCHealthy 检查指定 DC 是否健康
func (m *MultiDCManager) IsDCHealthy(dc string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	healthy, ok := m.dcStatus[dc]
	return ok && healthy
}

// GetRouteMember DC 感知路由：优先路由到同 DC 节点
func (m *MultiDCManager) GetRouteMember(identity, kind string) *Member {
	if !m.config.LocalRoutePriority {
		// 不启用本地优先，使用默认一致性哈希
		return m.cluster.GetMemberForIdentity(identity, kind)
	}

	// 优先在本地 DC 查找
	localMembers := m.LocalMembers()
	candidate := m.findBestMember(identity, kind, localMembers)
	if candidate != nil {
		return candidate
	}

	// 本地 DC 无可用节点，检查故障转移
	if m.config.FailoverEnabled {
		m.mu.RLock()
		backupDC, hasFailover := m.failoverState[m.config.DCName]
		m.mu.RUnlock()

		if hasFailover {
			backupMembers := m.MembersInDC(backupDC)
			candidate = m.findBestMember(identity, kind, backupMembers)
			if candidate != nil {
				return candidate
			}
		}
	}

	// 回退到全局路由
	return m.cluster.GetMemberForIdentity(identity, kind)
}

// GetRouteMemberForRead 读路由：优先从本地 DC 副本读取，降低延迟
// 本地 DC 有节点则就近读取，否则依次检查故障转移 DC 和全局路由
func (m *MultiDCManager) GetRouteMemberForRead(identity, kind string) *Member {
	// 读操作始终优先本地 DC，忽略 LocalRoutePriority 配置
	localMembers := m.LocalMembers()
	candidate := m.findBestMember(identity, kind, localMembers)
	if candidate != nil {
		return candidate
	}

	// 本地 DC 无可用节点，检查故障转移 DC
	if m.config.FailoverEnabled {
		m.mu.RLock()
		backupDC, hasFailover := m.failoverState[m.config.DCName]
		m.mu.RUnlock()

		if hasFailover {
			backupMembers := m.MembersInDC(backupDC)
			candidate = m.findBestMember(identity, kind, backupMembers)
			if candidate != nil {
				return candidate
			}
		}
	}

	// 回退到全局路由
	return m.cluster.GetMemberForIdentity(identity, kind)
}

// GetRouteMemberForWrite 写路由：始终路由到主节点（一致性哈希确定的权威节点）
// 写操作需要发送到主节点以保证数据一致性，不做本地 DC 优先
func (m *MultiDCManager) GetRouteMemberForWrite(identity, kind string) *Member {
	// 写操作直接使用一致性哈希选主，不做 DC 偏好
	return m.cluster.GetMemberForIdentity(identity, kind)
}

// GetRouteMemberByMode 根据路由模式选择目标节点
func (m *MultiDCManager) GetRouteMemberByMode(identity, kind string, mode RouteMode) *Member {
	switch mode {
	case RouteRead:
		return m.GetRouteMemberForRead(identity, kind)
	case RouteWrite:
		return m.GetRouteMemberForWrite(identity, kind)
	default:
		return m.GetRouteMember(identity, kind)
	}
}

// findBestMember 在给定成员列表中使用 Rendezvous Hash 选择最佳节点
func (m *MultiDCManager) findBestMember(identity, kind string, members []*Member) *Member {
	var bestMember *Member
	var bestHash uint32

	for _, member := range members {
		if !member.HasKind(kind) || member.Status != MemberAlive {
			continue
		}
		h := hashCombine(identity, member.Address)
		if bestMember == nil || h > bestHash {
			bestHash = h
			bestMember = member
		}
	}

	return bestMember
}

// ShouldUseCrossDCTiming 判断与目标成员通信是否应使用跨 DC 时序
func (m *MultiDCManager) ShouldUseCrossDCTiming(member *Member) bool {
	return m.GetMemberDC(member) != m.config.DCName
}

// CrossDCHeartbeatInterval 获取跨 DC 心跳间隔
func (m *MultiDCManager) CrossDCHeartbeatInterval() time.Duration {
	base := m.cluster.Config().HeartbeatInterval
	return base * time.Duration(m.config.CrossDCHeartbeatMultiplier)
}

// CrossDCHeartbeatTimeout 获取跨 DC 心跳超时
func (m *MultiDCManager) CrossDCHeartbeatTimeout() time.Duration {
	base := m.cluster.Config().HeartbeatTimeout
	return base * time.Duration(m.config.CrossDCHeartbeatTimeoutMultiplier)
}

// onTopologyChange 拓扑变更时更新 DC 成员分组和检测 DC 健康
func (m *MultiDCManager) onTopologyChange() {
	allMembers := m.cluster.Members()

	// 按 DC 分组
	grouped := make(map[string][]*Member)
	for _, member := range allMembers {
		dc := m.GetMemberDC(member)
		grouped[dc] = append(grouped[dc], member)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 检测 DC 状态变化
	for dc, members := range grouped {
		wasHealthy := m.dcStatus[dc]
		isHealthy := len(members) >= m.config.FailoverThreshold
		m.dcStatus[dc] = isHealthy
		m.dcMembers[dc] = members

		if wasHealthy && !isHealthy && m.config.FailoverEnabled {
			m.handleDCFailure(dc, grouped)
		} else if !wasHealthy && isHealthy {
			m.handleDCRecovery(dc)
		}
	}

	// 检测已经不存在的 DC（所有成员都离开了）
	for dc := range m.dcStatus {
		if _, exists := grouped[dc]; !exists && dc != m.config.DCName {
			if m.dcStatus[dc] {
				m.dcStatus[dc] = false
				m.dcMembers[dc] = nil
				if m.config.FailoverEnabled {
					m.handleDCFailure(dc, grouped)
				}
			}
		}
	}
}

// handleDCFailure 处理 DC 故障（调用方需持有锁）
func (m *MultiDCManager) handleDCFailure(failedDC string, grouped map[string][]*Member) {
	// 冷却时间检查
	if last, ok := m.lastFailover[failedDC]; ok {
		if time.Since(last) < m.config.FailoverCooldown {
			return
		}
	}

	// 选择最佳备用 DC（成员数最多的健康 DC）
	var bestDC string
	bestCount := 0
	for dc, members := range grouped {
		if dc == failedDC {
			continue
		}
		if len(members) > bestCount {
			bestCount = len(members)
			bestDC = dc
		}
	}

	if bestDC == "" {
		log.Warn("DC failover: no backup DC available for %s", failedDC)
		return
	}

	m.failoverState[failedDC] = bestDC
	m.lastFailover[failedDC] = time.Now()

	log.Warn("DC failover: %s -> %s (backup)", failedDC, bestDC)

	m.cluster.System().EventStream.Publish(&DCFailoverEvent{
		FailedDC:  failedDC,
		BackupDC:  bestDC,
		Timestamp: time.Now(),
	})
}

// handleDCRecovery 处理 DC 恢复（调用方需持有锁）
func (m *MultiDCManager) handleDCRecovery(dc string) {
	// 如果有故障转移到其他 DC，清除转发
	delete(m.failoverState, dc)

	log.Info("DC recovered: %s", dc)

	m.cluster.System().EventStream.Publish(&DCRecoveredEvent{
		DC:        dc,
		Timestamp: time.Now(),
	})
}

// GetDCLabel 生成 DC 标签，添加到 ClusterConfig.Kinds 中
// 约定使用 "dc:<name>" 格式
func GetDCLabel(dcName string) string {
	return fmt.Sprintf("dc:%s", dcName)
}

// ParseDCLabel 从标签中解析 DC 名称
func ParseDCLabel(label string) (string, bool) {
	if len(label) > 3 && label[:3] == "dc:" {
		return label[3:], true
	}
	return "", false
}

// WithDC 为集群配置添加 DC 标签（便捷方法）
func (c *ClusterConfig) WithDC(dcName string) *ClusterConfig {
	dcLabel := GetDCLabel(dcName)
	// 避免重复添加
	for _, k := range c.Kinds {
		if k == dcLabel {
			return c
		}
	}
	c.Kinds = append(c.Kinds, dcLabel)
	return c
}
