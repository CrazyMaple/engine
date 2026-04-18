package network

import "time"

// KCPConfig KCP 协议配置
//
// 参考王者荣耀/原神等实时游戏使用的可靠 UDP 方案的关键参数：
// NoDelay/Interval/Resend/NC 四元组决定了延迟与带宽的权衡。
type KCPConfig struct {
	// NoDelay 0 普通模式（默认 RTO 翻倍退避），1 急速模式（RTO * 1.5 退避）
	NoDelay int
	// Interval 内部更新间隔，急速模式建议 10ms，普通模式建议 40ms
	Interval time.Duration
	// Resend 快速重传触发次数：收到 N 次跨过 sn 的 ACK 即认为该包丢失立即重传
	// 0 表示关闭快速重传
	Resend int
	// NC 流控开关：0 启用，1 禁用（保留语义；当前实现固定窗口控流）
	NC int
	// SendWindow 发送窗口（待 ACK 的最大包数）
	SendWindow int
	// RecvWindow 接收窗口（乱序缓冲最大包数）
	RecvWindow int
	// Mtu 单包最大字节（含 KCP 包头），通常 1400 兼容主流网络
	Mtu int
	// RTOMin RTO 下限
	RTOMin time.Duration
	// RTOMax RTO 上限
	RTOMax time.Duration
	// DeadLink 单包重传次数超过此值视为死链，连接关闭
	DeadLink int
	// SessionTimeout 长时间无入站包视为会话超时
	SessionTimeout time.Duration
	// FEC 是否启用前向纠错（保留字段，当前实现暂未启用）
	FEC bool
}

// DefaultKCPConfig 普通模式默认配置（适合一般场景）
func DefaultKCPConfig() KCPConfig {
	return KCPConfig{
		NoDelay:        0,
		Interval:       40 * time.Millisecond,
		Resend:         0,
		NC:             0,
		SendWindow:     32,
		RecvWindow:     128,
		Mtu:            1400,
		RTOMin:         100 * time.Millisecond,
		RTOMax:         60 * time.Second,
		DeadLink:       20,
		SessionTimeout: 30 * time.Second,
	}
}

// FastKCPConfig 急速模式配置（适合 FPS/MOBA 等延迟敏感场景）
func FastKCPConfig() KCPConfig {
	return KCPConfig{
		NoDelay:        1,
		Interval:       10 * time.Millisecond,
		Resend:         2,
		NC:             1,
		SendWindow:     128,
		RecvWindow:     256,
		Mtu:            1400,
		RTOMin:         30 * time.Millisecond,
		RTOMax:         5 * time.Second,
		DeadLink:       10,
		SessionTimeout: 30 * time.Second,
	}
}
