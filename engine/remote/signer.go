package remote

import (
	"crypto/hmac"
	"crypto/sha256"
)

// MessageSigner 远程消息签名器（HMAC-SHA256）
type MessageSigner struct {
	key []byte
}

// NewMessageSigner 创建远程消息签名器
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
