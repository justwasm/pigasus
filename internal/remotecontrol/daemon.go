// Package remotecontrol is the openagy recovered implementation of the
// antigravity `--remote-control` daemon.
//
// AGY 1.2.16 changelog:
//
//	"Added the `--remote-control` daemon flag that exposes a small set of MCP
//	 tools (status, list_agents, get_output) over a Unix socket so external
//	 dashboards can read live session state."
//
// The package is split across several files:
//
//   - daemon.go         — the Unix-socket server facade. Start /
//     Daemon / Shutdown / SocketPath / Accepted are preserved
//     so the existing cmd/wagyu/remote_control.go wiring and
//     the daemon_test.go suite keep working unchanged. The
//     accept loop lives here too (NOT moved to
//     remotetransport) so the package stays
//     import-cycle-free: the daemon_test.go test uses
//     *remotecontrol.Daemon directly, and a test file in
//     remotetransport imports this package for
//     *remotecontrol.Dispatcher. An import-direction
//     dependency from this package onto remotetransport
//     would cycle through that test.
//   - daemon_session.go — the SessionState struct + its
//     lock-protected mutators (SetTurnOutput, AppendTurnOutput,
//     SetConversationID, SetModel, SetBackend) + the ChunkSink
//     callback plumbing + the read-only snapshot helpers +
//     the JSON statusSnapshot renderer.
//   - daemon_meta.go    — the DaemonMeta struct (the
//     rich-status `daemon` block) + the AgentLister / AgentInfo
//     / StaticAgentLister / defaultAgentLister /
//     DefaultAgentLister types + the OutputReader /
//     DefaultOutputReader / ErrNotFound contract.
//   - daemon_config.go  — the Start-time Config struct (the
//     30+ seam fields the dispatcher fabricates from at
//     NewDispatcher time). Pure data; no methods.
//   - dispatch.go       — the transport-agnostic JSON-RPC
//     engine (Dispatcher). Every transport drives its accept
//     loop into Dispatcher.Dispatch, which holds the wire
//     format (newline-delimited JSON-RPC 2.0) and the tool
//     catalogue (initialize / tools/list / tools/call / ping /
//     notifications/initialized).
//   - host.go           — production wiring helpers used by
//     the host binary (cmd/wagyu/remote_control.go). Imports
//     internal/auth for the version string.
//   - wire.go           — production OutputReader backed by
//     internal/convstore.
//
// remotetransport/unix.go holds a parallel UnixServer
// implementation (same wire format, same Dispatcher). The two are
// functionally equivalent; the duplication is documented in the
// daemon.go acceptLoop comment.
//
// Wire format (unchanged):
//
//	{ "jsonrpc": "2.0", "id": <any>, "method": "<name>", "params": {…} }
//
// Responses follow JSON-RPC 2.0 (result or error). tools/call
// results carry an MCP envelope:
//
//	{ "content": [ { "type": "text", "text": "…" } ], "isError": false }
package remotecontrol

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
	"sync/atomic"
	"time"
)

// ProtocolVersion is the MCP protocolVersion the daemon advertises.
// MCP uses date-stamped versions; 2024-11-05 is the snapshot PiG's
// mcpext recognises and is also what `agy --help`-equivalent tools
// typically send. Dashboards that pin a different version still get a
// successful initialize response — we just echo their value back so
// the client thinks negotiation succeeded.
const ProtocolVersion = "2024-11-05"

// ServerName / ServerVersion are reported as the serverInfo.name /
// .version fields in the initialize response. Pinning these makes
// the daemon easy to fingerprint from a dashboard's perspective and
// matches the convention `agy --version` already uses for the CLI.
const (
	ServerName    = "openagy-remote-control"
	ServerVersion = "0.1.0"
)

// Daemon is the Unix-socket JSON-RPC server. Use Config + Start to
// construct; call Shutdown to close.
//
// Cycle rc-ws-transport: Daemon keeps its own accept loop here
// rather than wrapping *remotetransport.UnixServer. The wire
// format is unchanged because the dispatcher (shared with the
// TCP+WS transport) holds the JSON-RPC parser + tool catalogue;
// only the framing layer conceptually moved. (The
// remotetransport/unix.go file is a parallel
// implementation of the same accept loop — same Dispatcher, same
// wire format. Daemon and the unix-server dual-exist for the
// reason called out in this file's package doc: importing
// remotetransport from here would create a cycle through
// the smoketest in tcp_test.go.)
type Daemon struct {
	cfg        Config
	dispatcher *Dispatcher
	listener   net.Listener
	socketAbs  string

	wg       sync.WaitGroup
	stopOnce sync.Once
	stopCh   chan struct{}

	// accepted counts the cumulative number of connections since
	// Start; surfaced in status and on the wire for diagnostics.
	accepted atomic.Uint64
}

// Start brings up the daemon. It returns once the socket is bound;
// Shutdown tears it down. A non-nil error means the daemon never
// started (caller should print the error and exit non-zero).
//
// Start is safe to call from main(); it does NOT block on the
// accept loop. The accept loop runs in a goroutine and exits when
// Shutdown is called or the socket is closed externally.
func Start(ctx context.Context, cfg Config) (*Daemon, error) {
	if cfg.Session == nil {
		return nil, errors.New("remotecontrol: Config.Session is required")
	}
	if cfg.Permissions == 0 {
		cfg.Permissions = DefaultPermissions
	}
	if cfg.Agents == nil {
		cfg.Agents = DefaultAgentLister
	}
	if cfg.Reader == nil {
		cfg.Reader = DefaultOutputReader{}
	}

	disp, err := NewDispatcher(cfg)
	if err != nil {
		return nil, err
	}

	ln, sockPath, err := bindUnixSocket(cfg.SocketPath, cfg.Permissions)
	if err != nil {
		return nil, err
	}

	d := &Daemon{
		cfg:        cfg,
		dispatcher: disp,
		listener:   ln,
		socketAbs:  sockPath,
		stopCh:     make(chan struct{}),
	}
	// Accept loop. One connection at a time: a dashboard is the
	// only expected client and JSON-RPC requests are atomic
	// (newline-delimited), so concurrent goroutines add no value.
	d.wg.Add(1)
	go d.acceptLoop(ctx)

	return d, nil
}

