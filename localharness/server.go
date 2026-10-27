// server.go — the server side of the localharness transport.
//
// # What this is
//
// `transport.go` implements the *client* half of Google Antigravity's
// `localharness` protocol: it spawns the closed-source harness binary,
// performs the stdio handshake and speaks the websocket. This file is the
// mirror image — the half a harness implements. It exists because the
// clone's own `cmd/harness` is a reimplementation of that binary, and
// reimplementing the framing twice would guarantee the two halves drift.
//
// The two halves share three things, and sharing them is the point:
//
//   - the 4-byte little-endian length prefix (`FrameLength`);
//   - the `x-goog-api-key` header (`apiKeyHeader`);
//   - the protojson frame dialect (`frame.go`).
//
// # The handshake, from this side
//
//	phase 1 — stdio
//	    client  --[4-byte LE length][InputConfig protobuf]-->  harness stdin
//	    client  <--[4-byte LE length][OutputConfig protobuf]--  harness stdout
//
//	phase 2 — websocket
//	    client  --ws://<host>:<port>/ , header x-goog-api-key: <key]--> harness
//	    client  <--[protojson OutputEvent]--   harness
//	    client  --[protojson InputEvent]-->    harness
//
// # Hard constraints (硬约束)
//
//   - The length prefix is **little-endian** and exactly four bytes. A
//     big-endian prefix desynchronises the stream permanently.
//   - The API key is checked **before** the websocket upgrade: a connection
//     without the right key must not become a websocket at all.
//   - Frames are not size limited. `coder/websocket` defaults to a 32 KiB
//     read limit, which a single `StepUpdate` with a large tool payload
//     exceeds, so the limit is disabled (`SetReadLimit(-1)`).
//   - The first frame the client sends is an `InitializeConversationEvent`,
//     sent standalone. It is *not* wrapped in an `InputEvent` — that
//     message's `oneof` has no member for it.
//
// # Limitations
//
//   - One connection carries one conversation. The reference harness may
//     multiplex more; nothing here does.
//   - The origin check is disabled (`InsecureSkipVerify`). The listener is
//     loopback-only and gated on an API key, and the client is not a
//     browser — but a browser on the same machine could reach it if it
//     learned the key.
package localharness

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

const (
	// ServerAPIKeyBytes is the number of random bytes in a minted API key.
	// The key travels in a header, not in the URL, so 16 bytes (128 bits)
	// is ample for a loopback-only listener.
	ServerAPIKeyBytes = 16

	// serverHandshakeTimeout bounds the HTTP handshake. A client that
	// opens a socket and never finishes the request must not pin a
	// goroutine forever. It does *not* bound the session: a conversation
	// may run for hours.
	serverHandshakeTimeout = 30 * time.Second

	// writeTimeout bounds one frame write. The write does not run with
	// the caller's context — a cancelled context would drop the whole
	// connection — so it needs a bound of its own.
	writeTimeout = 30 * time.Second
)

