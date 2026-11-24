# Crypto-Gateway — Roadmap de Implementação e Estudo

Bem-vindo ao seu guia de arquitetura para construir o **crypto-gateway** em Go!
A filosofia aqui é **"aprender construindo"**: você é o engenheiro responsável por transformar os esqueletos de código em um API Gateway resiliente, seguro e observável.

Siga as fases na ordem proposta. Não avance para a próxima fase antes de validar todos os itens do checklist com os testes de terminal sugeridos.

---

## Fase 1: Servidor HTTP Base, Contexto e Graceful Shutdown

O objetivo desta fase é subir um servidor HTTP robusto em Go, configurado via variáveis de ambiente, que saiba encerrar suas conexões ativas de forma limpa (sem derrubar requisições em andamento) ao receber um sinal do sistema operacional (`SIGINT` ou `SIGTERM`).

### Conceitos para Estudar
- Pacote `net/http`: Como `http.Server`, `http.ServeMux` e a interface `http.Handler` funcionam por baixo dos panos.
- Pacote `context`: `context.WithTimeout`, `context.WithCancel` e propagação de cancelamento.
- Concorrência e Sinais de SO: `os/signal`, `signal.NotifyContext` (ou canais com `syscall.SIGINT`/`syscall.SIGTERM`).
- Logs estruturados nativos (Go 1.21+): `log/slog`.

### Checklist de Implementação
- [ ] Inicializar o módulo Go na raiz do projeto (`go mod init ...`).
- [ ] Em `internal/config/config.go`: Definir os campos da struct `Config` e implementar `Load()` lendo variáveis de ambiente com valores padrão (default).
- [ ] Em `cmd/gateway/main.go`: Inicializar o logger estruturado (`log/slog`) como JSON ou texto.
- [ ] Em `cmd/gateway/main.go`: Carregar a configuração e criar uma rota simples `/health` que retorna `200 OK` (`{"status": "up"}`).
- [ ] Em `cmd/gateway/main.go`: Configurar `http.Server` com timeouts explícitos (`ReadTimeout`, `WriteTimeout`, `IdleTimeout`).
- [ ] Em `cmd/gateway/main.go`: Implementar **Graceful Shutdown** escutando sinais do SO e chamando `server.Shutdown(ctx)`.

### Como Testar via Terminal (`curl`)
1. Inicie o servidor:
   ```bash
   go run ./cmd/gateway/main.go
   ```
2. Em outro terminal, teste o healthcheck:
   ```bash
   curl -i http://localhost:8080/health
   ```
   *Resultado esperado:* `HTTP/1.1 200 OK` e payload `{"status":"up"}`.
3. Teste o Graceful Shutdown pressionando `Ctrl+C` no terminal do servidor e verifique se a mensagem de log de encerramento limpo aparece sem erros abruptos.

---

## Fase 2: Reverse Proxy Nativo e Roteamento Dinâmico

Agora que o servidor HTTP está de pé, vamos fazê-lo atuar como um verdadeiro Gateway, encaminhando requisições recebidas para serviços de backend (upstreams) e manipulando cabeçalhos de proxy.

### Conceitos para Estudar
- `net/http/httputil`: Especificamente `httputil.ReverseProxy` e `httputil.NewSingleHostReverseProxy`.
- Função `Director` vs `Rewrite` no `ReverseProxy`: Como reescrever URLs, Host headers e adicionar headers como `X-Forwarded-For` e `X-Request-ID`.
- Tratamento de erros de upstream com `ErrorHandler` customizado no `ReverseProxy` (ex: retornar `502 Bad Gateway` formatado em JSON quando o destino estiver fora do ar).

### Checklist de Implementação
- [ ] Em `internal/proxy/reverse_proxy.go`: Implementar o construtor `NewReverseProxy(targetURL string)` fazendo o parse da URL com `url.Parse`.
- [ ] Em `internal/proxy/reverse_proxy.go`: Customizar o `Director` (ou `Rewrite`) para garantir que o header `Host` e `X-Forwarded-Host` sejam configurados corretamente.
- [ ] Em `internal/proxy/reverse_proxy.go`: Configurar o `ErrorHandler` do proxy para capturar falhas de conexão (ex: backend desligado) e responder com `502 Bad Gateway` sem vazar detalhes internos.
- [ ] Em `cmd/gateway/main.go`: Registrar o proxy em uma rota (ex: `/api/`) apontando para um serviço de teste (pode ser `https://httpbin.org` ou um servidor local simples).

