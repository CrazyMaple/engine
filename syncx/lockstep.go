package syncx

import (
	"hash/crc32"
	"sort"
)

// FrameSyncRoom 帧同步（Lockstep）实现
// 收集所有玩家输入后广播确定性帧数据
type FrameSyncRoom struct {
	config    SyncConfig
	broadcast BroadcastFunc

	players  map[string]bool             // 当前玩家集合
	frameNum uint64                      // 当前帧号
	buffer   map[uint64]map[string]*PlayerInput // frameNum → playerID → input
}

// NewFrameSyncRoom 创建帧同步房间
func NewFrameSyncRoom(config SyncConfig, broadcast BroadcastFunc) *FrameSyncRoom {
	return &FrameSyncRoom{
		config:    config,
		broadcast: broadcast,
		players:   make(map[string]bool),
		buffer:    make(map[uint64]map[string]*PlayerInput),
	}
}

func (fs *FrameSyncRoom) OnPlayerJoin(playerID string) {
	fs.players[playerID] = true
}

func (fs *FrameSyncRoom) OnPlayerLeave(playerID string) {
	delete(fs.players, playerID)
	// 清理该玩家的缓冲输入
	for _, inputs := range fs.buffer {
		delete(inputs, playerID)
	}
}

func (fs *FrameSyncRoom) OnPlayerInput(playerID string, input *PlayerInput) {
	if !fs.players[playerID] {
		return
	}

	targetFrame := input.FrameNum
	// 限制输入缓冲范围
	if targetFrame < fs.frameNum {
		targetFrame = fs.frameNum
	}
	maxFrame := fs.frameNum + uint64(fs.config.InputBufferSize)
	if targetFrame > maxFrame {
		targetFrame = maxFrame
	}

	if fs.buffer[targetFrame] == nil {
		fs.buffer[targetFrame] = make(map[string]*PlayerInput)
	}
	fs.buffer[targetFrame][playerID] = input
}

func (fs *FrameSyncRoom) Tick(frameNum uint64) {
	fs.frameNum = frameNum

	// 收集当前帧所有玩家输入
	inputs := fs.collectInputs(frameNum)

	// 计算帧数据哈希
	hash := fs.computeHash(inputs)

	// 广播帧数据
	frame := &FrameData{
		FrameNum: frameNum,
		Inputs:   inputs,
		Hash:     hash,
	}
	fs.broadcast(nil, frame)

	// 清理已处理的帧缓冲
	delete(fs.buffer, frameNum)
}

func (fs *FrameSyncRoom) FrameNum() uint64 {
	return fs.frameNum
}

// collectInputs 收集指定帧的所有玩家输入
func (fs *FrameSyncRoom) collectInputs(frameNum uint64) []PlayerInput {
	frameInputs := fs.buffer[frameNum]
	inputs := make([]PlayerInput, 0, len(fs.players))

	// 按 playerID 排序保证确定性
	playerIDs := make([]string, 0, len(fs.players))
	for pid := range fs.players {
		playerIDs = append(playerIDs, pid)
	}
	sort.Strings(playerIDs)

	for _, pid := range playerIDs {
		if input, ok := frameInputs[pid]; ok {
			inputs = append(inputs, *input)
		} else {
			// 缺少输入的玩家填充空输入
			inputs = append(inputs, PlayerInput{
				PlayerID: pid,
				FrameNum: frameNum,
			})
		}
	}
	return inputs
}

// computeHash 计算帧数据哈希（用于校验一致性）
func (fs *FrameSyncRoom) computeHash(inputs []PlayerInput) uint32 {
	h := crc32.NewIEEE()
	for _, input := range inputs {
		h.Write([]byte(input.PlayerID))
		for _, action := range input.Actions {
			b := []byte{byte(action.Type >> 8), byte(action.Type)}
			h.Write(b)
		}
	}
	return h.Sum32()
}

// VerifyHash 校验客户端帧哈希是否与服务端一致
func (fs *FrameSyncRoom) VerifyHash(frameNum uint64, clientHash uint32, serverFrame *FrameData) bool {
	return clientHash == serverFrame.Hash
}
