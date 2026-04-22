package timer

import (
	"fmt"
	"sync"
	"time"
)

// --- 分布式定时任务调度器增强 ---
//
// 在现有 timer.Dispatcher（本地定时器）基础上扩展，提供：
// - 分布式定时任务注册，通过选主保证仅一个节点执行
// - 任务持久化接口，重启后自动恢复
// - 执行结果日志记录

// TaskStatus 任务状态
type TaskStatus int

const (
	TaskPending  TaskStatus = iota // 待执行
	TaskRunning                    // 执行中
	TaskSuccess                    // 成功
	TaskFailed                     // 失败
	TaskTimeout                    // 超时
	TaskCanceled                   // 已取消
)

func (s TaskStatus) String() string {
	switch s {
	case TaskPending:
		return "pending"
	case TaskRunning:
		return "running"
	case TaskSuccess:
		return "success"
	case TaskFailed:
		return "failed"
	case TaskTimeout:
		return "timeout"
	case TaskCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// TaskDef 分布式定时任务定义
type TaskDef struct {
	ID       string     `json:"id"`        // 全局唯一任务 ID
	Name     string     `json:"name"`      // 任务名称
	CronExpr string     `json:"cron_expr"` // Cron 表达式
	Handler  func() error `json:"-"`        // 执行逻辑
	Timeout  time.Duration `json:"timeout,omitempty"` // 执行超时
}

// TaskLog 任务执行日志
type TaskLog struct {
	TaskID    string     `json:"task_id"`
	TaskName  string     `json:"task_name"`
	Status    TaskStatus `json:"status"`
	StartTime time.Time  `json:"start_time"`
	EndTime   time.Time  `json:"end_time,omitempty"`
	Error     string     `json:"error,omitempty"`
	NodeID    string     `json:"node_id"` // 执行节点
}

// TaskStore 任务持久化接口
type TaskStore interface {
	// SaveTask 注册/更新任务
	SaveTask(task *TaskDef) error
	// LoadTasks 加载所有已注册的任务
	LoadTasks() ([]*TaskDef, error)
	// DeleteTask 删除任务
	DeleteTask(id string) error
	// AppendLog 追加执行日志
	AppendLog(log *TaskLog) error
	// RecentLogs 获取最近的日志
	RecentLogs(taskID string, n int) ([]*TaskLog, error)
}

// MemoryTaskStore 内存任务存储
type MemoryTaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*taskDefRecord
	logs  []*TaskLog
}

// taskDefRecord 序列化友好的任务记录
type taskDefRecord struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	CronExpr string `json:"cron_expr"`
}

// NewMemoryTaskStore 创建内存任务存储
func NewMemoryTaskStore() *MemoryTaskStore {
	return &MemoryTaskStore{
		tasks: make(map[string]*taskDefRecord),
	}
}

func (m *MemoryTaskStore) SaveTask(task *TaskDef) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[task.ID] = &taskDefRecord{
		ID:       task.ID,
		Name:     task.Name,
		CronExpr: task.CronExpr,
	}
	return nil
}

func (m *MemoryTaskStore) LoadTasks() ([]*TaskDef, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*TaskDef, 0, len(m.tasks))
	for _, r := range m.tasks {
		result = append(result, &TaskDef{
			ID:       r.ID,
			Name:     r.Name,
			CronExpr: r.CronExpr,
		})
	}
	return result, nil
}

func (m *MemoryTaskStore) DeleteTask(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, id)
	return nil
}

func (m *MemoryTaskStore) AppendLog(log *TaskLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, log)
	// 限制最大日志条数
	if len(m.logs) > 1000 {
		m.logs = m.logs[len(m.logs)-500:]
	}
	return nil
}

func (m *MemoryTaskStore) RecentLogs(taskID string, n int) ([]*TaskLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var filtered []*TaskLog
	for _, l := range m.logs {
		if taskID == "" || l.TaskID == taskID {
			filtered = append(filtered, l)
		}
	}
	if n > 0 && len(filtered) > n {
		filtered = filtered[len(filtered)-n:]
	}
	return filtered, nil
}

// LogCount 日志条数（测试用）
func (m *MemoryTaskStore) LogCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.logs)
}

// --- 分布式任务调度器 ---

// IsLeaderFn 判定当前节点是否为 Leader 的函数（可由 ClusterSingleton 提供）
type IsLeaderFn func() bool

// DistributedScheduler 分布式定时任务调度器
// 仅在 Leader 节点执行任务，其他节点注册但不执行
type DistributedScheduler struct {
	dispatcher *Dispatcher
	store      TaskStore
	nodeID     string
	isLeader   IsLeaderFn

	mu    sync.RWMutex
	tasks map[string]*scheduledTask // id -> scheduled
	stopCh chan struct{}
}

