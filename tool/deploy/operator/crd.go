package operator

import (
	"time"
)

// EngineCluster CRD Go 类型定义（对应 deploy/crd/enginecluster_crd.yaml）
// apiVersion: engine.io/v1alpha1, kind: EngineCluster

// ClusterPhase CRD 状态阶段
type ClusterPhase string

const (
	PhasePending   ClusterPhase = "Pending"
	PhaseRunning   ClusterPhase = "Running"
	PhaseUpgrading ClusterPhase = "Upgrading"
	PhaseScaling   ClusterPhase = "Scaling"
	PhaseFailed    ClusterPhase = "Failed"
)

// EngineClusterSpec CRD spec 定义
type EngineClusterSpec struct {
	// Replicas 期望副本数
	Replicas int `json:"replicas"`
	// Version 引擎镜像版本标签
	Version string `json:"version"`
	// ClusterName Gossip 集群名
	ClusterName string `json:"clusterName"`
	// Image 容器镜像仓库
	Image string `json:"image"`
	// Kinds 支持的 Actor Kind 列表
	Kinds []string `json:"kinds,omitempty"`
	// UpgradeStrategy 升级策略
	UpgradeStrategy UpgradeStrategy `json:"upgradeStrategy,omitempty"`
	// ScalePolicy 自动扩缩容策略
	ScalePolicy *ScalePolicy `json:"scalePolicy,omitempty"`
}

// UpgradeStrategy 升级策略配置
type UpgradeStrategy struct {
	// Type 升级类型：RollingUpdate（默认）或 Recreate
	Type string `json:"type,omitempty"`
	// MaxUnavailable 滚动升级时最大不可用数
	MaxUnavailable int `json:"maxUnavailable,omitempty"`
	// DrainTimeout 节点排空超时
	DrainTimeout time.Duration `json:"drainTimeout,omitempty"`
	// CanaryNodes 金丝雀验证节点数
	CanaryNodes int `json:"canaryNodes,omitempty"`
	// CanaryDuration 金丝雀验证时长
	CanaryDuration time.Duration `json:"canaryDuration,omitempty"`
	// MigrateActors 缩容/升级前是否迁移 Actor
	MigrateActors bool `json:"migrateActors,omitempty"`
}

// ScalePolicy 自动扩缩容策略
type ScalePolicy struct {
	// MinReplicas 最小副本数
	MinReplicas int `json:"minReplicas"`
	// MaxReplicas 最大副本数
	MaxReplicas int `json:"maxReplicas"`
	// ConnectionThreshold 每节点连接数阈值，超过则扩容
	ConnectionThreshold int64 `json:"connectionThreshold,omitempty"`
	// ActorThreshold 每节点 Actor 数阈值
	ActorThreshold int `json:"actorThreshold,omitempty"`
	// CPUThreshold CPU 使用率阈值（百分比，如 80）
	CPUThreshold int `json:"cpuThreshold,omitempty"`
	// CooldownPeriod 扩缩容冷却期
	CooldownPeriod time.Duration `json:"cooldownPeriod,omitempty"`
}

// EngineClusterStatus CRD status 定义
type EngineClusterStatus struct {
	// Phase 当前阶段
	Phase ClusterPhase `json:"phase"`
	// ReadyReplicas 就绪副本数
	ReadyReplicas int `json:"readyReplicas"`
	// CurrentVersion 当前运行版本
	CurrentVersion string `json:"currentVersion,omitempty"`
	// TargetVersion 目标版本（升级时有值）
	TargetVersion string `json:"targetVersion,omitempty"`
	// Conditions 状态条件列表
	Conditions []ClusterCondition `json:"conditions,omitempty"`
	// LastScaleTime 上次扩缩容时间
	LastScaleTime *time.Time `json:"lastScaleTime,omitempty"`
}

// ClusterCondition 集群状态条件
type ClusterCondition struct {
	Type    string    `json:"type"`
	Status  string    `json:"status"` // "True", "False", "Unknown"
	Reason  string    `json:"reason,omitempty"`
	Message string    `json:"message,omitempty"`
	Updated time.Time `json:"updated"`
}

// EngineCluster 完整 CRD 对象
type EngineCluster struct {
	Name      string              `json:"name"`
	Namespace string              `json:"namespace"`
	Spec      EngineClusterSpec   `json:"spec"`
	Status    EngineClusterStatus `json:"status"`
}

// DefaultUpgradeStrategy 默认升级策略
func DefaultUpgradeStrategy() UpgradeStrategy {
	return UpgradeStrategy{
		Type:           "RollingUpdate",
		MaxUnavailable: 1,
		DrainTimeout:   60 * time.Second,
		CanaryNodes:    1,
		CanaryDuration: 120 * time.Second,
		MigrateActors:  true,
	}
}

// DefaultScalePolicy 默认扩缩容策略
func DefaultScalePolicy() *ScalePolicy {
	return &ScalePolicy{
		MinReplicas:         2,
		MaxReplicas:         20,
		ConnectionThreshold: 1000,
		ActorThreshold:      5000,
		CPUThreshold:        80,
		CooldownPeriod:      5 * time.Minute,
	}
}
