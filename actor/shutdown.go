package actor

import (
	"sync"
	"time"
)

const (
	// DefaultShutdownTimeout 默认停机超时时间
	DefaultShutdownTimeout = 30 * time.Second
)

// ShutdownConfig 停机配置
type ShutdownConfig struct {
	// Timeout 停机超时时间，超时后强制退出
	Timeout time.Duration
}

// Shutdown 优雅停机：停止接受新消息，等待现有消息处理完毕，自底向上关闭所有 Actor
func (system *ActorSystem) Shutdown() error {
	return system.ShutdownWithConfig(ShutdownConfig{Timeout: DefaultShutdownTimeout})
}

// ShutdownWithConfig 使用指定配置进行优雅停机
func (system *ActorSystem) ShutdownWithConfig(cfg ShutdownConfig) error {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultShutdownTimeout
	}

	done := make(chan struct{})
	go func() {
		system.shutdownActors()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(cfg.Timeout):
		// 超时强制退出：清空注册表
		system.ProcessRegistry.mu.Lock()
		system.ProcessRegistry.localActors = make(map[string]Process)
		system.ProcessRegistry.mu.Unlock()
		return &shutdownTimeoutError{Timeout: cfg.Timeout}
	}
}

// shutdownActors 自底向上关闭所有 Actor
func (system *ActorSystem) shutdownActors() {
	// 收集所有顶层 Actor（无父节点的 Actor，排除 deadletter）
	allProcs := system.ProcessRegistry.GetAll()

	// 找出所有子节点
	childSet := make(map[string]bool)
	for _, proc := range allProcs {
		if cell, ok := proc.(interface{ Children() []*PID }); ok {
			for _, child := range cell.Children() {
				childSet[child.Id] = true
			}
		}
	}

	// 顶层 Actor = 非子节点 且 非 deadletter
	var topLevel []*PID
	for id := range allProcs {
		if id == "deadletter" {
			continue
		}
		if !childSet[id] {
			topLevel = append(topLevel, &PID{Id: id})
		}
	}

	// 并行停止所有顶层 Actor（子 Actor 会被父 Actor 级联停止）
	var wg sync.WaitGroup
	for _, pid := range topLevel {
		proc, ok := system.ProcessRegistry.Get(pid)
		if !ok {
			continue
		}
		wg.Add(1)
		go func(p Process, pid *PID) {
			defer wg.Done()
			p.SendSystemMessage(pid, &Stopping{})
		}(proc, pid)
	}
	wg.Wait()

	// 等待所有进程被注销（轮询，最多等 5 秒）
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		// 只剩 deadletter 就算完成
		if system.ProcessRegistry.Count() <= 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// shutdownTimeoutError 停机超时错误
type shutdownTimeoutError struct {
	Timeout time.Duration
}

func (e *shutdownTimeoutError) Error() string {
	return "actor system shutdown timed out after " + e.Timeout.String()
}
