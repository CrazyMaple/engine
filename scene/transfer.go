package scene

import (
	"engine/actor"
	"engine/log"
)

// transferState 实体转移状态
type transferState struct {
	entityID      string
	targetSceneID string
	targetX       float32
	targetY       float32
	stash         []interface{} // 转移期间暂存的消息
}

// adjacentInfo 相邻场景信息
type adjacentInfo struct {
	sceneID   string
	scenePID  *actor.PID
	direction AdjacentDirection
	overlap   float32
}

// handleTransfer 处理跨场景转移请求
func (s *SceneActor) handleTransfer(ctx actor.Context, msg *TransferEntity) {
	entity := s.grid.Get(msg.EntityID)
	if entity == nil {
		if ctx.Sender() != nil {
			ctx.Respond(&TransferResult{
				Success:       false,
				SourceSceneID: s.config.SceneID,
				TargetSceneID: msg.TargetSceneID,
				EntityID:      msg.EntityID,
				Reason:        "entity not found in source scene",
			})
		}
		return
	}

	// 标记实体为转移中，后续消息将被暂存
	s.transferring[msg.EntityID] = &transferState{
		entityID:      msg.EntityID,
		targetSceneID: msg.TargetSceneID,
		targetX:       msg.TargetX,
		targetY:       msg.TargetY,
	}

	// 在源场景中执行离场逻辑（通知 AOI 内实体）
	neighbors := s.grid.GetAOI(msg.EntityID)
	s.grid.Remove(msg.EntityID)
	for _, n := range neighbors {
		if n.PID != nil {
			ctx.Send(n.PID, &EntityLeft{EntityID: msg.EntityID})
		}
	}

	// 查找目标场景并发送 TransferIn
	if s.manager != nil {
		targetPID, ok := s.manager.GetScene(msg.TargetSceneID)
		if !ok {
			// 目标场景不存在，恢复实体
			s.rollbackTransfer(ctx, entity, msg.EntityID)
			return
		}
		ctx.Request(targetPID, &TransferIn{
			EntityID: msg.EntityID,
			PID:      entity.PID,
			X:        msg.TargetX,
			Y:        msg.TargetY,
			Data:     entity.Data,
		})
	} else if ctx.Sender() != nil {
		// 无 manager 时回复失败
		s.rollbackTransfer(ctx, entity, msg.EntityID)
	}

	log.Info("[scene] %s: entity %s transferring to %s", s.config.SceneID, msg.EntityID, msg.TargetSceneID)
}

// handleTransferIn 处理实体转入
func (s *SceneActor) handleTransferIn(ctx actor.Context, msg *TransferIn) {
	entity := &GridEntity{
		ID:   msg.EntityID,
		X:    msg.X,
		Y:    msg.Y,
		PID:  msg.PID,
		Data: msg.Data,
	}
	s.grid.Add(entity)

	// 通知 AOI 内实体
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

	// 通知实体本身转移成功
	if msg.PID != nil {
		snapshots := make([]EntitySnapshot, 0, len(neighbors))
		for _, n := range neighbors {
			snapshots = append(snapshots, EntitySnapshot{
				ID: n.ID, X: n.X, Y: n.Y, Data: n.Data,
			})
		}
		ctx.Send(msg.PID, &TransferResult{
			Success:       true,
			TargetSceneID: s.config.SceneID,
			EntityID:      msg.EntityID,
		})
		ctx.Send(msg.PID, &SceneInfo{
			SceneID:  s.config.SceneID,
			Entities: snapshots,
		})
	}

	// 回复源场景（Respond 给 Request 的 sender）
	if ctx.Sender() != nil {
		ctx.Respond(&TransferResult{
			Success:       true,
			TargetSceneID: s.config.SceneID,
			EntityID:      msg.EntityID,
		})
	}

	log.Info("[scene] %s: entity %s transferred in at (%.1f, %.1f)", s.config.SceneID, msg.EntityID, msg.X, msg.Y)
}

// handleTransferResult 处理目标场景的转移结果回复
func (s *SceneActor) handleTransferResult(ctx actor.Context, msg *TransferResult) {
	ts, ok := s.transferring[msg.EntityID]
	if !ok {
		return
	}
	delete(s.transferring, msg.EntityID)

	if msg.Success {
		// 转移成功，冲刷暂存消息到实体 PID
		log.Info("[scene] %s: entity %s transfer completed, flushing %d stashed messages",
			s.config.SceneID, msg.EntityID, len(ts.stash))
	} else {
		// 转移失败，需要恢复实体到源场景
		log.Warn("[scene] %s: entity %s transfer failed: %s", s.config.SceneID, msg.EntityID, msg.Reason)
	}
}

// stashOrProcess 检查消息是否需要暂存（实体正在转移中）
// 返回 true 表示消息已被暂存，调用者无需继续处理
func (s *SceneActor) stashOrProcess(entityID string, message interface{}) bool {
	ts, ok := s.transferring[entityID]
	if !ok {
		return false
	}
	ts.stash = append(ts.stash, message)
	return true
}

// rollbackTransfer 回滚失败的转移
func (s *SceneActor) rollbackTransfer(ctx actor.Context, entity *GridEntity, entityID string) {
	ts, ok := s.transferring[entityID]
	if !ok {
		return
	}
	delete(s.transferring, entityID)

	// 恢复实体到场景
	s.grid.Add(entity)
	neighbors := s.grid.GetAOI(entityID)
	for _, n := range neighbors {
		if n.PID != nil {
			ctx.Send(n.PID, &EntityEntered{
				EntityID: entity.ID, X: entity.X, Y: entity.Y, Data: entity.Data,
			})
		}
	}

	if entity.PID != nil {
		ctx.Send(entity.PID, &TransferResult{
			Success:       false,
			SourceSceneID: s.config.SceneID,
			TargetSceneID: ts.targetSceneID,
			EntityID:      entityID,
			Reason:        "target scene not found",
		})
	}
}

