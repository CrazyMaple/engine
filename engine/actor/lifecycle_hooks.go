package actor

import "time"

// LifecycleHook 生命周期钩子函数类型
type LifecycleHook func(ctx Context)

// LifecycleHooks Actor 生命周期钩子集合
// 所有钩子均可选，nil 表示不注册
type LifecycleHooks struct {
	// PreStart 在 Actor 启动前执行（Started 消息处理前）
	// 可用于依赖检查、资源预分配
	PreStart LifecycleHook

	// PostStop 在 Actor 停止后执行（Stopped 消息处理后）
	// 可用于资源清理确认、通知外部系统
	PostStop LifecycleHook

	// PreRestart 在 Actor 重启前执行（旧实例销毁前）
	// 可用于保存临时状态
	PreRestart LifecycleHook

	// PostRestart 在 Actor 重启后执行（新实例启动后）
	// 可用于状态迁移、资源恢复
	PostRestart LifecycleHook

	// HookTimeout 钩子执行超时时间（防止钩子阻塞 Actor 生命周期）
	// 默认 5 秒，0 表示不限制
	HookTimeout time.Duration
}

// DefaultHookTimeout 默认钩子超时时间
const DefaultHookTimeout = 5 * time.Second

// WithPreStart 设置 PreStart 生命周期钩子
func (props *Props) WithPreStart(fn LifecycleHook) *Props {
	props.ensureHooks()
	props.lifecycleHooks.PreStart = fn
	return props
}

// WithPostStop 设置 PostStop 生命周期钩子
func (props *Props) WithPostStop(fn LifecycleHook) *Props {
	props.ensureHooks()
	props.lifecycleHooks.PostStop = fn
	return props
}

// WithPreRestart 设置 PreRestart 生命周期钩子
func (props *Props) WithPreRestart(fn LifecycleHook) *Props {
	props.ensureHooks()
	props.lifecycleHooks.PreRestart = fn
	return props
}

// WithPostRestart 设置 PostRestart 生命周期钩子
func (props *Props) WithPostRestart(fn LifecycleHook) *Props {
	props.ensureHooks()
	props.lifecycleHooks.PostRestart = fn
	return props
}

// WithHookTimeout 设置生命周期钩子超时时间
func (props *Props) WithHookTimeout(timeout time.Duration) *Props {
	props.ensureHooks()
	props.lifecycleHooks.HookTimeout = timeout
	return props
}

// WithLifecycleHooks 一次性设置所有生命周期钩子
func (props *Props) WithLifecycleHooks(hooks LifecycleHooks) *Props {
	props.lifecycleHooks = &hooks
	return props
}

// ensureHooks 确保 hooks 已初始化
func (props *Props) ensureHooks() {
	if props.lifecycleHooks == nil {
		props.lifecycleHooks = &LifecycleHooks{}
	}
}

// executeHook 执行生命周期钩子（带超时保护）
func executeHook(ctx Context, hook LifecycleHook, timeout time.Duration) {
	if hook == nil {
		return
	}

	if timeout <= 0 {
		timeout = DefaultHookTimeout
	}

	done := make(chan struct{})
	go func() {
		defer func() {
			recover() // 钩子 panic 不应影响 Actor 生命周期
			close(done)
		}()
		hook(ctx)
	}()

	select {
	case <-done:
		// 钩子正常完成
	case <-time.After(timeout):
		// 钩子超时，继续执行生命周期（不阻塞）
	}
}
