package remotetransport

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/rcterminal"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/lsproto"
	"github.com/justwasm/pigasus/originpolicy"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// postRPC sends a unary Connect-RPC call to the server under test.
func postRPC(t *testing.T, srv *TCPServer, path, contentType string, body []byte) *http.Response {
	t.Helper()
	url := "http://" + srv.Addr().String() + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set(headerViaRemoteControl, "true")
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	return resp
}

// TestTCPServer_RPCRouteAnswersBinaryProto checks the dialect the
// cloud's v2 client speaks: the request announces
// `application/proto`, and the answer has to be binary protobuf — a
// JSON body does not decode in the web UI ("invalid end group tag",
// "premature EOF"), which is why the remote-control page spun forever
// on "Connecting to instance…".
func TestTCPServer_RPCRouteAnswersBinaryProto(t *testing.T) {
	srv, cleanup := newTestRPCServer(t)
	defer cleanup()

	resp := postRPC(t, srv, "/exa.language_server_pb.LanguageServerService/GetStatus", contentTypeProto, nil)
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != contentTypeProto {
		t.Fatalf("Content-Type = %q, want %q", got, contentTypeProto)
	}
	body := readAll(t, resp)

	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	decoded, err := codec.DecodeResponse("GetStatus", body)
	if err != nil {
		t.Fatalf("DecodeResponse: %v (%x)", err, body)
	}
	var got map[string]any
	if err := json.Unmarshal(decoded, &got); err != nil {
		t.Fatalf("decode json: %v (%s)", err, decoded)
	}
	status, ok := got["status"].(map[string]any)
	if !ok {
		t.Fatalf("decoded response has no status block: %s", decoded)
	}
	if status["message"] != "ready" {
		t.Fatalf("status.message = %v, want ready (%s)", status["message"], decoded)
	}
}

// TestTCPServer_RPCRouteKeepsJSONDialect checks that the JSON dialect
// still works: the loopback UI and the v1 protocol speak it.
func TestTCPServer_RPCRouteKeepsJSONDialect(t *testing.T) {
	srv, cleanup := newTestRPCServer(t)
	defer cleanup()

	resp := postRPC(t, srv, "/exa.language_server_pb.LanguageServerService/GetStatus", contentTypeJSON, nil)
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != contentTypeJSON {
		t.Fatalf("Content-Type = %q, want %q", got, contentTypeJSON)
	}
	body := readAll(t, resp)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode json: %v (%s)", err, body)
	}
	if _, ok := got["status"]; !ok {
		t.Fatalf("response has no status block: %s", body)
	}
}

// TestTCPServer_RPCRouteStreamsBinaryProtoFrames checks the streaming
// dialect: the data frames carry binary protobuf — the web UI decodes
// every frame with the protobuf reader, whatever the unary mode is —
// and the trailer stays JSON.
func TestTCPServer_RPCRouteStreamsBinaryProtoFrames(t *testing.T) {
	srv, cleanup := newTestRPCServer(t)
	defer cleanup()

	url := "http://" + srv.Addr().String() + "/exa.language_server_pb.LanguageServerService/GetStatus"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set(headerViaRemoteControl, "true")
	req.Header.Set(headerStreamingFlag, "true")
	req.Header.Set("Content-Type", contentTypeConnectProto)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != contentTypeConnectProto {
		t.Fatalf("Content-Type = %q, want %q", got, contentTypeConnectProto)
	}
	envelopes := parseConnectEnvelopes(t, readAll(t, resp))
	if len(envelopes) < 2 {
		t.Fatalf("got %d envelopes, want at least 2", len(envelopes))
	}
	last := envelopes[len(envelopes)-1]
	if last.flags != ConnectFlagEndStream {
		t.Fatalf("last envelope flags = %#x, want %#x", last.flags, ConnectFlagEndStream)
	}
	// The trailer is parsed as JSON by the web UI, so it must not be
	// a protobuf message.
	var trailer map[string]any
	if err := json.Unmarshal(last.payload, &trailer); err != nil {
		t.Fatalf("trailer is not JSON: %v (%q)", err, last.payload)
	}
	// The data frame is binary protobuf.
	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	decoded, err := codec.DecodeResponse("GetStatus", envelopes[0].payload)
	if err != nil {
		t.Fatalf("DecodeResponse: %v (%x)", err, envelopes[0].payload)
	}
	var got map[string]any
	if err := json.Unmarshal(decoded, &got); err != nil {
		t.Fatalf("decode json: %v (%s)", err, decoded)
	}
	if _, ok := got["status"]; !ok {
		t.Fatalf("frame has no status block: %s", decoded)
	}
}

