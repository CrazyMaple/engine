package security

import (
	"crypto/hmac"
	"crypto/sha256"
)

// hmacSHA256Size HMAC-SHA256 签名固定长度（字节）。
const hmacSHA256Size = sha256.Size

// HMACSigner 基于 HMAC-SHA256 的远程消息签名器。实现 engine/remote.MessageSigner。
type HMACSigner struct {
	key []byte
}

// NewHMACSigner 创建 HMAC-SHA256 签名器。
func NewHMACSigner(key []byte) *HMACSigner {
	return &HMACSigner{key: key}
}

// Sign 对 payload 计算 HMAC-SHA256，输出固定 32 字节签名。
func (s *HMACSigner) Sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(payload)
	return mac.Sum(nil)
}

// Verify 使用恒定时间比较校验签名。
func (s *HMACSigner) Verify(payload, sig []byte) bool {
	expected := s.Sign(payload)
	return hmac.Equal(expected, sig)
}

// SignatureSize 返回 HMAC-SHA256 签名长度（恒定 32 字节）。
func (s *HMACSigner) SignatureSize() int { return hmacSHA256Size }
