package gate

import (
	"fmt"
	"time"

	"engine/log"
)

// TokenVerifier Token 验证回调函数类型
// 返回 userID、过期时间和 error
type TokenVerifier func(token string) (userID string, expiry time.Time, err error)

// TokenFilter 登录态验证过滤器
// 验证客户端的认证 Token，未认证连接只允许发送认证消息
type TokenFilter struct {
	verifier        TokenVerifier
	authTimeout     time.Duration // 连接后多久必须完成认证
	extractToken    func(data []byte) (string, bool) // 从消息中提取 token
}

// NewTokenFilter 创建 Token 验证过滤器
// verifier: Token 校验函数
// extractToken: 从原始消息数据中提取 token 的函数（返回 token 和 是否是认证消息）
func NewTokenFilter(verifier TokenVerifier, extractToken func([]byte) (string, bool)) *TokenFilter {
	return &TokenFilter{
		verifier:     verifier,
		authTimeout:  30 * time.Second,
		extractToken: extractToken,
	}
}

// WithAuthTimeout 设置认证超时时间
func (f *TokenFilter) WithAuthTimeout(d time.Duration) *TokenFilter {
	f.authTimeout = d
	return f
}

func (f *TokenFilter) Name() string { return "token_verify" }

func (f *TokenFilter) OnConnect(_ *SecurityContext) error {
	return nil
}

func (f *TokenFilter) OnMessage(ctx *SecurityContext, data []byte) FilterResult {
	// 已认证的连接直接放行
	if ctx.Authenticated {
		return FilterPass
	}

	// 检查认证超时
	if f.authTimeout > 0 && time.Since(ctx.ConnectedAt) > f.authTimeout {
		log.Warn("[%s] conn=%s auth timeout after %v", f.Name(), ctx.ConnID, f.authTimeout)
		return FilterKick
	}

	// 尝试从消息中提取 token
	if f.extractToken == nil {
		return FilterPass
	}

	token, isAuthMsg := f.extractToken(data)
	if !isAuthMsg {
		// 非认证消息，未认证连接不允许发送
		log.Warn("[%s] conn=%s sent non-auth message before authentication", f.Name(), ctx.ConnID)
		ctx.AddViolation()
		return FilterReject
	}

	// 验证 token
	userID, expiry, err := f.verifier(token)
	if err != nil {
		log.Warn("[%s] conn=%s token verification failed: %v", f.Name(), ctx.ConnID, err)
		ctx.AddViolation()
		return FilterReject
	}

	// 检查过期
	if !expiry.IsZero() && time.Now().After(expiry) {
		log.Warn("[%s] conn=%s token expired", f.Name(), ctx.ConnID)
		ctx.AddViolation()
		return FilterReject
	}

	// 认证成功
	ctx.Authenticated = true
	ctx.UserID = userID
	if ctx.Metadata == nil {
		ctx.Metadata = make(map[string]interface{})
	}
	ctx.Metadata["auth_time"] = time.Now()
	ctx.Metadata["token_expiry"] = expiry
	log.Info("[%s] conn=%s authenticated as user=%s", f.Name(), ctx.ConnID, userID)

	return FilterPass
}

func (f *TokenFilter) OnDisconnect(_ *SecurityContext) {}

// String 返回过滤器描述
func (f *TokenFilter) String() string {
	return fmt.Sprintf("TokenFilter(timeout=%v)", f.authTimeout)
}
