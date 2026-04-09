package saga

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SagaExecution Saga 执行实例
type SagaExecution struct {
	ID          string
	Saga        *Saga
	Context     *SagaContext
	Status      SagaStatus
	StepResults []StepResult
	StartedAt   time.Time
	EndedAt     time.Time
	Error       error

	mu sync.RWMutex
}

// SagaLog Saga 日志条目
type SagaLog struct {
	Timestamp time.Time
	SagaID    string
	SagaName  string
	StepName  string
	Action    string // "execute" / "compensate"
	Status    string // "start" / "success" / "failed"
	Error     string
}

// SagaLogger Saga 日志接口
type SagaLogger interface {
	Log(entry SagaLog)
}

// SagaStore Saga 状态持久化接口（可选）
type SagaStore interface {
	Save(ctx context.Context, execution *SagaExecution) error
	Load(ctx context.Context, sagaID string) (*SagaExecution, error)
}

// Coordinator Saga 协调器
// 负责 Saga 的编排执行：顺序执行各步骤，失败时反向补偿
type Coordinator struct {
	store  SagaStore  // 可选持久化
	logger SagaLogger // 可选日志
	mu     sync.RWMutex
	// 活跃执行列表
	executions map[string]*SagaExecution
}

// CoordinatorOption 协调器配置选项
type CoordinatorOption func(*Coordinator)

// WithStore 设置持久化存储
func WithStore(store SagaStore) CoordinatorOption {
	return func(c *Coordinator) {
		c.store = store
	}
}

// WithLogger 设置日志记录器
func WithLogger(logger SagaLogger) CoordinatorOption {
	return func(c *Coordinator) {
		c.logger = logger
	}
}

