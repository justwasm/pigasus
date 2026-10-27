// Package lsserver — server.go: the two listeners the language
// server opens.
//
// The reference opens two:
//
//	Language server listening on random port at <n> for HTTPS (gRPC)
//	Language server listening on random port at <n> for HTTP
//
// The IDE talks to the HTTPS one — it loads the page from
// `https://127.0.0.1:<port>/` (out/ide-language-server/ide-js/dist/
// constants.js) and speaks Connect-RPC to it (@connectrpc/
// connect-node is the IDE's own client dependency). The HTTP one
// serves the same service in the clear, which is what the Web UI's
// own tooling and the clone's reference proxy use.
//
// Both listeners answer the same routes: the Connect-RPC service, the
// WebSocket endpoint, the web UI and `/healthz`.
package lsserver

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/justwasm/pigasus/remotetransport"
)

// ServerOptions is the wiring the two listeners need.
type ServerOptions struct {
	// HTTPAddr is the plain listener's bind address.
	HTTPAddr string
	// HTTPSAddr is the TLS listener's bind address.
	HTTPSAddr string
	// WebUI serves the web UI. It is never nil: outside the hub
	// mode the reference answers 404 on every path but the service
	// itself, and the notFound handler is that answer.
	WebUI http.Handler
	// Healthz answers the /healthz route.
	Healthz http.Handler
	// CSRFToken is the secret the transport's gate asks for. Empty
	// means "no secret" — which is what the reference does when the
	// IDE does not pass one.
	CSRFToken string
	// RequireTokenOnLoopback makes the secret mandatory for every
	// peer, the loopback interface included.
	//
	// The reference's language server asks the secret of the service
	// calls from every peer, loopback included (out/langserver/
	// answers.json: a call without the secret is answered
	// `{"code":"unauthenticated","message":"unauthenticated: missing
	// CSRF token"}`).
	RequireTokenOnLoopback bool
	// AllowUnauthenticatedWebUI serves the page without the secret.
	//
	// The reference serves its page to every peer and asks the secret
	// of the service calls instead — the IDE loads the page from
	// `https://127.0.0.1:<port>/` without a secret.
	AllowUnauthenticatedWebUI bool
	// ReferenceTokenGate reads the secret from the one header the
	// reference reads it from and answers a denied request with the
	// reference's Connect-RPC error body.
	ReferenceTokenGate bool
}

// Servers are the two running listeners.
type Servers struct {
	// HTTP is the plain listener. It owns the transport and with it
	// the WebSocket endpoint and the Connect router.
	HTTP *remotetransport.TCPServer
	// HTTPS is the TLS listener. It serves the HTTP listener's
	// handler, so both ports answer the same routes.
	HTTPS *http.Server
	// HTTPSListener is the bound TLS listener.
	HTTPSListener net.Listener
}

// StartServers brings both listeners up.
//
// The HTTP listener comes up first: the reference prints its port
// before the HTTPS one, and the discovery file carries both.
func StartServers(ctx context.Context, disp remotetransport.Dispatcher, opts ServerOptions) (*Servers, error) {
	httpSrv, err := remotetransport.NewTCPServer(ctx, disp, remotetransport.TCPConfig{
		Addr:                      opts.HTTPAddr,
		Path:                      "/rpc",
		WebUIHandler:              opts.WebUI,
		HealthzHandler:            opts.Healthz,
		CSRFToken:                 opts.CSRFToken,
		RequireTokenOnLoopback:    opts.RequireTokenOnLoopback,
		AllowUnauthenticatedWebUI: opts.AllowUnauthenticatedWebUI,
		ReferenceTokenGate:        opts.ReferenceTokenGate,
	})
	if err != nil {
		return nil, fmt.Errorf("lsserver: listen %s: %w", opts.HTTPAddr, err)
	}
	tlsConfig, err := SelfSignedTLSConfig()
	if err != nil {
		_ = httpSrv.Shutdown(context.Background())
		return nil, err
	}
	listener, err := net.Listen("tcp", opts.HTTPSAddr)
	if err != nil {
		_ = httpSrv.Shutdown(context.Background())
		return nil, fmt.Errorf("lsserver: listen %s: %w", opts.HTTPSAddr, err)
	}
	tlsListener := tls.NewListener(listener, tlsConfig)
	httpsSrv := &http.Server{
		Handler:           httpSrv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		_ = httpsSrv.Serve(tlsListener)
	}()
	return &Servers{HTTP: httpSrv, HTTPS: httpsSrv, HTTPSListener: tlsListener}, nil
}

// HTTPPort is the plain listener's port.
func (s *Servers) HTTPPort() int { return portOf(s.HTTP.Addr()) }

// HTTPSPort is the TLS listener's port.
func (s *Servers) HTTPSPort() int { return portOf(s.HTTPSListener.Addr()) }

// Shutdown stops both listeners.
func (s *Servers) Shutdown(ctx context.Context) error {
	var firstErr error
	if s.HTTPS != nil {
		if err := s.HTTPS.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.HTTP != nil {
		if err := s.HTTP.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// portOf returns the port of an address.
func portOf(addr net.Addr) int {
	if addr == nil {
		return 0
	}
	_, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return 0
	}
	n := 0
	for _, c := range port {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