// TestTCPServer_RPCRouteStreamsAppStateAsProtobuf pins the frame the
// remote-control app needs to boot.
//
// 【一手】 2026-10-26, live: the CLI bundle's stream readers decode a
// frame with the *binary* protobuf decoder and nothing else, so a
// stream frame has to be protobuf. The app reads the app-state frame
// to learn whether the agent onboarding is done
// (`agentOnboardingCompleted === 2`), and the manager layout only
// mounts when that and the signed-in auth state agree — a frame the
// app cannot read means no layout, no `uiReady` message, and a
// dashboard that spins on "Connecting to instance…" forever.
func TestTCPServer_RPCRouteStreamsAppStateAsProtobuf(t *testing.T) {
	srv, cleanup := newTestRPCServer(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	url := "http://" + srv.Addr().String() + "/exa.language_server_pb.LanguageServerService/JetboxSubscribeToState"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set(headerViaRemoteControl, "true")
	req.Header.Set(headerStreamingFlag, "true")
	req.Header.Set("Content-Type", contentTypeConnectProto)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	head := make([]byte, 5)
	if _, err := io.ReadFull(resp.Body, head); err != nil {
		t.Fatalf("read envelope header: %v", err)
	}
	if head[0] != ConnectFlagMessage {
		t.Fatalf("envelope flags = %#x, want %#x", head[0], ConnectFlagMessage)
	}
	payload := make([]byte, binary.BigEndian.Uint32(head[1:5]))
	if _, err := io.ReadFull(resp.Body, payload); err != nil {
		t.Fatalf("read envelope payload: %v", err)
	}

	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	decoded, err := codec.DecodeResponse("JetboxSubscribeToState", payload)
	if err != nil {
		t.Fatalf("DecodeResponse: %v (%x)", err, payload)
	}
	var got struct {
		AppState struct {
			AgentOnboardingCompleted string `json:"agentOnboardingCompleted"`
		} `json:"appState"`
		UserConfig map[string]any `json:"userConfig"`
	}
	if err := json.Unmarshal(decoded, &got); err != nil {
		t.Fatalf("decode json: %v (%s)", err, decoded)
	}
	if got.AppState.AgentOnboardingCompleted != "AGENT_ONBOARDING_STATE_COMPLETED" {
		t.Fatalf("agentOnboardingCompleted = %q, want the completed state (%s)",
			got.AppState.AgentOnboardingCompleted, decoded)
	}
}

// TestStripConnectEnvelope checks the envelope handling: a streaming
// request from the cloud carries its message inside an envelope, the
// local UI sends the bare message.
func TestStripConnectEnvelope(t *testing.T) {
	payload := []byte("hello")
	enveloped := append([]byte{0x00, 0x00, 0x00, 0x00, byte(len(payload))}, payload...)

	if got := stripConnectEnvelope(enveloped); string(got) != "hello" {
		t.Errorf("stripConnectEnvelope(enveloped) = %q, want hello", got)
	}
	if got := stripConnectEnvelope(payload); string(got) != "hello" {
		t.Errorf("stripConnectEnvelope(bare) = %q, want hello", got)
	}
	if got := stripConnectEnvelope(nil); len(got) != 0 {
		t.Errorf("stripConnectEnvelope(nil) = %q, want empty", got)
	}
}

// TestDialectFor checks the content-type → dialect mapping.
func TestDialectFor(t *testing.T) {
	cases := map[string]connectDialect{
		"application/proto":            dialectProto,
		"application/connect+proto":    dialectProto,
		"application/json":             dialectJSON,
		"application/connect+json":     dialectJSON,
		"":                             dialectJSON,
		"application/proto; charset=x": dialectProto,
	}
	for ct, want := range cases {
		if got := dialectFor(ct); got != want {
			t.Errorf("dialectFor(%q) = %v, want %v", ct, got, want)
		}
	}
}

func TestTCPServer_CreateTerminalAndStreamOutput(t *testing.T) {
	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}

	termReg := rcterminal.NewRegistry()
	sess := remotecontrol.NewSessionState("1.4.7-test")
	sess.Workspace = t.TempDir()
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:      sess,
		Agents:       remotecontrol.DefaultAgentLister,
		Reader:       remotecontrol.DefaultOutputReader{},
		WebTerminals: termReg,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	policy := originpolicy.OriginPolicy{Patterns: []string{"https://antigravity.google.com"}}
	srv, err := NewTCPServer(context.Background(), disp, TCPConfig{
		Addr:         "127.0.0.1:0",
		Path:         "/rpc",
		OriginPolicy: &policy,
		WebUIHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	})
	if err != nil {
		t.Fatalf("NewTCPServer: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	// 1. Send CreateTerminal with binary proto
	inMsg, ok := codec.InputMessage("CreateTerminal")
	if !ok {
		t.Fatalf("InputMessage(CreateTerminal) not found")
	}
	dynReq := dynamicpb.NewMessage(inMsg)
	bCreateReq, err := proto.Marshal(dynReq)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}

	createResp := postRPC(t, srv, "/exa.language_server_pb.LanguageServerService/CreateTerminal", contentTypeProto, bCreateReq)
	defer createResp.Body.Close()

	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("CreateTerminal status = %d", createResp.StatusCode)
	}
	createBody := readAll(t, createResp)
	decCreate, err := codec.DecodeResponse("CreateTerminal", createBody)
	if err != nil {
		t.Fatalf("DecodeResponse CreateTerminal: %v (%x)", err, createBody)
	}
	t.Logf("CreateTerminal response: %s", string(decCreate))

	var createAns struct {
		Terminal struct {
			TerminalID string `json:"terminalId"`
		} `json:"terminal"`
	}
	if err := json.Unmarshal(decCreate, &createAns); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	terminalID := createAns.Terminal.TerminalID
	if terminalID == "" {
		t.Fatalf("no terminalId in response: %s", string(decCreate))
	}

	// 2. Send StreamTerminalOutput with streaming Connect proto
	streamInMsg, ok := codec.InputMessage("StreamTerminalOutput")
	if !ok {
		t.Fatalf("InputMessage(StreamTerminalOutput) not found")
	}
	streamDynReq := dynamicpb.NewMessage(streamInMsg)
	streamDynReq.Set(streamInMsg.Fields().ByName("terminal_id"), protoreflect.ValueOfString(terminalID))
	bStreamReq, err := proto.Marshal(streamDynReq)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}

	// Connect envelope around stream request
	envelopedReq := append([]byte{ConnectFlagMessage, 0, 0, 0, byte(len(bStreamReq))}, bStreamReq...)

	url := "http://" + srv.Addr().String() + "/exa.language_server_pb.LanguageServerService/StreamTerminalOutput"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(envelopedReq))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set(headerViaRemoteControl, "true")
	req.Header.Set(headerStreamingFlag, "true")
	req.Header.Set("Content-Type", contentTypeConnectProto)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST StreamTerminalOutput: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StreamTerminalOutput status = %d", resp.StatusCode)
	}

	// Read stream frames
	reader := bufioReader(resp.Body)
	// Read first envelope: header (5 bytes)
	hdr := make([]byte, 5)
	if _, err := io.ReadFull(reader, hdr); err != nil {
		t.Fatalf("read first envelope header: %v", err)
	}
	flags := hdr[0]
	length := binary.BigEndian.Uint32(hdr[1:5])
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatalf("read first envelope payload: %v", err)
	}
	t.Logf("First envelope: flags=%#x, len=%d", flags, length)

	decFrame, err := codec.DecodeResponse("StreamTerminalOutput", payload)
	if err != nil {
		t.Fatalf("DecodeResponse StreamTerminalOutput frame: %v", err)
	}
	t.Logf("First frame: %s", string(decFrame))
}

func bufioReader(r io.Reader) io.Reader {
	return r
}