### Como Testar via Terminal (`curl`)
1. Suba um backend fake rápido (ou aponte para `http://httpbin.org`):
   ```bash
   # Exemplo usando python para simular um backend na porta 9000 (opcional):
   python3 -m http.server 9000
   ```
2. Faça uma requisição passando pelo Gateway (porta 8080):
   ```bash
   curl -i http://localhost:8080/api/get
   ```
3. Desligue o backend na porta 9000 e repita o `curl`:
   *Resultado esperado:* `HTTP/1.1 502 Bad Gateway` tratado pelo seu `ErrorHandler`.

---

## Fase 3: Camada Criptográfica (Validação HMAC-SHA256 e Manipulação de Request Body)

Nenhum tráfego não autenticado deve chegar ao upstream. Nesta fase, você implementará um middleware que valida a integridade e autenticidade da requisição usando assinatura **HMAC-SHA256** (e opcionalmente decriptografia de payload/headers sensíveis com AES-GCM).

### Conceitos para Estudar
- Padrão de Middleware em Go: `func(next http.Handler) http.Handler`.
- `io.ReadCloser`, `io.ReadAll` e `bytes.NewBuffer`: **Por que ler `r.Body` no middleware consome o stream e faz o proxy enviar um body vazio se você não restaurá-lo?**
- `crypto/hmac`, `crypto/sha256` e `encoding/hex`.
- Timing Attacks e `crypto/subtle`: Uso obrigatório de `subtle.ConstantTimeCompare` ao comparar assinaturas criptográficas.
- Proteção contra Replay Attack: Validação de header `X-Timestamp` dentro de uma janela de tolerância (ex: ±5 minutos).

### Checklist de Implementação
- [ ] Em `internal/middleware/crypto_auth.go`: Extrair os headers `X-Signature` e `X-Timestamp` da requisição.
- [ ] Validar se o `X-Timestamp` é válido e não expirou (evitar Replay Attacks).
- [ ] Ler os bytes de `r.Body` (limitando o tamanho máximo com `http.MaxBytesReader` ou `io.LimitReader`) e **reconstruir** `r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))`.
- [ ] Calcular o HMAC-SHA256 usando a chave secreta + timestamp + body.
- [ ] Comparar a assinatura calculada com `X-Signature` usando `subtle.ConstantTimeCompare`.
- [ ] Plugar o middleware na cadeia de execução em `cmd/gateway/main.go`.

### Como Testar via Terminal (`curl`)
1. Teste sem headers de segurança (deve falhar com `401 Unauthorized`):
   ```bash
   curl -i -X POST http://localhost:8080/api/post -d '{"valor": 100}'
   ```
2. Gere um timestamp e uma assinatura HMAC válida no próprio terminal e envie:
   ```bash
   TS=$(date +%s)
   BODY='{"valor": 100}'
   SECRET="minha-chave-secreta-super-segura"
   # Se o seu formato de assinatura for HMAC(secret, timestamp + "." + body):
   SIG=$(printf "%s.%s" "$TS" "$BODY" | openssl dgst -sha256 -hmac "$SECRET" -hex | awk '{print $2}')

   curl -i -X POST http://localhost:8080/api/post \
     -H "Content-Type: application/json" \
     -H "X-Timestamp: $TS" \
     -H "X-Signature: $SIG" \
     -d "$BODY"
   ```
   *Resultado esperado:* `HTTP/1.1 200 OK` e o backend recebendo o JSON intacto!

---

## Fase 4: Rate Limiting Distribuído com Redis (Token Bucket / Sliding Window)

Para proteger os serviços de backend contra abusos ou ataques DDoS, vamos implementar um Rate Limiter distribuído apoiado no Redis. Assim, mesmo que você rode 10 instâncias do `crypto-gateway`, o limite por cliente/IP será compartilhado de forma atômica.

