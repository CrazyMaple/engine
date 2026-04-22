package gate

import (
	"engine/log"
)

// MessageValidatorFilter 消息合法性校验过滤器
// 检查消息包大小和可选的消息 ID 白名单
type MessageValidatorFilter struct {
	maxPacketSize int              // 最大包大小（字节），0 表示不限制
	allowedMsgIDs map[uint16]bool  // 消息 ID 白名单，nil 表示不限制
}

// NewMessageValidatorFilter 创建消息校验过滤器
func NewMessageValidatorFilter(maxPacketSize int) *MessageValidatorFilter {
	return &MessageValidatorFilter{
		maxPacketSize: maxPacketSize,
	}
}

// WithAllowedMsgIDs 设置消息 ID 白名单
func (f *MessageValidatorFilter) WithAllowedMsgIDs(ids []uint16) *MessageValidatorFilter {
	f.allowedMsgIDs = make(map[uint16]bool, len(ids))
	for _, id := range ids {
		f.allowedMsgIDs[id] = true
	}
	return f
}

func (f *MessageValidatorFilter) Name() string { return "msg_validator" }

func (f *MessageValidatorFilter) OnConnect(_ *SecurityContext) error {
	return nil
}

func (f *MessageValidatorFilter) OnMessage(ctx *SecurityContext, data []byte) FilterResult {
	// 检查包大小
	if f.maxPacketSize > 0 && len(data) > f.maxPacketSize {
		log.Warn("[%s] conn=%s packet too large: %d > %d",
			f.Name(), ctx.ConnID, len(data), f.maxPacketSize)
		ctx.AddViolation()
		return FilterReject
	}

	// 检查消息 ID 白名单（消息体前 2 字节为 msgID）
	if f.allowedMsgIDs != nil && len(data) >= 2 {
		msgID := uint16(data[0])<<8 | uint16(data[1])
		if !f.allowedMsgIDs[msgID] {
			log.Warn("[%s] conn=%s unknown msg ID: %d", f.Name(), ctx.ConnID, msgID)
			ctx.AddViolation()
			return FilterReject
		}
	}

	return FilterPass
}

func (f *MessageValidatorFilter) OnDisconnect(_ *SecurityContext) {}
