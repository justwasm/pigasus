// Package remotetransport — unix.go: the Unix-socket JSON-RPC
// transport.
//
// Cycle rc-ws-transport moved this code out of
// internal/remotecontrol/daemon.go so the same dispatcher can be
// served by either the Unix-socket transport (the per-turn
// --remote-control flag's default) or the new TCP+WebSocket
// transport (the new --remote-control-tcp flag's transport).
//
// Wire format is unchanged: newline-delimited JSON-RPC, one request
// per line, one response per line. The framing is the only thing
// that differs from the WebSocket transport; everything else is
// shared via the Dispatcher interface (which *remotecontrol.Dispatcher
// satisfies structurally).
//
// Why an interface (and not a concrete pointer to *remotecontrol.Dispatcher):
// the daemon facade in internal/remotecontrol needs to construct
// a UnixServer, and the UnixServer needs a Dispatcher. Importing
// the concrete type both ways would create an import cycle; defining
// the interface here, and having *remotecontrol.Dispatcher satisfy
// it, breaks the cycle without a third package.
package remotetransport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// StreamDispatcher is the optional capability a Dispatcher may
// implement to serve server-streaming Connect-RPC calls. The
// transport asserts for it at call time, so a dispatcher that only
// implements Dispatcher keeps working — the transport then falls back
// to its one-frame behaviour.
//
// The dispatcher owns the frame cadence: it calls emit once per
// frame it wants to push and returns when the call is over (usually
// because ctx was cancelled). A returned error ends the stream with
// a Connect-RPC error status.
//
// The Web UI's conversation rendering depends on this: it reads the
// Antigravity Web UI's StreamAgentStateUpdates stream, and a stream
// that ends early leaves the conversation frozen.
type StreamDispatcher interface {
	// DispatchStream serves one server-streaming RPC. It reports
	// handled=false for a method it does not stream, so the
	// transport can fall back.
	DispatchStream(ctx context.Context, method string, payloadText string, emit func(json.RawMessage) error) (handled bool, err error)
}

// Dispatcher is the protocol engine interface. The concrete
// implementation lives in internal/remotecontrol (the only public
// type that satisfies it today), but the transport package
// deliberately imports only this interface so the package graph
// stays acyclic.
type Dispatcher interface {
	// Dispatch routes one JSON-RPC request frame to its handler
	// and returns the response frame (or nil for notifications).
	Dispatch(ctx context.Context, raw []byte) map[string]any
	// DispatchDirect dispatches an RPC method directly with a JSON payload string
	// (e.g. from Connect-RPC or WebChannel proxy), returning the JSON result string.
	DispatchDirect(ctx context.Context, method string, payloadText string) (string, error)
	// Accepted returns the cumulative connection count. The accept
	// loop calls MarkAccepted once per connection; the counter is
	// shared with every transport so dashboards see a single
	// connection count regardless of which transport the dial went
	// through.
	Accepted() uint64
	// MarkAccepted bumps the cumulative counter.
	MarkAccepted()
	// NotifyConnected / NotifyDisconnected bump the per-session
	// connected counter that the status tool surfaces.
	NotifyConnected()
	NotifyDisconnected()
	// Notify pushes one JSON-RPC notification frame (no "id"
	// field) to the connection identified by connID. Used by
	// TCPServer's server-push path; UnixServer never calls it
	// because the Unix-socket transport is request/response only.
	// A nil Notifier is the default for any dispatcher; Notify
	// returns nil in that case.
	Notify(ctx context.Context, connID, method string, params any) error
	// SetNotifier registers the callback Notify uses to deliver
	// frames. Called once by NewTCPServer; never called by the
	// Unix-socket transport (which is request/response only).
	// The parameter type is the inline function signature (not
	// remotecontrol.Notifier) because remotetransport cannot
	// import remotecontrol (the test cycle through tcp_test.go)
	// — and a named function type would not match this interface
	// method's parameter type under Go's type-identity rules.
	// The concrete *remotecontrol.Dispatcher uses the
	// `remotecontrol.Notifier` type alias for the same signature,
	// which Go treats as identical to the inline form, so the
	// interface is satisfied.
	SetNotifier(func(ctx context.Context, connID string, frame []byte) error)
	// WithConnID returns a context carrying the given connID.
	// Cycle rc-cascade-stream adds this so the transport can
	// thread the per-connection connID into the dispatcher's
	// per-request context (the cascade handler reads it via
	// its private connIDFromContext helper to route
	// mid-turn notifications back to the originating ws
	// connection). The Unix-socket transport never calls
	// this (it has no connID); the TCP+WS transport calls
	// it once per handleWS connection.
	WithConnID(ctx context.Context, connID string) context.Context
	// SetBroadcaster registers the callback the
	// dispatcher's BroadcastNotify uses to fan a
	// notification out to every open connection on the
	// transport. Cycle rc-permissions-grants adds
	// this; the four Set/Add/Remove permission-mutation
	// handlers fan a "permissions/changed"
	// notification on every successful mutation. Future
	// cycles (rc-plugins, rc-triggers) will share the
	// seam. The parameter type is the inline function
	// signature for the same named-type vs alias reason
	// SetNotifier's parameter is inline (see the
	// comment on SetNotifier); remotetransport
	// cannot import remotecontrol.
	SetBroadcaster(func(ctx context.Context, method string, params any) error)
}

