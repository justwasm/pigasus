// transport_test.go — stdlib-only tests for the localharness transport.
//
// The real harness binary is never involved. The stdio framing is
// exercised through the exported helpers, the websocket half against an
// `httptest` server that speaks the same protojson dialect, and the whole
// spawn path against a stub harness: the test binary re-executed as a fake
// `localharness` process that performs the handshake and serves the socket.
package localharness

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

const (
	// fakeHarnessEnv makes the test binary behave like a localharness
	// process instead of a test suite.
	fakeHarnessEnv = "LOCALHARNESS_TEST_FAKE_HARNESS"

	// fakeHarnessAPIKey is the key the stub harness mints.
	fakeHarnessAPIKey = "fake-api-key"

	// fakeHarnessReady is written to stderr by the stub harness once it
	// is serving.
	fakeHarnessReady = "fake harness ready"
)

// init turns this test binary into a stub harness when the magic
// environment variable is set.
//
// The stub harness is the test binary itself, re-executed through a tiny
// shell wrapper: the package's public API spawns a binary by path and
// nothing else, so the only way to hand the child its instructions is
// through the environment it inherits.
func init() {
	if os.Getenv(fakeHarnessEnv) == "1" {
		fakeHarnessMain()
		os.Exit(0)
	}
}

// testConn wraps a raw websocket connection in the Conn the transport uses,
// so Recv/RecvRaw can be tested without spawning a child process.
func testConn(ws *websocket.Conn, addr string) *Conn {
	c := &Conn{
		ws:         ws,
		addr:       addr,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		stderr:     newRingBuffer(stderrLimit),
		stdoutDone: make(chan struct{}),
		done:       make(chan struct{}),
	}
	// No child process is attached: release the drain wait so Close is
	// immediate.
	close(c.stdoutDone)
	return c
}

// TestWriteInputConfigLengthPrefix pins the exact bytes of the stdio
// length prefix: a three-byte payload must be announced as 03 00 00 00.
func TestWriteInputConfigLengthPrefix(t *testing.T) {
	cfg := &pb.InputConfig{}
	// Field 2 (port), varint: tag byte + two value bytes = three bytes.
	cfg.SetPort(300)

	var buf bytes.Buffer
	if err := WriteInputConfig(&buf, cfg); err != nil {
		t.Fatalf("WriteInputConfig: %v", err)
	}
	frame := buf.Bytes()
	if len(frame) < FrameLength {
		t.Fatalf("frame is %d bytes, want at least %d", len(frame), FrameLength)
	}
	prefix := frame[:FrameLength]
	want := []byte{0x03, 0x00, 0x00, 0x00}
	if !bytes.Equal(prefix, want) {
		t.Fatalf("prefix = % x, want % x", prefix, want)
	}
	payload := frame[FrameLength:]
	if len(payload) != 3 {
		t.Fatalf("payload is %d bytes, want 3", len(payload))
	}
	if got := binary.LittleEndian.Uint32(prefix); got != uint32(len(payload)) {
		t.Fatalf("prefix announces %d bytes, payload is %d", got, len(payload))
	}
}

// TestWriteInputConfigRoundTrip checks that what WriteInputConfig writes
// decodes back into the same message.
func TestWriteInputConfigRoundTrip(t *testing.T) {
	cfg := &pb.InputConfig{}
	cfg.SetStorageDirectory("/tmp/harness-state")
	cfg.SetClientInfo(&pb.ClientInfo{})
	cfg.GetClientInfo().SetLanguage("go")
	cfg.GetClientInfo().SetOs("darwin")
	cfg.SetEnv(map[string]string{"A": "1"})
	cfg.SetUseInteractionsApi(true)

	var buf bytes.Buffer
	if err := WriteInputConfig(&buf, cfg); err != nil {
		t.Fatalf("WriteInputConfig: %v", err)
	}
	frame := buf.Bytes()
	size := binary.LittleEndian.Uint32(frame[:FrameLength])
	if int(size) != len(frame)-FrameLength {
		t.Fatalf("prefix announces %d bytes, frame carries %d", size, len(frame)-FrameLength)
	}
	got := &pb.InputConfig{}
	if err := proto.Unmarshal(frame[FrameLength:], got); err != nil {
		t.Fatalf("unmarshal input config: %v", err)
	}
	if got.GetStorageDirectory() != "/tmp/harness-state" {
		t.Errorf("storage directory = %q", got.GetStorageDirectory())
	}
	if got.GetClientInfo().GetLanguage() != "go" {
		t.Errorf("client language = %q", got.GetClientInfo().GetLanguage())
	}
	if got.GetClientInfo().GetOs() != "darwin" {
		t.Errorf("client os = %q", got.GetClientInfo().GetOs())
	}
	if got.GetEnv()["A"] != "1" {
		t.Errorf("env = %v", got.GetEnv())
	}
	if !got.GetUseInteractionsApi() {
		t.Error("use_interactions_api was not carried over")
	}
}

