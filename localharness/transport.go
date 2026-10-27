// Package localharness — transport.go: the client side of Google
// Antigravity's `localharness` protocol.
//
// # What this is
//
// The `agy` CLI and the `google-antigravity` SDKs do not talk to a model
// directly: they spawn a closed-source Go binary (`localharness`) that owns
// the agent loop, and drive it over a two-phase transport:
//
//	phase 1 — stdio handshake
//	    client  --[4-byte LE length][InputConfig protobuf]-->  harness stdin
//	    client  <--[4-byte LE length][OutputConfig protobuf]--  harness stdout
//
//	phase 2 — websocket
//	    client  --ws://localhost:<port>/ , header x-goog-api-key: <key]--> harness
//	    client  <--[protojson OutputEvent]--   harness
//	    client  --[protojson InputEvent]-->    harness
//
// This file implements that transport: spawn, handshake, connect, and the
// frame codec. The protobuf bindings it speaks are **already generated** at
// `localharness/pb/localharness` (Go package `localharness`,
// imported here as `pb`); this package does not regenerate or modify them.
//
// The **other** side of the same protocol lives in `server.go`: the
// handshake mirror (`ReadInputConfig` / `WriteOutputConfig`), the
// listener, the API-key check and the server-side frame codec. Both sides
// share the length prefix, the API-key header and the protojson dialect,
// so the two halves cannot drift apart.
//
// # The two wire dialects
//
// The stdio handshake is **binary protobuf** with a length prefix; the
// websocket frames are **protojson text**. Mixing them up is the single
// easiest mistake to make here, so the two paths are kept in separate
// functions (`WriteInputConfig` / `ReadOutputConfig` versus
// `Conn.Send` / `Conn.Recv`).
//
// # Provenance
//
// 【一手】 The message contract (`InputConfig`, `OutputConfig`, `ClientInfo`,
// `InitializeConversationEvent`, `OutputEvent`, `InputEvent`,
// `InitializeConversationResponse`) is the recovered descriptor at
// `out/localharness/proto/localharness.proto`, extracted from the
// `localharness_pb2.py` descriptor embedded in the `google-antigravity`
// PyPI wheel (v0.1.22, macosx_11_0_arm64).
//
// 【一手】 The stdio handshake (a 4-byte little-endian `uint32` length prefix
// in front of a binary-protobuf payload), the websocket URL shape
// (`ws://<host>:<port>/`), the `x-goog-api-key` header and the
// `InitializeConversationEvent`-first frame order come from the shipped
// Python SDK's local connection transport, read out of the same wheel.
//
// 【一手】 The frame encoding is protojson: camelCase field names, `int64` /
// `uint64` as JSON **strings**, `int32` / `uint32` as JSON **numbers**, enums
// as their name, `oneof` members flattened into the parent object, `bytes` as
// base64. That is exactly what `google.golang.org/protobuf/encoding/protojson`
// produces with default options, so this package uses the standard library
// codec rather than a hand-rolled one.
//
// 【strings】 The header name is in the harness binary's own string table
// (`x-goog-api-key` at `0x103355c50` — the *harness* address space, not
// the CLI's; `out/localharness/FINDINGS.md` §112).
//
// 【推测】 The retry schedule (5 rounds; each round tries `localhost` then
// `127.0.0.1`; `0.1 * 2^attempt` seconds of backoff between rounds) is
// inherited from the SDK rather than observed in the binary — the harness
// itself does not police the client's reconnect policy. The number of rounds
// and the exact sleep values are therefore a *client-side* choice, and this
// package makes them configurable (`Options.ConnectRetries`).
//
// # Hard constraints (硬约束)
//
//   - The stdio length prefix is **little-endian** and exactly four bytes.
//     A big-endian prefix desynchronises the stream permanently: the harness
//     would read a garbage length and the handshake would never recover.
//   - The `x-goog-api-key` header carries the key the harness minted in its
//     `OutputConfig`; the harness rejects the websocket upgrade without it.
//   - The **first** websocket frame is an `InitializeConversationEvent`, sent
//     standalone. It is *not* wrapped in an `InputEvent` — `InputEvent`'s
//     `oneof` has no `initialize_conversation_event` member, so there is
//     nowhere to put it.
//   - The harness may send fields this build's descriptor does not know about
//     (the descriptor was recovered from a shipped SDK, not from the binary's
//     own schema). Decoding therefore uses
//     `protojson.UnmarshalOptions{DiscardUnknown: true}`; a strict decode
//     would break the moment the harness ships a new field.
//   - Frames are **not** size-limited. `coder/websocket` defaults to a 32 KiB
//     read limit, which a single `StepUpdate` can exceed, so the limit is
//     disabled explicitly (`SetReadLimit(-1)`).
//
// # Limitations
//
//   - The bindings in `localharness/pb/localharness` were generated
//     from a *recovered* descriptor. Field *numbers* and *types* are believed
//     correct, but the recovered `.proto` had several scalar types stripped
//     (e.g. `BudgetConfig.max_total_tokens` is typed `int64` here), so a
//     descriptor drift would surface as a wire mismatch, not as a compile
//     error. `DiscardUnknown` hides *extra* fields but not *wrong* types.
//   - `Conn.Close` kills the child with `SIGKILL`, mirroring the SDK's
//     `process.kill()`. There is no graceful shutdown of the harness itself.
//   - This package speaks no part of the agent protocol: no hooks, no tool
//     results, no policy decisions. It is a transport, nothing more.
//   - Nothing here has been run against the real `localharness` binary — the
//     handshake is transcribed from the SDK, and the tests exercise it
//     against a stub server, not against the harness.
package localharness

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

