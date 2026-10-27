// Package language_server / server.go — gRPC server lifecycle
// (RegisterServer / Serve / Stream).
//
// The recovered antigravity 1.3.5 binary defines the
// google3/third_party/jetski/language_server_pb.LanguageServerService
// as the Connect-RPC / gRPC service that fronts every language server
// RPC. The recovered build:
//
//   - Defines a Server type that holds the routing client, host bridge,
//     audio/narration session managers, telemetry client, and
//     turn-attribution tracker.
//   - RegisterServer registers the service with an external gRPC
//     /Connect dispatcher (mirrors
//     google3/third_party/jetski/language_server_pb.RegisterLanguageServerServiceServer
//     in the upstream package).
//   - Serve runs the HTTP/2 server on a listener and blocks until
//     ctx.Done().
//   - Stream returns a server-side stream wrapper for the streaming
//     RPCs (StreamAgentUpdates, StreamCascadeUpdates).
//   - The 7-RPC handler set is implemented in handler.go as method
//     stubs (5+ of them: StartCascade, SendUserCascadeMessage,
//     CancelCascade, SkipBrowserSubagent, GetAgentInstance,
//     StreamAgentUpdates, Shutdown).
//
// Integration with grpc/: the LanguageServerService
// descriptor in grpc/ is the v4 unified surface; this
// package's Server struct is the v1.3.5 language_server process. The
// antigravity TUI / IDE / CLI dispatch to either via the
// grpc AllServices list.
package language_server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Recovered top-level entry points (pclntab:
// google3/third_party/jetski/language_server/language_server.<Func>).
// ---------------------------------------------------------------------------

// CreateLanguageServerAndServe is the recovered main entry point of
// the language server process. In v1.3.5 it does:
//  1. Set up the metadata provider, telemetry error reporter,
//     tracing, and Unleash client.
//  2. Set up the LSP / file watcher, MCP manager, workspace
//     manager, trajectory saver, and trajectory manager.
//  3. Set up installation ID, sec plugins.
//  4. Set up the server port listeners (gRPC + admin HTTP).
//  5. Block on a context done channel.
//
// The recovered build reduces this to: build a Server, start the
// HTTP/2 listener, block on ctx.Done().
func CreateLanguageServerAndServe(ctx context.Context, cfg *LanguageServerConfig) (*Server, error) {
	if cfg == nil {
		cfg = &LanguageServerConfig{Port: 0, MaxConcurrentRPCs: 64, RPCTimeout: 60 * time.Second}
	}
	srv := NewServer(cfg)
	if err := srv.Serve(ctx); err != nil {
		return srv, err
	}
	return srv, nil
}

// NewServer is the recovered Server constructor.
func NewServer(cfg *LanguageServerConfig) *Server {
	if cfg == nil {
		cfg = &LanguageServerConfig{}
	}
	return &Server{
		config:    cfg,
		router:    NewRoutingClient(""),
		host:      NewConnectHostBridge(""),
		attrib:    &turnAttributionTracker{},
		telemetry: NewBAICTelemetryClient(""),
		audioMgr:  NewAudioSessionManager(),
		narration: NewNarrationSessionManager(),
	}
}

// ---------------------------------------------------------------------------
// Service registration (mirrors the upstream
// google3/third_party/jetski/language_server_pb.RegisterLanguageServerServiceServer).
// ---------------------------------------------------------------------------

// LanguageServerServiceHandler is the recovered handler interface that
// the upstream package's RegisterLanguageServerServiceServer would
// consume. We expose it directly so the antigravity TUI / CLI can
// pass a custom handler.
type LanguageServerServiceHandler interface {
	StartCascade(ctx context.Context, req *StartCascadeRequest) (*StartCascadeResponse, error)
	SendUserCascadeMessage(ctx context.Context, req *SendUserCascadeMessageRequest) (*SendUserCascadeMessageResponse, error)
	CancelCascade(ctx context.Context, req *CancelCascadeRequest) (*CancelCascadeResponse, error)
	SkipBrowserSubagent(ctx context.Context, req *SkipBrowserSubagentRequest) (*SkipBrowserSubagentResponse, error)
	GetAgentInstance(ctx context.Context, req *GetAgentInstanceRequest) (*GetAgentInstanceResponse, error)
	StreamAgentUpdates(req *StreamAgentUpdatesRequest, stream LanguageServerService_StreamAgentUpdatesServer) error
	Shutdown(ctx context.Context, req *ShutdownRequest) (*ShutdownResponse, error)
}

// RegisterServer wires the Server into a gRPC/Connect handler
// registry. The recovered build mirrors the upstream
// RegisterLanguageServerServiceServer signature exactly:
//
//	func RegisterLanguageServerServiceServer(s grpc.ServiceRegistrar, srv LanguageServerServiceServer)
//
// Here we use a simpler registry abstraction: pass a *Server
// directly, and RegisterServer returns an http.Handler that
// dispatches the 7 RPCs.
func RegisterServer(srv *Server) http.Handler {
	if srv == nil {
		panic("language_server: RegisterServer called with nil Server")
	}
	mux := http.NewServeMux()
	registerCascadeRoutes(mux, srv)
	registerAgentRoutes(mux, srv)
	registerSystemRoutes(mux, srv)
	return mux
}

func registerCascadeRoutes(mux *http.ServeMux, srv *Server) {
	mux.HandleFunc("/"+ServiceName+"/StartCascade", func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeStartCascadeRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := srv.StartCascade(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		encodeStartCascadeResponse(w, resp)
	})
	mux.HandleFunc("/"+ServiceName+"/SendUserCascadeMessage", func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeSendUserCascadeMessageRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := srv.SendUserCascadeMessage(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		encodeSendUserCascadeMessageResponse(w, resp)
	})
	mux.HandleFunc("/"+ServiceName+"/CancelCascade", func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeCancelCascadeRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := srv.CancelCascade(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		encodeCancelCascadeResponse(w, resp)
	})
}

func registerAgentRoutes(mux *http.ServeMux, srv *Server) {
	mux.HandleFunc("/"+ServiceName+"/SkipBrowserSubagent", func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeSkipBrowserSubagentRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := srv.SkipBrowserSubagent(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		encodeSkipBrowserSubagentResponse(w, resp)
	})
	mux.HandleFunc("/"+ServiceName+"/GetAgentInstance", func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeGetAgentInstanceRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := srv.GetAgentInstance(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		encodeGetAgentInstanceResponse(w, resp)
	})
	mux.HandleFunc("/"+ServiceName+"/StreamAgentUpdates", func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeStreamAgentUpdatesRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		stream := newStreamResponseWriter(w)
		if err := srv.StreamAgentUpdates(req, stream); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}

func registerSystemRoutes(mux *http.ServeMux, srv *Server) {
	mux.HandleFunc("/"+ServiceName+"/Shutdown", func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeShutdownRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := srv.Shutdown(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		encodeShutdownResponse(w, resp)
	})
}

// RegisterServerWithRegistrar is the recovered convenience for
// callers that want to pass a custom gRPC-style registrar. The
// recovered registrar is a simple interface (analogous to
// google.golang.org/grpc.ServiceRegistrar).
type ServiceRegistrar interface {
	RegisterService(desc ServiceDesc, impl any)
}

// ServiceDesc is the recovered service descriptor.
type ServiceDesc struct {
	ServiceName string
	HandlerType any
	Methods     []MethodDesc
	Streams     []StreamDesc
	Metadata    string
}

// MethodDesc is the recovered method descriptor.
type MethodDesc struct {
	MethodName string
	Handler    func(srv any, ctx context.Context, dec func(any) error, interceptor UnaryServerInterceptor) (any, error)
}

// StreamDesc is the recovered stream descriptor.
type StreamDesc struct {
	StreamName    string
	Handler       func(srv any, stream ServerStream) error
	ServerStreams bool
	ClientStreams bool
}

// UnaryServerInterceptor is the recovered interceptor signature.
type UnaryServerInterceptor func(ctx context.Context, req any, info *UnaryServerInfo, handler UnaryHandler) (any, error)

// UnaryHandler is the recovered handler signature.
type UnaryHandler func(ctx context.Context, req any) (any, error)

// UnaryServerInfo is the recovered info struct.
type UnaryServerInfo struct {
	Server     any
	FullMethod string
}

// ServerStream is the recovered server-stream interface.
type ServerStream interface {
	Context() context.Context
	SendMsg(any) error
	RecvMsg(any) error
	SetHeader(http.Header)
	SendHeader(http.Header) error
	SetTrailer(http.Header)
	SendAndClose(any) error
}

// RegisterServerWithRegistrar registers the Server with a custom
// gRPC-style registrar. This is the most generic entry point; the
// antigravity TUI / IDE / CLI usually go through RegisterServer (the
// http.Handler variant) instead.
func RegisterServerWithRegistrar(r ServiceRegistrar, srv *Server) {
	if r == nil || srv == nil {
		panic("language_server: RegisterServerWithRegistrar requires non-nil registrar and server")
	}
	r.RegisterService(ServiceDesc{
		ServiceName: ServiceName,
		HandlerType: (*LanguageServerServiceHandler)(nil),
		Methods: []MethodDesc{
			{MethodName: "StartCascade"},
			{MethodName: "SendUserCascadeMessage"},
			{MethodName: "CancelCascade"},
			{MethodName: "SkipBrowserSubagent"},
			{MethodName: "GetAgentInstance"},
			{MethodName: "Shutdown"},
		},
		Streams: []StreamDesc{
			{StreamName: "StreamAgentUpdates", ServerStreams: true},
		},
		Metadata: "language_server.proto",
	}, srv)
}

// Serve starts the HTTP/2 server on the configured port and blocks
// until ctx.Done(). Returns nil on graceful shutdown.
func (s *Server) Serve(ctx context.Context) error {
	if s == nil {
		return errors.New("language_server: Serve called on nil Server")
	}
	if s.config == nil {
		s.config = &LanguageServerConfig{Port: 0, RPCTimeout: 60 * time.Second}
	}
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	if s.config.Host == "" {
		addr = fmt.Sprintf(":%d", s.config.Port)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("language_server: listen %s: %w", addr, err)
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	handler := RegisterServer(s)
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: s.config.RPCTimeout,
	}
	s.mu.Lock()
	s.srv = srv
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Stream is the recovered server-side stream constructor (used by the
// antigravity TUI / IDE to consume the streaming RPCs).
type Stream struct {
	ctx    context.Context
	mu     sync.Mutex
	closed bool
	ch     chan *AgentUpdate
}

// NewStream returns a new server-side stream.
func NewStream(ctx context.Context) *Stream {
	return &Stream{ctx: ctx, ch: make(chan *AgentUpdate, 16)}
}

// Send implements the server-side stream send.
func (s *Stream) Send(update *AgentUpdate) error {
	if s == nil {
		return errors.New("language_server: nil stream")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("language_server: stream closed")
	}
	select {
	case s.ch <- update:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// Recv blocks until an update is available or the stream is closed.
func (s *Stream) Recv() (*AgentUpdate, bool) {
	if s == nil {
		return nil, false
	}
	update, ok := <-s.ch
	return update, ok
}

// Close marks the stream as closed.
func (s *Stream) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
}

// Context returns the underlying context.
func (s *Stream) Context() context.Context {
	if s == nil {
		return context.Background()
	}
	return s.ctx
}

// streamResponseWriter is the recovered HTTP/2 stream response writer
// used by RegisterServer's StreamAgentUpdates handler.
type streamResponseWriter struct {
	w          http.ResponseWriter
	flusher    http.Flusher
	mu         sync.Mutex
	headerSent bool
}

func newStreamResponseWriter(w http.ResponseWriter) LanguageServerService_StreamAgentUpdatesServer {
	flusher, _ := w.(http.Flusher)
	return &streamResponseWriter{w: w, flusher: flusher}
}

// Send implements the stream server-side Send.
func (s *streamResponseWriter) Send(update *AgentUpdate) error {
	if s == nil {
		return errors.New("language_server: nil stream response writer")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.headerSent {
		s.w.Header().Set("Content-Type", "application/grpc+proto")
		s.w.WriteHeader(http.StatusOK)
		s.headerSent = true
	}
	if _, err := s.w.Write([]byte(update.Payload + "\n")); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

// Context implements the stream server-side Context.
func (s *streamResponseWriter) Context() context.Context {
	return context.Background()
}

// ---------------------------------------------------------------------------
// Recovered error helpers.
// ---------------------------------------------------------------------------

// ShouldSampleError is the recovered telemetry sampling helper.
// Source: 0x86051c0.
func ShouldSampleError(err error) bool {
	if err == nil {
		return false
	}
	return true
}

// asConnectError is the recovered error-conversion helper.
func asConnectError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("connect: %w", err)
}

// authClientUnavailableError is the recovered sentinel error.
var authClientUnavailableError = errors.New("language_server: auth client unavailable")

// battleArms is the recovered sentinel.
var battleArms = "battle-arms"

// bearerTokenInterceptor is the recovered interceptor constructor
// (placeholder; the real one extracts the OAuth token from ctx).
func bearerTokenInterceptor() UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *UnaryServerInfo, handler UnaryHandler) (any, error) {
		return handler(ctx, req)
	}
}
