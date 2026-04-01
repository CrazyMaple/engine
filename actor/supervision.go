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

// StoppingStrategy 停止策略
var StoppingStrategy = NewOneForOneStrategy(0, 0, func(reason interface{}) Directive {
	return StopDirective
})

// RestartingStrategy 重启策略
var RestartingStrategy = NewOneForOneStrategy(10, 10*time.Second, AlwaysRestartDecider)
