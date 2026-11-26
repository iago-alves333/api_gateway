# Crypto-Gateway

API Gateway em Go (`net/http`, `httputil.ReverseProxy`, Redis + Lua Script, Prometheus e `log/slog`) construído do zero para atuar na borda de APIs backend (como o projeto **Titureco — Backend de Atrações Turísticas**).

## Arquitetura e Cadeia de Middlewares

```text
Cliente / SPA ──(JWT + CORS /api/v1/*)────────┐
                                              ▼
                                    ┌───────────────────┐
Parceiro M2M  ──(HMAC /api/v1/webhooks/*)──▶ Crypto-Gateway (:8080)
                                    └─────────┬─────────┘
                                              │
          ┌───────────────────────────────────┼───────────────────────────────────┐
          ▼                                   ▼                                   ▼
   [Metrics & slog]                  [Rate Limit (Redis)]                 [Reverse Proxy]
   X-Request-ID + RED                Lua INCR+PEXPIRE+PTTL               http://backend:8080
                                      (Fail-Open)                        (rede interna)
```

### Roteamento (Modo Híbrido vs. Estrito)

- **Padrão (`HMAC_PROTECT_ALL=false`):**
  - `/api/v1/webhooks/`: exige `X-Timestamp` e `X-Signature` (**HMAC-SHA256**), além de Rate Limit e Métricas. Ideal para webhooks e integrações serviço-a-serviço (M2M), onde o segredo nunca é exposto ao navegador.
  - `/api/`: aplica Métricas/Logs e Rate Limit, encaminhando requisições e preflights `OPTIONS` (CORS) para o upstream validar JWT do usuário final.
- **Modo estrito (`HMAC_PROTECT_ALL=true`):** exige HMAC-SHA256 em **todas** as rotas sob `/api/`.
- **`/health` e `/metrics`:** endpoints operacionais do próprio gateway (fora do proxy).

---

## Variáveis de Ambiente

Definidas e validadas em [`internal/config/config.go`](internal/config/config.go):

| Variável | Descrição | Padrão |
|---|---|---|
| `SERVER_PORT` | Porta HTTP do gateway | `8080` |
| `UPSTREAM_URL` | URL do serviço backend de destino | `http://localhost:8081` |
| `REDIS_ADDR` | Endereço do Redis (`host:port`) | `localhost:6379` |
| `REDIS_PASSWORD` | Senha do Redis (opcional) | vazio |
| `REDIS_DB` | Índice do banco no Redis (ex.: `1` para isolar do backend) | `0` |
| `HMAC_SECRET` | Chave secreta HMAC-SHA256 (**obrigatória, mín. 32 chars**) | — |
| `HMAC_TOLERANCE` | Janela máxima aceita para `X-Timestamp` (anti-replay) | `5m` |
| `HMAC_PROTECT_ALL` | Se `true`, exige HMAC também em `/api/` além de `/api/v1/webhooks/` | `false` |
| `AES_KEY` | Chave AES opcional (`16`, `24` ou `32` bytes) para `DecryptAESGCM` | vazio |
| `STRIP_PREFIX` | Prefixo removido antes de encaminhar ao upstream (ex.: `/api`) | vazio |
| `RATE_LIMIT_REQUESTS` | Máximo de requisições por IP na janela | `100` |
| `RATE_LIMIT_WINDOW` | Duração da janela de rate limit | `1m` |
| `READ_TIMEOUT` | Timeout de leitura do `http.Server` | `5s` |
| `WRITE_TIMEOUT` | Timeout de escrita do `http.Server` | `10s` |
| `IDLE_TIMEOUT` | Timeout de conexões keep-alive | `30s` |

---

## Como o Rate Limiter se Comporta Sob Carga

1. **Identificação do cliente:** extrai o IP da conexão TCP (`r.RemoteAddr`, removendo a porta efêmera) sem confiar cegamente em `X-Forwarded-For` na borda ([`extractClientIP`](internal/middleware/ratelimit.go)).
2. **Atomicidade no Redis:** executa um Lua Script (`INCR` + `PEXPIRE` + `PTTL`) em [`internal/storage/redis.go`](internal/storage/redis.go). Como o script roda atomicamente no servidor Redis, múltiplas réplicas do gateway compartilham o contador sem *race condition* e nenhuma chave fica presa sem TTL.
3. **Respostas:** adiciona `X-RateLimit-Limit` e `X-RateLimit-Remaining` em todas as chamadas; ao exceder, responde `429 Too Many Requests` com `Retry-After` em segundos antes mesmo de acionar HMAC ou proxy.
4. **Estratégia Fail-Open:** se o Redis oscilar ou ficar indisponível, o erro é logado e a requisição segue para não transformar o rate limiter auxiliar em ponto único de falha.

