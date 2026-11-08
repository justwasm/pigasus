package mockremoteagent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Option configures Server.
type Option func(*Server)

// WithAddr configures the bind address (e.g. "127.0.0.1:0").
func WithAddr(addr string) Option {
	return func(s *Server) {
		s.addr = addr
	}
}

// CustomTurnHandler is the function type for custom agent turn execution.
type CustomTurnHandler func(ctx context.Context, session *CascadeSession, userPrompt string)

// WithCustomTurnHandler configures a custom agent turn handler function.
func WithCustomTurnHandler(fn CustomTurnHandler) Option {
	return func(s *Server) {
		s.service.CustomTurnHandler = fn
	}
}

// WithRequestLogger configures a logger callback for every HTTP request received.
func WithRequestLogger(fn func(r *http.Request, method string, body []byte)) Option {
	return func(s *Server) {
		s.service.RequestLogger = fn
	}
}

// Server is a local mock server for google.gca.aicode.v1main.JetskiRemoteAgentProxyService.
type Server struct {
	addr       string
	listener   net.Listener
	httpServer *http.Server
	cascades   *CascadeManager
	service    *Service
	wg         sync.WaitGroup
}

// NewServer starts a new mock remote agent server.
func NewServer(opts ...Option) (*Server, error) {
	cm := NewCascadeManager()
	svc := NewService(cm)

	s := &Server{
		addr:     "127.0.0.1:0", // default random loopback port
		cascades: cm,
		service:  svc,
	}

	for _, opt := range opts {
		opt(s)
	}

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return nil, fmt.Errorf("mockremoteagent: listen %s: %w", s.addr, err)
	}
	s.listener = ln
	s.addr = ln.Addr().String()

	mux := http.NewServeMux()
	// Catch all paths under /google.gca.aicode.v1main.JetskiRemoteAgentProxyService/
	mux.Handle("/google.gca.aicode.v1main.JetskiRemoteAgentProxyService/", svc)
	// Catch all paths under /JetskiRemoteAgentProxyService/
	mux.Handle("/JetskiRemoteAgentProxyService/", svc)
	// Catch root and REST aliases
	mux.Handle("/", svc)

	s.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		_ = s.httpServer.Serve(ln)
	}()

	return s, nil
}

// URL returns the base HTTP URL of the server (e.g. "http://127.0.0.1:54321").
func (s *Server) URL() string {
	return fmt.Sprintf("http://%s", s.listener.Addr().String())
}

// Addr returns the listener address.
func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

// Cascades returns the internal CascadeManager.
func (s *Server) Cascades() *CascadeManager {
	return s.cascades
}

// Close gracefully stops the server.
func (s *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := s.httpServer.Shutdown(ctx)
	_ = s.listener.Close()
	s.wg.Wait()
	return err
}
