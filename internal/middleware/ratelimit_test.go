package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type mockRateLimitStore struct {
	mu     sync.Mutex
	counts map[string]int64
	err    error
}

func (m *mockRateLimitStore) AllowRequest(_ context.Context, key string, limit int64, window time.Duration) (bool, int64, time.Duration, error) {
	if m.err != nil {
		return false, 0, 0, m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.counts == nil {
		m.counts = make(map[string]int64)
	}
	m.counts[key]++
	cur := m.counts[key]
	return cur <= limit, max(limit-cur, 0), window, nil
}

func TestRateLimiterMiddleware(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("strips ephemeral port and blocks requests past limit", func(t *testing.T) {
		store := &mockRateLimitStore{}
		rl, err := NewRateLimiter(store, 2, 30*time.Second)
		if err != nil {
			t.Fatalf("NewRateLimiter failed: %v", err)
		}
		handler := rl.Middleware(okHandler)

		for i, port := range []string{"50001", "50002"} {
			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			req.RemoteAddr = "10.0.0.1:" + port
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("req %d: expected 200, got %d", i+1, rec.Code)
			}
			if got := rec.Header().Get("X-RateLimit-Limit"); got != "2" {
				t.Fatalf("expected X-RateLimit-Limit=2, got %q", got)
			}
		}

		// Terceira requisição do mesmo IP com outra porta efêmera deve retornar 429.
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.RemoteAddr = "10.0.0.1:50003"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("expected 429, got %d", rec.Code)
		}
		if got := rec.Header().Get("X-RateLimit-Remaining"); got != "0" {
			t.Fatalf("expected X-RateLimit-Remaining=0, got %q", got)
		}
		if got := rec.Header().Get("Retry-After"); got != "30" {
			t.Fatalf("expected Retry-After=30, got %q", got)
		}
	})

	t.Run("fails open when store returns error", func(t *testing.T) {
		store := &mockRateLimitStore{err: errors.New("redis connection refused")}
		rl, err := NewRateLimiter(store, 1, time.Minute)
		if err != nil {
			t.Fatalf("NewRateLimiter failed: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.RemoteAddr = "10.0.0.2:1234"
		rec := httptest.NewRecorder()
		rl.Middleware(okHandler).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected fail-open 200, got %d", rec.Code)
		}
	})
}
