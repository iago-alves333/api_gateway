#!/usr/bin/env bash
set -euo pipefail

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
HMAC_SECRET="${HMAC_SECRET:-}"
RESERVATION_ID="${RESERVATION_ID:-00000000-0000-0000-0000-000000000001}"
BURST_COUNT="${BURST_COUNT:-15}"

if [[ -z "$HMAC_SECRET" ]]; then
  echo "Erro: defina HMAC_SECRET no ambiente (deve ser a mesma chave configurada no gateway)."
  echo "Exemplo: HMAC_SECRET='sua-chave-secreta-com-32-caracteres-ou-mais' $0"
  exit 1
fi

sign_payload() {
  local ts="$1"
  local body="$2"
  printf "%s.%s" "$ts" "$body" | openssl dgst -sha256 -hmac "$HMAC_SECRET" | awk '{print $NF}'
}

echo "=== 1. Healthcheck do Gateway ==="
curl -sS -w "\nHTTP %{http_code}\n\n" "${GATEWAY_URL}/health"

echo "=== 2. Rota da SPA/Frontend via Proxy (sem HMAC, preservando /api/v1) ==="
curl -sS -D - -o /dev/null "${GATEWAY_URL}/api/v1/attractions" | grep -E "HTTP/|X-Request-ID|X-RateLimit" || true
echo

echo "=== 3. Webhook SEM assinatura HMAC (esperado: 401 no Gateway) ==="
curl -sS -w "\nHTTP %{http_code}\n\n" -X POST \
  -H "Content-Type: application/json" \
  -d '{"event":"payment.approved"}' \
  "${GATEWAY_URL}/api/v1/webhooks/reservations/${RESERVATION_ID}/confirm"

echo "=== 4. Webhook com corpo adulterado / assinatura inválida (esperado: 401 no Gateway) ==="
TS=$(date +%s)
SIG_ORIG=$(sign_payload "$TS" '{"event":"payment.approved","amount":100}')
curl -sS -w "\nHTTP %{http_code}\n\n" -X POST \
  -H "Content-Type: application/json" \
  -H "X-Timestamp: ${TS}" \
  -H "X-Signature: ${SIG_ORIG}" \
  -d '{"event":"payment.approved","amount":999}' \
  "${GATEWAY_URL}/api/v1/webhooks/reservations/${RESERVATION_ID}/confirm"

echo "=== 5. Webhook ASSINADO válido (passa pelo Gateway e chega ao Spring Boot) ==="
BODY='{"event":"payment.approved","provider":"pix"}'
TS=$(date +%s)
SIG=$(sign_payload "$TS" "$BODY")
curl -sS -w "\nHTTP %{http_code} (200 se reserva PENDING existir; 404/409 indica que autenticou no Gateway e chegou ao backend)\n\n" -X POST \
  -H "Content-Type: application/json" \
  -H "X-Timestamp: ${TS}" \
  -H "X-Signature: ${SIG}" \
  -d "$BODY" \
  "${GATEWAY_URL}/api/v1/webhooks/reservations/${RESERVATION_ID}/confirm"

echo "=== 6. Teste de Rajada (${BURST_COUNT} requisições em sequência — esperado: 429 após atingir GATEWAY_RATE_LIMIT_REQUESTS) ==="
for i in $(seq 1 "$BURST_COUNT"); do
  code=$(curl -s -o /dev/null -w "%{http_code}" "${GATEWAY_URL}/api/v1/attractions")
  printf "Req %02d: HTTP %s\n" "$i" "$code"
done
echo

echo "=== 7. Métricas RED expostas para o Prometheus (/metrics) ==="
curl -sS "${GATEWAY_URL}/metrics" | grep -E "^gateway_http_(requests_total|request_duration_seconds_count)" || true
