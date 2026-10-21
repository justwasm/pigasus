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
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
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
	s.httpServer = &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

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
