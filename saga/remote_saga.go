package saga

import (
	"context"
	"fmt"
	"sync"
	"time"

	"engine/actor"
	"engine/remote"
)

// RemoteStepRequest 远程步骤执行请求
type RemoteStepRequest struct {
	SagaID   string `json:"saga_id"`
	StepName string `json:"step_name"`
	Action   string `json:"action"` // "execute" 或 "compensate"
	// Data 携带 SagaContext 数据，远程节点执行后可修改
	Data map[string]interface{} `json:"data,omitempty"`
}

// RemoteStepResponse 远程步骤执行响应
type RemoteStepResponse struct {
	SagaID   string                 `json:"saga_id"`
	StepName string                 `json:"step_name"`
	Success  bool                   `json:"success"`
	Error    string                 `json:"error,omitempty"`
	Data     map[string]interface{} `json:"data,omitempty"` // 执行后更新的数据
}

// RemoteStep 远程 Saga 步骤定义
type RemoteStep struct {
	Name       string        // 步骤名称
	TargetPID  *actor.PID    // 远程 Actor PID
	Timeout    time.Duration // 步骤超时（0 使用全局超时）
}

// RemoteSagaConfig 远程 Saga 协调器配置
type RemoteSagaConfig struct {
	Remote     *remote.Remote  // 远程通信层
	Store      SagaStore       // 可选持久化
	Logger     SagaLogger      // 可选日志
	Timeout    time.Duration   // 全局超时（默认 30s）
	MaxRetries int             // 补偿最大重试（默认 3）
	RetryDelay time.Duration   // 补偿重试间隔（默认 1s）
}

// RemoteSagaCoordinator 跨节点 Saga 协调器
// 基于 remote.RequestRemote 实现分布式步骤编排
type RemoteSagaCoordinator struct {
	remote     *remote.Remote
	store      SagaStore
	logger     SagaLogger
	timeout    time.Duration
	maxRetries int
	retryDelay time.Duration
	mu         sync.RWMutex
	executions map[string]*SagaExecution
}

// NewRemoteSagaCoordinator 创建跨节点 Saga 协调器
func NewRemoteSagaCoordinator(cfg RemoteSagaConfig) *RemoteSagaCoordinator {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = time.Second
	}
	return &RemoteSagaCoordinator{
		remote:     cfg.Remote,
		store:      cfg.Store,
		logger:     cfg.Logger,
		timeout:    cfg.Timeout,
		maxRetries: cfg.MaxRetries,
		retryDelay: cfg.RetryDelay,
		executions: make(map[string]*SagaExecution),
	}
}

// RemoteSagaDefinition 远程 Saga 定义
// 步骤分为本地步骤（LocalSteps）和远程步骤（RemoteSteps），按 AllSteps 顺序执行
type RemoteSagaDefinition struct {
	Name      string
	Steps     []RemoteSagaStep
}

// RemoteSagaStep 可以是本地或远程的步骤
type RemoteSagaStep struct {
	Name string

	// 本地步骤（二选一）
	LocalAction     StepAction
	LocalCompensate StepCompensate

	// 远程步骤（二选一）
	TargetPID *actor.PID // 非 nil 表示远程步骤
	Timeout   time.Duration
}

