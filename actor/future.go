package actor

import (
	"sync"
	"time"
)

// Future 异步结果
type Future struct {
	pid    *PID
	result chan interface{}
	once   sync.Once
}

// NewFuture 创建Future
func NewFuture(timeout time.Duration) *Future {
	f := &Future{
		result: make(chan interface{}, 1),
	}

	if timeout > 0 {
		time.AfterFunc(timeout, func() {
			f.complete(&ErrTimeout{})
		})
	}

	return f
}

// PID 返回Future的PID
func (f *Future) PID() *PID {
	return f.pid
}

// SetPID 设置Future的PID
func (f *Future) SetPID(pid *PID) {
	f.pid = pid
}

// Result 等待并返回结果
func (f *Future) Result() interface{} {
	return <-f.result
}

// Wait 等待结果（带超时）
func (f *Future) Wait() (interface{}, error) {
	result := <-f.result
	if err, ok := result.(error); ok {
		return nil, err
	}
	return result, nil
}

// complete 完成Future
func (f *Future) complete(result interface{}) {
	f.once.Do(func() {
		f.result <- result
	})
}

// ErrTimeout 超时错误
type ErrTimeout struct{}

func (e *ErrTimeout) Error() string {
	return "future: timeout"
}

// futureProcess Future的进程实现
type futureProcess struct {
	future *Future
}

func (fp *futureProcess) SendUserMessage(pid *PID, message interface{}) {
	// 解包信封，Future 只需要消息内容
	msg, _ := UnwrapEnvelope(message)
	fp.future.complete(msg)
}

func (fp *futureProcess) SendSystemMessage(pid *PID, message interface{}) {
	fp.future.complete(message)
}

func (fp *futureProcess) Stop(pid *PID) {
	fp.future.complete(&ErrTimeout{})
}
