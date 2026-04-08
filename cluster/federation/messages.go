package federation

import "engine/actor"

// FederatedMessage 跨集群消息信封
type FederatedMessage struct {
	SourceCluster string     `json:"source_cluster"` // 源集群 ID
	TargetCluster string     `json:"target_cluster"` // 目标集群 ID
	TargetActor   string     `json:"target_actor"`   // 目标 Actor 路径
	Sender        *actor.PID `json:"sender"`         // 发送者 PID
	Payload       interface{} `json:"payload"`        // 消息体
	TypeName      string     `json:"type_name"`      // 消息类型名（用于反序列化）
}

// FederatedPing 跨集群健康检查 Ping
type FederatedPing struct {
	ClusterID string `json:"cluster_id"`
	Timestamp int64  `json:"timestamp"`
}

// FederatedPong 跨集群健康检查 Pong
type FederatedPong struct {
	ClusterID string `json:"cluster_id"`
	Timestamp int64  `json:"timestamp"`
}

// FederatedRegister 集群注册消息（对端自我介绍）
type FederatedRegister struct {
	ClusterID      string   `json:"cluster_id"`
	GatewayAddress string   `json:"gateway_address"`
	Kinds          []string `json:"kinds"` // 该集群支持的 Actor 类型
}