// scheduledTask 已调度的任务
type scheduledTask struct {
	def  *TaskDef
	cron *Cron
}

// DistributedSchedulerConfig 调度器配置
type DistributedSchedulerConfig struct {
	Dispatcher *Dispatcher // 底层定时器调度器
	Store      TaskStore   // 持久化存储
	NodeID     string      // 当前节点 ID
	IsLeader   IsLeaderFn  // Leader 判定函数
}

// NewDistributedScheduler 创建分布式定时任务调度器
func NewDistributedScheduler(cfg DistributedSchedulerConfig) *DistributedScheduler {
	if cfg.IsLeader == nil {
		cfg.IsLeader = func() bool { return true } // 单节点模式默认为 leader
	}
	return &DistributedScheduler{
		dispatcher: cfg.Dispatcher,
		store:      cfg.Store,
		nodeID:     cfg.NodeID,
		isLeader:   cfg.IsLeader,
		tasks:      make(map[string]*scheduledTask),
		stopCh:     make(chan struct{}),
	}
}

// Register 注册定时任务
func (ds *DistributedScheduler) Register(task *TaskDef) error {
	if task.ID == "" {
		return fmt.Errorf("task ID required")
	}
	if task.CronExpr == "" {
		return fmt.Errorf("cron expression required")
	}
	if task.Handler == nil {
		return fmt.Errorf("task handler required")
	}
	if task.Timeout <= 0 {
		task.Timeout = 30 * time.Second
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	// 如果已存在，先取消旧调度
	if old, ok := ds.tasks[task.ID]; ok && old.cron != nil {
		old.cron.Stop()
	}

	// 持久化
	if ds.store != nil {
		if err := ds.store.SaveTask(task); err != nil {
			return fmt.Errorf("save task: %w", err)
		}
	}

	st := &scheduledTask{def: task}
	ds.tasks[task.ID] = st

	// 调度执行
	ds.scheduleTask(st)

	return nil
}

// Unregister 注销定时任务
func (ds *DistributedScheduler) Unregister(taskID string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if st, ok := ds.tasks[taskID]; ok {
		if st.cron != nil {
			st.cron.Stop()
		}
		delete(ds.tasks, taskID)

		if ds.store != nil {
			_ = ds.store.DeleteTask(taskID)
		}
	}
}

// ListTasks 列出已注册的任务
func (ds *DistributedScheduler) ListTasks() []*TaskDef {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	result := make([]*TaskDef, 0, len(ds.tasks))
	for _, st := range ds.tasks {
		result = append(result, st.def)
	}
	return result
}

// Start 启动调度器（从持久化存储恢复已注册任务）
func (ds *DistributedScheduler) Start() error {
	if ds.store != nil {
		tasks, err := ds.store.LoadTasks()
		if err != nil {
			return fmt.Errorf("load tasks: %w", err)
		}
		for _, task := range tasks {
			// 恢复的任务没有 Handler，需要业务层重新注册
			ds.mu.Lock()
			ds.tasks[task.ID] = &scheduledTask{def: task}
			ds.mu.Unlock()
		}
	}
	return nil
}

// Stop 停止调度器，取消所有定时任务
func (ds *DistributedScheduler) Stop() {
	close(ds.stopCh)

	ds.mu.Lock()
	defer ds.mu.Unlock()

	for _, st := range ds.tasks {
		if st.cron != nil {
			st.cron.Stop()
		}
	}
}

// scheduleTask 调度单个任务
func (ds *DistributedScheduler) scheduleTask(st *scheduledTask) {
	if st.def.Handler == nil {
		return
	}

	cronExpr, err := NewCronExpr(st.def.CronExpr)
	if err != nil {
		return
	}

	taskDef := st.def
	st.cron = ds.dispatcher.CronFunc(cronExpr, func() {
		ds.executeTask(taskDef)
	})
}

// executeTask 执行任务
func (ds *DistributedScheduler) executeTask(task *TaskDef) {
	// 非 Leader 节点跳过执行
	if !ds.isLeader() {
		return
	}

	log := &TaskLog{
		TaskID:    task.ID,
		TaskName:  task.Name,
		Status:    TaskRunning,
		StartTime: time.Now(),
		NodeID:    ds.nodeID,
	}

	// 带超时执行
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		done <- task.Handler()
	}()

	select {
	case err := <-done:
		log.EndTime = time.Now()
		if err != nil {
			log.Status = TaskFailed
			log.Error = err.Error()
		} else {
			log.Status = TaskSuccess
		}
	case <-time.After(task.Timeout):
		log.EndTime = time.Now()
		log.Status = TaskTimeout
		log.Error = "execution timeout"
	}

	// 记录日志
	if ds.store != nil {
		_ = ds.store.AppendLog(log)
	}
}
