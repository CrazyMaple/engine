package syncx

// RollbackConfig 回滚配置
type RollbackConfig struct {
	// HistorySize 历史状态环形缓冲大小（保留最近 N 帧状态）
	HistorySize int
	// MaxRollbackFrames 最大回滚帧数
	MaxRollbackFrames int
}

// DefaultRollbackConfig 默认回滚配置
func DefaultRollbackConfig() RollbackConfig {
	return RollbackConfig{
		HistorySize:       64,
		MaxRollbackFrames: 16,
	}
}

// SimulateFunc 模拟函数——给定状态和该帧的所有输入，返回下一帧状态
// 必须是纯函数（确定性），相同输入必须产生相同输出
type SimulateFunc func(state map[string]interface{}, inputs []PlayerInput) map[string]interface{}

// RollbackManager 服务端回滚重算管理器
// 维护历史状态环形缓冲，当收到迟到的输入时回滚到对应帧并重新模拟
type RollbackManager struct {
	config     RollbackConfig
	simulateFn SimulateFunc
	broadcast  BroadcastFunc

	// 历史状态环形缓冲
	history     []frameState
	historyHead int  // 最旧帧在缓冲中的位置
	historyLen  int  // 当前缓冲中有效帧数

	// 当前帧号和状态
	currentFrame uint64
	currentState map[string]interface{}

	// 每帧的输入记录（帧号 → 输入列表）
	frameInputs map[uint64][]PlayerInput

	// 是否发生过回滚（可用于通知客户端）
	lastRollbackFrame uint64
}

type frameState struct {
	frameNum uint64
	state    map[string]interface{}
}

// NewRollbackManager 创建回滚管理器
func NewRollbackManager(config RollbackConfig, simulateFn SimulateFunc, broadcast BroadcastFunc) *RollbackManager {
	if config.HistorySize <= 0 {
		config.HistorySize = 64
	}
	return &RollbackManager{
		config:      config,
		simulateFn:  simulateFn,
		broadcast:   broadcast,
		history:     make([]frameState, config.HistorySize),
		frameInputs: make(map[uint64][]PlayerInput),
		currentState: make(map[string]interface{}),
	}
}

// Tick 推进一帧——保存当前状态到历史，执行模拟，广播结果
func (rm *RollbackManager) Tick(frameNum uint64) {
	rm.currentFrame = frameNum

	// 保存当前状态到历史环形缓冲
	rm.saveHistory(frameNum, rm.currentState)

	// 获取本帧输入
	inputs := rm.frameInputs[frameNum]

	// 执行模拟
	rm.currentState = rm.simulateFn(rm.currentState, inputs)

	// 广播权威状态
	snapshot := &StateSnapshot{
		FrameNum: frameNum,
		State:    rm.copyState(rm.currentState),
	}
	rm.broadcast(nil, snapshot)
}

// OnInput 接收玩家输入
// 如果输入对应的帧已经过去（迟到输入），则触发回滚重算
// 返回 true 表示触发了回滚
func (rm *RollbackManager) OnInput(input PlayerInput) bool {
	targetFrame := input.FrameNum

	// 记录输入
	rm.frameInputs[targetFrame] = append(rm.frameInputs[targetFrame], input)

	// 如果输入属于未来帧或当前帧，无需回滚
	if targetFrame >= rm.currentFrame {
		return false
	}

	// 检查是否超过最大回滚帧数
	rollbackFrames := rm.currentFrame - targetFrame
	if rollbackFrames > uint64(rm.config.MaxRollbackFrames) {
		// 超过最大回滚距离，忽略该迟到输入
		return false
	}

	// 执行回滚重算
	return rm.rollbackAndResimulate(targetFrame)
}

// rollbackAndResimulate 回滚到指定帧并重新模拟到当前帧
func (rm *RollbackManager) rollbackAndResimulate(targetFrame uint64) bool {
	// 从历史缓冲中恢复目标帧的状态
	state, found := rm.loadHistory(targetFrame)
	if !found {
		return false
	}

	rm.lastRollbackFrame = targetFrame

	// 从目标帧开始重新模拟到当前帧
	resimState := rm.copyState(state)
	for frame := targetFrame; frame < rm.currentFrame; frame++ {
		inputs := rm.frameInputs[frame]
		resimState = rm.simulateFn(resimState, inputs)
		// 更新历史记录（重算后的状态可能不同）
		rm.saveHistory(frame+1, resimState)
	}

	// 更新当前状态
	rm.currentState = resimState

	// 广播校正后的状态
	snapshot := &StateSnapshot{
		FrameNum: rm.currentFrame,
		State:    rm.copyState(rm.currentState),
	}
	rm.broadcast(nil, snapshot)

	return true
}

// saveHistory 保存帧状态到环形缓冲
func (rm *RollbackManager) saveHistory(frameNum uint64, state map[string]interface{}) {
	idx := rm.frameToIndex(frameNum)
	rm.history[idx] = frameState{
		frameNum: frameNum,
		state:    rm.copyState(state),
	}
	if rm.historyLen < rm.config.HistorySize {
		rm.historyLen++
	}
}

// loadHistory 从环形缓冲中加载帧状态
func (rm *RollbackManager) loadHistory(frameNum uint64) (map[string]interface{}, bool) {
	idx := rm.frameToIndex(frameNum)
	entry := rm.history[idx]
	if entry.frameNum != frameNum {
		return nil, false
	}
	return entry.state, true
}

func (rm *RollbackManager) frameToIndex(frameNum uint64) int {
	return int(frameNum % uint64(rm.config.HistorySize))
}

// CurrentState 返回当前权威状态
func (rm *RollbackManager) CurrentState() map[string]interface{} {
	return rm.copyState(rm.currentState)
}

// CurrentFrame 返回当前帧号
func (rm *RollbackManager) CurrentFrame() uint64 {
	return rm.currentFrame
}

// LastRollbackFrame 返回最后一次回滚的目标帧号
func (rm *RollbackManager) LastRollbackFrame() uint64 {
	return rm.lastRollbackFrame
}

// SetState 设置初始状态（游戏开始前调用）
func (rm *RollbackManager) SetState(state map[string]interface{}) {
	rm.currentState = rm.copyState(state)
}

// CleanupOldInputs 清理过老的输入记录（可定期调用节省内存）
func (rm *RollbackManager) CleanupOldInputs(keepFromFrame uint64) {
	for frame := range rm.frameInputs {
		if frame < keepFromFrame {
			delete(rm.frameInputs, frame)
		}
	}
}

func (rm *RollbackManager) copyState(state map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{}, len(state))
	for k, v := range state {
		cp[k] = v
	}
	return cp
}
