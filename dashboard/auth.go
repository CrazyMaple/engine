package dashboard

import (
	"net/http"
	"strings"
)

// AuthConfig Dashboard 鉴权配置
type AuthConfig struct {
	// BasicAuth HTTP Basic 认证（用户名/密码）
	BasicAuth *BasicAuthConfig
	// TokenAuth Bearer Token 认证
	TokenAuth *TokenAuthConfig
}

// BasicAuthConfig Basic 认证配置
type BasicAuthConfig struct {
	Username string
	Password string
}

// TokenAuthConfig Token 认证配置
type TokenAuthConfig struct {
	Tokens []string // 有效的 Bearer Token 列表
}

// authMiddleware 返回带鉴权的 HTTP Handler
func authMiddleware(cfg *AuthConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if checkAuth(r, cfg) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="Dashboard"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

func checkAuth(r *http.Request, cfg *AuthConfig) bool {
	// 尝试 Basic Auth
	if cfg.BasicAuth != nil {
		if checkBasicAuth(r, cfg.BasicAuth) {
			return true
		}
	}
	// 尝试 Token Auth
	if cfg.TokenAuth != nil {
		if checkTokenAuth(r, cfg.TokenAuth) {
			return true
		}
	}
	// 如果没有配置任何认证方式则拒绝
	return false
}

func checkBasicAuth(r *http.Request, cfg *BasicAuthConfig) bool {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}
	return user == cfg.Username && pass == cfg.Password
}

func checkTokenAuth(r *http.Request, cfg *TokenAuthConfig) bool {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return false
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	for _, valid := range cfg.Tokens {
		if token == valid {
			return true
		}
	}
	return false
}

// extractOperator 从请求中提取操作人标识
func extractOperator(r *http.Request) string {
	user, _, ok := r.BasicAuth()
	if ok && user != "" {
		return user
	}
	return "anonymous"
}