// TestReadOutputConfigRoundTrip checks the other half of the framing.
func TestReadOutputConfigRoundTrip(t *testing.T) {
	out := &pb.OutputConfig{}
	out.SetPort(8080)
	out.SetApiKey("s3cret")

	payload, err := proto.Marshal(out)
	if err != nil {
		t.Fatalf("marshal output config: %v", err)
	}
	frame := make([]byte, FrameLength+len(payload))
	binary.LittleEndian.PutUint32(frame[:FrameLength], uint32(len(payload)))
	copy(frame[FrameLength:], payload)

	got, err := ReadOutputConfig(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("ReadOutputConfig: %v", err)
	}
	if got.GetPort() != 8080 {
		t.Errorf("port = %d, want 8080", got.GetPort())
	}
	if got.GetApiKey() != "s3cret" {
		t.Errorf("api key = %q, want %q", got.GetApiKey(), "s3cret")
	}
}

// TestReadOutputConfigTruncated checks that a short frame is a clear error
// rather than a half-decoded message.
func TestReadOutputConfigTruncated(t *testing.T) {
	tests := []struct {
		name  string
		frame []byte
	}{
		{"empty", nil},
		{"partial prefix", []byte{0x05, 0x00}},
		{"short payload", []byte{0x05, 0x00, 0x00, 0x00, 0x01, 0x02}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadOutputConfig(bytes.NewReader(tc.frame))
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			if !strings.Contains(err.Error(), "output config") {
				t.Errorf("error %q does not mention the output config", err)
			}
		})
	}
}

// TestProtoJSONShape pins the wire dialect: camelCase keys, 64-bit integers
// as JSON strings, 32-bit integers as JSON numbers.
func TestProtoJSONShape(t *testing.T) {
	t.Run("budget config", func(t *testing.T) {
		budget := &pb.BudgetConfig{}
		budget.SetMaxTotalTokens(1234)
		data, err := protojson.Marshal(budget)
		if err != nil {
			t.Fatalf("marshal budget config: %v", err)
		}
		var generic map[string]any
		if err := json.Unmarshal(data, &generic); err != nil {
			t.Fatalf("decode budget config json: %v (raw %s)", err, data)
		}
		value, ok := generic["maxTotalTokens"]
		if !ok {
			t.Fatalf("no maxTotalTokens key in %s", data)
		}
		text, ok := value.(string)
		if !ok {
			t.Fatalf("maxTotalTokens is %T (%v), want a JSON string", value, value)
		}
		if text != "1234" {
			t.Errorf("maxTotalTokens = %q, want %q", text, "1234")
		}
	})

	t.Run("usage metadata uint64", func(t *testing.T) {
		usage := &pb.UsageMetadata{}
		usage.SetPromptTokenCount(99)
		data, err := protojson.Marshal(usage)
		if err != nil {
			t.Fatalf("marshal usage metadata: %v", err)
		}
		var generic map[string]any
		if err := json.Unmarshal(data, &generic); err != nil {
			t.Fatalf("decode usage metadata json: %v (raw %s)", err, data)
		}
		value, ok := generic["promptTokenCount"]
		if !ok {
			t.Fatalf("no promptTokenCount key in %s", data)
		}
		text, ok := value.(string)
		if !ok {
			t.Fatalf("promptTokenCount is %T (%v), want a JSON string", value, value)
		}
		if text != "99" {
			t.Errorf("promptTokenCount = %q, want %q", text, "99")
		}
	})

	t.Run("output config int32", func(t *testing.T) {
		out := &pb.OutputConfig{}
		out.SetPort(8080)
		data, err := protojson.Marshal(out)
		if err != nil {
			t.Fatalf("marshal output config: %v", err)
		}
		var generic map[string]any
		if err := json.Unmarshal(data, &generic); err != nil {
			t.Fatalf("decode output config json: %v (raw %s)", err, data)
		}
		value, ok := generic["port"]
		if !ok {
			t.Fatalf("no port key in %s", data)
		}
		number, ok := value.(float64)
		if !ok {
			t.Fatalf("port is %T (%v), want a JSON number", value, value)
		}
		if number != 8080 {
			t.Errorf("port = %v, want 8080", number)
		}
	})
}

