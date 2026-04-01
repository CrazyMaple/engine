package scene

import (
	"engine/actor"
	"engine/log"
)

// SceneConfig 场景配置
type SceneConfig struct {
	SceneID    string
	GridConfig GridConfig
}

// SceneActor 场景 Actor，管理空间实体和 AOI
type SceneActor struct {
	config SceneConfig
	grid   *Grid
}

// NewSceneActor 创建场景 Actor 生产函数
func NewSceneActor(config SceneConfig) actor.Producer {
	return func() actor.Actor {
		return &SceneActor{config: config}
	}
}

func (s *SceneActor) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		s.grid = NewGrid(s.config.GridConfig)
		log.Info("[scene] %s started", s.config.SceneID)

	case *EnterScene:
		s.handleEnter(ctx, msg)

	case *LeaveScene:
		s.handleLeave(ctx, msg)

	case *MoveInScene:
		s.handleMove(ctx, msg)

	case *BroadcastToScene:
		s.handleBroadcastScene(ctx, msg)

	case *BroadcastToAOI:
		s.handleBroadcastAOI(ctx, msg)

	case *GetSceneInfo:
		s.handleGetInfo(ctx)

	case *actor.Stopping:
		log.Info("[scene] %s stopping, entities=%d", s.config.SceneID, s.grid.EntityCount())
	}
}

func (s *SceneActor) handleEnter(ctx actor.Context, msg *EnterScene) {
	entity := &GridEntity{
		ID:   msg.EntityID,
		X:    msg.X,
		Y:    msg.Y,
		PID:  msg.PID,
		Data: msg.Data,
	}
	s.grid.Add(entity)

	// 通知 AOI 内已有实体：有新实体进入
	neighbors := s.grid.GetAOI(msg.EntityID)
	for _, n := range neighbors {
		if n.PID != nil {
			ctx.Send(n.PID, &EntityEntered{
				EntityID: msg.EntityID,
				X:        msg.X,
				Y:        msg.Y,
				Data:     msg.Data,
			})
		}
	}

	// 回复进入者当前场景信息
	if ctx.Sender() != nil {
		snapshots := make([]EntitySnapshot, 0, len(neighbors))
		for _, n := range neighbors {
			snapshots = append(snapshots, EntitySnapshot{
				ID: n.ID, X: n.X, Y: n.Y, Data: n.Data,
			})
		}
		ctx.Respond(&SceneInfo{
			SceneID:  s.config.SceneID,
			Entities: snapshots,
		})
	}
}

func (s *SceneActor) handleLeave(ctx actor.Context, msg *LeaveScene) {
	entity := s.grid.Get(msg.EntityID)
	if entity == nil {
		return
	}

	// 先获取 AOI 再移除
	neighbors := s.grid.GetAOI(msg.EntityID)
	s.grid.Remove(msg.EntityID)

	// 通知 AOI 内实体：该实体离开
	for _, n := range neighbors {
		if n.PID != nil {
			ctx.Send(n.PID, &EntityLeft{EntityID: msg.EntityID})
		}
	}
}

func (s *SceneActor) handleMove(ctx actor.Context, msg *MoveInScene) {
	entity := s.grid.Get(msg.EntityID)
	if entity == nil {
		return
	}

	entered, left := s.grid.Move(msg.EntityID, msg.X, msg.Y)

	// 通知新进入视野的实体：移动者的信息
	for _, e := range entered {
		if e.PID != nil {
			ctx.Send(e.PID, &EntityEntered{
				EntityID: msg.EntityID, X: msg.X, Y: msg.Y, Data: entity.Data,
			})
		}
		// 通知移动者：新看到的实体
		if entity.PID != nil {
			ctx.Send(entity.PID, &EntityEntered{
				EntityID: e.ID, X: e.X, Y: e.Y, Data: e.Data,
			})
		}
	}

	// 通知离开视野的实体
	for _, e := range left {
		if e.PID != nil {
			ctx.Send(e.PID, &EntityLeft{EntityID: msg.EntityID})
		}
		if entity.PID != nil {
			ctx.Send(entity.PID, &EntityLeft{EntityID: e.ID})
		}
	}

	// 通知仍在 AOI 内的实体：位置变化
	aoi := s.grid.GetAOI(msg.EntityID)
	for _, e := range aoi {
		if e.PID != nil {
			ctx.Send(e.PID, &EntityMoved{
				EntityID: msg.EntityID, X: msg.X, Y: msg.Y,
			})
		}
	}
}

func (s *SceneActor) handleBroadcastScene(ctx actor.Context, msg *BroadcastToScene) {
	for _, e := range s.grid.lookup {
		if e.ID != msg.ExcludeID && e.PID != nil {
			ctx.Send(e.PID, msg.Message)
		}
	}
}

func (s *SceneActor) handleBroadcastAOI(ctx actor.Context, msg *BroadcastToAOI) {
	aoi := s.grid.GetAOI(msg.EntityID)
	for _, e := range aoi {
		if e.PID != nil {
			ctx.Send(e.PID, msg.Message)
		}
	}

	if msg.IncludeSelf {
		entity := s.grid.Get(msg.EntityID)
		if entity != nil && entity.PID != nil {
			ctx.Send(entity.PID, msg.Message)
		}
	}
}

func (s *SceneActor) handleGetInfo(ctx actor.Context) {
	entities := make([]EntitySnapshot, 0, s.grid.EntityCount())
	for _, e := range s.grid.lookup {
		entities = append(entities, EntitySnapshot{
			ID: e.ID, X: e.X, Y: e.Y, Data: e.Data,
		})
	}
	ctx.Respond(&SceneInfo{
		SceneID:  s.config.SceneID,
		Entities: entities,
	})
}
