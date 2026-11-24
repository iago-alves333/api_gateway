package middleware

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RateLimitStore abstrai a verificação atômica de taxa de requisições (implementada por storage.RedisStore).
type RateLimitStore interface {
	AllowRequest(ctx context.Context, key string, limit int64, window time.Duration) (allowed bool, remaining int64, resetAfter time.Duration, err error)
}

// RateLimiter aplica controle de taxa distribuído por IP de origem.
type RateLimiter struct {
	store  RateLimitStore
	limit  int64
	window time.Duration
}

// NewRateLimiter instancia o middleware de Rate Limiting validando suas dependências e limites.
func NewRateLimiter(store RateLimitStore, limit int64, window time.Duration) (*RateLimiter, error) {
	if store == nil {
		return nil, errors.New("rate limit store must not be nil")
	}
	if limit <= 0 {
		return nil, errors.New("rate limit must be positive")
	}
	if window <= 0 {
		return nil, errors.New("rate limit window must be positive")
	}

	return &RateLimiter{
		store:  store,
		limit:  limit,
		window: window,
	}, nil
}

// Middleware intercepta requisições, consulta o contador atômico no Redis e bloqueia excessos com HTTP 429.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := extractClientIP(r)
		key := "ratelimit:" + clientIP

		allowed, remaining, resetAfter, err := rl.store.AllowRequest(r.Context(), key, rl.limit, rl.window)
		if err != nil {
			// Estratégia Fail-Open: uma indisponibilidade momentânea do Redis não derruba a API inteira.
			slog.Error("Rate limiter store error; allowing request", "client_ip", clientIP, "error", err)
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(rl.limit, 10))
		w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))

		if !allowed {
			retryAfter := max(int64(math.Ceil(resetAfter.Seconds())), 1)
			w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
			slog.Warn("Rate limit exceeded", "client_ip", clientIP, "method", r.Method, "path", r.URL.Path)
			writeJSONError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "Limite de requisições excedido. Tente novamente mais tarde.")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// extractClientIP obtém o IP real da conexão TCP sem porta efêmera.
// Não confia em X-Forwarded-For diretamente para evitar IP spoofing quando o Gateway está na borda.
func extractClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if addr := strings.TrimSpace(r.RemoteAddr); addr != "" {
		return addr
	}
	return "unknown"
}
