package persistence

import (
	"sync"

	"engine/actor"
)

// EventSourcedConfig 事件溯源中间件配置
type EventSourcedConfig struct {
	// Journal 事件日志存储
	Journal EventJournal
	// SnapshotStore 快照存储（可选，为 nil 则不使用快照）
	SnapshotStore SnapshotStore
	// SnapshotEvery 每 N 条事件自动创建快照（默认 100）
	SnapshotEvery int
}

// eventSourcedActor 包装层 Actor，实现 EventSourced 功能
type eventSourcedActor struct {
	inner   actor.Actor
	config  EventSourcedConfig
	esCtx   *EventSourcedContext
	escInit bool
}

// NewEventSourcedMiddleware 创建事件溯源中间件
// 在 Started 时自动恢复状态，业务 Actor 应实现 EventSourced 接口
// 业务 Actor 通过 GetEventSourcedContext(ctx) 获取 EventSourcedContext 调用 Persist
func NewEventSourcedMiddleware(cfg EventSourcedConfig) actor.ReceiverMiddleware {
	if cfg.SnapshotEvery <= 0 {
		cfg.SnapshotEvery = 100
	}

	return func(next actor.Actor) actor.Actor {
		return &eventSourcedActor{
			inner:  next,
			config: cfg,
		}
	}
}

func (esa *eventSourcedActor) Receive(ctx actor.Context) {
	switch ctx.Message().(type) {
	case *actor.Started:
		// 初始化 EventSourcedContext 并恢复状态
		esa.initIfNeeded()
		if esa.esCtx != nil {
			_ = esa.esCtx.Recover()
			registerESCtx(ctx.Self().Id, esa.esCtx)
		}
		esa.inner.Receive(ctx)

	case *actor.Stopping:
		if esa.esCtx != nil {
			// 停止前保存一次快照
			_ = esa.esCtx.SaveSnapshot()
			unregisterESCtx(ctx.Self().Id)
		}
		esa.inner.Receive(ctx)

	default:
		esa.inner.Receive(ctx)
	}
}

func (esa *eventSourcedActor) initIfNeeded() {
	if esa.escInit {
		return
	}
	esa.escInit = true

	es, ok := esa.inner.(EventSourced)
	if !ok {
		return
	}

	esa.esCtx = NewEventSourcedContext(
		es,
		esa.config.Journal,
		esa.config.SnapshotStore,
		esa.config.SnapshotEvery,
	)
}

// --- EventSourcedContext 全局注册表 ---
// 由于 actor.Context 无法直接携带自定义字段，通过 PID ID 全局映射
// 业务 Actor 在 Receive 中可通过 GetEventSourcedContext(ctx) 获取

var (
	escRegistry   = make(map[string]*EventSourcedContext)
	escRegistryMu sync.RWMutex
)

func registerESCtx(pidID string, esc *EventSourcedContext) {
	escRegistryMu.Lock()
	defer escRegistryMu.Unlock()
	escRegistry[pidID] = esc
}

func unregisterESCtx(pidID string) {
	escRegistryMu.Lock()
	defer escRegistryMu.Unlock()
	delete(escRegistry, pidID)
}

// GetEventSourcedContext 根据 actor.Context 获取对应的 EventSourcedContext
// 业务 Actor 在 Receive 中调用，用于 Persist 事件
func GetEventSourcedContext(ctx actor.Context) *EventSourcedContext {
	escRegistryMu.RLock()
	defer escRegistryMu.RUnlock()
	return escRegistry[ctx.Self().Id]
}
