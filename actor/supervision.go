package actor

import "time"

// Directive 监管指令
type Directive int

const (
	// ResumeDirective 恢复Actor
	ResumeDirective Directive = iota
	// RestartDirective 重启Actor
	RestartDirective
	// StopDirective 停止Actor
	StopDirective
	// EscalateDirective 上报给父Actor
	EscalateDirective
)

// SupervisorStrategy 监管策略接口
type SupervisorStrategy interface {
	HandleFailure(supervisor Supervisor, child *PID, restartStats *RestartStatistics, reason interface{}) Directive
}

// Supervisor 监管者接口
type Supervisor interface {
	Children() []*PID
	EscalateFailure(reason interface{}, message interface{})
	RestartChildren(pids ...*PID)
	StopChildren(pids ...*PID)
	ResumeChildren(pids ...*PID)
}

// DeciderFunc 决策函数
type DeciderFunc func(reason interface{}) Directive

// OneForOneStrategy 一对一监管策略
type OneForOneStrategy struct {
	maxRetries      int
	withinDuration  time.Duration
	decider         DeciderFunc
}

// NewOneForOneStrategy 创建一对一监管策略
func NewOneForOneStrategy(maxRetries int, withinDuration time.Duration, decider DeciderFunc) SupervisorStrategy {
	return &OneForOneStrategy{
		maxRetries:     maxRetries,
		withinDuration: withinDuration,
		decider:        decider,
	}
}

func (s *OneForOneStrategy) HandleFailure(supervisor Supervisor, child *PID, restartStats *RestartStatistics, reason interface{}) Directive {
	// 使用决策函数
	directive := s.decider(reason)

	// 检查重启次数限制
	if directive == RestartDirective {
		if s.shouldStop(restartStats) {
			return StopDirective
		}
		restartStats.FailureCount++
		restartStats.LastFailureTime = time.Now()
	}

	return directive
}

func (s *OneForOneStrategy) shouldStop(restartStats *RestartStatistics) bool {
	if s.maxRetries == 0 {
		return false
	}

	if s.withinDuration == 0 {
		return restartStats.FailureCount >= s.maxRetries
	}

	if time.Since(restartStats.LastFailureTime) > s.withinDuration {
		restartStats.FailureCount = 0
	}

	return restartStats.FailureCount >= s.maxRetries
}

// DefaultDecider 默认决策函数
func DefaultDecider(reason interface{}) Directive {
	return RestartDirective
}

// AlwaysRestartDecider 总是重启
func AlwaysRestartDecider(reason interface{}) Directive {
	return RestartDirective
}

// AllForOneStrategy 全体监管策略
// 当一个子 Actor 失败时，根据决策函数的指令对所有子 Actor 执行相同操作
type AllForOneStrategy struct {
	maxRetries     int
	withinDuration time.Duration
	decider        DeciderFunc
}

// NewAllForOneStrategy 创建全体监管策略
func NewAllForOneStrategy(maxRetries int, withinDuration time.Duration, decider DeciderFunc) SupervisorStrategy {
	return &AllForOneStrategy{
		maxRetries:     maxRetries,
		withinDuration: withinDuration,
		decider:        decider,
	}
}

func (s *AllForOneStrategy) HandleFailure(supervisor Supervisor, child *PID, restartStats *RestartStatistics, reason interface{}) Directive {
	directive := s.decider(reason)

	switch directive {
	case RestartDirective:
		if s.shouldStop(restartStats) {
			return StopDirective
		}
		restartStats.FailureCount++
		restartStats.LastFailureTime = time.Now()
		// 重启所有子 Actor，而非仅失败的那个
		supervisor.RestartChildren(supervisor.Children()...)
		return ResumeDirective // 已在此处处理，不需要 actorCell 再执行

	case StopDirective:
		supervisor.StopChildren(supervisor.Children()...)
		return ResumeDirective

	case EscalateDirective:
		return EscalateDirective

	default:
		// ResumeDirective — 仅恢复失败的子 Actor
		return directive
	}
}

func (s *AllForOneStrategy) shouldStop(restartStats *RestartStatistics) bool {
	if s.maxRetries == 0 {
		return false
	}

	if s.withinDuration == 0 {
		return restartStats.FailureCount >= s.maxRetries
	}

	if time.Since(restartStats.LastFailureTime) > s.withinDuration {
		restartStats.FailureCount = 0
	}

	return restartStats.FailureCount >= s.maxRetries
}

// StoppingStrategy 停止策略
var StoppingStrategy = NewOneForOneStrategy(0, 0, func(reason interface{}) Directive {
	return StopDirective
})

// RestartingStrategy 重启策略
var RestartingStrategy = NewOneForOneStrategy(10, 10*time.Second, AlwaysRestartDecider)
