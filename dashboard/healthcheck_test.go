package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthChecker_AllUp(t *testing.T) {
	hc := NewHealthChecker()
	hc.AddLivenessCheck("process", func() HealthCheckResult {
		return HealthCheckResult{Status: HealthStatusUp}
	})
	hc.AddReadinessCheck("actor_system", func() HealthCheckResult {
		return HealthCheckResult{Status: HealthStatusUp}
	})

	resp := hc.CheckLiveness()
	if resp.Status != HealthStatusUp {
		t.Fatalf("expected UP, got %s", resp.Status)
	}

	resp = hc.CheckReadiness()
	if resp.Status != HealthStatusUp {
		t.Fatalf("expected UP, got %s", resp.Status)
	}
}

func TestHealthChecker_OneDown(t *testing.T) {
	hc := NewHealthChecker()
	hc.AddReadinessCheck("ok_check", func() HealthCheckResult {
		return HealthCheckResult{Status: HealthStatusUp}
	})
	hc.AddReadinessCheck("failing_check", func() HealthCheckResult {
		return HealthCheckResult{
			Status:  HealthStatusDown,
			Details: map[string]interface{}{"error": "connection refused"},
		}
	})

	resp := hc.CheckReadiness()
	if resp.Status != HealthStatusDown {
		t.Fatalf("expected DOWN when any check fails, got %s", resp.Status)
	}
	if resp.Checks["failing_check"].Status != HealthStatusDown {
		t.Error("expected failing_check to be DOWN")
	}
	if resp.Checks["ok_check"].Status != HealthStatusUp {
		t.Error("expected ok_check to be UP")
	}
}

func TestHealthChecker_PanicRecovery(t *testing.T) {
	hc := NewHealthChecker()
	hc.AddLivenessCheck("panic_check", func() HealthCheckResult {
		panic("unexpected error")
	})

	resp := hc.CheckLiveness()
	if resp.Status != HealthStatusDown {
		t.Fatalf("expected DOWN on panic, got %s", resp.Status)
	}
}

func TestHealthChecker_EmptyChecks(t *testing.T) {
	hc := NewHealthChecker()

	resp := hc.CheckLiveness()
	if resp.Status != HealthStatusUp {
		t.Fatalf("no checks should default to UP, got %s", resp.Status)
	}
}

func TestHealthRoutes_Liveness(t *testing.T) {
	hc := NewHealthChecker()
	hc.AddLivenessCheck("process", func() HealthCheckResult {
		return HealthCheckResult{Status: HealthStatusUp}
	})

	mux := http.NewServeMux()
	RegisterHealthRoutes(mux, hc)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp HealthCheckResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Status != HealthStatusUp {
		t.Fatalf("expected UP, got %s", resp.Status)
	}
}

func TestHealthRoutes_ReadinessDown(t *testing.T) {
	hc := NewHealthChecker()
	hc.AddReadinessCheck("db", func() HealthCheckResult {
		return HealthCheckResult{Status: HealthStatusDown}
	})

	mux := http.NewServeMux()
	RegisterHealthRoutes(mux, hc)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}
