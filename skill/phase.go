package skill

import (
	"time"
)

// PhaseKind 阶段类别
type PhaseKind int

const (
	PhasePreCast  PhaseKind = iota // 前摇（吟唱）
	PhaseCast                      // 施放瞬间
	PhaseChannel                   // 持续引导
	PhaseBackswing                 // 后摇
)

// String 阶段名称
func (k PhaseKind) String() string {
	switch k {
	case PhasePreCast:
		return "pre_cast"
	case PhaseCast:
		return "cast"
	case PhaseChannel:
		return "channel"
	case PhaseBackswing:
		return "backswing"
	}
	return "unknown"
}

// PhaseDef 单个阶段定义
type PhaseDef struct {
	Kind PhaseKind // 阶段类别
	// Duration 阶段时长（0 表示瞬发）
	Duration time.Duration
	// Effects 阶段执行的效果 ID 列表
	Effects []string
	// Triggers 阶段完成时求值的触发器
	Triggers []*Trigger
	// TickInterval Channel 阶段的 Tick 间隔（仅 PhaseChannel 有意义，0 = 整个阶段只执行一次）
	TickInterval time.Duration
	// Interruptible 是否可被打断（前摇/持续常用）
	Interruptible bool
}

// PhasedSkill 技能的多阶段定义（可选扩展）
// 当 SkillDef.Phases 非空时，优先走 PhasedSkill 管线；否则退化到旧 Effects 执行。
type PhasedSkill struct {
	Phases []PhaseDef
}

// TotalDuration 返回技能全流程总时长
func (ps *PhasedSkill) TotalDuration() time.Duration {
	var total time.Duration
	for _, p := range ps.Phases {
		total += p.Duration
	}
	return total
}

// PhaseState 运行时阶段状态
type PhaseState int

const (
	PhaseStateIdle       PhaseState = iota
	PhaseStateRunning               // 阶段运行中
	PhaseStateCompleted             // 全部阶段完成
	PhaseStateInterrupted           // 被打断
)

// CastSession 一次技能释放的运行时会话
// 由 ECS SkillCastSystem 管理生命周期，每帧 Tick 推进。
type CastSession struct {
	SkillID    string
	CasterID   string
	TargetID   string
	AOETargets []string
	Skill      *SkillDef
	Phased     *PhasedSkill

	// 运行状态
	CurrentPhase int
	State        PhaseState
	StartedAt    time.Time
	PhaseStart   time.Time
	LastTick     time.Time

	// 累积效果结果（跨阶段）
	Results []EffectResult

	// 待派生的链式动作（phase.Triggers 产出）
	ChainActions []ChainAction
}

// NewCastSession 创建一次新会话
func NewCastSession(def *SkillDef, phased *PhasedSkill, casterID, targetID string, aoeTargets []string, now time.Time) *CastSession {
	return &CastSession{
		SkillID:    def.ID,
		CasterID:   casterID,
		TargetID:   targetID,
		AOETargets: aoeTargets,
		Skill:      def,
		Phased:     phased,
		State:      PhaseStateRunning,
		StartedAt:  now,
		PhaseStart: now,
		LastTick:   now,
	}
}

// ActivePhase 当前激活的阶段（可能为 nil）
func (s *CastSession) ActivePhase() *PhaseDef {
	if s.Phased == nil || s.CurrentPhase < 0 || s.CurrentPhase >= len(s.Phased.Phases) {
		return nil
	}
	return &s.Phased.Phases[s.CurrentPhase]
}

// PhaseElapsed 当前阶段已过时间
func (s *CastSession) PhaseElapsed(now time.Time) time.Duration {
	return now.Sub(s.PhaseStart)
}

// Elapsed 会话总时长
func (s *CastSession) Elapsed(now time.Time) time.Duration {
	return now.Sub(s.StartedAt)
}

// IsDone 会话是否结束
func (s *CastSession) IsDone() bool {
	return s.State == PhaseStateCompleted || s.State == PhaseStateInterrupted
}

// Interrupt 打断当前会话
func (s *CastSession) Interrupt() {
	if s.State == PhaseStateRunning {
		s.State = PhaseStateInterrupted
	}
}

// ChainAction 链式派生动作：一个阶段的触发器产出的后续任务
type ChainAction struct {
	SkillID   string    // 要连锁触发的技能 ID（可选）
	TargetID  string    // 目标
	BuffID    string    // 要施加的 Buff ID（可选）
	Effects   []string  // 额外执行的效果 ID 列表（可选）
	ExecuteAt time.Time // 执行时间（delay 处理后得出）
	Source    string    // 来源触发器名（日志用）
}
