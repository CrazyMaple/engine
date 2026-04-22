package gate

import (
	"errors"

	"engine/network"
)

// KCP 接入层说明
//
// KCPConn 已经实现 network.Conn 接口，上层消息循环与 TCPConn 完全一致（长度分帧由 KCP 协议内部保证）。
// 本文件只承担两件事：
//   1. 暴露 NewKCPClient，方便示例和测试拨号到 gate 的 KCP 端口；
//   2. 提供 KCPAgent 显式类型断言辅助，便于业务层查询底层 KCP 连接做 NoDelay 参数微调。

// NewKCPClient 以默认急速参数拨号建立 KCP 客户端连接
//
// addr 形如 "127.0.0.1:9100"；
// 返回的 Conn 可直接配合 gate.ClientSession 或业务方自定义的读写循环使用。
func NewKCPClient(addr string) (network.Conn, error) {
	if addr == "" {
		return nil, errors.New("gate: kcp client addr required")
	}
	return network.DialKCP(addr, network.FastKCPConfig())
}

// NewKCPClientWithConfig 允许调用方指定完整的 KCP 参数（含 NoDelay / Interval / 窗口大小等）
func NewKCPClientWithConfig(addr string, cfg network.KCPConfig) (network.Conn, error) {
	if addr == "" {
		return nil, errors.New("gate: kcp client addr required")
	}
	if cfg.Interval <= 0 {
		cfg = network.FastKCPConfig()
	}
	return network.DialKCP(addr, cfg)
}

// IsKCP 判断 Agent 是否运行在 KCP 传输之上
func IsKCP(a *Agent) bool {
	return a != nil && a.Transport() == "kcp"
}

// KCPConn 若 Agent 底层为 KCPConn 则返回该连接，便于调用方做 NoDelay 等高级配置
// 非 KCP 连接返回 nil
func KCPConn(a *Agent) *network.KCPConn {
	if a == nil {
		return nil
	}
	if kc, ok := a.conn.(*network.KCPConn); ok {
		return kc
	}
	return nil
}