// MintAPIKey returns a fresh API key: ServerAPIKeyBytes of random bytes,
// hex-encoded.
//
// The key is the only thing standing between a loopback listener and any
// process on the machine, so it comes from crypto/rand and never from a
// counter or a timestamp.
func MintAPIKey() (string, error) {
	buf := make([]byte, ServerAPIKeyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("localharness: mint api key: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// ReadInputConfig reads a length-prefixed InputConfig from r.
//
// This is the server's half of the phase-1 handshake: the client writes the
// `InputConfig`, the harness reads it.
//
// A truncated frame is an error, never a partial message: the harness is
// mid-handshake, and a half-read config would mean binding the wrong port.
func ReadInputConfig(r io.Reader) (*pb.InputConfig, error) {
	if r == nil {
		return nil, errors.New("localharness: nil reader")
	}
	var prefix [FrameLength]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return nil, fmt.Errorf("localharness: read input config length: %w", err)
	}
	size := binary.LittleEndian.Uint32(prefix[:])
	if size > maxFrameBytes {
		return nil, fmt.Errorf("localharness: input config claims %d bytes, over the %d byte limit", size, maxFrameBytes)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("localharness: read input config payload (%d bytes): %w", size, err)
	}
	cfg := &pb.InputConfig{}
	if err := proto.Unmarshal(payload, cfg); err != nil {
		return nil, fmt.Errorf("localharness: decode input config: %w", err)
	}
	return cfg, nil
}

// WriteOutputConfig writes a length-prefixed OutputConfig to w.
//
// This is the server's half of the phase-1 handshake: the harness answers
// with the port it bound and the API key it minted.
//
// Prefix and payload are written in a single call so the frame cannot be
// interleaved with another writer's bytes.
func WriteOutputConfig(w io.Writer, cfg *pb.OutputConfig) error {
	if w == nil {
		return errors.New("localharness: nil writer")
	}
	payload, err := proto.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("localharness: marshal output config: %w", err)
	}
	if len(payload) > maxFrameBytes {
		return fmt.Errorf("localharness: output config is %d bytes, over the %d byte limit", len(payload), maxFrameBytes)
	}
	frame := make([]byte, FrameLength+len(payload))
	binary.LittleEndian.PutUint32(frame[:FrameLength], uint32(len(payload)))
	copy(frame[FrameLength:], payload)
	if _, err := w.Write(frame); err != nil {
		return fmt.Errorf("localharness: write output config: %w", err)
	}
	return nil
}

// ServerConfig configures a harness-side listener.
type ServerConfig struct {
	// BindAddress is the address the listener binds. An empty value binds
	// the loopback interface only.
	BindAddress string

	// Port is the TCP port to bind. Zero picks a free one; the port that
	// was actually bound is reported by Server.Port.
	Port int

	// APIKey is the key clients must present in the `x-goog-api-key`
	// header. An empty value mints one.
	APIKey string

	// Logger receives debug-level diagnostics. A nil Logger discards them.
	Logger *slog.Logger

	// HandshakeTimeout bounds the HTTP handshake. Zero uses
	// serverHandshakeTimeout. It does *not* bound the session.
	HandshakeTimeout time.Duration
}

// Server is a harness-side websocket server.
//
// The zero value is not usable; call Listen.
type Server struct {
	cfg       ServerConfig
	ln        net.Listener
	key       string
	logger    *slog.Logger
	handshake time.Duration
	http      *http.Server
}

// Listen binds the server's listener and mints its API key.
//
// The listener is bound before the handshake completes, because the port
// has to be in the OutputConfig the client is waiting for.
func Listen(cfg ServerConfig) (*Server, error) {
	host := cfg.BindAddress
	if host == "" {
		host = "127.0.0.1"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(cfg.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("localharness: listen on %s: %w", addr, err)
	}
	key := cfg.APIKey
	if key == "" {
		key, err = MintAPIKey()
		if err != nil {
			_ = ln.Close()
			return nil, err
		}
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	handshakeTimeout := cfg.HandshakeTimeout
	if handshakeTimeout <= 0 {
		handshakeTimeout = serverHandshakeTimeout
	}
	return &Server{cfg: cfg, ln: ln, key: key, logger: logger, handshake: handshakeTimeout}, nil
}

// Addr returns the bound address.
func (s *Server) Addr() net.Addr {
	if s == nil || s.ln == nil {
		return nil
	}
	return s.ln.Addr()
}

// Port returns the bound TCP port.
func (s *Server) Port() int {
	if s == nil || s.ln == nil {
		return 0
	}
	addr, ok := s.ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0
	}
	return addr.Port
}

// APIKey returns the key clients must present.
func (s *Server) APIKey() string {
	if s == nil {
		return ""
	}
	return s.key
}

// Close closes the listener.
func (s *Server) Close() error {
	if s == nil || s.ln == nil {
		return nil
	}
	return s.ln.Close()
}

// SessionHandler handles one accepted connection.
//
// The handler owns the connection until it returns; the server closes the
// websocket afterwards.
type SessionHandler func(ctx context.Context, conn *ServerConn) error

// Serve accepts connections and hands each to handler.
//
// The API key is checked before the upgrade: a request without the right
// key gets a 401 and never becomes a websocket.
func (s *Server) Serve(ctx context.Context, handler SessionHandler) error {
	if s == nil || s.ln == nil {
		return errors.New("localharness: server is not listening")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(apiKeyHeader); got != s.key {
			http.Error(w, "localharness: missing or invalid "+apiKeyHeader, http.StatusUnauthorized)
			return
		}
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			s.logger.Debug("localharness: websocket accept failed", "error", err)
			return
		}
		// The frames are not size bounded: one StepUpdate with a large
		// tool payload easily exceeds coder/websocket's 32 KiB default.
		ws.SetReadLimit(-1)
		conn := &ServerConn{ws: ws}
		defer func() { _ = conn.Close() }()
		// The session lives as long as the client is there and the server
		// is running. It is *not* bounded by the handshake timeout: a
		// conversation may legitimately run for hours, and the HTTP
		// handshake is already bounded by ReadHeaderTimeout.
		sessionCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			select {
			case <-r.Context().Done():
				// The client went away.
				cancel()
			case <-sessionCtx.Done():
			}
		}()
		if err := handler(sessionCtx, conn); err != nil {
			s.logger.Debug("localharness: session ended", "error", err)
		}
	})
	s.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: s.handshake,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		_ = s.http.Close()
	}()
	err := s.http.Serve(s.ln)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		// The server was shut down, or the listener was closed under it.
		return nil
	}
	return err
}

