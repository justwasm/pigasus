// server_test.go — the harness side of the transport.
//
// The tests never spawn the reference binary: the server side is exercised
// directly, and the client side is a plain websocket client.
package localharness

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// TestReadInputConfig pins the stdio handshake the server side performs.
//
// 【一手】 The frame is a 4-byte little-endian length followed by that many
// bytes of binary protobuf.
func TestReadInputConfig(t *testing.T) {
	input := &pb.InputConfig{}
	input.SetPort(4321)
	input.SetBindAddress("localhost")
	payload, err := proto.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	frame := make([]byte, FrameLength+len(payload))
	binary.LittleEndian.PutUint32(frame[:FrameLength], uint32(len(payload)))
	copy(frame[FrameLength:], payload)

	cfg, err := ReadInputConfig(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("ReadInputConfig: %v", err)
	}
	if got := cfg.GetPort(); got != 4321 {
		t.Errorf("port = %d, want 4321", got)
	}
	if got := cfg.GetBindAddress(); got != "localhost" {
		t.Errorf("bind address = %q, want %q", got, "localhost")
	}
}

// TestReadInputConfigRejectsTruncatedFrame pins that a half-read config is
// an error: binding the wrong port is worse than failing.
func TestReadInputConfigRejectsTruncatedFrame(t *testing.T) {
	frame := []byte{0x10, 0x00, 0x00, 0x00, 0x01, 0x02}
	if _, err := ReadInputConfig(bytes.NewReader(frame)); err == nil {
		t.Fatal("ReadInputConfig accepted a truncated frame")
	}
}