// UnixConfig is the start-time wiring for a Unix-socket transport.
// Mirrors the subset of remotecontrol.Config that drives the listener
// (SocketPath + Permissions); everything else (Session, Agents,
// Reader) lives on the shared Dispatcher.
type UnixConfig struct {
	// SocketPath is the absolute Unix socket path. Required;
	// resolveUnixSocketPath below fills in the default
	// "$TMPDIR/openagy-remote-control-<pid>.sock" when empty.
	SocketPath string
	// Permissions is the file mode applied to the socket file.
	// 0o600 by default — the daemon only needs to be reachable
	// from the same user, and the dashboard connects via the
	// user's own session. Tests may lower it.
	Permissions os.FileMode
}

// UnixServer is the running Unix-socket JSON-RPC server. Construct
// with NewUnixServer, call Shutdown to tear down. A non-nil error
// from NewUnixServer means the listener never came up.
type UnixServer struct {
	cfg        UnixConfig
	dispatcher Dispatcher
	listener   net.Listener
	socketAbs  string

	wg       sync.WaitGroup
	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewUnixServer brings up the Unix-socket transport sharing the
// given dispatcher. The dispatcher carries SessionState / Agents /
// Reader — same shape remotecontrol.Config used to, so the wire
// format is unchanged.
//
// NewUnixServer does NOT block on the accept loop. The accept
// loop runs in a goroutine and exits when Shutdown is called or
// the socket is closed externally.
//
// The function is split so each step is a flat named action:
//
//   - validate dispatcher + default Permissions;
//   - resolve socket path (or fall back to the TMPDIR default);
//   - bind + chmod the socket (the bindUnixSocket helper below
//     owns the parent-dir mkdir + stale-socket unlink +
//     net.Listen + os.Chmod steps; the helper also owns the
//     "close the listener on chmod failure" rollback);
//   - assemble the *UnixServer;
//   - spawn the accept-loop goroutine.
func NewUnixServer(ctx context.Context, disp Dispatcher, cfg UnixConfig) (*UnixServer, error) {
	if disp == nil {
		return nil, errors.New("remotetransport: dispatcher is required")
	}
	if cfg.Permissions == 0 {
		cfg.Permissions = 0o600
	}

	sockPath, err := resolveUnixSocketPath(cfg.SocketPath)
	if err != nil {
		return nil, err
	}
	ln, err := bindUnixSocket(sockPath, cfg.Permissions)
	if err != nil {
		return nil, err
	}

	u := &UnixServer{
		cfg:        cfg,
		dispatcher: disp,
		listener:   ln,
		socketAbs:  sockPath,
		stopCh:     make(chan struct{}),
	}
	// Accept loop. One connection at a time: a dashboard is the
	// only expected client and JSON-RPC requests are atomic
	// (newline-delimited), so concurrent goroutines add no value.
	u.wg.Add(1)
	go u.acceptLoop(ctx)

	return u, nil
}

// bindUnixSocket brings up a Unix-domain listener at sockPath and
// locks down the socket file. Extracted from NewUnixServer so
// the orchestrator above stays a flat list of named actions.
//
// Steps:
//   - Ensure parent dir exists (TempDir already does; user-
//     supplied paths might not). We are deliberately not creating
//     parent directories above the user's $TMPDIR — that would
//     surprise callers who pass /var/run/agy/remote-control.sock.
//   - Remove a stale socket from a previous crashed run. Bind
//     will fail with EADDRINUSE otherwise.
//   - net.Listen + os.Chmod. The chmod step has a "close the
//     listener on failure" rollback so the caller never has to
//     clean up a half-bound socket.
//
// The Dashboard connects via the user's own session, so the
// default 0o600 perm is what callers wire here.
func bindUnixSocket(sockPath string, perm os.FileMode) (net.Listener, error) {
	if dir := filepath.Dir(sockPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("remotetransport: mkdir %s: %w", dir, err)
		}
	}
	if _, err := os.Stat(sockPath); err == nil {
		_ = os.Remove(sockPath)
	}
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("remotetransport: listen %s: %w", sockPath, err)
	}
	if err := os.Chmod(sockPath, perm); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("remotetransport: chmod %s: %w", sockPath, err)
	}
	return ln, nil
}

