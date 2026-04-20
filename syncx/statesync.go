package syncx

import (
	"sync/atomic"
)

// UpdateFunc 状态更新函数，由业务层提供
// 接收玩家输入列表，更新并返回当前完整状态
type UpdateFunc func(inputs []PlayerInput) map[string]interface{}

// DeltaMetrics Delta 模式下的带宽/压缩效果指标
//
// 所有计数器都是累加的；调用方可在不同时间点读取并计算周期差值
// 用于 Prometheus 导出：delta_compression_ratio、delta_frames_total、snapshot_frames_total
type DeltaMetrics struct {
	// SnapshotFrames 已发送的完整快照帧数（含周期性快照 + 重同步响应）
	SnapshotFrames atomic.Uint64
	// DeltaFrames 已发送的增量帧数
	DeltaFrames atomic.Uint64
	// EmptyFrames Tick 未产生变化（delta 为空）的帧数
	EmptyFrames atomic.Uint64
	// FullFieldBytes 以"若按全量编码"估算的字段字节数累计
	FullFieldBytes atomic.Uint64
	// DeltaFieldBytes 实际增量编码的字段字节数累计
	DeltaFieldBytes atomic.Uint64
	// ResyncRequests 已处理的 ResyncRequest 数
	ResyncRequests atomic.Uint64
}

// CompressionRatio 返回 delta 相对全量的瞬时压缩率：DeltaFieldBytes / FullFieldBytes
// 无数据时返回 0；取值 [0, 1]，越小表示压缩效果越好
func (m *DeltaMetrics) CompressionRatio() float64 {
	full := m.FullFieldBytes.Load()
	if full == 0 {
		return 0
	}
	return float64(m.DeltaFieldBytes.Load()) / float64(full)
}

// StateSyncRoom 状态同步实现
// 服务端权威运算 → 增量状态下发
type StateSyncRoom struct {
	config    SyncConfig
	broadcast BroadcastFunc
	updateFn  UpdateFunc

	players       map[string]bool
	frameNum      uint64
	prevState     map[string]interface{} // 上一帧状态（用于计算 delta）
	currentState  map[string]interface{} // 当前帧状态
	pendingInputs []PlayerInput          // 当前帧待处理的玩家输入

	// v1.10：可选启用紧凑增量编码
	deltaEncoder *DeltaEncoder
	deltaSchema  *DeltaSchema

	// v1.11：挂起的重同步请求（Tick 前响应）
	pendingResync map[string]string

	// v1.11：指标
	metrics DeltaMetrics
}