const (
	// FrameLength is the fixed width of the stdio length prefix.
	//
	// The prefix is a little-endian uint32 counting the bytes of the
	// serialized protobuf that follows it. It is four bytes wide, always —
	// a variable-width prefix would desynchronise the stream.
	FrameLength = 4

	// defaultHandshakeTimeout bounds the whole spawn → handshake → connect
	// sequence when Options.HandshakeTimeout is not set.
	defaultHandshakeTimeout = 30 * time.Second

	// defaultConnectRetries is the number of websocket connect rounds when
	// Options.ConnectRetries is not set.
	defaultConnectRetries = 5

	// stderrLimit is the size of the ring buffer that keeps the child's
	// stderr. Only the tail is kept: the harness is chatty, and the useful
	// part of a crash is always the last thing it printed.
	stderrLimit = 64 << 10

	// retryBaseDelay is the first backoff step of the websocket connect
	// retry loop. Each round waits `retryBaseDelay * 2^attempt`.
	retryBaseDelay = 100 * time.Millisecond

	// apiKeyHeader is the HTTP header that carries the harness-minted API
	// key on the websocket upgrade request.
	apiKeyHeader = "x-goog-api-key"

	// maxFrameBytes caps the size of a stdio frame this package is willing
	// to read. It exists so a corrupt or hostile length prefix cannot make
	// the client allocate an arbitrary amount of memory; the harness itself
	// sends frames of a few kilobytes.
	maxFrameBytes = 64 << 20

	// closeGraceTimeout bounds each blocking step of Conn.Close so that a
	// wedged child or an unresponsive peer cannot make Close hang.
	closeGraceTimeout = 2 * time.Second
)

// loopbackHosts are the two names the harness may have bound its websocket
// listener to. The SDK tries both, in this order, inside every retry round:
// a harness that binds `localhost` may resolve to either address depending
// on the machine's resolver, so trying only one of them is a coin flip.
var loopbackHosts = []string{"localhost", "127.0.0.1"}

// ClientInfo identifies the SDK talking to the harness.
//
// The harness records this on the conversation (it shows up in the harness's
// own logs and in trajectory metadata), so it should describe the *client*
// truthfully: the language, the SDK version, and the host OS.
type ClientInfo struct {
	// Language is the client language, e.g. "go". It defaults to "go"
	// when empty.
	Language string

	// Version is the client SDK version, e.g. "0.1.22".
	Version string

	// LanguageVersion is the language runtime version, e.g. "go1.26.2".
	LanguageVersion string

	// OS is the host operating system, e.g. "darwin".
	OS string

	// OSVersion is the host OS version, e.g. "15.3".
	OSVersion string
}

