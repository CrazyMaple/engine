package actor

import (
	"os"
	"os/signal"
	"syscall"

	"engine/log"
)

// OnShutdown 注册系统信号处理（SIGTERM/SIGINT），收到信号时执行回调并优雅停机
// 回调函数在停机前调用，可用于关闭 Gate、Remote、Cluster 等外部组件
// 此函数会阻塞直到收到信号
func (system *ActorSystem) OnShutdown(beforeShutdown ...func()) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.Info("Received signal: %v, starting graceful shutdown...", sig)

	// 按注册顺序执行前置回调
	for _, fn := range beforeShutdown {
		fn()
	}

	if err := system.Shutdown(); err != nil {
		log.Error("Shutdown error: %v", err)
	} else {
		log.Info("Graceful shutdown completed")
	}
}

// WaitForShutdown 是 OnShutdown 的便捷方法，直接在默认系统上等待
func WaitForShutdown(beforeShutdown ...func()) {
	defaultSystem.OnShutdown(beforeShutdown...)
}