// NewStateSyncRoom 创建状态同步房间
func NewStateSyncRoom(config SyncConfig, broadcast BroadcastFunc, updateFn UpdateFunc) *StateSyncRoom {
	r := &StateSyncRoom{
		config:        config,
		broadcast:     broadcast,
		updateFn:      updateFn,
		players:       make(map[string]bool),
		prevState:     make(map[string]interface{}),
		currentState:  make(map[string]interface{}),
		pendingResync: make(map[string]string),
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

// DeltaEnabled 是否启用增量编码
func (ss *StateSyncRoom) DeltaEnabled() bool { return ss.deltaEncoder != nil }

// Metrics 返回指标快照指针（线程安全，读取各计数器即可）
func (ss *StateSyncRoom) Metrics() *DeltaMetrics { return &ss.metrics }

func (ss *StateSyncRoom) OnPlayerJoin(playerID string) {
	ss.players[playerID] = true
	// 新玩家发送完整状态快照
	if ss.deltaEncoder != nil {
		// Delta 模式：通过编码器给出全量快照，保证客户端 Decoder 位图与服务端一致
		snap := ss.deltaEncoder.Snapshot(ss.frameNum)
		ss.metrics.SnapshotFrames.Add(1)
		ss.metrics.FullFieldBytes.Add(estimateFieldBytes(snap))
		ss.metrics.DeltaFieldBytes.Add(estimateFieldBytes(snap))
		ss.broadcast([]string{playerID}, snap)
		return
	}
	snapshot := &StateSnapshot{
		FrameNum: ss.frameNum,
		State:    ss.copyState(ss.currentState),
	}
	ss.broadcast([]string{playerID}, snapshot)
}

func (ss *StateSyncRoom) OnPlayerLeave(playerID string) {
	delete(ss.players, playerID)
	delete(ss.pendingResync, playerID)
}

func (ss *StateSyncRoom) OnPlayerInput(playerID string, input *PlayerInput) {
	if !ss.players[playerID] {
		return
	}
	ss.pendingInputs = append(ss.pendingInputs, *input)
}

// OnResyncRequest 处理客户端的全量重同步请求
//
// 收到后挂起，在下一次 Tick 开始时统一响应；若同一玩家多次请求，保留最后一次原因。
// Delta 未启用时退化为直接给玩家一次 StateSnapshot。
func (ss *StateSyncRoom) OnResyncRequest(req *ResyncRequest) {
	if req == nil || req.PlayerID == "" {
		return
	}
	if !ss.players[req.PlayerID] {
		return
	}
	ss.metrics.ResyncRequests.Add(1)
	if ss.deltaEncoder == nil {
		// 非 Delta 模式直接发送完整快照
		snapshot := &StateSnapshot{
			FrameNum: ss.frameNum,
			State:    ss.copyState(ss.currentState),
		}
		ss.broadcast([]string{req.PlayerID}, snapshot)
		return
	}
	ss.pendingResync[req.PlayerID] = req.Reason
}

func (ss *StateSyncRoom) Tick(frameNum uint64) {
	ss.frameNum = frameNum

	// 先响应挂起的重同步请求（给需要重建的玩家单播当前状态的全量快照）
	ss.flushPendingResync()

	// 保存上一帧状态
	ss.prevState = ss.copyState(ss.currentState)

	// 服务端权威运算
	ss.currentState = ss.updateFn(ss.pendingInputs)
	ss.pendingInputs = ss.pendingInputs[:0]

	// 定期发送完整快照（用于校正和新加入玩家）
	isSnapshotFrame := frameNum > 0 && ss.config.SnapshotInterval > 0 && frameNum%uint64(ss.config.SnapshotInterval) == 0
	if isSnapshotFrame {
		if ss.deltaEncoder != nil {
			ss.deltaEncoder.Reset()
			fd := ss.deltaEncoder.Encode(frameNum, ss.asEntityState())
			fd.Snapshot = true
			full := estimateFieldBytes(fd)
			ss.metrics.SnapshotFrames.Add(1)
			ss.metrics.FullFieldBytes.Add(full)
			ss.metrics.DeltaFieldBytes.Add(full)
			ss.broadcast(nil, fd)
			return
		}
		snapshot := &StateSnapshot{
			FrameNum: frameNum,
			State:    ss.copyState(ss.currentState),
		}
		ss.broadcast(nil, snapshot)
		return
	}

	// 优先使用紧凑 Delta 编码
	if ss.deltaEncoder != nil {
		fd := ss.deltaEncoder.Encode(frameNum, ss.asEntityState())
		full := estimateFullBytes(ss.currentState)
		ss.metrics.FullFieldBytes.Add(full)
		if len(fd.Entities) == 0 {
			ss.metrics.EmptyFrames.Add(1)
			return
		}
		ss.metrics.DeltaFrames.Add(1)
		ss.metrics.DeltaFieldBytes.Add(estimateFieldBytes(fd))
		ss.broadcast(nil, fd)
		return
	}

	// 兼容路径：旧 StateDelta（map 级差分）
	delta := ss.computeDelta(ss.prevState, ss.currentState)
	if delta != nil {
		ss.broadcast(nil, delta)
	}
}

// flushPendingResync 把 pendingResync 中的玩家统一响应一次全量快照
//
// 采用"最新编码器快照"而非立即重新 Encode：这样可避免 Reset 影响正常帧的广播增量。
func (ss *StateSyncRoom) flushPendingResync() {
	if len(ss.pendingResync) == 0 || ss.deltaEncoder == nil {
		return
	}
	snap := ss.deltaEncoder.Snapshot(ss.frameNum)
	full := estimateFieldBytes(snap)
	for playerID := range ss.pendingResync {
		ss.metrics.SnapshotFrames.Add(1)
		ss.metrics.FullFieldBytes.Add(full)
		ss.metrics.DeltaFieldBytes.Add(full)
		ss.broadcast([]string{playerID}, snap)
	}
	// 清空
	for k := range ss.pendingResync {
		delete(ss.pendingResync, k)
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

// estimateFullBytes 估算 currentState 以全量编码的字节规模
//
// 估算规则与 MarshalDelta 的类型分支保持一致：
//   int64/float64 = 8B、bool = 1B、string = 2B+len、nil = 0B
// 不精确追求每字节，只用于压缩率指标计算
func estimateFullBytes(state map[string]interface{}) uint64 {
	var n uint64
	for _, v := range state {
		n += sizeOfValue(v)
	}
	return n
}

// estimateFieldBytes 估算 FrameDelta 字段载荷字节数
func estimateFieldBytes(fd *FrameDelta) uint64 {
	if fd == nil {
		return 0
	}
	var n uint64
	for _, ed := range fd.Entities {
		for _, v := range ed.Fields {
			n += sizeOfValue(v)
		}
	}
	return n
}

func sizeOfValue(v interface{}) uint64 {
	switch x := v.(type) {
	case nil:
		return 0
	case bool:
		return 1
	case int:
		return 8
	case int32:
		return 8
	case int64:
		return 8
	case uint32:
		return 8
	case uint64:
		return 8
	case float32:
		return 8
	case float64:
		return 8
	case string:
		return 2 + uint64(len(x))
	default:
		return 16 // fallback 保守估算
	}
}