// Frame is one decoded websocket frame.
//
// The protocol has two shapes on the wire and this is the union of them:
// the standalone `InitializeConversationEvent` and the `InputEvent`s that
// follow it.
type Frame struct {
	// Initialize is set when the frame carried the conversation's
	// configuration. Only the first frame may do so.
	Initialize *pb.HarnessConfig

	// Event is set when the frame carried an ordinary input event.
	Event *pb.InputEvent
}

// ServerConn is a harness-side websocket connection.
//
// Writes are serialised internally: `coder/websocket` allows exactly one
// concurrent writer, and the agent loop, the hook dispatcher and the
// client-tool delegate all write from their own goroutines.
type ServerConn struct {
	ws *websocket.Conn

	writeMu sync.Mutex
}

// Send writes one OutputEvent as a protojson text frame.
//
// A cancelled context means the caller gave up: the frame is dropped and
// the error says so.
//
// The write itself runs with a context of its own. The websocket closes
// the **whole connection** when the context of a write in flight is
// cancelled, and a turn's context is cancelled when the turn ends — so
// writing with it would drop the connection under the client's feet
// instead of ending the turn. The write is bounded by a timeout of its
// own: a peer that stopped reading must not block the harness forever.
func (c *ServerConn) Send(ctx context.Context, ev *pb.OutputEvent) error {
	if c == nil || c.ws == nil {
		return errors.New("localharness: connection is not open")
	}
	if ev == nil {
		return errors.New("localharness: nil OutputEvent")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("localharness: write output event: %w", err)
	}
	data, err := MarshalFrame(ev)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	if err := c.ws.Write(writeCtx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("localharness: write output event: %w", err)
	}
	return nil
}

// Read reads the next frame.
//
// The first frame of a conversation is the standalone
// `InitializeConversationEvent`; every frame after it is an `InputEvent`.
// A frame that carries neither is an error.
func (c *ServerConn) Read(ctx context.Context) (*Frame, error) {
	if c == nil || c.ws == nil {
		return nil, errors.New("localharness: connection is not open")
	}
	typ, data, err := c.ws.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("localharness: read frame: %w", err)
	}
	if typ != websocket.MessageText {
		return nil, fmt.Errorf("localharness: unexpected frame type %v (want text)", typ)
	}
	return decodeFrame(data)
}

// decodeFrame classifies a raw frame.
//
// The initialize event is a different message type than the input events,
// and it is not wrapped in one, so the frame is tried against both. A
// frame that decodes as neither is rejected.
func decodeFrame(data []byte) (*Frame, error) {
	init := &pb.InitializeConversationEvent{}
	if err := UnmarshalFrame(data, init); err == nil && init.HasConfig() {
		return &Frame{Initialize: init.GetConfig()}, nil
	}
	ev := &pb.InputEvent{}
	if err := UnmarshalFrame(data, ev); err != nil {
		return nil, fmt.Errorf("localharness: decode frame: %w", err)
	}
	return &Frame{Event: ev}, nil
}

// Close closes the websocket. It is safe to call more than once.
func (c *ServerConn) Close() error {
	if c == nil || c.ws == nil {
		return nil
	}
	// CloseNow, not Close: the graceful handshake waits up to five seconds
	// for the peer's close frame, and the peer is a client that may already
	// be gone.
	return c.ws.CloseNow()
}

// CloseGracefully ends the connection with the WebSocket close handshake.
//
// The reference closes the connection when a run fails, and the client sees
// a *normal* close (1000) — not a dropped connection. The handshake is
// worth doing there: Close drops the connection without one, and a client
// that watches for it sees an error where the reference shows an end.
//
// The handshake waits up to five seconds for the peer's close frame, so it
// belongs to the paths where the peer is known to be there.
func (c *ServerConn) CloseGracefully() error {
	if c == nil || c.ws == nil {
		return nil
	}
	return c.ws.Close(websocket.StatusNormalClosure, "")
}
