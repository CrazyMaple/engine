package testkit

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"engine/actor"
)

// --- 场景测试 DSL ---
// 描述式多步骤测试流程，简化复杂场景测试编写

// Scenario 场景测试用例
// 支持链式调用定义多步骤测试流程
type Scenario struct {
	t     testing.TB
	name  string
	steps []scenarioStep
	ctx   *ScenarioContext
}

// ScenarioContext 场景上下文，在步骤间共享数据
type ScenarioContext struct {
	mu     sync.RWMutex
	values map[string]interface{}
	nodes  []*TestNode
	probes map[string]*actor.TestProbe
	pids   map[string]*actor.PID
	errors []error
}

type scenarioStep struct {
	name    string
	action  func(ctx *ScenarioContext) error
	timeout time.Duration
}

// NewScenario 创建场景测试
func NewScenario(t testing.TB, name string) *Scenario {
	return &Scenario{
		t:    t,
		name: name,
		ctx: &ScenarioContext{
			values: make(map[string]interface{}),
			probes: make(map[string]*actor.TestProbe),
			pids:   make(map[string]*actor.PID),
		},
	}
}

// WithNodes 注入测试节点
func (s *Scenario) WithNodes(nodes []*TestNode) *Scenario {
	s.ctx.nodes = nodes
	return s
}

// Step 添加一个执行步骤
func (s *Scenario) Step(name string, action func(ctx *ScenarioContext) error) *Scenario {
	s.steps = append(s.steps, scenarioStep{
		name:    name,
		action:  action,
		timeout: 10 * time.Second,
	})
	return s
}

// StepWithTimeout 添加带自定义超时的步骤
func (s *Scenario) StepWithTimeout(name string, timeout time.Duration, action func(ctx *ScenarioContext) error) *Scenario {
	s.steps = append(s.steps, scenarioStep{
		name:    name,
		action:  action,
		timeout: timeout,
	})
	return s
}

// Setup 添加初始化步骤（语义化别名，等同于 Step）
func (s *Scenario) Setup(action func(ctx *ScenarioContext) error) *Scenario {
	return s.Step("setup", action)
}

// Verify 添加验证步骤（语义化别名，等同于 Step）
func (s *Scenario) Verify(name string, action func(ctx *ScenarioContext) error) *Scenario {
	return s.Step("verify: "+name, action)
}

// Wait 添加等待步骤
func (s *Scenario) Wait(d time.Duration) *Scenario {
	return s.Step(fmt.Sprintf("wait %v", d), func(ctx *ScenarioContext) error {
		time.Sleep(d)
		return nil
	})
}

// Run 执行所有步骤
func (s *Scenario) Run() {
	s.t.Helper()
	s.t.Logf("=== Scenario: %s (%d steps) ===", s.name, len(s.steps))

	for i, step := range s.steps {
		s.t.Logf("  [%d/%d] %s", i+1, len(s.steps), step.name)

		done := make(chan error, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					done <- fmt.Errorf("panic in step %q: %v", step.name, r)
				}
			}()
			done <- step.action(s.ctx)
		}()

		select {
		case err := <-done:
			if err != nil {
				s.t.Fatalf("  FAIL at step %d %q: %v", i+1, step.name, err)
			}
		case <-time.After(step.timeout):
			s.t.Fatalf("  TIMEOUT at step %d %q (limit %v)", i+1, step.name, step.timeout)
		}
	}

	s.t.Logf("=== Scenario %q: PASSED ===", s.name)
}

// --- ScenarioContext 方法 ---

// Set 存储值
func (c *ScenarioContext) Set(key string, value interface{}) {
	c.mu.Lock()
	c.values[key] = value
	c.mu.Unlock()
}

// Get 获取值
func (c *ScenarioContext) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.values[key]
	return v, ok
}

// MustGet 获取值，不存在则返回 error
func (c *ScenarioContext) MustGet(key string) (interface{}, error) {
	v, ok := c.Get(key)
	if !ok {
		return nil, fmt.Errorf("key %q not found in scenario context", key)
	}
	return v, nil
}

// Node 获取第 i 个节点
func (c *ScenarioContext) Node(i int) *TestNode {
	if i < 0 || i >= len(c.nodes) {
		return nil
	}
	return c.nodes[i]
}

// StorePID 存储命名 PID
func (c *ScenarioContext) StorePID(name string, pid *actor.PID) {
	c.mu.Lock()
	c.pids[name] = pid
	c.mu.Unlock()
}

// GetPID 获取命名 PID
func (c *ScenarioContext) GetPID(name string) *actor.PID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pids[name]
}

// AddError 记录非致命错误
func (c *ScenarioContext) AddError(err error) {
	c.mu.Lock()
	c.errors = append(c.errors, err)
	c.mu.Unlock()
}

// Errors 返回所有记录的错误
func (c *ScenarioContext) Errors() []error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]error, len(c.errors))
	copy(result, c.errors)
	return result
}

// --- 预置步骤构建器 ---

// SpawnStep 创建一个 Spawn Actor 的步骤
func SpawnStep(nodeIdx int, pidName string, props *actor.Props) func(*ScenarioContext) error {
	return func(ctx *ScenarioContext) error {
		node := ctx.Node(nodeIdx)
		if node == nil {
			return fmt.Errorf("node %d not available", nodeIdx)
		}
		pid := node.System.Root.Spawn(props)
		ctx.StorePID(pidName, pid)
		return nil
	}
}

// SendStep 创建一个发送消息的步骤
func SendStep(nodeIdx int, pidName string, msg interface{}) func(*ScenarioContext) error {
	return func(ctx *ScenarioContext) error {
		node := ctx.Node(nodeIdx)
		if node == nil {
			return fmt.Errorf("node %d not available", nodeIdx)
		}
		pid := ctx.GetPID(pidName)
		if pid == nil {
			return fmt.Errorf("PID %q not found", pidName)
		}
		node.System.Root.Send(pid, msg)
		return nil
	}
}

// AssertStep 创建一个断言步骤
func AssertStep(check func() error) func(*ScenarioContext) error {
	return func(ctx *ScenarioContext) error {
		return check()
	}
}

// RepeatStep 重复执行步骤 N 次
func RepeatStep(n int, action func(ctx *ScenarioContext, i int) error) func(*ScenarioContext) error {
	return func(ctx *ScenarioContext) error {
		for i := 0; i < n; i++ {
			if err := action(ctx, i); err != nil {
				return fmt.Errorf("repeat %d/%d: %w", i+1, n, err)
			}
		}
		return nil
	}
}

// ParallelStep 并行执行多个步骤
func ParallelStep(actions ...func(*ScenarioContext) error) func(*ScenarioContext) error {
	return func(ctx *ScenarioContext) error {
		var wg sync.WaitGroup
		errCh := make(chan error, len(actions))
		for _, action := range actions {
			wg.Add(1)
			go func(a func(*ScenarioContext) error) {
				defer wg.Done()
				if err := a(ctx); err != nil {
					errCh <- err
				}
			}(action)
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			return err
		}
		return nil
	}
}
