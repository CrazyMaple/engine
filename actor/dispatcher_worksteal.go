package actor

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// WorkStealingDispatcher 工作窃取调度器
// 维护 N 个 worker，每个 worker 有独立的本地队列
// 空闲 worker 会从忙碌 worker 的队列尾部窃取任务
// 提升多核利用率，减少任务积压
type WorkStealingDispatcher struct {
	workers    []*workStealWorker
	numWorkers int
	throughput int
	rr         uint64 // 轮询计数器（用于任务分发）
	stopChan   chan struct{}
	started    int32
}

// workStealWorker 工作线程
type workStealWorker struct {
	id       int
	mu       sync.Mutex
	tasks    []func()
	cond     *sync.Cond
	parent   *WorkStealingDispatcher
	steals   int64 // 窃取次数
	executed int64 // 执行任务数
}

// NewWorkStealingDispatcher 创建工作窃取调度器
// numWorkers 工作线程数（0 时使用 runtime.NumCPU）
func NewWorkStealingDispatcher(numWorkers, throughput int) *WorkStealingDispatcher {
	if numWorkers <= 0 {
		numWorkers = 4
	}
	if throughput <= 0 {
		throughput = 10
	}

	d := &WorkStealingDispatcher{
		numWorkers: numWorkers,
		throughput: throughput,
		workers:    make([]*workStealWorker, numWorkers),
		stopChan:   make(chan struct{}),
	}

	for i := 0; i < numWorkers; i++ {
		w := &workStealWorker{
			id:     i,
			parent: d,
			tasks:  make([]func(), 0, 64),
		}
		w.cond = sync.NewCond(&w.mu)
		d.workers[i] = w
	}

	d.start()
	return d
}

// start 启动所有 worker goroutine
func (d *WorkStealingDispatcher) start() {
	if !atomic.CompareAndSwapInt32(&d.started, 0, 1) {
		return
	}
	for _, w := range d.workers {
		go w.loop()
	}
}

// Schedule 调度一个任务（轮询分配到某个 worker）
func (d *WorkStealingDispatcher) Schedule(fn func()) {
	if fn == nil {
		return
	}
	// 轮询分发到 worker
	idx := atomic.AddUint64(&d.rr, 1) % uint64(d.numWorkers)
	d.workers[idx].push(fn)
}

// Throughput 返回调度器吞吐量
func (d *WorkStealingDispatcher) Throughput() int {
	return d.throughput
}

// Stop 停止调度器
func (d *WorkStealingDispatcher) Stop() {
	if !atomic.CompareAndSwapInt32(&d.started, 1, 2) {
		return
	}
	close(d.stopChan)
	for _, w := range d.workers {
		w.mu.Lock()
		w.cond.Broadcast()
		w.mu.Unlock()
	}
}

// Stats 返回各 worker 的统计信息
func (d *WorkStealingDispatcher) Stats() []WorkerStats {
	stats := make([]WorkerStats, len(d.workers))
	for i, w := range d.workers {
		stats[i] = WorkerStats{
			WorkerID: w.id,
			Steals:   atomic.LoadInt64(&w.steals),
			Executed: atomic.LoadInt64(&w.executed),
		}
	}
	return stats
}

// push 向 worker 队列追加任务
func (w *workStealWorker) push(fn func()) {
	w.mu.Lock()
	w.tasks = append(w.tasks, fn)
	w.cond.Signal()
	w.mu.Unlock()
}

// popLocal 从本地队列头部取出任务
func (w *workStealWorker) popLocal() func() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.tasks) == 0 {
		return nil
	}
	fn := w.tasks[0]
	w.tasks = w.tasks[1:]
	return fn
}

// steal 从目标 worker 的队列尾部窃取任务
func (w *workStealWorker) steal(victim *workStealWorker) func() {
	victim.mu.Lock()
	defer victim.mu.Unlock()
	if len(victim.tasks) <= 1 {
		// 至少留一个给原 worker 避免频繁窃取
		return nil
	}
	// 从尾部窃取
	n := len(victim.tasks)
	fn := victim.tasks[n-1]
	victim.tasks = victim.tasks[:n-1]
	return fn
}

// trySteal 尝试从其他 worker 窃取任务
func (w *workStealWorker) trySteal() func() {
	n := len(w.parent.workers)
	// 随机起点避免多个 worker 同时针对同一受害者
	start := rand.Intn(n)
	for i := 0; i < n; i++ {
		idx := (start + i) % n
		if idx == w.id {
			continue
		}
		victim := w.parent.workers[idx]
		if fn := w.steal(victim); fn != nil {
			atomic.AddInt64(&w.steals, 1)
			return fn
		}
	}
	return nil
}

// loop worker 主循环
func (w *workStealWorker) loop() {
	for {
		// 检查停止信号
		select {
		case <-w.parent.stopChan:
			return
		default:
		}

		// 本地优先
		fn := w.popLocal()
		if fn == nil {
			// 本地空，尝试窃取
			fn = w.trySteal()
		}

		if fn != nil {
			w.execute(fn)
			continue
		}

		// 既无本地任务也无可窃取，等待新任务
		w.mu.Lock()
		// 二次检查避免错过信号
		if len(w.tasks) == 0 {
			// 使用短超时避免错过窃取机会
			waitChan := make(chan struct{}, 1)
			go func() {
				time.Sleep(time.Millisecond)
				waitChan <- struct{}{}
			}()
			w.cond.Wait()
			select {
			case <-waitChan:
			default:
			}
		}
		w.mu.Unlock()

		// 再次检查停止
		select {
		case <-w.parent.stopChan:
			return
		default:
		}
	}
}

// execute 执行任务，带 panic 恢复
func (w *workStealWorker) execute(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			// panic 被吞没，避免单个任务失败拖垮整个 worker
			_ = r
		}
	}()
	fn()
	atomic.AddInt64(&w.executed, 1)
}

// WorkerStats worker 统计信息
type WorkerStats struct {
	WorkerID int
	Steals   int64
	Executed int64
}