// toProto converts the client info into its wire message.
func (ci ClientInfo) toProto() *pb.ClientInfo {
	out := &pb.ClientInfo{}
	out.SetLanguage(ci.Language)
	out.SetVersion(ci.Version)
	out.SetLanguageVersion(ci.LanguageVersion)
	out.SetOs(ci.OS)
	out.SetOsVersion(ci.OSVersion)
	return out
}

// Options configures Dial and Spawn.
type Options struct {
	// BinaryPath is the path of the `localharness` executable. It is
	// required; Dial and Spawn fail without it.
	BinaryPath string

	// StorageDirectory is the directory the harness uses for its own
	// state (trajectories, caches). It is passed through in the
	// `InputConfig` and is *not* created by this package.
	StorageDirectory string

	// ClientInfo identifies the SDK to the harness. An empty Language
	// defaults to "go".
	ClientInfo ClientInfo

	// Env is the environment the harness's tools run with. It travels in
	// the `InputConfig.env` map — it is *not* applied to the harness
	// process itself, which inherits this process's environment.
	Env map[string]string

	// UseInteractionsAPI selects the harness's Interactions API surface
	// instead of the legacy one.
	UseInteractionsAPI bool

	// HandshakeTimeout bounds the spawn → handshake → connect sequence.
	// It defaults to 30s.
	HandshakeTimeout time.Duration

	// ConnectRetries is the number of websocket connect rounds. Each round
	// tries `localhost` and then `127.0.0.1`, with a `0.1 * 2^attempt`
	// second backoff after it. It defaults to 5.
	ConnectRetries int

	// Logger receives debug-level diagnostics. A nil Logger discards them.
	Logger *slog.Logger
}

// withDefaults returns a copy of the options with the documented defaults
// applied.
func (o Options) withDefaults() Options {
	if o.HandshakeTimeout <= 0 {
		o.HandshakeTimeout = defaultHandshakeTimeout
	}
	if o.ConnectRetries <= 0 {
		o.ConnectRetries = defaultConnectRetries
	}
	if o.ClientInfo.Language == "" {
		o.ClientInfo.Language = "go"
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return o
}

// Conn is a live connection to a localharness process.
//
// A Conn owns three things: the child process, the websocket to it, and the
// stderr the child produced. Closing the Conn tears all three down; it is
// safe to call Close more than once.
type Conn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	ws   *websocket.Conn
	addr string

	logger *slog.Logger
	stderr *ringBuffer

	// stdoutDone is closed once the stdout drain goroutine has finished.
	// The drain exists so the child never blocks on a full stdout pipe
	// after the handshake is over.
	stdoutDone chan struct{}

	// draining records whether the stdout drain goroutine was started.
	// Close must not wait for a drain that never began.
	draining atomic.Bool

	// done is closed by Close; the watchdog goroutine kills the child
	// when it sees that.
	done chan struct{}

	closeOnce sync.Once
	killOnce  sync.Once
	closeErr  error
}

