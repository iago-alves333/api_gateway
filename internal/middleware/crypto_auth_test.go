package middleware

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCryptoAuthMiddleware(t *testing.T) {
	const secret = "super-secret-key-with-32-bytes!!"
	auth, err := NewCryptoAuth(secret, "", 5*time.Minute)
	if err != nil {
		t.Fatalf("NewCryptoAuth failed: %v", err)
	}

	var downstreamBody string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("downstream failed to read body: %v", err)
		}
		downstreamBody = string(b)
		w.WriteHeader(http.StatusOK)
	})
	handler := auth.Middleware(next)

	sign := func(ts string, body []byte) string {
		return hex.EncodeToString(auth.computeHMAC(ts, body))
	}

	t.Run("valid signature and body restored for downstream", func(t *testing.T) {
		downstreamBody = ""
		body := `{"valor": 100}`
		ts := strconv.FormatInt(time.Now().Unix(), 10)

		req := httptest.NewRequest(http.MethodPost, "/post", strings.NewReader(body))
		req.Header.Set(headerTimestamp, ts)
		req.Header.Set(headerSignature, sign(ts, []byte(body)))

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		if downstreamBody != body {
			t.Fatalf("expected downstream body %q, got %q", body, downstreamBody)
		}
	})

	t.Run("missing headers returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/post", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("expired or future timestamp returns 401", func(t *testing.T) {
		for _, offset := range []time.Duration{-10 * time.Minute, 10 * time.Minute} {
			body := []byte(`{"a":1}`)
			ts := strconv.FormatInt(time.Now().Add(offset).Unix(), 10)

			req := httptest.NewRequest(http.MethodPost, "/post", bytes.NewReader(body))
			req.Header.Set(headerTimestamp, ts)
			req.Header.Set(headerSignature, sign(ts, body))

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("offset %v: expected 401, got %d", offset, rec.Code)
			}
		}
	})

	t.Run("tampered body returns 401", func(t *testing.T) {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req := httptest.NewRequest(http.MethodPost, "/post", strings.NewReader(`{"valor": 999}`))
		req.Header.Set(headerTimestamp, ts)
		req.Header.Set(headerSignature, sign(ts, []byte(`{"valor": 100}`)))

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("oversized body returns 413", func(t *testing.T) {
		smallLimitAuth, err := NewCryptoAuth(secret, "", 5*time.Minute)
		if err != nil {
			t.Fatalf("NewCryptoAuth failed: %v", err)
		}
		smallLimitAuth.maxBodyBytes = 8

		body := []byte("0123456789")
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req := httptest.NewRequest(http.MethodPost, "/post", bytes.NewReader(body))
		req.Header.Set(headerTimestamp, ts)
		req.Header.Set(headerSignature, sign(ts, body))

		rec := httptest.NewRecorder()
		smallLimitAuth.Middleware(next).ServeHTTP(rec, req)

		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413, got %d", rec.Code)
		}
	})
}

func TestDecryptAESGCM(t *testing.T) {
	const aesKey = "12345678901234567890123456789012" // 32 bytes (AES-256)
	auth, err := NewCryptoAuth("super-secret-key-with-32-bytes!!", aesKey, 5*time.Minute)
	if err != nil {
		t.Fatalf("NewCryptoAuth failed: %v", err)
	}

	block, err := aes.NewCipher([]byte(aesKey))
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}

	nonce := bytes.Repeat([]byte{0x01}, gcm.NonceSize())
	plain := []byte("segredo-protegido")
	sealed := gcm.Seal(nonce, nonce, plain, nil)

	got, err := auth.DecryptAESGCM(sealed)
	if err != nil {
		t.Fatalf("DecryptAESGCM failed: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("expected %q, got %q", plain, got)
	}

	if _, err := auth.DecryptAESGCM([]byte("short")); err == nil {
		t.Fatal("expected error for ciphertext shorter than nonce")
	}
}