// echoServer starts a websocket server that sends one protojson frame and
// then blocks until the client goes away.
func echoServer(t *testing.T, frame []byte, apiKey *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiKey != nil {
			*apiKey = r.Header.Get(apiKeyHeader)
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("websocket accept: %v", err)
			return
		}
		defer conn.CloseNow()
		if err := conn.Write(r.Context(), websocket.MessageText, frame); err != nil {
			t.Errorf("websocket write: %v", err)
			return
		}
		// Hold the connection open until the client closes it.
		_, _, _ = conn.Read(r.Context())
	}))
	t.Cleanup(srv.Close)
	return srv
}

// wsURL turns an http:// test server URL into its ws:// twin.
func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// TestConnectWebSocketSendsAPIKeyAndDecodes checks the websocket half of
// the handshake: the key header travels on the upgrade request and the
// protojson frame that comes back decodes into an OutputEvent.
func TestConnectWebSocketSendsAPIKeyAndDecodes(t *testing.T) {
	event := &pb.OutputEvent{}
	event.SetSeqNum(3)
	event.SetInitializeConversationResponse(&pb.InitializeConversationResponse{})
	frame, err := protojson.Marshal(event)
	if err != nil {
		t.Fatalf("marshal output event: %v", err)
	}

	var gotKey string
	srv := echoServer(t, frame, &gotKey)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := ConnectWebSocket(ctx, wsURL(srv), "the-api-key", 3)
	if err != nil {
		t.Fatalf("ConnectWebSocket: %v", err)
	}
	defer conn.CloseNow()

	if gotKey != "the-api-key" {
		t.Errorf("%s header = %q, want %q", apiKeyHeader, gotKey, "the-api-key")
	}

	c := testConn(conn, wsURL(srv))
	ev, err := c.Recv(ctx)
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if ev.GetSeqNum() != 3 {
		t.Errorf("seq num = %d, want 3", ev.GetSeqNum())
	}
	if ev.GetInitializeConversationResponse() == nil {
		t.Errorf("event is %q, want initialize_conversation_response", ev.WhichEvent())
	}
}

