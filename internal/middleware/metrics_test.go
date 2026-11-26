package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestMetricsCollectorMiddleware(t *testing.T) {
	var logBuf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, nil)))
	t.Cleanup(func() {
		slog.SetDefault(prevLogger)
	})

	reg := prometheus.NewRegistry()
	mc := newMetricsCollector(reg)

	mux := http.NewServeMux()
	apiHandler := mc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(headerRequestID) == "" {
			t.Error("expected X-Request-ID propagated to downstream request")
		}
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusUnauthorized)
			w.WriteHeader(http.StatusOK) // segunda chamada deve ser ignorada pelo wrapper
			return
		}
		_, _ = w.Write([]byte("ok")) // status implícito 200 sem chamar WriteHeader
	}))
	mux.Handle("/api/", apiHandler)

	// Duas rotas dinâmicas distintas devem compartilhar o label de padrão "/api/".
	req1 := httptest.NewRequest(http.MethodGet, "/api/users/123", nil)
	rec1 := httptest.NewRecorder()
	mux.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec1.Code)
	}
	if rec1.Header().Get(headerRequestID) == "" {
		t.Fatal("expected X-Request-ID on response")
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/users/456", nil)
	req2.Header.Set(headerRequestID, "req-custom-123")
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec2.Code)
	}
	if got := rec2.Header().Get(headerRequestID); got != "req-custom-123" {
		t.Fatalf("expected X-Request-ID=req-custom-123, got %q", got)
	}

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	byName := make(map[string]*dto.MetricFamily, len(families))
	for _, f := range families {
		byName[f.GetName()] = f
	}

	reqTotal, ok := byName["gateway_http_requests_total"]
	if !ok {
		t.Fatal("missing gateway_http_requests_total metric")
	}
	if len(reqTotal.GetMetric()) != 2 {
		t.Fatalf("expected 2 label combinations, got %d", len(reqTotal.GetMetric()))
	}
	for _, m := range reqTotal.GetMetric() {
		for _, lp := range m.GetLabel() {
			if lp.GetName() == "path" && lp.GetValue() != "/api/" {
				t.Fatalf("expected bounded path label '/api/', got %q", lp.GetValue())
			}
		}
	}

	if _, ok := byName["gateway_http_request_duration_seconds"]; !ok {
		t.Fatal("missing gateway_http_request_duration_seconds metric")
	}

	// Verifica que o último log estruturado contém os 5 campos exigidos.
	lines := bytes.Split(bytes.TrimSpace(logBuf.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("expected 2 structured log lines, got %d", len(lines))
	}
	var entry map[string]any
	if err := json.Unmarshal(lines[1], &entry); err != nil {
		t.Fatalf("unmarshal log json: %v", err)
	}
	for _, field := range []string{"method", "path", "status", "duration_ms", "request_id"} {
		if _, exists := entry[field]; !exists {
			t.Fatalf("expected log field %q in %v", field, entry)
		}
	}
	if entry["request_id"] != "req-custom-123" {
		t.Fatalf("expected request_id=req-custom-123, got %v", entry["request_id"])
	}
}
