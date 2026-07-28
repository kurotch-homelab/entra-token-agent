package metrics

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

type Metrics struct {
	mu       sync.RWMutex
	last     time.Time
	expiry   time.Time
	failures uint64
	ready    bool
}

func (m *Metrics) Success(expiry time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.last = time.Now()
	m.expiry = expiry
	m.ready = true
}
func (m *Metrics) Failure() { m.mu.Lock(); defer m.mu.Unlock(); m.failures++ }
func (m *Metrics) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		m.mu.RLock()
		defer m.mu.RUnlock()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "# TYPE entra_token_agent_last_refresh_success_timestamp_seconds gauge\nentra_token_agent_last_refresh_success_timestamp_seconds %d\n# TYPE entra_token_agent_token_expiry_timestamp_seconds gauge\nentra_token_agent_token_expiry_timestamp_seconds %d\n# TYPE entra_token_agent_refresh_failures_total counter\nentra_token_agent_refresh_failures_total %d\n", m.last.Unix(), m.expiry.Unix(), m.failures)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		m.mu.RLock()
		ready := m.ready && time.Now().Before(m.expiry)
		m.mu.RUnlock()
		if !ready {
			http.Error(w, "token unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}
