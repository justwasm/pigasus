// Package remotetransport — tcp_server.go: the TCP+WebSocket
// transport's constructor and graceful-shutdown orchestrators.
//
// NewTCPServer is split into:
//
//   - NewTCPServer (orchestrator: validate config, bind the
//     listener, install notifier + broadcaster callbacks, spawn
//     the http.Server.Serve goroutine).
//   - applyTCPConfigDefaults (mutates cfg to its default shape
//     and validates the resulting Path starts with '/').
//   - newTCPServer (assembles the *TCPServer value from cfg +
//     listener + the OriginPolicy copy + the mux + http.Server).
//   - (s *TCPServer).startServeLoop (the goroutine that calls
//     http.Server.Serve under s.wg; separated so the
//     orchestrator stays a flat list of named actions).
//
// Shutdown is split into:
//
//   - Shutdown (orchestrator: close listener, close all conns,
//     wait for per-conn goroutines, tear down the http.Server).
//   - closeAllConns (snapshot + parallel Close via closeOnce).
//   - shutdownHTTPServer (the 5-second timeout that wraps the
//     http.Server.Shutdown call).
//
// Every helper is ≤ 50 LOC; the orchestrators stay readable.
package remotetransport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/justwasm/pigasus/rctoken"
)

// NewTCPServer brings up the TCP+WebSocket transport sharing the
// given dispatcher. The HTTP listener is bound immediately so
// callers can print the bound address before any client connects
// (tests need this to pick the random port the OS gave them).
//
// NewTCPServer does NOT block on Serve. The HTTP server runs in a
// goroutine and exits when Shutdown is called.
//
// Cycle rc-auth-tokens: TCPServer takes an optional OriginPolicy
// via the OriginPolicy field on TCPConfig (default nil →
// "allow all" — the pre-cycle semantics). Use (*TCPServer).SetOriginPolicy
// at runtime to swap in a non-nil policy. The construction
// happens before the listener starts so the first Accept already
// sees the policy.
func NewTCPServer(ctx context.Context, disp Dispatcher, cfg TCPConfig) (*TCPServer, error) {
	if disp == nil {
		return nil, errors.New("remotetransport: dispatcher is required")
	}
	if err := applyTCPConfigDefaults(&cfg); err != nil {
		return nil, err
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("remotetransport: listen %s: %w", cfg.Addr, err)
	}

	s := newTCPServer(disp, cfg, ln)
	// Cycle rc-streaming-protocol: server-push notifier
	// lets dispatcher.Notify(connID, method, params) push
	// JSON-RPC notifications to a specific ws client.
	// Cycle rc-cascade-stream uses it to stream model chunks
	// to dashboards. Cycle rc-bidi-loop replaces the inline
	// ws.Write path with a bounded-channel enqueue so pushFrame
	// and the dispatch goroutine's response write share a single
	// writer goroutine.
	disp.SetNotifier(s.pushFrame)
	// Cycle rc-permissions-grants: register the broadcaster
	// so the dispatcher's BroadcastNotify helper can fan a
	// notification out to every open ws connection. The
	// broadcaster callback walks s.conns under the same
	// connsMu RLock pushFrame uses, so the snapshot is
	// consistent with concurrent handleWS insert/delete.
	disp.SetBroadcaster(s.broadcastFrame)

	s.startServeLoop(ctx)
	return s, nil
}

// applyTCPConfigDefaults mutates cfg in place to its canonical
// shape and validates that Path starts with '/'. Extracted from
// NewTCPServer so the orchestrator above stays a flat list of
// named actions.
func applyTCPConfigDefaults(cfg *TCPConfig) error {
	if cfg.Addr == "" {
		// Default: bind to all interfaces on an OS-chosen
		// port. Tests rely on this; production wiring can
		// override.
		cfg.Addr = "0.0.0.0:0"
	}
	if cfg.Path == "" {
		cfg.Path = "/rpc"
	}
	if !strings.HasPrefix(cfg.Path, "/") {
		return fmt.Errorf("remotetransport: TCPConfig.Path must start with '/', got %q", cfg.Path)
	}
	return nil
}

