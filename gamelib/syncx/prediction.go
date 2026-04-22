package syncx

import "time"

// CorrectionMode 客户端预测校正模式
type CorrectionMode int

const (
	// CorrectionInstant 瞬间校正：直接采用服务端权威值
	CorrectionInstant CorrectionMode = iota
	// CorrectionLerp 线性插值校正：在 LerpFrames 帧内逐帧线性趋近目标
	CorrectionLerp
	// CorrectionSpring 弹簧阻尼校正：通过物理模拟带来更"软"的视觉过渡
	CorrectionSpring
)

// PredictionConfig 客户端预测配置
type PredictionConfig struct {
	// MaxPredictionFrames 最大预测帧数（超过此帧数的预测将被丢弃）
	MaxPredictionFrames int
	// ReconcileThreshold 和解阈值——服务端与预测状态差异超过此值时强制校正
	// 差异以 state key 数量衡量
	ReconcileThreshold int
	// SmoothCorrection 是否启用平滑校正（否则瞬间跳变）
	// 兼容字段：当 CorrectionMode == CorrectionInstant 且 SmoothCorrection=true 时
	// 自动升级为 CorrectionLerp。
	SmoothCorrection bool
	// CorrectionRate 每帧校正比例 (0,1]，1.0 = 瞬间校正（旧 Lerp 接口保留）
	CorrectionRate float64

	// CorrectionMode 校正模式（v1.10 引入）
	CorrectionMode CorrectionMode
	// LerpFrames Lerp 模式下完成校正所需帧数（>=1）
	LerpFrames int
	// SpringStiffness 弹簧刚度 (0,1]，越大越快到达目标
	SpringStiffness float64
	// SpringDamping 阻尼系数 [0,1]，越大震荡越小
	SpringDamping float64
}

// DefaultPredictionConfig 默认预测配置
func DefaultPredictionConfig() PredictionConfig {
	return PredictionConfig{
		MaxPredictionFrames: 10,
		ReconcileThreshold:  0, // 任何差异都校正
		SmoothCorrection:    true,
		CorrectionRate:      0.3,
		CorrectionMode:      CorrectionInstant,
		LerpFrames:          5,
		SpringStiffness:     0.3,
		SpringDamping:       0.6,
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
	target   interface{}    // 目标值
	start    interface{}    // 起始值（Lerp/Spring 用）
	mode     CorrectionMode // 校正模式
	rate     float64        // CorrectionInstant/Lerp 用：每帧前进比例
	elapsed  int            // 已经过帧数
	duration int            // Lerp 总帧数
	velocity float64        // Spring 模式下的当前速度（仅 float64 字段有效）
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
		mode := pc.effectiveMode()
		if mode != CorrectionInstant {
			// 记录校正残差：start 取当前显示值，target 取权威值
			for k, v := range reconciledState {
				cur, exists := pc.displayState[k]
				if !exists {
					// 新键无起点可插值，直接采用权威值
					pc.displayState[k] = v
					continue
				}
				if cur != v {
					pc.corrections[k] = correction{
						target:   v,
						start:    cur,
						mode:     mode,
						rate:     pc.config.CorrectionRate,
						duration: pc.config.LerpFrames,
					}
				}
			}
			// 删除权威状态中不再存在的字段
			for k := range pc.displayState {
				if _, ok := reconciledState[k]; !ok {
					delete(pc.displayState, k)
				}
			}
			// Spring/Lerp 模式：保留 displayState，由 Tick() 逐帧推进
			return pc.copyState(pc.displayState)
		}
	}

	// CorrectionInstant 或没有显著差异 → 直接采用权威状态
	pc.displayState = reconciledState
	pc.corrections = make(map[string]correction)
	return pc.copyState(pc.displayState)
}

// Tick 每帧更新（处理平滑校正）
func (pc *PredictionClient) Tick() {
	if len(pc.corrections) == 0 {
		return
	}
	for k, c := range pc.corrections {
		next, done := stepCorrection(c, pc.config)
		pc.corrections[k] = next
		pc.displayState[k] = next.start // start 字段在 step 中被刷新为当前插值
		if done {
			pc.displayState[k] = c.target
			delete(pc.corrections, k)
		}
	}
}

// stepCorrection 推进单个校正一步；返回新状态及是否完成
func stepCorrection(c correction, cfg PredictionConfig) (correction, bool) {
	switch c.mode {
	case CorrectionLerp:
		c.elapsed++
		duration := c.duration
		if duration <= 0 {
			duration = 1
		}
		if c.elapsed >= duration {
			c.start = c.target
			return c, true
		}
		// 仅对 float64 做线性插值；其他类型在最后一帧切换
		sf, sok := toFloat(c.start)
		tf, tok := toFloat(c.target)
		if sok && tok {
			t := float64(c.elapsed) / float64(duration)
			c.start = sf + (tf-sf)*t
			return c, false
		}
		return c, false
	case CorrectionSpring:
		sf, sok := toFloat(c.start)
		tf, tok := toFloat(c.target)
		if !sok || !tok {
			// 非数值字段：一次性赋值
			c.start = c.target
			return c, true
		}
		stiffness := cfg.SpringStiffness
		damping := cfg.SpringDamping
		if stiffness <= 0 {
			stiffness = 0.3
		}
		if damping < 0 {
			damping = 0
		}
		if damping > 1 {
			damping = 1
		}
		// 简化弹簧物理：accel = stiffness*(target-pos); v = (v+accel)*(1-damping)
		accel := stiffness * (tf - sf)
		c.velocity = (c.velocity + accel) * (1 - damping)
		newPos := sf + c.velocity
		c.start = newPos
		// 收敛判定：位移和速度都足够小
		if absFloat(tf-newPos) < 0.001 && absFloat(c.velocity) < 0.001 {
			c.start = c.target
			return c, true
		}
		return c, false
	default: // CorrectionInstant：理论上不应进入 Tick，兜底直接到达
		c.start = c.target
		return c, true
	}
}

func toFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	}
	return 0, false
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// effectiveMode 根据兼容字段推断真正使用的校正模式
func (pc *PredictionClient) effectiveMode() CorrectionMode {
	if pc.config.CorrectionMode != CorrectionInstant {
		return pc.config.CorrectionMode
	}
	if pc.config.SmoothCorrection {
		return CorrectionLerp
	}
	return CorrectionInstant
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
