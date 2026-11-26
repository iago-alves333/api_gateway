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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
			slog.Warn("Failed to write healthcheck response", "error", err)
		}
	})
	mux.Handle("GET /metrics", promhttp.Handler())

	// Cadeia de execução: Observabilidade (Metrics/Logs) -> Rate Limit -> Crypto Auth (HMAC) -> Reverse Proxy.
	var apiHandler http.Handler = http.StripPrefix("/api", reverseProxy)
	apiHandler = cryptoAuth.Middleware(apiHandler)
	apiHandler = rateLimiter.Middleware(apiHandler)
	apiHandler = metricsCollector.Middleware(apiHandler)
	mux.Handle("/api/", apiHandler)

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