---

## Executando Integrado com o Titureco (Docker Compose)

No repositório `backend_atracao_turisticas`, o `docker-compose.yml` sobe toda a stack na mesma rede interna (`gateway` na porta `8080`, `backend` apenas com `expose: 8080`, `db`, `redis`, `prometheus` e `grafana`):

```bash
# No diretório backend_atracao_turisticas:
# 1. Garanta HMAC_SECRET (>= 32 chars) no .env
docker compose up -d --build
```

---

## Testando Fluxos (`curl` e Script de Demonstração)

### Script completo de demonstração

```bash
# Para ver 429 rapidamente, suba com GATEWAY_RATE_LIMIT_REQUESTS=5 (ou use BURST_COUNT=105)
HMAC_SECRET="sua-chave-secreta-com-pelo-menos-32-caracteres" ./scripts/demo-gateway.sh
```

### Requisição assinada manualmente com `curl`

O HMAC é calculado sobre `"<timestamp>.<body>"`:

```bash
TS=$(date +%s)
BODY='{"event":"payment.approved","provider":"pix"}'
SECRET="sua-chave-secreta-com-pelo-menos-32-caracteres"
SIG=$(printf "%s.%s" "$TS" "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $NF}')

curl -i -X POST "http://localhost:8080/api/v1/webhooks/reservations/<UUID_RESERVA>/confirm" \
  -H "Content-Type: application/json" \
  -H "X-Timestamp: $TS" \
  -H "X-Signature: $SIG" \
  -d "$BODY"
```

### Teste de rajada (`429 Too Many Requests`) e logs JSON (`slog`)

```bash
for i in $(seq 1 15); do
  curl -s -o /dev/null -w "Req $i: HTTP %{http_code}\n" http://localhost:8080/api/v1/attractions
done

# Logs estruturados em JSON emitidos pelo gateway:
docker logs -f titureco_gateway
```

---

## Decisões de Engenharia (Trade-offs)

- **Ler e restaurar `r.Body` ([`crypto_auth.go`](internal/middleware/crypto_auth.go)):** `r.Body` é um `io.ReadCloser` (stream de leitura única). Ler os bytes com `http.MaxBytesReader` (limite de 1 MiB contra *memory exhaustion*) para calcular o HMAC consome o stream; restaurá-lo com `io.NopCloser(bytes.NewReader(bodyBytes))` garante que o `ReverseProxy` envie o payload intacto ao upstream.
- **Comparação em tempo constante (`subtle.ConstantTimeCompare`):** comparar strings/slices com `==` retorna no primeiro byte divergente, vazando informação via *timing attack*. A comparação em tempo constante elimina esse canal lateral.
- **Janela temporal (`X-Timestamp`):** sem timestamp assinado no cálculo do HMAC, um atacante poderia reenviar infinitamente uma requisição válida capturada (*replay attack*).
- **`Shutdown` com contexto separado ([`main.go`](cmd/gateway/main.go)):** o `ctx` criado com `signal.NotifyContext` já está cancelado quando `SIGINT`/`SIGTERM` chega. Passar esse mesmo `ctx` para `srv.Shutdown` faria o encerramento abortar imediatamente sem drenar requisições em voo; por isso cria-se um novo `context.WithTimeout(context.Background(), 10*time.Second)`.
- **Baixa cardinalidade no Prometheus ([`metrics.go`](internal/middleware/metrics.go)):** a label `path` usa `r.Pattern` do `http.ServeMux` (ex.: `/api/`, `/api/v1/webhooks/`) em vez de `r.URL.Path`, evitando explosão de séries temporais com UUIDs/IDs dinâmicos.
- **Autenticação híbrida na borda:** HMAC-SHA256 protege chamadas M2M (`/api/v1/webhooks/`), enquanto o tráfego de navegador (`/api/`) usa JWT + CORS no Spring Boot — evitando expor `HMAC_SECRET` no bundle JavaScript da SPA.
