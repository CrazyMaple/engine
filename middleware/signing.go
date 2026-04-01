package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"reflect"

	"engine/actor"
	"engine/log"
)

// SignedMessage 签名消息，包装原始消息及其 HMAC 签名
type SignedMessage struct {
	Inner     interface{} // 原始消息
	Signature []byte      // HMAC-SHA256 签名
	Payload   []byte      // 签名用的序列化数据
}

// MessageSigner 消息签名器（HMAC-SHA256）
type MessageSigner struct {
	key []byte
}

// NewMessageSigner 创建消息签名器
func NewMessageSigner(key []byte) *MessageSigner {
	return &MessageSigner{key: key}
}

// Sign 对数据进行 HMAC-SHA256 签名
func (s *MessageSigner) Sign(data []byte) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(data)
	return mac.Sum(nil)
}

// Verify 验证 HMAC-SHA256 签名
func (s *MessageSigner) Verify(data, signature []byte) bool {
	expected := s.Sign(data)
	return hmac.Equal(expected, signature)
}

// signingActor 签名验证中间件 Actor
type signingActor struct {
	inner  actor.Actor
	signer *MessageSigner
}

// NewSigningMiddleware 创建签名验证中间件
// 对收到的 SignedMessage 进行签名验证，验证失败则丢弃
// 非 SignedMessage 类型的消息直接放行
func NewSigningMiddleware(signer *MessageSigner) actor.ReceiverMiddleware {
	return func(next actor.Actor) actor.Actor {
		return &signingActor{inner: next, signer: signer}
	}
}

func (a *signingActor) Receive(ctx actor.Context) {
	// 系统生命周期消息直接放行
	switch ctx.Message().(type) {
	case *actor.Started, *actor.Stopping, *actor.Stopped, *actor.Restarting:
		a.inner.Receive(ctx)
		return
	}

	// 如果是签名消息，验证签名
	if signed, ok := ctx.Message().(*SignedMessage); ok {
		if !a.signer.Verify(signed.Payload, signed.Signature) {
			msgType := reflect.TypeOf(signed.Inner).String()
			log.Debug("[signing] verification failed for msg=%s to=%s", msgType, ctx.Self())
			return
		}
		// 验签通过，解包为原始消息继续处理
		// 注意：这里需要通过上下文传递解包后的消息
		a.inner.Receive(ctx)
		return
	}

	// 非签名消息直接放行
	a.inner.Receive(ctx)
}

// WrapSigned 将消息包装为签名消息
func WrapSigned(signer *MessageSigner, payload []byte, inner interface{}) *SignedMessage {
	return &SignedMessage{
		Inner:     inner,
		Payload:   payload,
		Signature: signer.Sign(payload),
	}
}
