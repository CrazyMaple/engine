package gate

import "time"

// SecurityConfig 安全配置
type SecurityConfig struct {
	// IP 限流
	EnableIPLimit     bool
	MaxConnsPerIP     int
	IPLimitWindow     time.Duration

	// 消息校验
	EnableMsgValidation bool
	MaxPacketSize       int
	AllowedMsgIDs       []uint16

	// Token 验证
	EnableTokenVerify bool
	TokenVerifier     TokenVerifier
	TokenExtractor    func([]byte) (string, bool) // 从消息中提取 token
	AuthTimeout       time.Duration

	// 防重放
	EnableAntiReplay bool
	SeqOffset        int // 序列号在消息中的偏移
	TsOffset         int // 时间戳在消息中的偏移
	TimestampWindow  time.Duration

	// 异常检测
	EnableAnomalyDetect bool
	MaxViolations       int
	BanDuration         time.Duration
}

// DefaultSecurityConfig 返回默认安全配置（所有功能禁用）
func DefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		MaxConnsPerIP:   10,
		IPLimitWindow:   time.Second,
		MaxPacketSize:   65536,
		AuthTimeout:     30 * time.Second,
		TimestampWindow: 30 * time.Second,
		MaxViolations:   10,
		BanDuration:     5 * time.Minute,
	}
}

// BuildChain 根据配置构建安全过滤器链
func (c *SecurityConfig) BuildChain() *SecurityChain {
	var filters []SecurityFilter

	if c.EnableIPLimit {
		filters = append(filters, NewIPRateLimitFilter(c.MaxConnsPerIP, c.IPLimitWindow))
	}

	if c.EnableAnomalyDetect {
		filters = append(filters, NewAnomalyDetectorFilter(c.MaxViolations, c.BanDuration))
	}

	if c.EnableMsgValidation {
		f := NewMessageValidatorFilter(c.MaxPacketSize)
		if len(c.AllowedMsgIDs) > 0 {
			f.WithAllowedMsgIDs(c.AllowedMsgIDs)
		}
		filters = append(filters, f)
	}

	if c.EnableTokenVerify && c.TokenVerifier != nil {
		f := NewTokenFilter(c.TokenVerifier, c.TokenExtractor)
		if c.AuthTimeout > 0 {
			f.WithAuthTimeout(c.AuthTimeout)
		}
		filters = append(filters, f)
	}

	if c.EnableAntiReplay {
		filters = append(filters, NewAntiReplayFilter(c.SeqOffset, c.TsOffset, c.TimestampWindow))
	}

	if len(filters) == 0 {
		return nil
	}
	return NewSecurityChain(filters...)
}