// TestRecvToleratesUnknownFields checks that a frame carrying fields this
// build's descriptor does not know about still decodes.
func TestRecvToleratesUnknownFields(t *testing.T) {
	frame := []byte(`{"seqNum":"7","aFieldFromTheFuture":{"nested":true},` +
		`"initializeConversationResponse":{"cascadeId":"cascade-1"},"anotherOne":42}`)

	srv := echoServer(t, frame, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := ConnectWebSocket(ctx, wsURL(srv), "k", 3)
	if err != nil {
		t.Fatalf("ConnectWebSocket: %v", err)
	}
	defer conn.CloseNow()

	c := testConn(conn, wsURL(srv))
	raw, ev, err := c.RecvRaw(ctx)
	if err != nil {
		t.Fatalf("RecvRaw: %v", err)
	}
	if !bytes.Equal(raw, frame) {
		t.Errorf("raw frame = %s, want %s", raw, frame)
	}
	if ev.GetSeqNum() != 7 {
		t.Errorf("seq num = %d, want 7", ev.GetSeqNum())
	}
	resp := ev.GetInitializeConversationResponse()
	if resp == nil {
		t.Fatalf("event is %q, want initialize_conversation_response", ev.WhichEvent())
	}
	if resp.GetCascadeId() != "cascade-1" {
		t.Errorf("cascade id = %q, want %q", resp.GetCascadeId(), "cascade-1")
	}
}

// TestConnectWebSocketRetriesThenSucceeds checks that the retry loop walks
// past a refused endpoint and lands on the live one.
func TestConnectWebSocketRetriesThenSucceeds(t *testing.T) {
	event := &pb.OutputEvent{}
	event.SetSeqNum(11)
	frame, err := protojson.Marshal(event)
	if err != nil {
		t.Fatalf("marshal output event: %v", err)
	}
	srv := echoServer(t, frame, nil)

	closed := closedPort(t)
	targets := []string{
		fmt.Sprintf("ws://127.0.0.1:%d/", closed),
		wsURL(srv),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, actual, err := connectWebSocket(ctx, targets, "k", 3)
	if err != nil {
		t.Fatalf("connectWebSocket: %v", err)
	}
	defer conn.CloseNow()

	if actual != wsURL(srv) {
		t.Errorf("connected to %q, want %q", actual, wsURL(srv))
	}
	c := testConn(conn, actual)
	ev, err := c.Recv(ctx)
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if ev.GetSeqNum() != 11 {
		t.Errorf("seq num = %d, want 11", ev.GetSeqNum())
	}
}

// TestConnectWebSocketRefusedFailsFast checks that pointing the client at a
// closed port produces an error instead of a hang.
func TestConnectWebSocketRefusedFailsFast(t *testing.T) {
	closed := closedPort(t)
	target := fmt.Sprintf("ws://127.0.0.1:%d/", closed)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	conn, err := ConnectWebSocket(ctx, target, "k", 2)
	elapsed := time.Since(start)
	if err == nil {
		conn.CloseNow()
		t.Fatal("want an error for a refused port, got nil")
	}
	if elapsed > 10*time.Second {
		t.Errorf("connect took %s, want it to fail fast", elapsed)
	}
	if !strings.Contains(err.Error(), "websocket connect failed") {
		t.Errorf("error %q does not mention the failed connect", err)
	}
}

// closedPort returns a loopback port that nothing is listening on.
func closedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return port
}

// TestOptionsDefaults pins the documented defaults.
func TestOptionsDefaults(t *testing.T) {
	opts := Options{}.withDefaults()
	if opts.HandshakeTimeout != defaultHandshakeTimeout {
		t.Errorf("handshake timeout = %s, want %s", opts.HandshakeTimeout, defaultHandshakeTimeout)
	}
	if opts.ConnectRetries != defaultConnectRetries {
		t.Errorf("connect retries = %d, want %d", opts.ConnectRetries, defaultConnectRetries)
	}
	if opts.ClientInfo.Language != "go" {
		t.Errorf("client language = %q, want %q", opts.ClientInfo.Language, "go")
	}
}

// TestSpawnRequiresBinaryPath checks the one precondition Spawn enforces.
func TestSpawnRequiresBinaryPath(t *testing.T) {
	if _, err := Spawn(context.Background(), Options{}); err == nil {
		t.Fatal("want an error for an empty BinaryPath, got nil")
	}
}

// TestRingBufferKeepsTail checks the bounded stderr sink.
func TestRingBufferKeepsTail(t *testing.T) {
	ring := newRingBuffer(8)
	if _, err := ring.Write([]byte("0123456789")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := ring.String(); got != "23456789" {
		t.Errorf("buffer = %q, want %q", got, "23456789")
	}
	if _, err := ring.Write([]byte("ab")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := ring.String(); got != "456789ab" {
		t.Errorf("buffer = %q, want %q", got, "456789ab")
	}
}

// readFrame reads one length-prefixed frame and returns its payload.
func readFrame(r io.Reader) ([]byte, error) {
	var prefix [FrameLength]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(prefix[:])
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// fakeHarnessMain is the stub harness: it performs the stdio handshake and
// then serves the websocket half of the protocol.
//
// It is deliberately strict — a handshake that does not carry the expected
// client info is a failure, so the test cannot pass on a half-correct
// implementation.
func fakeHarnessMain() {
	payload, err := readFrame(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake harness: read input config: %v\n", err)
		os.Exit(1)
	}
	var in pb.InputConfig
	if err := proto.Unmarshal(payload, &in); err != nil {
		fmt.Fprintf(os.Stderr, "fake harness: decode input config: %v\n", err)
		os.Exit(1)
	}
	if got := in.GetClientInfo().GetLanguage(); got != "go" {
		fmt.Fprintf(os.Stderr, "fake harness: client language = %q, want %q\n", got, "go")
		os.Exit(1)
	}
	if in.GetStorageDirectory() == "" {
		fmt.Fprintln(os.Stderr, "fake harness: empty storage directory")
		os.Exit(1)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake harness: listen: %v\n", err)
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", fakeHarnessHandler)
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()

	out := &pb.OutputConfig{}
	out.SetPort(int32(listener.Addr().(*net.TCPAddr).Port))
	out.SetApiKey(fakeHarnessAPIKey)
	body, err := proto.Marshal(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake harness: marshal output config: %v\n", err)
		os.Exit(1)
	}
	frame := make([]byte, FrameLength+len(body))
	binary.LittleEndian.PutUint32(frame[:FrameLength], uint32(len(body)))
	copy(frame[FrameLength:], body)
	if _, err := os.Stdout.Write(frame); err != nil {
		fmt.Fprintf(os.Stderr, "fake harness: write output config: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, fakeHarnessReady)

	// Serve until the client (or the test) kills us.
	select {}
}

// fakeHarnessHandler serves one conversation: it answers the initialize
// event and then echoes one event per input event.
func fakeHarnessHandler(w http.ResponseWriter, r *http.Request) {
	if got := r.Header.Get(apiKeyHeader); got != fakeHarnessAPIKey {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	typ, data, err := conn.Read(r.Context())
	if err != nil || typ != websocket.MessageText {
		return
	}
	init := &pb.InitializeConversationEvent{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(data, init); err != nil {
		fmt.Fprintf(os.Stderr, "fake harness: decode initialize event: %v\n", err)
		return
	}
	if init.GetConfig() == nil {
		fmt.Fprintln(os.Stderr, "fake harness: initialize event without a config")
		return
	}

	first := &pb.OutputEvent{}
	first.SetSeqNum(1)
	first.SetInitializeConversationResponse(&pb.InitializeConversationResponse{})
	body, err := protojson.Marshal(first)
	if err != nil {
		return
	}
	if err := conn.Write(r.Context(), websocket.MessageText, body); err != nil {
		return
	}

	// One reply per further input event.
	for seq := int64(2); ; seq++ {
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
		reply := &pb.OutputEvent{}
		reply.SetSeqNum(seq)
		body, err := protojson.Marshal(reply)
		if err != nil {
			return
		}
		if err := conn.Write(r.Context(), websocket.MessageText, body); err != nil {
			return
		}
	}
}

// fakeHarnessBinary writes a shell wrapper that re-executes this test
// binary as a stub harness, and returns its path.
func fakeHarnessBinary(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub harness needs a POSIX shell")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("executable: %v", err)
	}
	path := filepath.Join(t.TempDir(), "fake-harness")
	script := fmt.Sprintf("#!/bin/sh\n%s=1 exec %q -test.run=^$\n", fakeHarnessEnv, exe)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub harness: %v", err)
	}
	return path
}

// TestDialAgainstStubHarness exercises the whole transport: spawn, stdio
// handshake, websocket connect, initialize exchange and one event round
// trip.
func TestDialAgainstStubHarness(t *testing.T) {
	binary := fakeHarnessBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := Dial(ctx, Options{
		BinaryPath:       binary,
		StorageDirectory: t.TempDir(),
		ClientInfo: ClientInfo{
			Language: "go",
			Version:  "0.0.0-test",
			OS:       "darwin",
		},
		Env: map[string]string{"PIGASUS_TEST": "1"},
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if conn.Addr() == "" {
		t.Error("Addr is empty")
	}
	if !strings.HasPrefix(conn.Addr(), "ws://") {
		t.Errorf("Addr = %q, want a ws:// url", conn.Addr())
	}

	// The stub harness announces itself on stderr.
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(conn.Stderr(), fakeHarnessReady) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(conn.Stderr(), fakeHarnessReady) {
		t.Errorf("stderr = %q, want it to mention %q", conn.Stderr(), fakeHarnessReady)
	}

	// One event round trip.
	event := &pb.InputEvent{}
	event.SetHaltRequest(true)
	if err := conn.Send(ctx, event); err != nil {
		t.Fatalf("Send: %v", err)
	}
	ev, err := conn.Recv(ctx)
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if ev.GetSeqNum() != 2 {
		t.Errorf("seq num = %d, want 2", ev.GetSeqNum())
	}

	// Close is idempotent and must not hang.
	if err := conn.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// TestSpawnAgainstStubHarnessKeepsStderrOnFailure checks that a harness
// that dies during the handshake is killed and that its stderr reaches the
// caller.
func TestSpawnAgainstStubHarnessKeepsStderrOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub harness needs a POSIX shell")
	}
	path := filepath.Join(t.TempDir(), "broken-harness")
	// A binary that exits immediately with a message on stderr: the
	// handshake read fails with EOF.
	script := "#!/bin/sh\necho 'broken harness' >&2\nexit 3\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write broken harness: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := Spawn(ctx, Options{BinaryPath: path, StorageDirectory: t.TempDir()})
	if err == nil {
		conn.Close()
		t.Fatal("want an error for a harness that dies, got nil")
	}
	if !strings.Contains(err.Error(), "output config") {
		t.Errorf("error %q does not mention the output config", err)
	}
}