// TestWriteOutputConfigRoundTrip pins the mirror of the handshake: the
// frame is a little-endian length followed by the binary protobuf.
func TestWriteOutputConfigRoundTrip(t *testing.T) {
	out := &pb.OutputConfig{}
	out.SetPort(5555)
	out.SetApiKey("secret")

	var buf bytes.Buffer
	if err := WriteOutputConfig(&buf, out); err != nil {
		t.Fatalf("WriteOutputConfig: %v", err)
	}
	raw := buf.Bytes()
	size := binary.LittleEndian.Uint32(raw[:FrameLength])
	if int(size) != len(raw)-FrameLength {
		t.Fatalf("the prefix says %d bytes, the frame carries %d", size, len(raw)-FrameLength)
	}
	decoded := &pb.OutputConfig{}
	if err := proto.Unmarshal(raw[FrameLength:], decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := decoded.GetPort(); got != 5555 {
		t.Errorf("port = %d, want 5555", got)
	}
	if got := decoded.GetApiKey(); got != "secret" {
		t.Errorf("api key = %q, want %q", got, "secret")
	}
}

// TestMintAPIKey pins that the key is fresh and long enough to matter.
func TestMintAPIKey(t *testing.T) {
	first, err := MintAPIKey()
	if err != nil {
		t.Fatalf("MintAPIKey: %v", err)
	}
	second, err := MintAPIKey()
	if err != nil {
		t.Fatalf("MintAPIKey: %v", err)
	}
	if first == second {
		t.Error("two minted keys are equal")
	}
	if len(first) != ServerAPIKeyBytes*2 {
		t.Errorf("the key is %d characters, want %d", len(first), ServerAPIKeyBytes*2)
	}
}

// TestServerRejectsMissingAPIKey pins that the key is checked *before* the
// websocket upgrade: a request without the right key never becomes a
// websocket.
func TestServerRejectsMissingAPIKey(t *testing.T) {
	srv, err := Listen(ServerConfig{BindAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() { _ = srv.Serve(ctx, func(context.Context, *ServerConn) error { return nil }) }()

	waitForPort(t, srv.Port())

	resp, err := http.Get("http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(srv.Port())) + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// TestServerSessionRoundTrip pins the websocket half: the client sends the
// standalone initialize event, the server reads it, and the answer comes
// back as an OutputEvent.
func TestServerSessionRoundTrip(t *testing.T) {
	srv, err := Listen(ServerConfig{BindAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = srv.Close() }()

	type received struct {
		cfg *pb.HarnessConfig
		err error
	}
	got := make(chan received, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go func() {
		_ = srv.Serve(ctx, func(ctx context.Context, conn *ServerConn) error {
			frame, err := conn.Read(ctx)
			if err != nil {
				got <- received{err: err}
				return err
			}
			got <- received{cfg: frame.Initialize}
			answer := &pb.OutputEvent{}
			answer.SetInitializeConversationResponse(&pb.InitializeConversationResponse{})
			return conn.Send(ctx, answer)
		})
	}()

	waitForPort(t, srv.Port())

	conn, err := dialServer(t, srv)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	init := &pb.InitializeConversationEvent{}
	cfg := &pb.HarnessConfig{}
	cfg.SetCascadeId("cascade-1")
	init.SetConfig(cfg)
	if err := conn.Write(ctx, websocket.MessageText, mustMarshal(t, init)); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("the server failed to read the initialize frame: %v", r.err)
		}
		if r.cfg == nil {
			t.Fatal("the server did not see an initialize event")
		}
		if r.cfg.GetCascadeId() != "cascade-1" {
			t.Errorf("cascade id = %q, want %q", r.cfg.GetCascadeId(), "cascade-1")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the initialize event")
	}

	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("frame type = %v, want text", typ)
	}
	answer := &pb.OutputEvent{}
	if err := UnmarshalFrame(data, answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if answer.GetInitializeConversationResponse() == nil {
		t.Error("the answer carries no initialize response")
	}
}

// TestServerReadsInputEvents pins that the frames after the initialize
// event decode as InputEvents.
func TestServerReadsInputEvents(t *testing.T) {
	srv, err := Listen(ServerConfig{BindAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = srv.Close() }()

	events := make(chan *pb.InputEvent, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go func() {
		_ = srv.Serve(ctx, func(ctx context.Context, conn *ServerConn) error {
			for {
				frame, err := conn.Read(ctx)
				if err != nil {
					return err
				}
				if frame.Event != nil {
					events <- frame.Event
					return nil
				}
			}
		})
	}()

	waitForPort(t, srv.Port())

	conn, err := dialServer(t, srv)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	ev := &pb.InputEvent{}
	ev.SetHaltRequest(true)
	if err := conn.Write(ctx, websocket.MessageText, mustMarshal(t, ev)); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case got := <-events:
		if !got.GetHaltRequest() {
			t.Error("the halt request was lost")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the input event")
	}
}

// TestSessionOutlivesTheHandshakeTimeout pins that the handshake timeout
// bounds the HTTP request and *not* the conversation.
//
// A conversation may legitimately run for hours; a thirty-second cap on
// the session would kill every real one.
func TestSessionOutlivesTheHandshakeTimeout(t *testing.T) {
	srv, err := Listen(ServerConfig{
		BindAddress:      "127.0.0.1",
		HandshakeTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cancelled := make(chan error, 1)
	go func() {
		_ = srv.Serve(ctx, func(ctx context.Context, conn *ServerConn) error {
			// Four times the handshake timeout.
			select {
			case <-time.After(400 * time.Millisecond):
				cancelled <- nil
			case <-ctx.Done():
				cancelled <- ctx.Err()
			}
			return nil
		})
	}()

	waitForPort(t, srv.Port())

	conn, err := dialServer(t, srv)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	select {
	case err := <-cancelled:
		if err != nil {
			t.Fatalf("the session was cut short: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the session")
	}
}

// TestGracefulCloseIsSeenByTheClient pins the close the harness sends when
// a run fails.
//
// 【一手】 The reference closes the connection with the normal close (1000)
// when the run fails, and the client sees it as a normal end — not as a
// broken pipe. The client's own close frame comes back, and the client
// reports "received 1000 (OK); then sent 1000 (OK)".
func TestGracefulCloseIsSeenByTheClient(t *testing.T) {
	srv, err := Listen(ServerConfig{BindAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	closed := make(chan error, 1)
	go func() {
		_ = srv.Serve(ctx, func(ctx context.Context, conn *ServerConn) error {
			err := conn.CloseGracefully()
			closed <- err
			return err
		})
	}()

	waitForPort(t, srv.Port())

	conn, err := dialServer(t, srv)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// The client's read ends with the close the harness sent.
	readErr := make(chan error, 1)
	go func() {
		_, _, err := conn.Read(ctx)
		readErr <- err
	}()

	select {
	case err := <-readErr:
		if err == nil {
			t.Fatal("the client's read ended without an error")
		}
		var closeErr websocket.CloseError
		if !errors.As(err, &closeErr) {
			t.Fatalf("the client's read ended with %v, want a close error", err)
		}
		if closeErr.Code != websocket.StatusNormalClosure {
			t.Errorf("close code = %d, want %d", closeErr.Code, websocket.StatusNormalClosure)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the close")
	}

	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("timed out waiting for the harness to close")
	}
}

// TestACancelledContextDropsTheFrameAndKeepsTheConnection pins the rule
// the harness's turn context forces on the transport: a cancelled context
// means the caller gave up, so the frame is dropped.
//
// The connection must survive it. The websocket closes the **whole
// connection** when the context of a write in flight is cancelled, and a
// turn's context is cancelled when the turn ends — so a frame written
// with that context would take the connection down with it.
func TestACancelledContextDropsTheFrameAndKeepsTheConnection(t *testing.T) {
	srv, err := Listen(ServerConfig{BindAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dropped := make(chan error, 1)
	written := make(chan error, 1)
	go func() {
		_ = srv.Serve(ctx, func(ctx context.Context, conn *ServerConn) error {
			gone, cancel := context.WithCancel(ctx)
			cancel()
			answer := &pb.OutputEvent{}
			step := &pb.StepUpdate{}
			step.SetText("dropped")
			answer.SetStepUpdate(step)
			err := conn.Send(gone, answer)
			dropped <- err

			// The connection is still usable.
			answer = &pb.OutputEvent{}
			step = &pb.StepUpdate{}
			step.SetText("kept")
			answer.SetStepUpdate(step)
			err = conn.Send(ctx, answer)
			written <- err
			return err
		})
	}()

	waitForPort(t, srv.Port())

	conn, err := dialServer(t, srv)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	frame := &pb.OutputEvent{}
	if err := UnmarshalFrame(data, frame); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := frame.GetStepUpdate().GetText(); got != "kept" {
		t.Errorf("the frame that arrived = %q, want %q", got, "kept")
	}

	select {
	case err := <-dropped:
		if err == nil {
			t.Error("a frame written with a cancelled context was not dropped")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the dropped write")
	}
	select {
	case err := <-written:
		if err != nil {
			t.Errorf("the write failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the write")
	}
}

// TestACancelledWriteDoesNotCloseTheConnection pins the other half of the
// same rule: a write that is *in flight* when the context is cancelled
// must still land.
//
// The websocket closes the whole connection when the context of a write in
// flight is cancelled — the frame is truncated and the client sees a dead
// connection instead of a close frame. The server writes a frame that does
// not fit into the socket buffers, so the write is still in flight when
// the context is cancelled.
func TestACancelledWriteDoesNotCloseTheConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("the write has to fill the socket buffers")
	}
	srv, err := Listen(ServerConfig{BindAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	written := make(chan time.Time, 1)
	go func() {
		_ = srv.Serve(ctx, func(ctx context.Context, conn *ServerConn) error {
			writeCtx, cancelWrite := context.WithCancel(ctx)
			defer cancelWrite()
			go func() {
				// The client is not reading yet, so the write is
				// still in flight when the context is cancelled.
				time.Sleep(300 * time.Millisecond)
				cancelWrite()
			}()
			answer := &pb.OutputEvent{}
			step := &pb.StepUpdate{}
			step.SetText(strings.Repeat("x", 16<<20))
			answer.SetStepUpdate(step)
			err := conn.Send(writeCtx, answer)
			written <- time.Now()
			return err
		})
	}()

	waitForPort(t, srv.Port())

	conn, err := dialServer(t, srv)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(64 << 20)

	// The client reads late: the write is still in flight when the
	// context is cancelled.
	time.Sleep(600 * time.Millisecond)
	started := time.Now()

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	frame := &pb.OutputEvent{}
	if err := UnmarshalFrame(data, frame); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := len(frame.GetStepUpdate().GetText()); got != 16<<20 {
		t.Errorf("the frame that arrived carries %d bytes, want %d", got, 16<<20)
	}

	select {
	case at := <-written:
		if at.Before(started) {
			t.Log("the write did not block; the cancellation did not race it")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the write")
	}
}

// dialServer opens a websocket to the server with the right API key.
func dialServer(t *testing.T, srv *Server) (*websocket.Conn, error) {
	t.Helper()
	header := http.Header{}
	header.Set(apiKeyHeader, srv.APIKey())
	addr := "ws://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(srv.Port())) + "/"
	conn, _, err := websocket.Dial(context.Background(), addr, &websocket.DialOptions{HTTPHeader: header})
	return conn, err
}

// mustMarshal encodes a message as a frame.
func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	data, err := MarshalFrame(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// waitForPort blocks until the server's listener answers.
func waitForPort(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the server did not start listening on port %d", port)
}
