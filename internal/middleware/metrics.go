package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const headerRequestID = "X-Request-ID"

// responseWriterWrapper envolve http.ResponseWriter para capturar o HTTP Status Code enviado ao cliente.
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

// WriteHeader captura o status code na primeira chamada antes de delegar ao ResponseWriter original.
func (rw *responseWriterWrapper) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.statusCode = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

// Write garante que wroteHeader seja marcado caso o handler escreva no corpo sem chamar WriteHeader antes.
func (rw *responseWriterWrapper) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.wroteHeader = true
	}
	return rw.ResponseWriter.Write(b)
}

// Unwrap expõe o ResponseWriter subjacente para suporte a http.ResponseController e streaming/flushing.
func (rw *responseWriterWrapper) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// MetricsCollector agrupa os coletores RED (Rate, Errors, Duration) do Prometheus para o API Gateway.
type MetricsCollector struct {
	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
}

// NewMetricsCollector inicializa e registra as métricas do Gateway no registrador padrão do Prometheus.
func NewMetricsCollector() *MetricsCollector {
	return newMetricsCollector(prometheus.DefaultRegisterer)
}

func newMetricsCollector(reg prometheus.Registerer) *MetricsCollector {
	factory := promauto.With(reg)
	return &MetricsCollector{
		requestsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gateway_http_requests_total",
				Help: "Total de requisições HTTP processadas pelo API Gateway.",
			},
			[]string{"method", "path", "status"},
		),
		requestDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "gateway_http_request_duration_seconds",
				Help:    "Distribuição da latência das requisições HTTP no API Gateway em segundos.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path"},
		),
	}
}

// Middleware mede latência, captura o status code, registra métricas Prometheus e emite logs estruturados.
func (mc *MetricsCollector) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		requestID := getOrCreateRequestID(r)
		r.Header.Set(headerRequestID, requestID)
		w.Header().Set(headerRequestID, requestID)

		rw := &responseWriterWrapper{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(rw, r)

		elapsed := time.Since(start)
		routeLabel := routePattern(r)
		statusStr := strconv.Itoa(rw.statusCode)

		mc.requestsTotal.WithLabelValues(r.Method, routeLabel, statusStr).Inc()
		mc.requestDuration.WithLabelValues(r.Method, routeLabel).Observe(elapsed.Seconds())

		slog.InfoContext(
			r.Context(),
			"HTTP request processed",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
			"duration_ms", elapsed.Milliseconds(),
			"request_id", requestID,
		)
	})
}

// routePattern usa o padrão de rota do ServeMux para evitar explosão de cardinalidade no Prometheus.
func routePattern(r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return "unmatched"
}

func getOrCreateRequestID(r *http.Request) string {
	if id := strings.TrimSpace(r.Header.Get(headerRequestID)); id != "" {
		return id
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf[:])
}