// newTCPServer assembles the *TCPServer value from the already-
// validated cfg and bound listener. Extracted from NewTCPServer
// so the orchestrator stays a flat list of named actions. Note:
// the OriginPolicy copy happens here so the first Accept already
// sees the policy (the pre-cycle constructor did this in the same
// place).
func newTCPServer(disp Dispatcher, cfg TCPConfig, ln net.Listener) *TCPServer {
	s := &TCPServer{
		cfg:        cfg,
		dispatcher: disp,
		listener:   ln,
		mux:        http.NewServeMux(),
		conns:      map[string]*wsConn{},
		// The binary-protobuf Connect dialect is answered when the
		// language-server descriptor set loads. A failure here
		// only costs the binary dialect: the server stays on JSON.
		protoCodec: protoCodecFor(),
	}
	// Cycle rc-auth-tokens: install the supplied
	// OriginPolicy (if any) before the listener starts so
	// the first Accept already sees the canonical
	// allowlist. A nil cfg.OriginPolicy keeps the
	// pre-cycle "allow all" semantics — the same Accept
	// path the rc-ws-transport cycle shipped.
	if cfg.OriginPolicy != nil {
		policyCopy := *cfg.OriginPolicy
		s.originPolicy = &policyCopy
	}
	// Register the upgrader at cfg.Path. Any other path
	// returns 404 so a port-scan sees "something is
	// listening, but the only endpoint is /rpc".
	s.mux.HandleFunc(cfg.Path, s.handleWS)
	if cfg.HealthzHandler != nil {
		s.mux.Handle("/healthz", cfg.HealthzHandler)
	}
	if cfg.WebUIHandler != nil {
		s.mux.HandleFunc("/connect-websocket", s.handleWS)
		s.mux.Handle("/", s.newConnectRPCRouter(cfg.WebUIHandler))
	}
	s.httpServer = &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

func (s *TCPServer) handleRPCCall(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	// A request that arrived through the remote-control tunnel is
	// not a browser on the loopback socket: its Origin belongs to the
	// dashboard, not to the local UI, so the origin allowlist does not
	// apply. The reference marks the same requests with the
	// `X-Jetski-Via-Remote-Control` header.
	if !isRemoteControlRequest(r) {
		decision := s.handleOriginCheck(r)
		if !decision.Allowed {
			writeOriginForbidden(w, decision)
			return
		}
	}
	// Cycle rc-auth-bearer-required: the secret gate. The tunnel is
	// exempt inside handleTokenCheck (the cloud authenticated that
	// request); every other client from outside the loopback
	// interface has to present the secret.
	authDecision := s.handleTokenCheck(r)
	if !authDecision.Allowed {
		s.writeTokenUnauthorizedFor(w, authDecision)
		return
	}
	method := methodFromPath(r.URL.Path)
	bodyBytes, _ := io.ReadAll(io.LimitReader(r.Body, 16<<20))

	if s.isStreamingRequest(r) {
		s.handleConnectStreamCall(w, r, method, bodyBytes)
		return
	}

	s.dispatchConnectCall(w, r, method, bodyBytes)
}

// dispatchConnectCall answers a unary Connect-RPC call with the
// dispatcher's result.
//
// The body is the Connect encoding of the request message and the
// answer is the Connect encoding of the response message. Which
// encoding that is follows the request's content type: the cloud's
// v2 tunnel sends `application/proto` (binary protobuf), the
// loopback UI sends `application/json`, the reference's own web bundle
// sends `application/grpc-web+json`.
func (s *TCPServer) dispatchConnectCall(w http.ResponseWriter, r *http.Request, method string, body []byte) {
	dialect := dialectFor(r.Header.Get("Content-Type"))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", dialect.contentTypeFor(false))

	payload := s.decodeRequestBody(method, dialect, body, false)

	type directDispatcher interface {
		DispatchDirect(ctx context.Context, method, payloadText string) (string, error)
	}
	if dd, ok := s.dispatcher.(directDispatcher); ok {
		res, err := dd.DispatchDirect(r.Context(), method, string(payload))
		if err == nil {
			out := s.encodeResponseBody(method, dialect, []byte(res))
			if dialect.isGRPCWeb() {
				s.writeGRPCWebUnary(w, out)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(out)
			return
		}
		if dialect.isGRPCWeb() {
			// The reference answers a failed call with the status in
			// the HTTP headers and no body at all: the gRPC-Web
			// client's "trailers-only" answer.
			status := grpcWebStatusInternal
			if code, ok := connectErrorFor(err); ok {
				status = code.grpc
			}
			w.Header().Set("Grpc-Status", strconv.Itoa(status))
			w.Header().Set("Grpc-Message", grpcWebErrorMessage(err))
			w.WriteHeader(http.StatusOK)
			return
		}
		// A failure that names its code answers the way the
		// reference answers it: the HTTP status the code maps to and
		// the JSON error body. A failure that names no code keeps the
		// catch-all below — the Web UI's own calls that fail in the
		// clone but not in the reference must not turn into a page
		// error.
		if code, ok := connectErrorFor(err); ok {
			out := s.encodeResponseBody(method, dialect, connectErrorBody(code, err))
			w.WriteHeader(code.status)
			_, _ = w.Write(out)
			return
		}
	}
	out := s.encodeResponseBody(method, dialect, []byte("{}"))
	if dialect.isGRPCWeb() {
		s.writeGRPCWebUnary(w, out)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// writeGRPCWebUnary writes a unary gRPC-Web answer: the message in
// its envelope, then the trailers envelope with the status.
//
// The gRPC-Web client reads both: an answer without the trailers
// envelope ends in "missing trailer", and one without the message ends
// in "missing message".
func (s *TCPServer) writeGRPCWebUnary(w http.ResponseWriter, payload []byte) {
	flusher, _ := w.(http.Flusher)
	w.WriteHeader(http.StatusOK)
	_ = writeGRPCWebMessage(w, flusher, payload)
	_ = writeGRPCWebTrailers(w, flusher, grpcWebStatusOK, "")
}

// methodFromPath returns the RPC method name in a Connect path
// ("/exa.language_server_pb.LanguageServerService/GetStatus").
func methodFromPath(path string) string {
	if idx := strings.LastIndex(path, "/"); idx != -1 {
		return path[idx+1:]
	}
	return path
}

// connectRPCPrefix is the path prefix every Connect-RPC call carries:
// the service name and the method name are both in the path
// ("/exa.language_server_pb.LanguageServerService/GetStatus").
const connectRPCPrefix = "/exa.language_server_pb."

// newConnectRPCRouter routes the Connect-RPC prefix to the RPC
// handler and every other path to the Web UI handler.
//
// `http.ServeMux` cannot express that route: a pattern without a
// trailing slash matches one exact path, and a trailing-slash pattern
// needs the prefix to end with a slash — but the Connect path puts
// the method name after the dot. The reference's language server
// serves the same prefix natively (it is a gRPC service name).
//
// Cycle rc-auth-bearer-required: the Web UI's own paths are behind
// the same secret gate as the RPC surface. The page carries the
// secret, so a peer that may not have it must not get the page — and
// the gate lives here, at the transport, so it covers every handler
// that serves the page and not only the clone's own one.
func (s *TCPServer) newConnectRPCRouter(webUI http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, connectRPCPrefix) {
			s.handleRPCCall(w, r)
			return
		}
		s.serveWebUI(w, r, webUI)
	})
}

// serveWebUI answers a Web UI request.
//
// The secret gate runs first (cycle rc-auth-bearer-required), and a
// peer that proved it knows the secret is handed the cookie that
// carries it afterwards — the browser presents the cookie with every
// later request (the bundled UI reads the secret from it), so the
// query string only has to carry it once.
//
// The cookie is set here rather than in the Web UI handler, because
// the gate is here: a handler that does not know about the secret
// cannot be trusted to hand it out.
//
// AllowUnauthenticatedWebUI lifts the gate for the page alone. The
// reference's language server serves its page to every peer and asks
// the secret of the service calls instead — the page hands the secret
// to whoever loads it.
func (s *TCPServer) serveWebUI(w http.ResponseWriter, r *http.Request, webUI http.Handler) {
	if !s.cfg.AllowUnauthenticatedWebUI {
		decision := s.handleTokenCheck(r)
		if !decision.Allowed {
			s.writeTokenUnauthorizedFor(w, decision)
			return
		}
	}
	if token := s.cfg.CSRFToken; token != "" {
		if presented := rctoken.Presented(r); rctoken.Verify(token, presented) {
			http.SetCookie(w, &http.Cookie{
				Name:     rctoken.CookieName,
				Value:    presented,
				Path:     "/",
				SameSite: http.SameSiteLaxMode,
			})
		}
	}
	webUI.ServeHTTP(w, r)
}

// Handler returns the HTTP handler that serves the RPC endpoint and
// the Web UI. The cloud tunnel uses it to serve the inbound proxy
// requests: the reference serves its tunnel traffic against its own
// language server handler (remotecontrol.newProxiedHTTPRequest →
// routeAllowlistFilter.ServeHTTP), so a request that arrives over the
// tunnel and a request that arrives over the loopback socket take the
// identical code path.
func (s *TCPServer) Handler() http.Handler { return s.mux }

// startServeLoop spawns the goroutine that runs
// http.Server.Serve. Tracked via s.wg so Shutdown's
// s.wg.Wait blocks until Serve returns (which happens once
// s.listener.Close has been called in step 1 of Shutdown).
//
// Extracted from NewTCPServer so the orchestrator above stays a
// flat list of named actions.
func (s *TCPServer) startServeLoop(_ context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// http.Server.Serve blocks until Shutdown closes the
		// listener; we ignore the ErrServerClosed return value.
		_ = s.httpServer.Serve(s.listener)
	}()
}