// Dial spawns the harness, performs the handshake, connects the websocket,
// sends the InitializeConversationEvent and waits for
// initialize_conversation_response.
//
// The initialize exchange carries an *empty* HarnessConfig — use Spawn and
// then Conn.Initialize when the conversation needs a real configuration.
//
// On any failure the child process is killed and the child's stderr is
// appended to the returned error, the way the SDK reports a crashed harness.
func Dial(ctx context.Context, opts Options) (*Conn, error) {
	c, err := Spawn(ctx, opts)
	if err != nil {
		return nil, err
	}
	// The initialize exchange is part of the handshake, so it shares the
	// handshake budget rather than running unbounded.
	initCtx, cancel := context.WithTimeout(ctx, opts.withDefaults().HandshakeTimeout)
	defer cancel()
	if _, err := c.Initialize(initCtx, &pb.HarnessConfig{}); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// Spawn does everything Dial does except the initialize exchange.
//
// The returned Conn has a live websocket but no conversation: the caller is
// expected to call Conn.Initialize with the configuration it wants.
func Spawn(ctx context.Context, opts Options) (*Conn, error) {
	opts = opts.withDefaults()
	if opts.BinaryPath == "" {
		return nil, errors.New("localharness: Options.BinaryPath is required")
	}

	// The child is spawned with exec.Command, not exec.CommandContext: the
	// process lifetime is owned by the Conn, not by the caller's context.
	// A context that only covers the handshake must not kill the harness
	// the moment the handshake returns.
	cmd := exec.Command(opts.BinaryPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("localharness: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("localharness: stdout pipe: %w", err)
	}
	tail := newRingBuffer(stderrLimit)
	cmd.Stderr = tail

	opts.Logger.Debug("localharness: spawning harness", "binary", opts.BinaryPath)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("localharness: start %q: %w", opts.BinaryPath, err)
	}

	c := &Conn{
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		logger:     opts.Logger,
		stderr:     tail,
		stdoutDone: make(chan struct{}),
		done:       make(chan struct{}),
	}
	go c.watchChild()

	if err := c.handshake(ctx, opts); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// handshake performs phase 1 (stdio) and phase 2 (websocket) of the setup.
func (c *Conn) handshake(ctx context.Context, opts Options) error {
	hsCtx, cancel := context.WithTimeout(ctx, opts.HandshakeTimeout)
	defer cancel()

	input := &pb.InputConfig{}
	if opts.StorageDirectory != "" {
		input.SetStorageDirectory(opts.StorageDirectory)
	}
	input.SetClientInfo(opts.ClientInfo.toProto())
	if len(opts.Env) > 0 {
		input.SetEnv(opts.Env)
	}
	if opts.UseInteractionsAPI {
		input.SetUseInteractionsApi(true)
	}

	if err := WriteInputConfig(c.stdin, input); err != nil {
		return fmt.Errorf("localharness: write input config: %w%s", err, c.stderrSuffix())
	}
	out, err := readOutputConfig(ctx, c.stdout)
	if err != nil {
		return fmt.Errorf("localharness: read output config: %w%s", err, c.stderrSuffix())
	}
	port := out.GetPort()
	if port <= 0 {
		return fmt.Errorf("localharness: harness reported no usable port (%d)%s", port, c.stderrSuffix())
	}

	// From here on stdout is not part of the protocol. Drain it so the
	// child never blocks writing to a full pipe.
	c.draining.Store(true)
	go c.drainStdout()

	c.logger.Debug("localharness: handshake done", "port", port)

	conn, actual, err := connectWebSocket(hsCtx, loopbackURLs(strconv.Itoa(int(port)), "/"), out.GetApiKey(), opts.ConnectRetries)
	if err != nil {
		return fmt.Errorf("localharness: connect websocket: %w%s", err, c.stderrSuffix())
	}
	c.ws = conn
	c.addr = actual
	return nil
}

// watchChild kills the child as soon as the connection is closed.
func (c *Conn) watchChild() {
	<-c.done
	c.killChild()
}

// killChild kills the child process. It is safe to call more than once.
func (c *Conn) killChild() {
	c.killOnce.Do(func() {
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
	})
}

// drainStdout consumes whatever the child writes to stdout after the
// handshake and then releases the pipe.
func (c *Conn) drainStdout() {
	defer close(c.stdoutDone)
	if c.stdout == nil {
		return
	}
	_, _ = io.Copy(io.Discard, c.stdout)
}

// Send writes one InputEvent as a protojson text frame.
//
// The frame is sent verbatim; the harness answers on the same connection,
// so the reply is picked up by Recv.
func (c *Conn) Send(ctx context.Context, ev *pb.InputEvent) error {
	if c == nil || c.ws == nil {
		return errors.New("localharness: connection is not open")
	}
	if ev == nil {
		return errors.New("localharness: nil InputEvent")
	}
	data, err := MarshalFrame(ev)
	if err != nil {
		return err
	}
	if err := c.ws.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("localharness: write input event: %w", err)
	}
	return nil
}

// Recv reads one OutputEvent.
//
// Unknown JSON fields are discarded: the harness may send fields this
// build's recovered descriptor does not know about.
func (c *Conn) Recv(ctx context.Context) (*pb.OutputEvent, error) {
	_, ev, err := c.RecvRaw(ctx)
	return ev, err
}

// RecvRaw reads one frame and returns both the raw JSON bytes and the
// decoded event.
//
// The raw bytes are returned even when decoding fails, so a caller can log
// exactly what the harness said.
func (c *Conn) RecvRaw(ctx context.Context) ([]byte, *pb.OutputEvent, error) {
	if c == nil || c.ws == nil {
		return nil, nil, errors.New("localharness: connection is not open")
	}
	typ, data, err := c.ws.Read(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("localharness: read frame: %w", err)
	}
	if typ != websocket.MessageText {
		return data, nil, fmt.Errorf("localharness: unexpected frame type %v (want text)", typ)
	}
	ev := &pb.OutputEvent{}
	if err := UnmarshalFrame(data, ev); err != nil {
		return data, nil, fmt.Errorf("localharness: decode output event: %w (raw: %s)", err, truncate(data))
	}
	return data, ev, nil
}

// Initialize sends the InitializeConversationEvent and blocks for the
// initialize_conversation_response.
//
// The event is sent standalone, not wrapped in an InputEvent: the
// InputEvent `oneof` has no member for it. The first frame the harness sees
// on the connection is therefore this one, and the first frame it sends back
// is the response.
//
// A nil cfg is sent as an empty configuration.
func (c *Conn) Initialize(ctx context.Context, cfg *pb.HarnessConfig) (*pb.InitializeConversationResponse, error) {
	if c == nil || c.ws == nil {
		return nil, errors.New("localharness: connection is not open")
	}
	if cfg == nil {
		cfg = &pb.HarnessConfig{}
	}
	init := &pb.InitializeConversationEvent{}
	init.SetConfig(cfg)

	data, err := MarshalFrame(init)
	if err != nil {
		return nil, err
	}
	if err := c.ws.Write(ctx, websocket.MessageText, data); err != nil {
		return nil, fmt.Errorf("localharness: write initialize event: %w", err)
	}
	raw, ev, err := c.RecvRaw(ctx)
	if err != nil {
		return nil, err
	}
	resp := ev.GetInitializeConversationResponse()
	if resp == nil {
		return nil, fmt.Errorf("localharness: expected initialize_conversation_response, got %q (raw: %s)",
			ev.WhichEvent(), truncate(raw))
	}
	return resp, nil
}

// Close terminates the child and closes the websocket. Idempotent.
//
// The child is killed with SIGKILL (the SDK's `process.kill()`), so nothing
// is left running: a harness that ignores a graceful shutdown would
// otherwise outlive the process that started it.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		// Closing done releases the watchdog, which kills the child.
		close(c.done)
		c.killChild()

		if c.ws != nil {
			// CloseNow, not Close: the graceful handshake waits up to five
			// seconds for the peer's close frame, and the peer is a
			// process we are about to kill.
			_ = c.ws.CloseNow()
		}

		if c.draining.Load() {
			select {
			case <-c.stdoutDone:
			case <-time.After(closeGraceTimeout):
			}
		}

		if c.cmd != nil {
			waited := make(chan error, 1)
			go func() { waited <- c.cmd.Wait() }()
			select {
			case err := <-waited:
				var exitErr *exec.ExitError
				if err != nil && !errors.As(err, &exitErr) {
					c.closeErr = err
				}
			case <-time.After(closeGraceTimeout):
				c.closeErr = fmt.Errorf("localharness: timed out waiting for the harness process to exit")
			}
		}
		if c.logger != nil {
			c.logger.Debug("localharness: connection closed", "addr", c.addr)
		}
	})
	return c.closeErr
}

