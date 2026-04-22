package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddlewareBasicAuth(t *testing.T) {
	cfg := &AuthConfig{
		BasicAuth: &BasicAuthConfig{
			Username: "admin",
			Password: "pass123",
		},
	}

	handler := authMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// 无凭据 → 401
	req := httptest.NewRequest("GET", "/api/system", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no auth: got %d, want 401", w.Code)
	}

	// 错误凭据 → 401
	req = httptest.NewRequest("GET", "/api/system", nil)
	req.SetBasicAuth("admin", "wrong")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong pass: got %d, want 401", w.Code)
	}

	// 正确凭据 → 200
	req = httptest.NewRequest("GET", "/api/system", nil)
	req.SetBasicAuth("admin", "pass123")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("correct auth: got %d, want 200", w.Code)
	}
}

func TestAuthMiddlewareTokenAuth(t *testing.T) {
	cfg := &AuthConfig{
		TokenAuth: &TokenAuthConfig{
			Tokens: []string{"valid-token-123"},
		},
	}

	handler := authMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// 无 token → 401
	req := httptest.NewRequest("GET", "/api/system", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", w.Code)
	}

	// 正确 token → 200
	req = httptest.NewRequest("GET", "/api/system", nil)
	req.Header.Set("Authorization", "Bearer valid-token-123")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("valid token: got %d, want 200", w.Code)
	}
}

func TestExtractOperator(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.SetBasicAuth("admin", "pass")
	op := extractOperator(req)
	if op != "admin" {
		t.Errorf("expected admin, got %s", op)
	}

	req2 := httptest.NewRequest("GET", "/", nil)
	op2 := extractOperator(req2)
	if op2 != "anonymous" {
		t.Errorf("expected anonymous, got %s", op2)
	}
}
