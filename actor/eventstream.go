package actor

import "sync"

// EventStream 事件流
type EventStream struct {
	subscriptions map[interface{}][]func(interface{})
	mu            sync.RWMutex
}

// NewEventStream 创建事件流
func NewEventStream() *EventStream {
	return &EventStream{
		subscriptions: make(map[interface{}][]func(interface{})),
	}
}

// Subscribe 订阅事件
func (es *EventStream) Subscribe(fn func(interface{})) *Subscription {
	es.mu.Lock()
	defer es.mu.Unlock()

	sub := &Subscription{
		es: es,
		fn: fn,
	}

	es.subscriptions[sub] = append(es.subscriptions[sub], fn)
	return sub
}

// Publish 发布事件
func (es *EventStream) Publish(event interface{}) {
	es.mu.RLock()
	defer es.mu.RUnlock()

	for _, handlers := range es.subscriptions {
		for _, handler := range handlers {
			handler(event)
		}
	}
}

// Unsubscribe 取消订阅
func (es *EventStream) Unsubscribe(sub *Subscription) {
	es.mu.Lock()
	defer es.mu.Unlock()

	delete(es.subscriptions, sub)
}

// Subscription 订阅句柄
type Subscription struct {
	es *EventStream
	fn func(interface{})
}

// Unsubscribe 取消订阅
func (s *Subscription) Unsubscribe() {
	s.es.Unsubscribe(s)
}