// --- 跨场景 AOI 边界处理 ---

// handleRegisterAdjacent 注册相邻场景
func (s *SceneActor) handleRegisterAdjacent(ctx actor.Context, msg *RegisterAdjacentScene) {
	s.adjacents[msg.SceneID] = &adjacentInfo{
		sceneID:   msg.SceneID,
		scenePID:  msg.ScenePID,
		direction: msg.Direction,
		overlap:   msg.Overlap,
	}
	log.Info("[scene] %s: registered adjacent scene %s (dir=%d, overlap=%.1f)",
		s.config.SceneID, msg.SceneID, msg.Direction, msg.Overlap)
}

// handleUnregisterAdjacent 注销相邻场景
func (s *SceneActor) handleUnregisterAdjacent(msg *UnregisterAdjacentScene) {
	delete(s.adjacents, msg.SceneID)
}

// handleBorderEntityUpdate 处理来自相邻场景的边界实体更新
func (s *SceneActor) handleBorderEntityUpdate(ctx actor.Context, msg *BorderEntityUpdate) {
	ghostID := "ghost:" + msg.SourceSceneID + ":" + msg.EntityID

	if msg.Entered {
		// 映射远程实体坐标到本地坐标
		localX, localY := s.mapBorderCoords(msg.SourceSceneID, msg.X, msg.Y)
		existing := s.grid.Get(ghostID)
		if existing != nil {
			// 更新位置
			s.grid.Move(ghostID, localX, localY)
		} else {
			// 添加幽灵实体（无 PID，不可交互，仅可见）
			ghost := &GridEntity{
				ID:   ghostID,
				X:    localX,
				Y:    localY,
				Data: msg.Data,
			}
			s.grid.Add(ghost)
		}
	} else {
		// 离开边界区域，移除幽灵实体
		s.grid.Remove(ghostID)
	}
}

// checkBorderZone 检查实体是否在边界区域，如果是则通知相邻场景
func (s *SceneActor) checkBorderZone(ctx actor.Context, entityID string, x, y float32) {
	if len(s.adjacents) == 0 {
		return
	}

	entity := s.grid.Get(entityID)
	if entity == nil {
		return
	}

	w := s.config.GridConfig.Width
	h := s.config.GridConfig.Height

	for _, adj := range s.adjacents {
		inBorder := s.isInBorderZone(adj.direction, adj.overlap, x, y, w, h)
		wasInBorder := s.borderEntities[borderKey{entityID, adj.sceneID}]

		if inBorder && !wasInBorder {
			// 进入边界区域
			ctx.Send(adj.scenePID, &BorderEntityUpdate{
				SourceSceneID: s.config.SceneID,
				EntityID:      entityID,
				X:             x,
				Y:             y,
				Data:          entity.Data,
				Entered:       true,
			})
			s.borderEntities[borderKey{entityID, adj.sceneID}] = true
		} else if inBorder && wasInBorder {
			// 仍在边界区域，更新位置
			ctx.Send(adj.scenePID, &BorderEntityUpdate{
				SourceSceneID: s.config.SceneID,
				EntityID:      entityID,
				X:             x,
				Y:             y,
				Data:          entity.Data,
				Entered:       true,
			})
		} else if !inBorder && wasInBorder {
			// 离开边界区域
			ctx.Send(adj.scenePID, &BorderEntityUpdate{
				SourceSceneID: s.config.SceneID,
				EntityID:      entityID,
				Entered:       false,
			})
			delete(s.borderEntities, borderKey{entityID, adj.sceneID})
		}
	}
}

// borderKey 边界实体追踪键
type borderKey struct {
	entityID string
	sceneID  string
}

// isInBorderZone 判断坐标是否在指定方向的边界区域内
func (s *SceneActor) isInBorderZone(dir AdjacentDirection, overlap, x, y, w, h float32) bool {
	switch dir {
	case AdjacentNorth:
		return y < overlap
	case AdjacentSouth:
		return y > h-overlap
	case AdjacentEast:
		return x > w-overlap
	case AdjacentWest:
		return x < overlap
	case AdjacentNorthEast:
		return y < overlap && x > w-overlap
	case AdjacentNorthWest:
		return y < overlap && x < overlap
	case AdjacentSouthEast:
		return y > h-overlap && x > w-overlap
	case AdjacentSouthWest:
		return y > h-overlap && x < overlap
	}
	return false
}

// mapBorderCoords 将相邻场景的坐标映射到本场景坐标
func (s *SceneActor) mapBorderCoords(sourceSceneID string, x, y float32) (float32, float32) {
	adj, ok := s.adjacents[sourceSceneID]
	if !ok {
		return x, y
	}

	w := s.config.GridConfig.Width
	h := s.config.GridConfig.Height

	switch adj.direction {
	case AdjacentNorth:
		return x, h + y // 北方场景的 y 映射到本场景的底部之下
	case AdjacentSouth:
		return x, y - h
	case AdjacentEast:
		return x - w, y
	case AdjacentWest:
		return w + x, y
	case AdjacentNorthEast:
		return x - w, h + y
	case AdjacentNorthWest:
		return w + x, h + y
	case AdjacentSouthEast:
		return x - w, y - h
	case AdjacentSouthWest:
		return w + x, y - h
	}
	return x, y
}
