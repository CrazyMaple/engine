package mailbox

import (
	"sync/atomic"
	"unsafe"
)

// MPSC 无锁队列（Multi-Producer Single-Consumer）。
// 由 engine/internal/queue.go 移植——engine/internal 是 internal 包，
// 跨 module 不可 import，因此在 gamelib mailbox 包内提供等价实现，供高级邮箱复用。

type queueNode struct {
	next unsafe.Pointer
	val  interface{}
}

type mpscQueue struct {
	head unsafe.Pointer
	tail unsafe.Pointer
}

func newMpscQueue() *mpscQueue {
	stub := &queueNode{}
	return &mpscQueue{
		head: unsafe.Pointer(stub),
		tail: unsafe.Pointer(stub),
	}
}

func (q *mpscQueue) Push(val interface{}) {
	n := &queueNode{val: val}
	prev := (*queueNode)(atomic.SwapPointer(&q.head, unsafe.Pointer(n)))
	atomic.StorePointer(&prev.next, unsafe.Pointer(n))
}

// Pop / Empty 用 atomic 读写 q.tail：mailbox 调度协议在 status 切回 idle 后还会
// 调用 Empty() 决定是否重新 schedule，可能与新一轮 run 在另一 goroutine 的 Pop
// 并发。详见 engine/internal/queue.go 的同样修正。
func (q *mpscQueue) Pop() interface{} {
	tail := (*queueNode)(atomic.LoadPointer(&q.tail))
	next := (*queueNode)(atomic.LoadPointer(&tail.next))

	if next == nil {
		return nil
	}

	atomic.StorePointer(&q.tail, unsafe.Pointer(next))
	val := next.val
	next.val = nil
	return val
}

func (q *mpscQueue) Empty() bool {
	tail := (*queueNode)(atomic.LoadPointer(&q.tail))
	next := (*queueNode)(atomic.LoadPointer(&tail.next))
	return next == nil
}
