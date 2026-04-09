package actor

import "errors"

// ErrStashFull 暂存栈已满
var ErrStashFull = errors.New("stash: capacity exceeded")

// DefaultStashCapacity 默认暂存栈容量
const DefaultStashCapacity = 1000

// stashedMessage 暂存的消息，保留完整信封信息
type stashedMessage struct {
	message interface{} // 原始消息（可能是 MessageEnvelope）
}

// messageStash 消息暂存栈
type messageStash struct {
	messages []stashedMessage
	capacity int
}

// newMessageStash 创建消息暂存栈
func newMessageStash(capacity int) *messageStash {
	if capacity <= 0 {
		capacity = DefaultStashCapacity
	}
	return &messageStash{
		messages: make([]stashedMessage, 0, 16),
		capacity: capacity,
	}
}

// push 将消息压入暂存栈
func (s *messageStash) push(msg interface{}) error {
	if len(s.messages) >= s.capacity {
		return ErrStashFull
	}
	s.messages = append(s.messages, stashedMessage{message: msg})
	return nil
}

// popAll 弹出所有暂存消息（FIFO 顺序）
func (s *messageStash) popAll() []stashedMessage {
	if len(s.messages) == 0 {
		return nil
	}
	msgs := s.messages
	s.messages = make([]stashedMessage, 0, 16)
	return msgs
}

// size 返回暂存消息数量
func (s *messageStash) size() int {
	return len(s.messages)
}

// clear 清空暂存栈
func (s *messageStash) clear() {
	s.messages = s.messages[:0]
}