// bindUnixSocket encapsulates the socket-create + chmod + parent-
// mkdir sequence. Split out of Start so the lifecycle function stays
// readable; returns (listener, abs path, err).
func bindUnixSocket(raw string, perms os.FileMode) (net.Listener, string, error) {
	sockPath, err := resolveSocketPath(raw)
	if err != nil {
		return nil, "", err
	}
	// Ensure parent dir exists (TempDir already does; user-supplied
	// paths might not). We are deliberately not creating parent
	// directories above the user's $TMPDIR — that would surprise
	// callers who pass /var/run/agy/remote-control.sock.
	if dir := filepath.Dir(sockPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, "", fmt.Errorf("remotecontrol: mkdir %s: %w", dir, err)
		}
	}
	// Remove a stale socket from a previous crashed run. Bind will
	// fail with EADDRINUSE otherwise.
	if _, err := os.Stat(sockPath); err == nil {
		_ = os.Remove(sockPath)
	}
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, "", fmt.Errorf("remotecontrol: listen %s: %w", sockPath, err)
	}
	// Lock down the socket file. The Dashboard can connect via the
	// user's own session, so 0o600 is the right default.
	if err := os.Chmod(sockPath, perms); err != nil {
		_ = ln.Close()
		return nil, "", fmt.Errorf("remotecontrol: chmod %s: %w", sockPath, err)
	}
	return ln, sockPath, nil
}

// SocketPath returns the absolute socket path the daemon is bound to.
// Useful for tests and for the host binary to print on startup.
func (d *Daemon) SocketPath() string { return d.socketAbs }

// Shutdown closes the listener and waits for the accept loop to
// drain. Idempotent; safe to call from defer and a signal handler.
func (d *Daemon) Shutdown() {
	d.stopOnce.Do(func() {
		close(d.stopCh)
		_ = d.listener.Close()
		// Best-effort socket cleanup; ignore ENOENT.
		if d.socketAbs != "" {
			_ = os.Remove(d.socketAbs)
		}
	})
	d.wg.Wait()
}

// Accepted returns the cumulative number of dashboard connections
// since Start; useful for tests and operator triage.
func (d *Daemon) Accepted() uint64 { return d.accepted.Load() }

// acceptLoop is the single goroutine that pulls connections off
// the listener and serves them. Per-connection handlers also run
// sequentially — a dashboard never needs parallel requests.
//
// This loop is intentionally a copy of the equivalent loop in
// remotetransport/unix.go. Sharing the code through a
// common helper would force internal/remotecontrol to import
// remotetransport, which cycles through the smoketest
// in remotetransport/tcp_test.go (that test imports
// *remotecontrol.Dispatcher from this package). The duplication
// is small (~30 lines) and the two loops must stay in sync; a
// future refactor that wants to break the duplication will need
// to lift both implementations onto a shared interface owned by
// remotetransport.
func (d *Daemon) acceptLoop(ctx context.Context) {
	defer d.wg.Done()
	for {
		conn, err := d.listener.Accept()
		if err != nil {
			if d.handleAcceptError(err) {
				return
			}
			continue
		}
		d.accepted.Add(1)
		d.dispatcher.NotifyConnected()
		d.handleConn(ctx, conn)
		d.dispatcher.NotifyDisconnected()
	}
}

// handleAcceptError classifies the listener's Accept() return:
// stop-on-Shutdown, stop-on-ErrClosed, and "transient — back off
// briefly and retry" otherwise. Returns true iff the accept loop
// should exit.
func (d *Daemon) handleAcceptError(err error) bool {
	select {
	case <-d.stopCh:
		return true
	default:
	}
	// Listener closed externally; exit.
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	// Transient accept error: brief pause, retry. We
	// can't be too eager or a flood of errors pins a CPU.
	select {
	case <-d.stopCh:
		return true
	case <-time.After(50 * time.Millisecond):
		return false
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
func (d *Daemon) handleConn(ctx context.Context, c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Time{}) // no per-conn timeout; the daemon is long-lived

	in := bufio.NewScanner(c)
	in.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	out := bufio.NewWriter(c)
	defer out.Flush()

	for in.Scan() {
		line := bytesTrimSpace(in.Bytes())
		if len(line) == 0 {
			continue
		}
		resp := d.dispatcher.Dispatch(ctx, line)
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

// resolveSocketPath normalises Config.SocketPath. Empty means
// "$TMPDIR/openagy-remote-control-<pid>.sock" (falling back to
// os.TempDir() when TMPDIR is unset). The PID component lets two
// instances coexist on the same machine without colliding.
func resolveSocketPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		if !filepath.IsAbs(raw) {
			return "", fmt.Errorf("remotecontrol: socket path must be absolute, got %q", raw)
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
// Used by the Unix-socket transport to keep the newline-delimited
// wire format byte-for-byte identical to the pre-rpc-ws-transport
// daemon. The TCP+WebSocket transport writes the bytes into a ws
// message instead (no '\n' suffix), so it does NOT call this helper.
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

// bytesTrimSpace is a tiny helper that avoids pulling in bytes for
// the one call site that needs it.
func bytesTrimSpace(b []byte) []byte {
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