// Shutdown gracefully closes the HTTP server, sends a
// StatusGoingAway close frame to every open ws client, and waits
// for all per-connection handlers (readers + writers) to drain.
// Idempotent; safe to call from defer and a signal handler. The
// shutdown context defaults to a 5-second timeout so a stuck
// connection can't hold Shutdown indefinitely.
//
// Sequence:
//
//  1. Close the listener — no new accepts.
//  2. Send a close frame (code 1001, "server shutdown") to every
//     open ws connection, in parallel. The library's Close uses
//     an internal 5-second close-handshake timeout (see
//     coder/websocket/close.go:closeHandshake), so a stuck peer
//     can't pin us forever.
//  3. Wait for every handleWS goroutine to drain. handleWS
//     registers itself in s.wg on entry, so this also waits for
//     http.Server.Serve to return after step 1.
//  4. Call http.Server.Shutdown to release the http.Server's
//     own resources. Should be near-instant since all handlers
//     have already exited.
//
// A Shutdown call after the first is a no-op (returns the first
// call's error).
//
// Cycle rc-bidi-loop: the per-conn close in step 2 is wrapped in
// (*wsConn).closeOnce so it composes safely with handleWS's
// deferred teardown (which also calls Close). Both paths race
// to the same closeOnce.Do; only the first wins, the second is
// a no-op.
func (s *TCPServer) Shutdown(ctx context.Context) error {
	var err error
	s.stopOnce.Do(func() {
		// 1. Stop accepting new connections. http.Server.Serve
		//    returns immediately after this; the goroutine
		//    running it calls s.wg.Done before Shutdown returns.
		_ = s.listener.Close()

		// 2. Snapshot + parallel close. closeAllConns returns
		//    once every per-conn Close has raced through the
		//    same closeOnce.Do.
		s.closeAllConns()

		// 3. Wait for every handleWS goroutine (and the
		//    writePump goroutine it spawned) to drain. The
		//    close frame in step 2 should make each
		//    handler's c.Read return a close-related error
		//    and the handler exit shortly. s.wg also
		//    tracks http.Server.Serve (which already
		//    returned after step 1).
		s.wg.Wait()

		// 4. Tear down the http.Server. Should be near-
		//    instant since all ws handlers have drained.
		err = s.shutdownHTTPServer(ctx)
	})
	return err
}

