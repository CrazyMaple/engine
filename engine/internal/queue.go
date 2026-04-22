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
func (q *Queue) Pop() interface{} {
	tail := (*node)(q.tail)
	next := (*node)(atomic.LoadPointer(&tail.next))

	if next == nil {
		return nil
	}

	q.tail = unsafe.Pointer(next)
	val := next.val
	next.val = nil
	return val
}

// Empty 检查队列是否为空
func (q *Queue) Empty() bool {
	tail := (*node)(q.tail)
	next := (*node)(atomic.LoadPointer(&tail.next))
	return next == nil
}
