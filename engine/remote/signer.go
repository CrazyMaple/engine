package remote

// MessageSigner 描述一次消息签名 / 验签的最小契约。
//
// engine/remote 在发送路径 append signer.Sign(payload) 的输出；
// 接收路径用 SignatureSize() 切出尾部签名段并 Verify。
//
// 实现可以是：
//   - HMAC-SHA256（gamelib/remote/security.HMACSigner，由 v1.12 的 32 字节实现迁出后改名）；
//   - Ed25519 等定长签名（gamelib 后续扩展）；
//   - 测试 fake（在 _test.go 中实现，覆盖 Sign / Verify 失败路径）。
type MessageSigner interface {
	// Sign 对 payload 计算签名；返回值长度必须等于 SignatureSize()。
	Sign(payload []byte) []byte

	// Verify 校验签名；返回 false 视为校验失败（payload 已被篡改 / sig 错误 / 密钥不匹配）。
	Verify(payload, sig []byte) bool

	// SignatureSize 返回此 signer 产出的签名字节长度。
	//
	// 约束：
	//   - 返回值必须 > 0；返回 0 视为禁用签名（构造时报错或拒绝注入）。
	//   - 接收路径若 len(data) < SignatureSize() 必须直接拒绝该消息。
	//   - 发送路径 append 的签名长度必须等于 SignatureSize()。
	//   - 实际产出签名长度若与 SignatureSize() 返回值不一致，视为实现 bug；
	//     fake / test 实现尤其需要保证两者严格一致，避免在测试通过、生产撕裂。
	SignatureSize() int
}
