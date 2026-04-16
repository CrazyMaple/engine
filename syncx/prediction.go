package syncx

import "time"

// PredictionConfig 客户端预测配置
type PredictionConfig struct {
	// MaxPredictionFrames 最大预测帧数（超过此帧数的预测将被丢弃）
	MaxPredictionFrames int
	// ReconcileThreshold 和解阈值——服务端与预测状态差异超过此值时强制校正
	// 差异以 state key 数量衡量
	ReconcileThreshold int
	// SmoothCorrection 是否启用平滑校正（否则瞬间跳变）
	SmoothCorrection bool
	// CorrectionRate 每帧校正比例 (0,1]，1.0 = 瞬间校正
	CorrectionRate float64
}

// DefaultPredictionConfig 默认预测配置
func DefaultPredictionConfig() PredictionConfig {
	return PredictionConfig{
		MaxPredictionFrames: 10,
		ReconcileThreshold:  0, // 任何差异都校正
		SmoothCorrection:    true,
		CorrectionRate:      0.3,
	}
}

// PredictFunc 预测函数——根据输入预测下一帧状态（纯函数，无副作用）
// 客户端本地执行此函数立即响应操作，无需等待服务端确认
type PredictFunc func(state map[string]interface{}, input *PlayerInput) map[string]interface{}

// PredictionClient 客户端预测管理器
// 维护本地预测状态，接收服务端权威数据后进行和解
type PredictionClient struct {
	config    PredictionConfig
	predictFn PredictFunc
	playerID  string

	// 当前显示状态（预测或已确认）
	displayState map[string]interface{}
	// 最后确认的服务端帧号
	confirmedFrame uint64
	// 待确认的预测输入队列（帧号 → 输入）
	pendingInputs []pendingInput
	// 校正残差（用于平滑校正）
	corrections map[string]correction
	// 延迟追踪器引用（用于自适应预测帧数）
	latency *LatencyTracker
	// 帧间隔
	frameInterval time.Duration
}

type pendingInput struct {
	frameNum uint64
	input    *PlayerInput
}

type correction struct {
	target interface{} // 目标值
	rate   float64     // 校正速率
}

// NewPredictionClient 创建客户端预测管理器
func NewPredictionClient(playerID string, config PredictionConfig, predictFn PredictFunc, latency *LatencyTracker, tickRate int) *PredictionClient {
	if tickRate <= 0 {
		tickRate = 20
	}
	return &PredictionClient{
		config:        config,
		predictFn:     predictFn,
		playerID:      playerID,
		displayState:  make(map[string]interface{}),
		corrections:   make(map[string]correction),
		latency:       latency,
		frameInterval: time.Second / time.Duration(tickRate),
	}
}

// PredictInput 客户端本地预测——立即应用输入并记录到待确认队列
// 返回预测后的显示状态
func (pc *PredictionClient) PredictInput(input *PlayerInput) map[string]interface{} {
	// 限制最大预测帧数
	maxPending := pc.adaptiveMaxFrames()
	if len(pc.pendingInputs) >= maxPending {
		// 预测过多，丢弃最早的
		pc.pendingInputs = pc.pendingInputs[1:]
	}

	// 记录待确认输入
	pc.pendingInputs = append(pc.pendingInputs, pendingInput{
		frameNum: input.FrameNum,
		input:    input,
	})

	// 本地立即预测
	pc.displayState = pc.predictFn(pc.displayState, input)

	return pc.copyState(pc.displayState)
}

// ReconcileServerState 接收服务端权威状态，进行和解
// serverFrame: 服务端确认的帧号
// serverState: 服务端权威状态
// 返回和解后的显示状态
func (pc *PredictionClient) ReconcileServerState(serverFrame uint64, serverState map[string]interface{}) map[string]interface{} {
	pc.confirmedFrame = serverFrame

	// 移除已被服务端确认的预测输入
	remaining := pc.pendingInputs[:0]
	for _, pi := range pc.pendingInputs {
		if pi.frameNum > serverFrame {
			remaining = append(remaining, pi)
		}
	}
	pc.pendingInputs = remaining

	// 从服务端状态开始，重新应用未确认的预测输入
	reconciledState := pc.copyState(serverState)
	for _, pi := range pc.pendingInputs {
		reconciledState = pc.predictFn(reconciledState, pi.input)
	}

	// 计算与当前显示状态的差异
	diffCount := pc.countDiffs(pc.displayState, reconciledState)

	if diffCount > pc.config.ReconcileThreshold {
		if pc.config.SmoothCorrection {
			// 记录校正残差，后续逐帧平滑
			for k, v := range reconciledState {
				if pc.displayState[k] != v {
					pc.corrections[k] = correction{target: v, rate: pc.config.CorrectionRate}
				}
			}
		} else {
			// 瞬间跳变到正确状态
			pc.displayState = reconciledState
		}
	}

	// 更新权威部分
	pc.displayState = reconciledState

	return pc.copyState(pc.displayState)
}

// Tick 每帧更新（处理平滑校正）
func (pc *PredictionClient) Tick() {
	// 清理已完成的校正
	for k, c := range pc.corrections {
		pc.displayState[k] = c.target
		delete(pc.corrections, k)
	}
}

// PendingCount 返回待确认的预测输入数量
func (pc *PredictionClient) PendingCount() int {
	return len(pc.pendingInputs)
}

// ConfirmedFrame 返回最后确认的服务端帧号
func (pc *PredictionClient) ConfirmedFrame() uint64 {
	return pc.confirmedFrame
}

// DisplayState 返回当前显示状态的副本
func (pc *PredictionClient) DisplayState() map[string]interface{} {
	return pc.copyState(pc.displayState)
}

// adaptiveMaxFrames 基于 RTT 自适应调整最大预测帧数
func (pc *PredictionClient) adaptiveMaxFrames() int {
	if pc.latency == nil || pc.frameInterval <= 0 {
		return pc.config.MaxPredictionFrames
	}
	// 预测帧数 = RTT / 帧间隔（取上整）+ 1 缓冲
	compFrames := pc.latency.CompensationFrames(pc.playerID, pc.frameInterval)
	adaptive := compFrames*2 + 1
	if adaptive > pc.config.MaxPredictionFrames {
		return pc.config.MaxPredictionFrames
	}
	if adaptive < 2 {
		return 2
	}
	return adaptive
}

func (pc *PredictionClient) countDiffs(a, b map[string]interface{}) int {
	count := 0
	for k, v := range b {
		if a[k] != v {
			count++
		}
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			count++
		}
	}
	return count
}

func (pc *PredictionClient) copyState(state map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{}, len(state))
	for k, v := range state {
		cp[k] = v
	}
	return cp
}
