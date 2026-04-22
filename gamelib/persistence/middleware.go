package persistence

import (
	"context"
	"time"

	"engine/actor"
	"engine/log"
)

// autoSaveMsg 自动保存内部消息
type autoSaveMsg struct{}

// PersistenceConfig 持久化中间件配置
type PersistenceConfig struct {
	Storage      Storage
	SaveInterval time.Duration // 自动保存间隔，默认 30s
	SaveOnStop   bool          // 停止时保存，默认 true
	StateFactory func() interface{} // 创建空状态用于 Load
}

type persistentActor struct {
	inner  actor.Actor
	config PersistenceConfig
	self   *actor.PID
	dirty  bool
	ticker *time.Ticker
	stopCh chan struct{}
}

// NewPersistenceMiddleware 创建持久化中间件
// 在 Started 时加载状态，定期自动保存，Stopping 时最终保存
func NewPersistenceMiddleware(cfg PersistenceConfig) actor.ReceiverMiddleware {
	if cfg.SaveInterval <= 0 {
		cfg.SaveInterval = 30 * time.Second
	}

	return func(next actor.Actor) actor.Actor {
		return &persistentActor{
			inner:  next,
			config: cfg,
		}
	}
}

func (pa *persistentActor) Receive(ctx actor.Context) {
	switch ctx.Message().(type) {
	case *actor.Started:
		pa.self = ctx.Self()
		pa.handleStarted(ctx)
		pa.inner.Receive(ctx)
		pa.startAutoSave(ctx)

	case *actor.Stopping:
		pa.stopAutoSave()
		if pa.config.SaveOnStop && pa.dirty {
			pa.save(ctx)
		}
		pa.inner.Receive(ctx)

	case *autoSaveMsg:
		if pa.dirty {
			pa.save(ctx)
		}

	default:
		pa.inner.Receive(ctx)
		pa.dirty = true
	}
}

func (pa *persistentActor) handleStarted(ctx actor.Context) {
	p, ok := pa.inner.(Persistable)
	if !ok {
		return
	}

	if pa.config.StateFactory == nil {
		return
	}

	target := pa.config.StateFactory()
	err := pa.config.Storage.Load(context.Background(), p.PersistenceID(), target)
	if err != nil {
		log.Debug("[persistence] load state for %s: %v (may be new actor)", p.PersistenceID(), err)
		return
	}

	if err := p.SetState(target); err != nil {
		log.Error("[persistence] set state for %s: %v", p.PersistenceID(), err)
	}
}

func (pa *persistentActor) save(ctx actor.Context) {
	p, ok := pa.inner.(Persistable)
	if !ok {
		return
	}

	state := p.GetState()
	if err := pa.config.Storage.Save(context.Background(), p.PersistenceID(), state); err != nil {
		log.Error("[persistence] save %s: %v", p.PersistenceID(), err)
		return
	}
	pa.dirty = false
}

func (pa *persistentActor) startAutoSave(ctx actor.Context) {
	pa.ticker = time.NewTicker(pa.config.SaveInterval)
	pa.stopCh = make(chan struct{})

	go func() {
		for {
			select {
			case <-pa.stopCh:
				return
			case <-pa.ticker.C:
				if pa.self != nil {
					actor.DefaultSystem().Root.Send(pa.self, &autoSaveMsg{})
				}
			}
		}
	}()
}

func (pa *persistentActor) stopAutoSave() {
	if pa.ticker != nil {
		pa.ticker.Stop()
	}
	if pa.stopCh != nil {
		close(pa.stopCh)
	}
}
