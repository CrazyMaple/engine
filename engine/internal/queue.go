package internal

import (
	"sync/atomic"
	"unsafe"
)

// MPSC 无锁队列（Multi-Producer Single-Consumer）
// 基于链表实现的无锁队列，支持多个生产者，单个消费者

type node struct {
	next unsafe.Pointer
	val  interface{}
}

// Queue MPSC队列
type Queue struct {
	head unsafe.Pointer
	tail unsafe.Pointer
}

// NewQueue 创建新队列
func NewQueue() *Queue {
	stub := &node{}
	return &Queue{
		head: unsafe.Pointer(stub),
		tail: unsafe.Pointer(stub),
	}
}

// Push 入队（多生产者安全）
func (q *Queue) Push(val interface{}) {
	n := &node{val: val}
	prev := (*node)(atomic.SwapPointer(&q.head, unsafe.Pointer(n)))
	atomic.StorePointer(&prev.next, unsafe.Pointer(n))
}

// Pop 出队（单消费者）
//
// q.tail 用 atomic 读写：MPSC 协议虽限定单消费者，但 mailbox 调度协议在 status
// 从 running 切回 idle 后仍会调用 Empty() 决定是否重新 schedule，可能与新一轮
// run 在另一 goroutine 上的 Pop 并发。用 atomic 同步 q.tail 的读写，避免被 race
// detector 标记为 data race。
func (q *Queue) Pop() interface{} {
	tail := (*node)(atomic.LoadPointer(&q.tail))
	next := (*node)(atomic.LoadPointer(&tail.next))

	if next == nil {
		return nil
	}

	atomic.StorePointer(&q.tail, unsafe.Pointer(next))
	val := next.val
	next.val = nil
	return val
}

// Empty 检查队列是否为空
func (q *Queue) Empty() bool {
	tail := (*node)(atomic.LoadPointer(&q.tail))
	next := (*node)(atomic.LoadPointer(&tail.next))
	return next == nil
}
