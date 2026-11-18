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

	"github.com/iago_alves333/crypto-gateway/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// TODO: (Fase 4) Inicialize o cliente Redis (`storage.NewRedisStore`) e garanta o fechamento (`defer`).

	// TODO: (Fase 2) Instancie o Reverse Proxy apontando para a URL de upstream da configuração.

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
			slog.Warn("Failed to write healthcheck response", "error", err)
		}
	})
	// TODO: (Fase 5) Registre o endpoint `/metrics` usando `promhttp.Handler()`.
	// TODO: (Fase 2) Registre a rota principal do proxy envolvida pelos middlewares das Fases 3, 4 e 5:
	//       Observabilidade (Metrics/Logs) -> Rate Limit -> Crypto Auth (HMAC) -> Reverse Proxy.

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
