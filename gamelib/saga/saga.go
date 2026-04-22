package saga

import (
	"fmt"
	"time"
)

// SagaStatus Saga 执行状态
type SagaStatus int

const (
	SagaStatusPending     SagaStatus = iota // 等待执行
	SagaStatusRunning                       // 正在执行
	SagaStatusCompleted                     // 全部成功
	SagaStatusCompensating                  // 正在补偿
	SagaStatusCompensated                   // 补偿完成
	SagaStatusFailed                        // 补偿也失败（需人工干预）
)

func (s SagaStatus) String() string {
	switch s {
	case SagaStatusPending:
		return "pending"
	case SagaStatusRunning:
		return "running"
	case SagaStatusCompleted:
		return "completed"
	case SagaStatusCompensating:
		return "compensating"
	case SagaStatusCompensated:
		return "compensated"
	case SagaStatusFailed:
		return "failed"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// StepStatus 步骤执行状态
type StepStatus int

const (
	StepPending      StepStatus = iota // 等待执行
	StepExecuted                       // 已执行成功
	StepFailed                         // 执行失败
	StepCompensated                    // 已补偿成功
	StepCompensateFailed               // 补偿失败
)

// SagaContext Saga 执行上下文，在步骤间传递数据
type SagaContext struct {
	// SagaID 全局唯一的 Saga 标识
	SagaID string

	// Data 步骤间传递的数据
	Data map[string]interface{}
}

// Set 设置上下文数据
func (c *SagaContext) Set(key string, value interface{}) {
	if c.Data == nil {
		c.Data = make(map[string]interface{})
	}
	c.Data[key] = value
}

// Get 获取上下文数据
func (c *SagaContext) Get(key string) (interface{}, bool) {
	if c.Data == nil {
		return nil, false
	}
	v, ok := c.Data[key]
	return v, ok
}

// GetString 获取字符串类型数据
func (c *SagaContext) GetString(key string) string {
	if v, ok := c.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// StepAction 步骤正向操作
type StepAction func(ctx *SagaContext) error

// StepCompensate 步骤补偿操作
type StepCompensate func(ctx *SagaContext) error

// Step Saga 步骤定义
type Step struct {
	Name       string         // 步骤名称
	Action     StepAction     // 正向操作
	Compensate StepCompensate // 补偿操作
	Timeout    time.Duration  // 步骤超时（0 使用全局超时）
}

// StepResult 步骤执行结果
type StepResult struct {
	StepName  string
	Status    StepStatus
	Error     error
	StartedAt time.Time
	EndedAt   time.Time
}

// Saga Saga 定义
type Saga struct {
	Name         string        // Saga 名称
	Steps        []Step        // 步骤列表（顺序执行）
	Timeout      time.Duration // 全局超时
	MaxRetries   int           // 补偿最大重试次数（默认 3）
	RetryDelay   time.Duration // 补偿重试间隔（默认 1 秒）
}

// SagaBuilder Saga 构建器
type SagaBuilder struct {
	saga Saga
}

// NewSaga 创建 Saga 构建器
func NewSaga(name string) *SagaBuilder {
	return &SagaBuilder{
		saga: Saga{
			Name:       name,
			MaxRetries: 3,
			RetryDelay: time.Second,
			Timeout:    30 * time.Second,
		},
	}
}

// Step 添加一个步骤
func (b *SagaBuilder) Step(name string, action StepAction, compensate StepCompensate) *SagaBuilder {
	b.saga.Steps = append(b.saga.Steps, Step{
		Name:       name,
		Action:     action,
		Compensate: compensate,
	})
	return b
}

// StepWithTimeout 添加带超时的步骤
func (b *SagaBuilder) StepWithTimeout(name string, action StepAction, compensate StepCompensate, timeout time.Duration) *SagaBuilder {
	b.saga.Steps = append(b.saga.Steps, Step{
		Name:       name,
		Action:     action,
		Compensate: compensate,
		Timeout:    timeout,
	})
	return b
}

// WithTimeout 设置全局超时
func (b *SagaBuilder) WithTimeout(timeout time.Duration) *SagaBuilder {
	b.saga.Timeout = timeout
	return b
}

// WithMaxRetries 设置补偿最大重试次数
func (b *SagaBuilder) WithMaxRetries(n int) *SagaBuilder {
	b.saga.MaxRetries = n
	return b
}

// WithRetryDelay 设置补偿重试间隔
func (b *SagaBuilder) WithRetryDelay(d time.Duration) *SagaBuilder {
	b.saga.RetryDelay = d
	return b
}

// Build 构建 Saga
func (b *SagaBuilder) Build() *Saga {
	s := b.saga
	return &s
}
