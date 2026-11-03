package agentlink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// ServerConfig configures a server.
type ServerConfig struct {
	// Addr is the address to listen on ("127.0.0.1:7777"). An empty
	// address means the loopback default.
	Addr string
	// Token is the bearer token the server demands. Empty mints one.
	Token string
	// ReadTimeout bounds a single frame's read. Zero uses the default.
	ReadTimeout time.Duration
	// Log receives server diagnostic and request logs. Optional.
	Log io.Writer
	// Handlers registers extra HTTP routes on the server's mux.
	Handlers map[string]http.HandlerFunc
}

// Server serves the protocol over WebSocket.
//
// The server is transport-only: it frames and unframes the protocol and
// hands the connection to a Handler. What a turn *is* belongs to the
// handler.
type Server struct {
	cfg   ServerConfig
	token string

	mu       sync.Mutex
	listener net.Listener
	http     *http.Server
}

// Handler serves one client connection.
//
// Serve returns when the connection ends. The handler owns the frames it
// reads and the frames it writes; the server does not interpret them.
type Handler interface {
	Serve(ctx context.Context, conn *Conn) error
}

// HandlerFunc adapts a function to the Handler interface.
type HandlerFunc func(ctx context.Context, conn *Conn) error

// Serve calls f.
func (f HandlerFunc) Serve(ctx context.Context, conn *Conn) error { return f(ctx, conn) }

// NewServer returns a server. The token is minted when the configuration
// leaves it empty, so a server always demands one.
func NewServer(cfg ServerConfig) (*Server, error) {
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		minted, err := MintToken()
		if err != nil {
			return nil, err
		}
		token = minted
	}
	return &Server{cfg: cfg, token: token}, nil
}

// Token is the bearer token the server demands.
func (s *Server) Token() string { return s.token }

// Addr is the address the server listens on. It is empty until the
// listener is open.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Listen opens the server's listener and returns it.
//
// The listener is opened separately from the serving so a caller that
// asked for port zero can learn the port the kernel picked before any
// client is answered.
func (s *Server) Listen() (net.Listener, error) {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("agentlink: cannot listen on %s: %w", s.cfg.Addr, err)
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	return ln, nil
}

// ListenAndServe opens the listener and serves until the context is
// cancelled.
func (s *Server) ListenAndServe(ctx context.Context, h Handler) error {
	ln, err := s.Listen()
	if err != nil {
		return err
	}
	return s.Serve(ctx, ln, h)
}

// Serve serves on an existing listener until the context is cancelled.
func (s *Server) Serve(ctx context.Context, ln net.Listener, h Handler) error {
	s.mu.Lock()
	if s.listener == nil {
		s.listener = ln
	}
	s.mu.Unlock()

	mux := http.NewServeMux()
	for p, fn := range s.cfg.Handlers {
		mux.HandleFunc(p, fn)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.serveWS(w, r, h)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	s.mu.Lock()
	s.http = srv
	s.mu.Unlock()

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// serveWS upgrades one request and hands the connection to the handler.
//
// The token is checked before the upgrade, so a client without the right
// one never reaches a websocket.
func (s *Server) serveWS(w http.ResponseWriter, r *http.Request, h Handler) {
	if !s.authorized(r) {
		if s.cfg.Log != nil {
			fmt.Fprintf(s.cfg.Log, "[agentlink %s] rejected unauthorized connection from %s\n", time.Now().Format("15:04:05"), r.RemoteAddr)
		}
		http.Error(w, "agentlink: missing or invalid "+TokenHeader, http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		if s.cfg.Log != nil {
			fmt.Fprintf(s.cfg.Log, "[agentlink %s] websocket upgrade failed from %s: %v\n", time.Now().Format("15:04:05"), r.RemoteAddr, err)
		}
		// Accept already wrote the HTTP error response.
		return
	}
	conn.SetReadLimit(MaxFrameBytes)
	c := &Conn{conn: conn}
	defer c.Close()
	if s.cfg.Log != nil {
		fmt.Fprintf(s.cfg.Log, "[agentlink %s] client connected from %s\n", time.Now().Format("15:04:05"), r.RemoteAddr)
		defer fmt.Fprintf(s.cfg.Log, "[agentlink %s] client disconnected from %s\n", time.Now().Format("15:04:05"), r.RemoteAddr)
	}
	_ = h.Serve(r.Context(), c)
}

// authorized reports whether the request carries the server's token.
//
// A request with no token at all is rejected: the server runs shell
// commands against its own workspace, so an unauthenticated peer is never
// welcome.
func (s *Server) authorized(r *http.Request) bool {
	got := r.Header.Get(TokenHeader)
	if got == "" {
		return false
	}
	return tokenMatches(s.token, strings.TrimPrefix(got, "Bearer "))
}

// Conn is the server side of one client connection.
type Conn struct {
	conn *websocket.Conn

	writeMu sync.Mutex

	closeOnce sync.Once
	closeErr  error
}

// Send writes one frame to the client.
func (c *Conn) Send(ctx context.Context, f Frame) error {
	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("agentlink: cannot encode a %s frame: %w", f.Kind, err)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("agentlink: cannot send a %s frame: %w", f.Kind, err)
	}
	return nil
}

// Next reads the next frame from the client.
//
// It returns io.EOF when the client closed the connection, and the
// transport's error when the connection broke.
func (c *Conn) Next(ctx context.Context) (Frame, error) {
	_, data, err := c.conn.Read(ctx)
	if err != nil {
		return Frame{}, err
	}
	var f Frame
	if err := json.Unmarshal(data, &f); err != nil {
		return Frame{}, fmt.Errorf("agentlink: cannot decode a frame: %w", err)
	}
	return f, nil
}

// Close closes the connection. It is safe to call more than once.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.conn.Close(websocket.StatusNormalClosure, "")
	})
	return c.closeErr
}