// Execute 执行远程 Saga（同步阻塞）
func (c *RemoteSagaCoordinator) Execute(sagaCtx *SagaContext, def *RemoteSagaDefinition) (*SagaExecution, error) {
	exec := &SagaExecution{
		ID:          sagaCtx.SagaID,
		Saga:        &Saga{Name: def.Name, MaxRetries: c.maxRetries, RetryDelay: c.retryDelay},
		Context:     sagaCtx,
		Status:      SagaStatusRunning,
		StepResults: make([]StepResult, 0, len(def.Steps)),
		StartedAt:   time.Now(),
	}

	c.mu.Lock()
	c.executions[exec.ID] = exec
	c.mu.Unlock()

	defer func() {
		exec.EndedAt = time.Now()
		if c.store != nil {
			c.store.Save(context.Background(), exec)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	// 顺序执行步骤
	for i, step := range def.Steps {
		select {
		case <-ctx.Done():
			exec.Error = fmt.Errorf("remote saga timeout after step %d (%s)", i, step.Name)
			exec.Status = SagaStatusCompensating
			c.compensateRemote(exec, def, i-1)
			return exec, exec.Error
		default:
		}

		result := c.executeRemoteStep(ctx, sagaCtx, step)
		exec.StepResults = append(exec.StepResults, result)

		if result.Status == StepFailed {
			exec.Error = fmt.Errorf("step %q failed: %v", step.Name, result.Error)
			exec.Status = SagaStatusCompensating
			c.compensateRemote(exec, def, i-1)
			return exec, exec.Error
		}
	}

	exec.Status = SagaStatusCompleted
	return exec, nil
}

// executeRemoteStep 执行单个步骤（本地或远程）
func (c *RemoteSagaCoordinator) executeRemoteStep(ctx context.Context, sagaCtx *SagaContext, step RemoteSagaStep) StepResult {
	result := StepResult{
		StepName:  step.Name,
		StartedAt: time.Now(),
	}

	c.log(SagaLog{
		Timestamp: result.StartedAt,
		SagaID:    sagaCtx.SagaID,
		StepName:  step.Name,
		Action:    "execute",
		Status:    "start",
	})

	var err error
	if step.TargetPID != nil {
		// 远程步骤
		err = c.sendRemoteRequest(sagaCtx, step, "execute")
	} else {
		// 本地步骤
		timeout := step.Timeout
		if timeout <= 0 {
			if deadline, ok := ctx.Deadline(); ok {
				timeout = time.Until(deadline)
			}
		}
		err = executeWithTimeout(func() error {
			return step.LocalAction(sagaCtx)
		}, timeout)
	}

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

// compensateRemote 反向补偿（从 lastSuccessIdx 到 0）
func (c *RemoteSagaCoordinator) compensateRemote(exec *SagaExecution, def *RemoteSagaDefinition, lastSuccessIdx int) {
	sagaCtx := exec.Context
	allCompensated := true

	for i := lastSuccessIdx; i >= 0; i-- {
		step := def.Steps[i]

		// 检查是否有补偿操作
		hasCompensate := step.LocalCompensate != nil || step.TargetPID != nil
		if !hasCompensate {
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
		for retry := 0; retry <= c.maxRetries; retry++ {
			if step.TargetPID != nil {
				compensateErr = c.sendRemoteRequest(sagaCtx, step, "compensate")
			} else {
				compensateErr = executeWithTimeout(func() error {
					return step.LocalCompensate(sagaCtx)
				}, step.Timeout)
			}
			if compensateErr == nil {
				break
			}
			if retry < c.maxRetries {
				time.Sleep(c.retryDelay)
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
		exec.Status = SagaStatusFailed
	}
}

// sendRemoteRequest 发送远程请求并等待响应
func (c *RemoteSagaCoordinator) sendRemoteRequest(sagaCtx *SagaContext, step RemoteSagaStep, action string) error {
	timeout := step.Timeout
	if timeout <= 0 {
		timeout = c.timeout
	}

	req := &RemoteStepRequest{
		SagaID:   sagaCtx.SagaID,
		StepName: step.Name,
		Action:   action,
		Data:     sagaCtx.Data,
	}

	future := c.remote.RequestRemote(step.TargetPID, req, timeout)
	resp, err := future.Wait()
	if err != nil {
		return fmt.Errorf("remote step %q %s: %w", step.Name, action, err)
	}

	// 解析响应
	if respMsg, ok := resp.(*RemoteStepResponse); ok {
		if !respMsg.Success {
			return fmt.Errorf("remote step %q %s failed: %s", step.Name, action, respMsg.Error)
		}
		// 合并远程节点返回的数据更新
		if respMsg.Data != nil {
			for k, v := range respMsg.Data {
				sagaCtx.Set(k, v)
			}
		}
		return nil
	}

	return fmt.Errorf("unexpected response type: %T", resp)
}

// GetExecution 获取执行状态
func (c *RemoteSagaCoordinator) GetExecution(sagaID string) (*SagaExecution, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	exec, ok := c.executions[sagaID]
	return exec, ok
}

func (c *RemoteSagaCoordinator) log(entry SagaLog) {
	if c.logger != nil {
		c.logger.Log(entry)
	}
}

// RemoteSagaBuilder 远程 Saga 构建器
type RemoteSagaBuilder struct {
	def RemoteSagaDefinition
}

// NewRemoteSaga 创建远程 Saga 构建器
func NewRemoteSaga(name string) *RemoteSagaBuilder {
	return &RemoteSagaBuilder{
		def: RemoteSagaDefinition{Name: name},
	}
}

// LocalStep 添加本地步骤
func (b *RemoteSagaBuilder) LocalStep(name string, action StepAction, compensate StepCompensate) *RemoteSagaBuilder {
	b.def.Steps = append(b.def.Steps, RemoteSagaStep{
		Name:            name,
		LocalAction:     action,
		LocalCompensate: compensate,
	})
	return b
}

// RemoteStep 添加远程步骤
func (b *RemoteSagaBuilder) RemoteStep(name string, target *actor.PID, timeout time.Duration) *RemoteSagaBuilder {
	b.def.Steps = append(b.def.Steps, RemoteSagaStep{
		Name:      name,
		TargetPID: target,
		Timeout:   timeout,
	})
	return b
}

// Build 构建远程 Saga 定义
func (b *RemoteSagaBuilder) Build() *RemoteSagaDefinition {
	def := b.def
	return &def
}
