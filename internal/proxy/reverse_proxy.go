package proxy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// GatewayProxy encapsula a lógica de Reverse Proxy e roteamento para os serviços de backend (upstreams).
type GatewayProxy struct {
	proxy     *httputil.ReverseProxy
	targetURL *url.URL
	logger    *slog.Logger
}

// NewReverseProxy cria e configura uma nova instância de Reverse Proxy apontando para o serviço de destino.
func NewReverseProxy(targetURL string) (*GatewayProxy, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream URL: %w", err)
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("upstream URL must include scheme and host (got %q)", targetURL)
	}

	logger := slog.Default()

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}

	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.SetXForwarded()
		},
		ErrorHandler: func(writer http.ResponseWriter, request *http.Request, err error) {
			logger.Error("Falha ao comunicar com upstream",
				"target", targetURL,
				"path", request.URL.Path,
				"error", err,
			)
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusBadGateway)
			resp := map[string]string{
				"error":   "bad_gateway",
				"message": "Servidor de destino indisponível ou com timeout. Tente novamente mais tarde.",
			}
			_ = json.NewEncoder(writer).Encode(resp)
		},
	}

	return &GatewayProxy{
		proxy:     proxy,
		targetURL: target,
		logger:    logger,
	}, nil
}

// ServeHTTP implementa a interface http.Handler, permitindo que GatewayProxy seja usado diretamente em rotas e middlewares.
func (p *GatewayProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.proxy.ServeHTTP(w, r)
}