// Addr returns the absolute socket path the server is bound to.
// Equivalent to remotecontrol.Daemon.SocketPath so the existing
// daemon_test.go continues to find the socket.
func (u *UnixServer) Addr() string { return u.socketAbs }

// Shutdown closes the listener and waits for the accept loop to
// drain. Idempotent; safe to call from defer and a signal handler.
func (u *UnixServer) Shutdown() {
	u.stopOnce.Do(func() {
		close(u.stopCh)
		_ = u.listener.Close()
		// Best-effort socket cleanup; ignore ENOENT.
		if u.socketAbs != "" {
			_ = os.Remove(u.socketAbs)
		}
	})
	u.wg.Wait()
}

// Accepted is a convenience accessor — delegates to the shared
// dispatcher. Kept on UnixServer so callers that only know about
// the transport (not the dispatcher) can still read the counter.
func (u *UnixServer) Accepted() uint64 { return u.dispatcher.Accepted() }

// acceptLoop is the single goroutine that pulls connections off
// the listener and serves them. Per-connection handlers also run
// sequentially — a dashboard never needs parallel requests.
func (u *UnixServer) acceptLoop(ctx context.Context) {
	defer u.wg.Done()
	for {
		conn, err := u.listener.Accept()
		if err != nil {
			select {
			case <-u.stopCh:
				return
			default:
			}
			// Listener closed externally; exit.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// Transient accept error: brief pause, retry. We
			// can't be too eager or a flood of errors pins a CPU.
			select {
			case <-u.stopCh:
				return
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}
		u.dispatcher.MarkAccepted()
		u.dispatcher.NotifyConnected()
		u.handleConn(ctx, conn)
		u.dispatcher.NotifyDisconnected()
	}
}

// handleConn serves a single connection. The wire format is one
// JSON-RPC request per line; the response is one JSON value per
// line. We use bufio.Scanner rather than json.Decoder so a single
// malformed request does not poison the rest of the stream —
// bufio.Scanner advances line-by-line.
//
// handleConn never returns an error to its caller; any per-request
// failure is encoded in the JSON-RPC response.
func (u *UnixServer) handleConn(ctx context.Context, c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Time{}) // no per-conn timeout; the daemon is long-lived

	in := bufio.NewScanner(c)
	in.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	out := bufio.NewWriter(c)
	defer out.Flush()

	for in.Scan() {
		line := trimSpaceBytes(in.Bytes())
		if len(line) == 0 {
			continue
		}
		resp := u.dispatcher.Dispatch(ctx, line)
		if resp == nil {
			// A notification (no id); we just skip.
			continue
		}
		if err := writeJSONLine(out, resp); err != nil {
			return
		}
		if err := out.Flush(); err != nil {
			return
		}
	}
}

// resolveUnixSocketPath normalises the configured socket path.
// Empty means "$TMPDIR/openagy-remote-control-<pid>.sock"
// (falling back to os.TempDir() when TMPDIR is unset). The PID
// component lets two instances coexist on the same machine
// without colliding.
func resolveUnixSocketPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		if !filepath.IsAbs(raw) {
			return "", fmt.Errorf("remotetransport: socket path must be absolute, got %q", raw)
		}
		return raw, nil
	}
	base := strings.TrimSpace(os.Getenv("TMPDIR"))
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, fmt.Sprintf("openagy-remote-control-%d.sock", os.Getpid())), nil
}

// writeJSONLine marshals v and writes it followed by '\n' to w.
// Kept here (not in remotecontrol/dispatch.go) because the
// newline-delimited framing is a transport-level concern: the
// TCP+WebSocket transport writes a single ws message per response
// without a trailing newline, so it does NOT call this helper.
func writeJSONLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	if _, err := w.Write([]byte{'\n'}); err != nil {
		return err
	}
	return nil
}

// trimSpaceBytes is a tiny helper that avoids pulling in bytes for
// the one call site that needs it. Used by the bufio.Scanner loop
// to skip leading/trailing whitespace on each JSON-RPC line.
func trimSpaceBytes(b []byte) []byte {
	start, end := 0, len(b)
	for start < end {
		c := b[start]
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			break
		}
		start++
	}
	for end > start {
		c := b[end-1]
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			break
		}
		end--
	}
	return b[start:end]
}
