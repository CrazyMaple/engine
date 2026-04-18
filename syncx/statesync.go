package syncx

// UpdateFunc 状态更新函数，由业务层提供
// 接收玩家输入列表，更新并返回当前完整状态
type UpdateFunc func(inputs []PlayerInput) map[string]interface{}

// StateSyncRoom 状态同步实现
// 服务端权威运算 → 增量状态下发
type StateSyncRoom struct {
	config    SyncConfig
	broadcast BroadcastFunc
	updateFn  UpdateFunc

	players     map[string]bool
	frameNum    uint64
	prevState   map[string]interface{} // 上一帧状态（用于计算 delta）
	currentState map[string]interface{} // 当前帧状态
	pendingInputs []PlayerInput         // 当前帧待处理的玩家输入

	// v1.10：可选启用紧凑增量编码
	deltaEncoder *DeltaEncoder
	deltaSchema  *DeltaSchema
}

// NewStateSyncRoom 创建状态同步房间
func NewStateSyncRoom(config SyncConfig, broadcast BroadcastFunc, updateFn UpdateFunc) *StateSyncRoom {
	r := &StateSyncRoom{
		config:       config,
		broadcast:    broadcast,
		updateFn:     updateFn,
		players:      make(map[string]bool),
		prevState:    make(map[string]interface{}),
		currentState: make(map[string]interface{}),
	}
	if config.EnableDeltaCompression {
		r.deltaSchema = NewDeltaSchema()
		r.deltaEncoder = NewDeltaEncoder(r.deltaSchema)
	}
	return r
}

// DeltaSchema 返回内部 DeltaSchema（仅启用 EnableDeltaCompression 时有意义）
// 客户端解码端需共享同一字段映射；可通过此对象同步注册字段。
func (ss *StateSyncRoom) DeltaSchema() *DeltaSchema { return ss.deltaSchema }

func (ss *StateSyncRoom) OnPlayerJoin(playerID string) {
	ss.players[playerID] = true
	// 新玩家发送完整状态快照
	snapshot := &StateSnapshot{
		FrameNum: ss.frameNum,
		State:    ss.copyState(ss.currentState),
	}
	ss.broadcast([]string{playerID}, snapshot)
}

func (ss *StateSyncRoom) OnPlayerLeave(playerID string) {
	delete(ss.players, playerID)
}

func (ss *StateSyncRoom) OnPlayerInput(playerID string, input *PlayerInput) {
	if !ss.players[playerID] {
		return
	}
	ss.pendingInputs = append(ss.pendingInputs, *input)
}

func (ss *StateSyncRoom) Tick(frameNum uint64) {
	ss.frameNum = frameNum

	// 保存上一帧状态
	ss.prevState = ss.copyState(ss.currentState)

	// 服务端权威运算
	ss.currentState = ss.updateFn(ss.pendingInputs)
	ss.pendingInputs = ss.pendingInputs[:0]

	// 定期发送完整快照（用于校正和新加入玩家）
	isSnapshotFrame := frameNum > 0 && ss.config.SnapshotInterval > 0 && frameNum%uint64(ss.config.SnapshotInterval) == 0
	if isSnapshotFrame {
		snapshot := &StateSnapshot{
			FrameNum: frameNum,
			State:    ss.copyState(ss.currentState),
		}
		ss.broadcast(nil, snapshot)
		// 重置 delta 编码器，下一帧从全量开始
		if ss.deltaEncoder != nil {
			ss.deltaEncoder.Reset()
			ss.deltaEncoder.Encode(frameNum, ss.asEntityState())
		}
		return
	}

	// 优先使用紧凑 Delta 编码
	if ss.deltaEncoder != nil {
		fd := ss.deltaEncoder.Encode(frameNum, ss.asEntityState())
		if len(fd.Entities) > 0 {
			ss.broadcast(nil, fd)
		}
		return
	}

	// 兼容路径：旧 StateDelta（map 级差分）
	delta := ss.computeDelta(ss.prevState, ss.currentState)
	if delta != nil {
		ss.broadcast(nil, delta)
	}
}

// asEntityState 把 currentState 包装为 DeltaEncoder 期待的实体级 map（单实体 _root）
func (ss *StateSyncRoom) asEntityState() map[string]map[string]interface{} {
	return map[string]map[string]interface{}{
		"_root": ss.currentState,
	}
}

func (ss *StateSyncRoom) FrameNum() uint64 {
	return ss.frameNum
}

// GetState 获取当前完整状态（外部查询用）
func (ss *StateSyncRoom) GetState() map[string]interface{} {
	return ss.copyState(ss.currentState)
}

// computeDelta 计算两个状态间的差异
func (ss *StateSyncRoom) computeDelta(prev, curr map[string]interface{}) *StateDelta {
	changed := make(map[string]interface{})
	var removed []string

	// 找出新增和变化的键
	for k, v := range curr {
		if pv, ok := prev[k]; !ok || pv != v {
			changed[k] = v
		}
	}

	// 找出被删除的键
	for k := range prev {
		if _, ok := curr[k]; !ok {
			removed = append(removed, k)
		}
	}

	if len(changed) == 0 && len(removed) == 0 {
		return nil
	}

	return &StateDelta{
		FrameNum: ss.frameNum,
		Changed:  changed,
		Removed:  removed,
	}
}

// copyState 深拷贝状态（浅层）
func (ss *StateSyncRoom) copyState(state map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{}, len(state))
	for k, v := range state {
		cp[k] = v
	}
	return cp
}
