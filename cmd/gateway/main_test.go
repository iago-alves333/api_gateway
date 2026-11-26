package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iago_alves333/crypto-gateway/internal/config"
	"github.com/iago_alves333/crypto-gateway/internal/middleware"
)

type stubRateLimitStore struct {
	remaining int64
}

func (s *stubRateLimitStore) AllowRequest(_ context.Context, _ string, limit int64, window time.Duration) (bool, int64, time.Duration, error) {
	if s.remaining <= 0 {
		return false, 0, window, nil
	}
	s.remaining--
	return true, s.remaining, window, nil
}

func signHMAC(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestBuildMuxRouting(t *testing.T) {
	const secret = "super-secret-key-with-32-bytes!!"

	cryptoAuth, err := middleware.NewCryptoAuth(secret, "", 5*time.Minute)
	if err != nil {
		t.Fatalf("NewCryptoAuth failed: %v", err)
	}
	metricsCollector := middleware.NewMetricsCollector()

	t.Run("hybrid mode preserves /api/v1 path for SPA and enforces HMAC only on webhooks", func(t *testing.T) {
		rateLimiter, err := middleware.NewRateLimiter(&stubRateLimitStore{remaining: 10}, 10, time.Minute)
		if err != nil {
			t.Fatalf("NewRateLimiter failed: %v", err)
		}

		var gotPath, gotBody string
		upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.WriteHeader(http.StatusOK)
		})

		mux := buildMux(&config.Config{}, rateLimiter, cryptoAuth, metricsCollector, upstream)

		// 1. Healthcheck aberta
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("health: expected 200, got %d", rec.Code)
		}

		// 2. Rota da SPA e CORS preflight passam sem HMAC preservando /api/v1/...
		for _, method := range []string{http.MethodGet, http.MethodOptions} {
			gotPath = ""
			req := httptest.NewRequest(method, "/api/v1/attractions", nil)
			req.RemoteAddr = "127.0.0.1:12345"
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("%s /api/v1/attractions: expected 200, got %d", method, rec.Code)
			}
			if gotPath != "/api/v1/attractions" {
				t.Fatalf("expected upstream path /api/v1/attractions, got %q", gotPath)
			}
		}

		// 3. Webhook sem assinatura é barrado com 401 antes do upstream
		gotPath = ""
		reqNoSig := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/reservations/123/confirm", strings.NewReader(`{"paid":true}`))
		reqNoSig.RemoteAddr = "127.0.0.1:12346"
		recNoSig := httptest.NewRecorder()
		mux.ServeHTTP(recNoSig, reqNoSig)
		if recNoSig.Code != http.StatusUnauthorized {
			t.Fatalf("webhook without signature: expected 401, got %d", recNoSig.Code)
		}
		if gotPath != "" {
			t.Fatalf("upstream should not be called on 401, got path %q", gotPath)
		}

		// 4. Webhook com assinatura HMAC válida chega ao upstream com path e body preservados
		body := `{"paid":true}`
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		reqSig := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/reservations/123/confirm", strings.NewReader(body))
		reqSig.RemoteAddr = "127.0.0.1:12347"
		reqSig.Header.Set("X-Timestamp", ts)
		reqSig.Header.Set("X-Signature", signHMAC(secret, ts, []byte(body)))

		recSig := httptest.NewRecorder()
		mux.ServeHTTP(recSig, reqSig)
		if recSig.Code != http.StatusOK {
			t.Fatalf("signed webhook: expected 200, got %d (%s)", recSig.Code, recSig.Body.String())
		}
		if gotPath != "/api/v1/webhooks/reservations/123/confirm" {
			t.Fatalf("expected upstream webhook path, got %q", gotPath)
		}
		if gotBody != body {
			t.Fatalf("expected upstream body %q, got %q", body, gotBody)
		}
	})

	t.Run("strict mode with StripPrefix and HMACProtectAll", func(t *testing.T) {
		rateLimiter, err := middleware.NewRateLimiter(&stubRateLimitStore{remaining: 10}, 10, time.Minute)
		if err != nil {
			t.Fatalf("NewRateLimiter failed: %v", err)
		}

		var gotPath string
		upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		})

		cfg := &config.Config{
			StripPrefix:    "/api",
			HMACProtectAll: true,
		}
		mux := buildMux(cfg, rateLimiter, cryptoAuth, metricsCollector, upstream)

		reqNoSig := httptest.NewRequest(http.MethodGet, "/api/v1/attractions", nil)
		reqNoSig.RemoteAddr = "127.0.0.1:22345"
		recNoSig := httptest.NewRecorder()
		mux.ServeHTTP(recNoSig, reqNoSig)
		if recNoSig.Code != http.StatusUnauthorized {
			t.Fatalf("strict mode without signature: expected 401, got %d", recNoSig.Code)
		}

		ts := strconv.FormatInt(time.Now().Unix(), 10)
		reqSig := httptest.NewRequest(http.MethodGet, "/api/v1/attractions", nil)
		reqSig.RemoteAddr = "127.0.0.1:22346"
		reqSig.Header.Set("X-Timestamp", ts)
		reqSig.Header.Set("X-Signature", signHMAC(secret, ts, nil))

		recSig := httptest.NewRecorder()
		mux.ServeHTTP(recSig, reqSig)
		if recSig.Code != http.StatusOK {
			t.Fatalf("strict mode with signature: expected 200, got %d", recSig.Code)
		}
		if gotPath != "/v1/attractions" {
			t.Fatalf("expected stripped path /v1/attractions, got %q", gotPath)
		}
	})
}