// Addr returns the websocket URL, for diagnostics.
//
// It is the URL that actually answered — the harness may have been reached
// on `localhost` or on `127.0.0.1`.
func (c *Conn) Addr() string {
	if c == nil {
		return ""
	}
	return c.addr
}

// Stderr returns the child's captured stderr (bounded to the last 64 KiB).
//
// Only the tail is kept; a harness that logs in a loop would otherwise
// grow without bound.
func (c *Conn) Stderr() string {
	if c == nil || c.stderr == nil {
		return ""
	}
	return c.stderr.String()
}

// stderrSuffix renders the child's stderr as an error-message suffix.
func (c *Conn) stderrSuffix() string {
	tail := strings.TrimSpace(c.Stderr())
	if tail == "" {
		return ""
	}
	return "\n--- harness stderr ---\n" + tail + "\n--- end stderr ---"
}

// WriteInputConfig writes a length-prefixed InputConfig to w.
//
// The prefix is FrameLength bytes wide and holds the little-endian uint32
// length of the serialized protobuf that follows. Prefix and payload are
// written in a single call so the frame cannot be interleaved with another
// writer's bytes.
func WriteInputConfig(w io.Writer, cfg *pb.InputConfig) error {
	if w == nil {
		return errors.New("localharness: nil writer")
	}
	payload, err := proto.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("localharness: marshal input config: %w", err)
	}
	if len(payload) > maxFrameBytes {
		return fmt.Errorf("localharness: input config is %d bytes, over the %d byte limit", len(payload), maxFrameBytes)
	}
	frame := make([]byte, FrameLength+len(payload))
	binary.LittleEndian.PutUint32(frame[:FrameLength], uint32(len(payload)))
	copy(frame[FrameLength:], payload)
	if _, err := w.Write(frame); err != nil {
		return fmt.Errorf("localharness: write input config: %w", err)
	}
	return nil
}

