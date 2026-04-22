package grain

import (
	"engine/actor"
	"engine/log"
)

// placementActor 负责在本节点上激活/去激活 Grain
type placementActor struct {
	kinds  *KindRegistry
	grains map[string]*actor.PID // identity string -> PID
}

func newPlacementActor(kinds *KindRegistry) actor.Actor {
	return &placementActor{
		kinds:  kinds,
		grains: make(map[string]*actor.PID),
	}
}

func (a *placementActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		// 初始化完成

	case *ActivateRequest:
		a.handleActivate(ctx, msg)

	case *DeactivateRequest:
		a.handleDeactivate(ctx, msg)

	case *actor.Terminated:
		// 被 Watch 的 Grain 停止了，清理记录
		a.handleTerminated(ctx, msg)
	}
}

func (a *placementActor) handleActivate(ctx actor.Context, msg *ActivateRequest) {
	key := msg.Identity.String()

	// 已经激活，直接返回
	if pid, ok := a.grains[key]; ok {
		ctx.Respond(&ActivateResponse{PID: pid})
		return
	}

	// 查找 Kind
	kind, ok := a.kinds.Get(msg.Identity.Kind)
	if !ok {
		log.Error("PlacementActor: unknown kind %s", msg.Identity.Kind)
		ctx.Respond(&ActivateResponse{Error: "unknown kind: " + msg.Identity.Kind})
		return
	}

	// 创建 Grain Actor（带 ReceiveTimeout 实现自动去激活）
	props := kind.Props.WithReceiveTimeout(kind.TTL)
	pid := ctx.SpawnNamed(props, "grain/"+key)

	a.grains[key] = pid
	ctx.Watch(pid)

	// 发送初始化消息
	ctx.Send(pid, &GrainInit{Identity: msg.Identity})

	log.Debug("PlacementActor: activated %s -> %s", key, pid.String())
	ctx.Respond(&ActivateResponse{PID: pid})
}

func (a *placementActor) handleDeactivate(ctx actor.Context, msg *DeactivateRequest) {
	key := msg.Identity.String()

	if pid, ok := a.grains[key]; ok {
		ctx.StopActor(pid)
		delete(a.grains, key)
		log.Debug("PlacementActor: deactivated %s", key)
	}
}

func (a *placementActor) handleTerminated(ctx actor.Context, msg *actor.Terminated) {
	// 找到对应的 grain 并清理
	for key, pid := range a.grains {
		if pid.Equal(msg.Who) {
			delete(a.grains, key)
			log.Debug("PlacementActor: grain stopped %s", key)
			break
		}
	}
}
