package remote

// MessageCipher 描述一次消息加 / 解密的最小契约。
//
// 实现可以是：
//   - 单密钥 AES-GCM（gamelib/remote/security.AESGCMCipher）；
//   - 多版本密钥环（gamelib/remote/security.CipherRing，支持滚动轮换）；
//   - X25519 ECDH 派生后的 AES-GCM（gamelib/remote/security.DerivedCipher）；
//   - 测试 fake（在 _test.go 中实现）。
//
// engine 层只持有接口；具体算法实现一律外迁 gamelib，避免在核心路径锁死特定加密库。
type MessageCipher interface {
	// Encrypt 加密 plaintext，返回包含 nonce + ciphertext + tag 的输出。
	// 失败必须返回非 nil 错误（如 keyID 未注册、随机源不可用）。
	Encrypt(plaintext []byte) ([]byte, error)

	// Decrypt 解密一个由相同（或同密钥环）实现产生的 data。
	// 失败必须返回非 nil 错误（如 keyID 未知、tag 校验失败、密钥已轮换销毁）。
	Decrypt(data []byte) ([]byte, error)

	// KeyID 返回当前默认加密所用的密钥版本号；解密侧从 data 头取 keyID 后查找对应解密 key。
	// 单密钥实现可恒定返回固定值。
	KeyID() uint32
}