// ReadOutputConfig reads a length-prefixed OutputConfig from r.
//
// A truncated frame is an error, never a partial message: the harness is
// mid-handshake, and a half-read config would mean connecting to port 0.
func ReadOutputConfig(r io.Reader) (*pb.OutputConfig, error) {
	if r == nil {
		return nil, errors.New("localharness: nil reader")
	}
	var prefix [FrameLength]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return nil, fmt.Errorf("localharness: read output config length: %w", err)
	}
	size := binary.LittleEndian.Uint32(prefix[:])
	if size > maxFrameBytes {
		return nil, fmt.Errorf("localharness: output config claims %d bytes, over the %d byte limit", size, maxFrameBytes)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("localharness: read output config payload (%d bytes): %w", size, err)
	}
	out := &pb.OutputConfig{}
	if err := proto.Unmarshal(payload, out); err != nil {
		return nil, fmt.Errorf("localharness: decode output config: %w", err)
	}
	return out, nil
}

// readOutputConfig is ReadOutputConfig with a cancellation hook.
//
// The read itself cannot be interrupted, so it runs on its own goroutine and
// the caller gives up when the context is done. The goroutine ends by
// itself once the child dies and the pipe closes.
func readOutputConfig(ctx context.Context, r io.Reader) (*pb.OutputConfig, error) {
	type result struct {
		cfg *pb.OutputConfig
		err error
	}
	done := make(chan result, 1)
	go func() {
		cfg, err := ReadOutputConfig(r)
		done <- result{cfg: cfg, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-done:
		return res.cfg, res.err
	}
}

// ConnectWebSocket dials ws://host:port/ with the x-goog-api-key header,
// retrying per the documented schedule. Exported for testing.
//
// The host component of url is ignored: only its port and path are used.
// The harness binds a loopback listener whose name may be `localhost` or
// `127.0.0.1` depending on how it was started, so every round tries
// `localhost` first and then `127.0.0.1`.
//
// The retry schedule is `retries` rounds; after each round the client waits
// `0.1 * 2^attempt` seconds (attempt counted from zero), so the default five
// rounds wait 0.1s, 0.2s, 0.4s, 0.8s and 1.6s.
func ConnectWebSocket(ctx context.Context, url, apiKey string, retries int) (*websocket.Conn, error) {
	conn, _, err := connectWebSocketURL(ctx, url, apiKey, retries)
	return conn, err
}

// connectWebSocketURL is ConnectWebSocket with the URL of the endpoint that
// actually answered.
func connectWebSocketURL(ctx context.Context, rawURL, apiKey string, retries int) (*websocket.Conn, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("localharness: parse websocket url %q: %w", rawURL, err)
	}
	port := parsed.Port()
	if port == "" {
		return nil, "", fmt.Errorf("localharness: websocket url %q has no port", rawURL)
	}
	path := parsed.Path
	if path == "" {
		path = "/"
	}
	return connectWebSocket(ctx, loopbackURLs(port, path), apiKey, retries)
}