// NewCoordinator 创建 Saga 协调器
func NewCoordinator(opts ...CoordinatorOption) *Coordinator {
	c := &Coordinator{
		executions: make(map[string]*SagaExecution),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Execute 执行 Saga（同步阻塞，直到完成或失败）
func (c *Coordinator) Execute(sagaCtx *SagaContext, saga *Saga) (*SagaExecution, error) {
	exec := &SagaExecution{
		ID:          sagaCtx.SagaID,
		Saga:        saga,
		Context:     sagaCtx,
		Status:      SagaStatusRunning,
		StepResults: make([]StepResult, 0, len(saga.Steps)),
		StartedAt:   time.Now(),
	}

	c.mu.Lock()
	c.executions[exec.ID] = exec
	c.mu.Unlock()

	defer func() {
		exec.EndedAt = time.Now()
		c.persistExecution(exec)
	}()

	// 创建全局超时上下文
	ctx, cancel := context.WithTimeout(context.Background(), saga.Timeout)
	defer cancel()

	// 顺序执行各步骤
	for i, step := range saga.Steps {
		select {
		case <-ctx.Done():
			exec.Error = fmt.Errorf("saga timeout after step %d (%s)", i, step.Name)
			exec.Status = SagaStatusCompensating
			c.compensate(exec, i-1)
			return exec, exec.Error
		default:
		}

		result := c.executeStep(ctx, sagaCtx, step)
		exec.StepResults = append(exec.StepResults, result)

		if result.Status == StepFailed {
			exec.Error = fmt.Errorf("step %q failed: %w", step.Name, result.Error)
			exec.Status = SagaStatusCompensating
			c.compensate(exec, i-1) // 补偿已成功的步骤（不含当前失败步骤）
			return exec, exec.Error
		}
	}

	exec.Status = SagaStatusCompleted
	return exec, nil
}

// ExecuteAsync 异步执行 Saga，返回执行 ID
func (c *Coordinator) ExecuteAsync(sagaCtx *SagaContext, saga *Saga) string {
	go c.Execute(sagaCtx, saga)
	return sagaCtx.SagaID
}

// GetExecution 获取 Saga 执行状态
func (c *Coordinator) GetExecution(sagaID string) (*SagaExecution, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	exec, ok := c.executions[sagaID]
	return exec, ok
}

// executeStep 执行单个步骤（带超时）
func (c *Coordinator) executeStep(ctx context.Context, sagaCtx *SagaContext, step Step) StepResult {
	result := StepResult{
		StepName:  step.Name,
		StartedAt: time.Now(),
	}

	c.log(SagaLog{
		Timestamp: result.StartedAt,
		SagaID:    sagaCtx.SagaID,
		SagaName:  "",
		StepName:  step.Name,
		Action:    "execute",
		Status:    "start",
	})

	// 确定步骤超时
	timeout := step.Timeout
	if timeout <= 0 {
		// 使用全局上下文的剩余时间
		if deadline, ok := ctx.Deadline(); ok {
			timeout = time.Until(deadline)
		}
	}

	err := executeWithTimeout(func() error {
		return step.Action(sagaCtx)
	}, timeout)

	result.EndedAt = time.Now()

	if err != nil {
		result.Status = StepFailed
		result.Error = err
		c.log(SagaLog{
			Timestamp: result.EndedAt,
			SagaID:    sagaCtx.SagaID,
			StepName:  step.Name,
			Action:    "execute",
			Status:    "failed",
			Error:     err.Error(),
		})
	} else {
		result.Status = StepExecuted
		c.log(SagaLog{
			Timestamp: result.EndedAt,
			SagaID:    sagaCtx.SagaID,
			StepName:  step.Name,
			Action:    "execute",
			Status:    "success",
		})
	}

	return result
}

// compensate 从指定步骤开始反向补偿
func (c *Coordinator) compensate(exec *SagaExecution, lastSuccessIdx int) {
	saga := exec.Saga
	sagaCtx := exec.Context
	allCompensated := true

	for i := lastSuccessIdx; i >= 0; i-- {
		step := saga.Steps[i]
		if step.Compensate == nil {
			continue
		}

		c.log(SagaLog{
			Timestamp: time.Now(),
			SagaID:    sagaCtx.SagaID,
			StepName:  step.Name,
			Action:    "compensate",
			Status:    "start",
		})

		var compensateErr error
		for retry := 0; retry <= saga.MaxRetries; retry++ {
			compensateErr = executeWithTimeout(func() error {
				return step.Compensate(sagaCtx)
			}, step.Timeout)

			if compensateErr == nil {
				break
			}

			if retry < saga.MaxRetries {
				time.Sleep(saga.RetryDelay)
			}
		}

		result := StepResult{
			StepName:  step.Name + " (compensate)",
			StartedAt: time.Now(),
			EndedAt:   time.Now(),
		}

		if compensateErr != nil {
			result.Status = StepCompensateFailed
			result.Error = compensateErr
			allCompensated = false
			c.log(SagaLog{
				Timestamp: time.Now(),
				SagaID:    sagaCtx.SagaID,
				StepName:  step.Name,
				Action:    "compensate",
				Status:    "failed",
				Error:     compensateErr.Error(),
			})
		} else {
			result.Status = StepCompensated
			c.log(SagaLog{
				Timestamp: time.Now(),
				SagaID:    sagaCtx.SagaID,
				StepName:  step.Name,
				Action:    "compensate",
				Status:    "success",
			})
		}

		exec.StepResults = append(exec.StepResults, result)
	}

	if allCompensated {
		exec.Status = SagaStatusCompensated
	} else {
		exec.Status = SagaStatusFailed // 补偿也失败，需人工干预
	}
}

// log 记录 Saga 日志
func (c *Coordinator) log(entry SagaLog) {
	if c.logger != nil {
		c.logger.Log(entry)
	}
}

// persistExecution 持久化执行状态
func (c *Coordinator) persistExecution(exec *SagaExecution) {
	if c.store != nil {
		c.store.Save(context.Background(), exec)
	}
}

// executeWithTimeout 带超时执行函数
func executeWithTimeout(fn func() error, timeout time.Duration) error {
	if timeout <= 0 {
		return fn()
	}

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		done <- fn()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("step timeout after %v", timeout)
	}
}
