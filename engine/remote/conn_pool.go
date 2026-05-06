package remote

import "time"

// ConnPoolConfig 连接池动态扩缩容配置。
//
// v1.13 处置说明：v1.12 提供了一个内部 `connPool` 实现并未在发送路径接入；
// Phase 2 扫描确认整个仓库无 newConnPool 调用方，按 v2.1 §2.5.2 / ADR 002 的
// "接入 / 删除 / 暂留但修正配置传播" 三选一约束，选择**删除** connPool 死代码，
// 仅保留此处的 ConnPoolConfig 数据类型，作为未来 Phase 3 / 4 重新接入多连接复用
// 的配置入口；同时让 health_check_test 中既有的配置基线保持稳定。
//
// 因此本文件不再持有 MessageSigner / MessageCipher 字段——签名 / 加密配置在
// EndpointManager 中完成传播（见 endpoint.go），无需在此处重复维护。
type ConnPoolConfig struct {
	// MinConns 最小连接数（默认 1）
	MinConns int
	// MaxConns 最大连接数（默认 8）
	MaxConns int
	// ScaleUpThreshold 待发送消息数超过此阈值时扩容（默认 100）
	ScaleUpThreshold int
	// ScaleDownTimeout 连接空闲超过此时间后缩容（默认 30s）
	ScaleDownTimeout time.Duration
	// ScaleCheckInterval 扩缩容检查间隔（默认 5s）
	ScaleCheckInterval time.Duration
}

// DefaultConnPoolConfig 返回默认连接池配置。
func DefaultConnPoolConfig() ConnPoolConfig {
	return ConnPoolConfig{
		MinConns:           1,
		MaxConns:           8,
		ScaleUpThreshold:   100,
		ScaleDownTimeout:   30 * time.Second,
		ScaleCheckInterval: 5 * time.Second,
	}
}

// IsEnabled 检查连接池是否启用（MaxConns > 1 视为启用）。
func (c *ConnPoolConfig) IsEnabled() bool {
	return c.MaxConns > 1
}
