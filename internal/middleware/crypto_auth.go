package middleware

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	headerSignature   = "X-Signature"
	headerTimestamp   = "X-Timestamp"
	defaultMaxBodyLen = 1 << 20 // 1 MiB
)

// CryptoAuth concentra as configurações e chaves da camada de segurança criptográfica do Gateway.
type CryptoAuth struct {
	hmacSecret   []byte
	aesKey       []byte
	tolerance    time.Duration
	maxBodyBytes int64
}

// NewCryptoAuth cria uma nova instância do middleware de autenticação criptográfica.
func NewCryptoAuth(hmacSecret, aesKey string, tolerance time.Duration) (*CryptoAuth, error) {
	if len(hmacSecret) == 0 {
		return nil, errors.New("hmac secret must not be empty")
	}
	if tolerance <= 0 {
		return nil, errors.New("hmac tolerance must be positive")
	}
	if aesKey != "" && len(aesKey) != 16 && len(aesKey) != 24 && len(aesKey) != 32 {
		return nil, fmt.Errorf("aes key must be 16, 24, or 32 bytes (got %d)", len(aesKey))
	}

	return &CryptoAuth{
		hmacSecret:   []byte(hmacSecret),
		aesKey:       []byte(aesKey),
		tolerance:    tolerance,
		maxBodyBytes: defaultMaxBodyLen,
	}, nil
}

// Middleware intercepta a requisição, valida o timestamp e a assinatura HMAC-SHA256 do payload.
func (ca *CryptoAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sigHeader := strings.TrimSpace(r.Header.Get(headerSignature))
		tsHeader := strings.TrimSpace(r.Header.Get(headerTimestamp))
		if sigHeader == "" || tsHeader == "" {
			slog.Warn("Cabeçalhos de autenticação ausentes", "method", r.Method, "path", r.URL.Path)
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Cabeçalhos X-Signature e X-Timestamp são obrigatórios.")
			return
		}

		ts, err := strconv.ParseInt(tsHeader, 10, 64)
		if err != nil {
			slog.Warn("Timestamp inválido", "method", r.Method, "path", r.URL.Path)
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Cabeçalho X-Timestamp inválido.")
			return
		}

		if time.Since(time.Unix(ts, 0)).Abs() > ca.tolerance {
			slog.Warn("Requisição fora da janela de tolerância HMAC", "method", r.Method, "path", r.URL.Path)
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Requisição expirada ou com timestamp inválido.")
			return
		}

		sigBytes, err := hex.DecodeString(sigHeader)
		if err != nil {
			slog.Warn("Assinatura HMAC malformada", "method", r.Method, "path", r.URL.Path)
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Formato de assinatura inválido.")
			return
		}

		var bodyBytes []byte
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, ca.maxBodyBytes)
			bodyBytes, err = io.ReadAll(r.Body)
			_ = r.Body.Close()
			if err != nil {
				var maxBytesErr *http.MaxBytesError
				if errors.As(err, &maxBytesErr) {
					writeJSONError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "Corpo da requisição excede o limite permitido.")
					return
				}
				writeJSONError(w, http.StatusBadRequest, "bad_request", "Falha ao ler o corpo da requisição.")
				return
			}
			// Restaura a stream para que o próximo handler (ex: ReverseProxy) receba o body intacto.
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		expectedMAC := ca.computeHMAC(tsHeader, bodyBytes)
		if subtle.ConstantTimeCompare(expectedMAC, sigBytes) != 1 {
			slog.Warn("Assinatura HMAC inválida", "method", r.Method, "path", r.URL.Path)
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Assinatura criptográfica inválida.")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// computeHMAC calcula o HMAC-SHA256 sobre "<timestamp>.<body>".
func (ca *CryptoAuth) computeHMAC(timestamp string, body []byte) []byte {
	mac := hmac.New(sha256.New, ca.hmacSecret)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return mac.Sum(nil)
}

// DecryptAESGCM decriptografa um payload cifrado com AES-GCM cujo nonce vem prefixado nos primeiros bytes.
func (ca *CryptoAuth) DecryptAESGCM(cipherText []byte) ([]byte, error) {
	if len(ca.aesKey) == 0 {
		return nil, errors.New("aes key not configured")
	}

	block, err := aes.NewCipher(ca.aesKey)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(cipherText) < nonceSize {
		return nil, errors.New("ciphertext shorter than nonce size")
	}

	nonce, encrypted := cipherText[:nonceSize], cipherText[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt aes-gcm: %w", err)
	}

	return plaintext, nil
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   code,
		"message": message,
	})
}
