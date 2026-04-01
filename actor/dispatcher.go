package actor

import "sync"

// Dispatcher 调度器接口
type Dispatcher interface {
	Schedule(fn func())
	Throughput() int
}

// goroutineDispatcher 基于goroutine的调度器
type goroutineDispatcher struct {
	throughput int
}

// NewGoroutineDispatcher 创建goroutine调度器
func NewGoroutineDispatcher(throughput int) Dispatcher {
	if throughput <= 0 {
		throughput = 10
	}
	return &goroutineDispatcher{
		throughput: throughput,
	}
}

func (d *goroutineDispatcher) Schedule(fn func()) {
	go fn()
}

func (d *goroutineDispatcher) Throughput() int {
	return d.throughput
}

// synchronizedDispatcher 同步调度器（用于测试）
type synchronizedDispatcher struct {
	throughput int
}

// NewSynchronizedDispatcher 创建同步调度器
func NewSynchronizedDispatcher(throughput int) Dispatcher {
	if throughput <= 0 {
		throughput = 10
	}
	return &synchronizedDispatcher{
		throughput: throughput,
	}
}

func (d *synchronizedDispatcher) Schedule(fn func()) {
	fn()
}

func (d *synchronizedDispatcher) Throughput() int {
	return d.throughput
}

var defaultDispatcher = NewGoroutineDispatcher(10)
var dispatcherPool = &sync.Pool{
	New: func() interface{} {
		return NewGoroutineDispatcher(10)
	},
}