// closeAllConns snapshots the open connections under the lock,
// then closes each one in its own goroutine so a stuck peer
// doesn't serialise the shutdown. StatusGoingAway (1001) is the
// RFC-correct code for "server is shutting down, please close
// gracefully". The library's writeControl has a 5-second
// internal timeout (see write.go:writeControl), so this can't
// pin shutdown forever even if the peer is unresponsive.
// closeOnce makes this composable with handleWS's deferred
// teardown (which calls Close too — only one wins).
//
// Extracted from Shutdown so the orchestrator above stays a flat
// list of named actions.
func (s *TCPServer) closeAllConns() {
	s.connsMu.Lock()
	open := make([]*wsConn, 0, len(s.conns))
	for _, wc := range s.conns {
		open = append(open, wc)
	}
	s.connsMu.Unlock()

	var closeWG sync.WaitGroup
	for _, wc := range open {
		closeWG.Add(1)
		go func(wc *wsConn) {
			defer closeWG.Done()
			wc.closeOnce.Do(func() {
				_ = wc.conn.Close(websocket.StatusGoingAway, "server shutdown")
			})
		}(wc)
	}
	closeWG.Wait()
}

// shutdownHTTPServer wraps http.Server.Shutdown in a 5-second
// timeout. ctx (the caller's Shutdown ctx) is the parent so a
// caller-supplied deadline wins; a nil ctx falls back to
// context.Background so we never panic on a typed-nil parent.
// Returns whatever http.Server.Shutdown returns.
//
// Extracted from Shutdown so the orchestrator above stays a flat
// list of named actions.
func (s *TCPServer) shutdownHTTPServer(ctx context.Context) error {
	if ctx != nil {
		// Use the caller's ctx as the parent so the
		// caller's deadline (if any) wins.
		timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(timeoutCtx)
	}
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(timeoutCtx)
}
