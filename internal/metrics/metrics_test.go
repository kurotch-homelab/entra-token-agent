package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEndpoints(t *testing.T) {
	m := &Metrics{}
	h := m.Handler()
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if r.Code != 503 {
		t.Fatalf("initial readiness = %d", r.Code)
	}
	m.Failure()
	m.Success(time.Now().Add(time.Hour))
	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if r.Code != 204 {
		t.Fatalf("readiness = %d", r.Code)
	}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(r.Body.String(), "entra_token_agent_refresh_failures_total 1") {
		t.Fatalf("metrics: %s", r.Body.String())
	}
}
