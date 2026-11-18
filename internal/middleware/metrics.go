package middleware

import (
	"net/http"
)

// Outros imports sugeridos para quando você for implementar:
// import (
// 	"log/slog"
// 	"strconv"
// 	"time"
//
// 	"github.com/prometheus/client_golang/prometheus"
// 	"github.com/prometheus/client_golang/prometheus/promauto"
// )

// responseWriterWrapper envolve o http.ResponseWriter padrão para capturar o HTTP Status Code enviado ao cliente.
type responseWriterWrapper struct {
	http.ResponseWriter
	// TODO: Adicione campos para guardar o `statusCode int` e um booleano `wroteHeader bool`
}

// WriteHeader intercepta a escrita do código de status HTTP antes de delegar ao ResponseWriter original.
func (rw *responseWriterWrapper) WriteHeader(code int) {
	// TODO: [CONCEITO]
	// Estude por que a interface nativa `http.ResponseWriter` do Go não possui um método `Status() int`.
	// Entenda também que se um handler chamar `w.Write([]byte("ok"))` diretamente sem chamar `w.WriteHeader(...)`,
	// o Go assume implicitamente `http.StatusOK` (200).
	//
	// TODO: [PASSO A PASSO]
	// 1. Se o header ainda não foi escrito, salve `code` no campo `statusCode` da struct e marque `wroteHeader = true`.
	// 2. Chame `rw.ResponseWriter.WriteHeader(code)` para enviar de fato na rede.
	//
	// TODO: [ARMADILHA COMUM]
	// - Não inicializar `statusCode` com `http.StatusOK` (200) no construtor do wrapper: se o handler downstream
	//   apenas chamar `w.Write(...)`, `WriteHeader` pode não ser invocado explicitamente pelo seu código e suas
	//   métricas registrarão status `0` no Prometheus!
}

// MetricsCollector agrupa os coletores do Prometheus para o API Gateway.
type MetricsCollector struct {
	// TODO: Defina os coletores do Prometheus:
	// - `requestsTotal *prometheus.CounterVec` (ex: labels "method", "path", "status")
	// - `requestDuration *prometheus.HistogramVec` (ex: labels "method", "path")
}

// NewMetricsCollector inicializa e registra as métricas do Gateway no Prometheus.
func NewMetricsCollector() *MetricsCollector {
	// TODO: [CONCEITO]
	// Estude o método RED de Observabilidade (Rate, Errors, Duration) e a diferença entre
	// `Counter`, `Gauge`, `Histogram` e `Summary` na biblioteca `client_golang` do Prometheus.
	// Estude como funcionam os `Buckets` de um Histograma (`prometheus.DefBuckets`).
	//
	// TODO: [PASSO A PASSO]
	// 1. Instancie `prometheus.NewCounterVec` para contar o total de requisições HTTP (`gateway_http_requests_total`).
	// 2. Instancie `prometheus.NewHistogramVec` para medir a latência em segundos (`gateway_http_request_duration_seconds`).
	// 3. Registre ambos com `prometheus.MustRegister(...)` (ou use `promauto.NewCounterVec` / `promauto.NewHistogramVec`).
	// 4. Retorne `&MetricsCollector{...}`.
	//
	// TODO: [DICA DE PACOTE]
	// - `github.com/prometheus/client_golang/prometheus`: `CounterOpts`, `HistogramOpts`, `DefBuckets`
	// - `github.com/prometheus/client_golang/prometheus/promauto`
	//
	// TODO: [ARMADILHA COMUM]
	// - Explosão de Cardinalidade (High Cardinality): NUNCA coloque valores ilimitados como labels do Prometheus
	//   (ex: IP do cliente, `X-Request-ID`, ou URLs com IDs dinâmicos como `/users/981237`). Cada combinação única
	//   de label cria uma nova série temporal na RAM do Prometheus e pode derrubar o servidor de monitoramento!

	return nil
}

// Middleware mede o tempo de execução, captura o status code e registra métricas e logs estruturados por requisição.
func (mc *MetricsCollector) Middleware(next http.Handler) http.Handler {
	// TODO: [PASSO A PASSO]
	// 1. Retorne um `http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ... })`.
	// 2. Capture o instante inicial: `start := time.Now()`.
	// 3. Envolva o `w` com o seu `&responseWriterWrapper{ResponseWriter: w, statusCode: http.StatusOK}`.
	// 4. Chame `next.ServeHTTP(wrappedWriter, r)`.
	// 5. Calcule a duração: `duration := time.Since(start).Seconds()`.
	// 6. Atualize o `CounterVec` (`WithLabelValues(...).Inc()`) e o `HistogramVec` (`WithLabelValues(...).Observe(duration)`).
	// 7. Emita um log estruturado com `slog.InfoContext` contendo método, rota, status code e latência.
	//
	// TODO: [ARMADILHA COMUM]
	// - Passar o `w` original (`http.ResponseWriter`) para `next.ServeHTTP(w, r)` em vez do `wrappedWriter`:
	//   se fizer isso, o seu wrapper nunca será chamado e o status code será sempre `200`!

	return nil
}