### Conceitos para Estudar
- Docker Compose: Subir o container do Redis e conectar via `github.com/redis/go-redis/v9`.
- Algoritmos de Rate Limiting: Fixed Window Counter, Sliding Window Log, ou Token Bucket.
- Atomicidade no Redis: Por que fazer `GET` no Go, checar o valor com `if`, e depois fazer `INCR` gera **Race Condition** sob alta concorrência? Como resolver usando `INCR` + `EXPIRE` em pipeline/Lua Script (`redis.NewScript`).

### Checklist de Implementação
- [ ] Em `docker-compose.yml`: Preencher o serviço do `redis` (e `postgres` se desejar persistir clientes/chaves) e subir com `docker compose up -d`.
- [ ] Em `internal/storage/redis.go`: Implementar a conexão com o Redis, configurando timeouts e testando com `Ping(ctx)`.
- [ ] Em `internal/middleware/ratelimit.go`: Identificar o cliente (por IP via `net.SplitHostPort(r.RemoteAddr)`, `X-Forwarded-For` ou API Key).
- [ ] Em `internal/middleware/ratelimit.go`: Executar a lógica de incremento/verificação no Redis respeitando o `r.Context()`.
- [ ] Adicionar headers informativos na resposta (`X-RateLimit-Limit`, `X-RateLimit-Remaining`, `Retry-After`) e retornar `429 Too Many Requests` quando o limite estourar.
- [ ] Encadear o middleware de Rate Limit no `cmd/gateway/main.go`.

### Como Testar via Terminal (`curl`)
1. Suba o Redis:
   ```bash
   docker compose up -d redis
   ```
2. Inicie o Gateway com `RATE_LIMIT_REQUESTS=5` e dispare múltiplas requisições seguidas para `/api/get`:
   ```bash
   for i in {1..10}; do
     curl -s -o /dev/null -w "Req $i: HTTP %{http_code}\n" http://localhost:8080/api/get
   done
   ```
   *Resultado esperado:* As 5 primeiras requisições chegam ao middleware de autenticação (`401` sem HMAC, ou `200` com HMAC válido) e da 6ª em diante retornam `429` diretamente do Rate Limiter (enquanto `/health` continua aberto para healthchecks).

---

## Fase 5: Instrumentação com Prometheus e Logs Estruturados (`log/slog`)

Um API Gateway em produção não pode ser uma caixa-preta. Nesta fase final, vamos instrumentar métricas de latência, throughput e taxa de erros (RED Metrics: Rate, Errors, Duration) para o Prometheus e visualizá-las no Grafana.

### Conceitos para Estudar
- Tipos de métricas no Prometheus (`github.com/prometheus/client_golang/prometheus`):
  - `CounterVec` (total de requisições por rota, método e status code).
  - `HistogramVec` (distribuição de latência das requisições em segundos).
- Wrapper de `http.ResponseWriter`: Como capturar o status code que o handler downstream escreveu (já que a interface nativa `http.ResponseWriter` não expõe um getter `StatusCode()`).
- Cardinalidade de Labels no Prometheus: Por que você **NUNCA** deve colocar IDs dinâmicos (ex: `/users/12345`, IPs ou UUIDs) diretamente como label de métrica?

### Checklist de Implementação
- [ ] Em `internal/middleware/metrics.go`: Criar um struct `responseWriterWrapper` que embute `http.ResponseWriter` e intercepta `WriteHeader(code int)`.
- [ ] Em `internal/middleware/metrics.go`: Registrar os coletores (`CounterVec` e `HistogramVec`) e medir `time.Since(start)` em cada requisição.
- [ ] Em `cmd/gateway/main.go`: Expor o endpoint `/metrics` usando `promhttp.Handler()`.
- [ ] Em `docker-compose.yml`: Configurar os serviços do `prometheus` e `grafana`.
- [ ] Garantir que cada requisição gere um log estruturado (`slog.InfoContext`) contendo `method`, `path`, `status`, `duration_ms` e `request_id`.

### Como Testar via Terminal (`curl`)
1. Gere tráfego no gateway (requisições `200`, `401` e `429`).
2. Consulte o endpoint de métricas:
   ```bash
   curl -s http://localhost:8080/metrics | grep gateway_http
   ```
   *Resultado esperado:* Ver as séries temporais `gateway_http_requests_total` e `gateway_http_request_duration_seconds_bucket` populadas com os respectivos status codes e métodos!
