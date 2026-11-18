package proxy

import (
	"net/http"
)

// Outros imports sugeridos para quando você for implementar:
// import (
// 	"fmt"
// 	"log/slog"
// 	"net/http/httputil"
// 	"net/url"
// 	"strings"
// )

// GatewayProxy encapsula a lógica de Reverse Proxy e roteamento para os serviços de backend (upstreams).
type GatewayProxy struct {
	// TODO: Defina os campos necessários (ex: ponteiro para `httputil.ReverseProxy`, `targetURL *url.URL`, logger, etc.)
}

// NewReverseProxy cria e configura uma nova instância de Reverse Proxy apontando para o serviço de destino.
func NewReverseProxy(targetURL string) (*GatewayProxy, error) {
	// TODO: [CONCEITO]
	// Estude o funcionamento interno do `httputil.ReverseProxy` na standard library do Go:
	// - Como ele clona a requisição original, gerencia conexões TCP/HTTP via `http.RoundTripper` (`Transport`) e faz streaming da resposta.
	// - Qual é a diferença entre customizar o campo `Director` versus o campo mais moderno `Rewrite` (introduzido no Go 1.20 por questões de segurança de headers `X-Forwarded-*`).
	// - Como funciona o `ModifyResponse` e o `ErrorHandler`.
	//
	// TODO: [PASSO A PASSO]
	// 1. Faça o parse da `targetURL` usando `url.Parse`. Valide se o `Scheme` e o `Host` não estão vazios.
	// 2. Crie a instância do proxy reverso (`httputil.NewSingleHostReverseProxy` ou instanciando `&httputil.ReverseProxy{Rewrite: ...}`).
	// 3. Configure a reescrita da requisição:
	//    - Ajuste o header `Host` da requisição de saída para `target.Host` (muitos servidores destino rejeitam a requisição se o header `Host` continuar sendo `localhost:8080`).
	//    - Remova ou reescreva prefixos de rota se necessário (ex: `/api/v1/users` -> `/users`).
	//    - Adicione cabeçalhos de rastreabilidade (`X-Forwarded-Host`, `X-Gateway-Proxy`).
	// 4. Configure um `ErrorHandler` customizado no `ReverseProxy`: se o backend cair ou der timeout, logue o erro com `slog` e retorne `http.StatusBadGateway` (502) com um JSON padronizado.
	// 5. Retorne a struct `&GatewayProxy{...}`.
	//
	// TODO: [DICA DE PACOTE]
	// - `net/url`: `url.Parse`
	// - `net/http/httputil`: `httputil.ReverseProxy`, `httputil.ProxyRequest`
	// - `net/http`: `http.DefaultTransport` (dica avançada: estude como customizar `MaxIdleConnsPerHost` no `http.Transport` para evitar gargalos sob alto tráfego).
	//
	// TODO: [ARMADILHA COMUM]
	// - `NewSingleHostReverseProxy` por padrão NÃO sobrescreve `req.Host` (ele apenas muda `req.URL.Host`). Se você apontar para um serviço externo ou VirtualHost (Nginx/Cloudflare), você receberá erros `400 Bad Request` ou `403 Forbidden` se não fizer `req.Host = target.Host`.
	// - Confiar cegamente no header `X-Forwarded-For` enviado pelo cliente sem limpá-lo (usando `Director` antigo), permitindo IP Spoofing para burlar o Rate Limiter! (O método `Rewrite` do `ReverseProxy` ajuda a evitar isso).

	return nil, nil
}

// ServeHTTP implementa a interface http.Handler, permitindo que GatewayProxy seja usado diretamente em rotas e middlewares.
func (p *GatewayProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// TODO: [PASSO A PASSO]
	// Delegue a execução da requisição `r` e resposta `w` para o seu `httputil.ReverseProxy` interno.
}
