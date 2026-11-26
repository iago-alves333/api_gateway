# Estágio de build (compilação estática)
FROM golang:1.26-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /gateway ./cmd/gateway

# Estágio de execução (imagem mínima)
FROM alpine:3.20
WORKDIR /app

RUN apk add --no-cache ca-certificates \
    && adduser -D -H -u 10001 gateway

COPY --from=builder --chown=gateway:gateway /gateway /app/gateway

USER gateway
EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=3s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/app/gateway"]
