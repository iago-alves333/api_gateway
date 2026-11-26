package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/iago_alves333/crypto-gateway/internal/config"
	"github.com/iago_alves333/crypto-gateway/internal/middleware"
	"github.com/iago_alves333/crypto-gateway/internal/proxy"
	"github.com/iago_alves333/crypto-gateway/internal/storage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	redisStore, err := storage.NewRedisStore(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		slog.Error("Failed to initialize Redis store", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := redisStore.Close(); err != nil {
			slog.Warn("Failed to close Redis store", "error", err)
		}
	}()

	reverseProxy, err := proxy.NewReverseProxy(cfg.UpstreamURL)
	if err != nil {
		slog.Error("Failed to initialize reverse proxy", "error", err)
		os.Exit(1)
	}

	cryptoAuth, err := middleware.NewCryptoAuth(cfg.HMACSecret, cfg.AESKey, cfg.HMACTolerance)
	if err != nil {
		slog.Error("Failed to initialize crypto auth middleware", "error", err)
		os.Exit(1)
	}

	rateLimiter, err := middleware.NewRateLimiter(redisStore, cfg.RateLimitRequests, cfg.RateLimitWindow)
	if err != nil {
		slog.Error("Failed to initialize rate limiter middleware", "error", err)
		os.Exit(1)
	}

	metricsCollector := middleware.NewMetricsCollector()
	mux := buildMux(cfg, rateLimiter, cryptoAuth, metricsCollector, reverseProxy)

	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errChan := make(chan error, 1)
	go func() {
		slog.Info("Starting server", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		slog.Error("Server failed", "error", err)
		return
	case <-ctx.Done():
		slog.Info("Shutting down server gracefully")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server shutdown failed", "error", err)
		os.Exit(1)
	}

	slog.Info("Server exited gracefully")
}

// buildMux monta o roteador HTTP separando rotas de webhooks protegidas por HMAC das rotas padrão de API.
func buildMux(
	cfg *config.Config,
	rateLimiter *middleware.RateLimiter,
	cryptoAuth *middleware.CryptoAuth,
	metricsCollector *middleware.MetricsCollector,
	upstream http.Handler,
) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
			slog.Warn("Failed to write healthcheck response", "error", err)
		}
	})
	mux.Handle("GET /metrics", promhttp.Handler())

	baseHandler := upstream
	if cfg.StripPrefix != "" {
		baseHandler = http.StripPrefix(cfg.StripPrefix, baseHandler)
	}

	// Cadeia completa para M2M/Webhooks: Observabilidade -> Rate Limit -> Crypto Auth (HMAC) -> Reverse Proxy.
	protectedHandler := metricsCollector.Middleware(rateLimiter.Middleware(cryptoAuth.Middleware(baseHandler)))

	// Rotas públicas/SPA aplicam Observabilidade + Rate Limit e delegam autenticação de usuário (JWT/CORS) ao upstream.
	apiHandler := metricsCollector.Middleware(rateLimiter.Middleware(baseHandler))
	if cfg.HMACProtectAll {
		apiHandler = protectedHandler
	}

	mux.Handle("/api/", apiHandler)
	mux.Handle("/api/v1/webhooks/", protectedHandler)
	return mux
}