// loopbackURLs builds the per-round candidate list for a port.
func loopbackURLs(port, path string) []string {
	out := make([]string, 0, len(loopbackHosts))
	for _, host := range loopbackHosts {
		out = append(out, (&url.URL{
			Scheme: "ws",
			Host:   net.JoinHostPort(host, port),
			Path:   path,
		}).String())
	}
	return out
}

// connectWebSocket runs the retry loop over a list of candidate URLs.
//
// Every round tries each URL in turn; a round that fails is followed by a
// `0.1 * 2^attempt` second backoff.
func connectWebSocket(ctx context.Context, urls []string, apiKey string, retries int) (*websocket.Conn, string, error) {
	if len(urls) == 0 {
		return nil, "", errors.New("localharness: no websocket url to try")
	}
	if retries <= 0 {
		retries = 1
	}
	var lastErr error
	for attempt := 0; attempt < retries; attempt++ {
		for _, target := range urls {
			conn, err := dialWebSocket(ctx, target, apiKey)
			if err == nil {
				return conn, target, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return nil, "", ctx.Err()
			}
		}
		delay := retryBaseDelay * time.Duration(1<<attempt)
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, "", fmt.Errorf("localharness: websocket connect failed after %d round(s): %w", retries, lastErr)
}

// dialWebSocket performs a single websocket handshake with the API key
// header.
func dialWebSocket(ctx context.Context, target, apiKey string) (*websocket.Conn, error) {
	header := http.Header{}
	header.Set(apiKeyHeader, apiKey)
	conn, resp, err := websocket.Dial(ctx, target, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("dial %s: %w (http %s)", target, err, resp.Status)
		}
		return nil, fmt.Errorf("dial %s: %w", target, err)
	}
	// The harness' frames are not size bounded: a single StepUpdate with a
	// large tool payload easily exceeds coder/websocket's 32 KiB default.
	conn.SetReadLimit(-1)
	return conn, nil
}

// ringBuffer keeps the tail of a byte stream.
type ringBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

// newRingBuffer returns a ring buffer that keeps at most max bytes.
func newRingBuffer(max int) *ringBuffer {
	if max <= 0 {
		max = 1
	}
	return &ringBuffer{max: max}
}

// Write appends p to the buffer, dropping the oldest bytes when it is full.
// It never fails: a stderr sink must not be able to break the child.
func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(p) >= r.max {
		r.buf = append(r.buf[:0], p[len(p)-r.max:]...)
		return len(p), nil
	}
	if overflow := len(r.buf) + len(p) - r.max; overflow > 0 {
		copy(r.buf, r.buf[overflow:])
		r.buf = r.buf[:len(r.buf)-overflow]
	}
	r.buf = append(r.buf, p...)
	return len(p), nil
}

// String returns the buffered tail.
func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}

// truncate shortens a blob for use in an error message.
func truncate(data []byte) string {
	const limit = 512
	s := string(data)
	if len(s) > limit {
		return s[:limit] + "…"
	}
	return s
}
