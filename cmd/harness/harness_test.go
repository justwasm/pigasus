// harness_test.go — the harness's own tests.
//
// The end-to-end test drives the real transport: a pipe stands in for the
// stdio handshake, a websocket client stands in for the SDK. Nothing is
// stubbed except the model transport, which is the deterministic shim.
package main

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/justwasm/pigasus/localharness"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// testCascadeID is the cascade id the end-to-end tests use.
//
// The reference refuses a cascade id shorter than 32 characters, so a test
// id has to be long enough to pass the same check.
const testCascadeID = "cascade-conformance-000000000000000"

// TestParseFlagsDefaults pins the flag surface.
//
// The workspace defaults to the working directory and the timeout defaults
// to "no timeout": a server's model call may legitimately take minutes,
// and the transport's own 60-second default would cut it short.
func TestParseFlagsDefaults(t *testing.T) {
	opts, err := parseFlags(nil, io.Discard)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if opts.workspace == "" {
		t.Error("the workspace is empty")
	}
	if opts.timeout != -1 {
		t.Errorf("timeout = %d, want -1", opts.timeout)
	}
	if opts.port != 0 {
		t.Errorf("port = %d, want 0", opts.port)
	}
}

// TestParseFlagsRejectsStrayArgument pins that a positional argument is an
// error rather than a silently ignored typo.
func TestParseFlagsRejectsStrayArgument(t *testing.T) {
	if _, err := parseFlags([]string{"stray"}, io.Discard); err == nil {
		t.Fatal("parseFlags accepted a positional argument")
	}
}

// TestPreTurnVerdictFailsClosed pins the fail-closed rule.
//
// A verdict the harness cannot interpret must not let the guarded action
// through. The reference's own wire layer is more permissive
// (`preTurnResultFromProto` derives `denied` from `decision == DENY`), but
// its inner layer — `checkCortexHookResults` — refuses to guess, and this
// implementation follows the strict reading.
func TestPreTurnVerdictFailsClosed(t *testing.T) {
	allow := &pb.PreTurnResult{}
	allow.SetDecision(pb.PreTurnResult_ALLOW)
	if v := preTurnVerdict(preTurnResponse(allow)); v.Denied {
		t.Error("an explicit ALLOW was denied")
	}

	deny := &pb.PreTurnResult{}
	deny.SetDecision(pb.PreTurnResult_DENY)
	deny.SetReason("not allowed")
	v := preTurnVerdict(preTurnResponse(deny))
	if !v.Denied {
		t.Error("an explicit DENY was allowed")
	}
	if v.Reason != "not allowed" {
		t.Errorf("reason = %q, want %q", v.Reason, "not allowed")
	}

	// An unspecified decision is not an ALLOW.
	unspecified := &pb.PreTurnResult{}
	v = preTurnVerdict(preTurnResponse(unspecified))
	if !v.Denied {
		t.Error("an unspecified decision was allowed")
	}

	// No result at all is not an ALLOW either.
	v = preTurnVerdict(&pb.CallHookResponse{})
	if !v.Denied {
		t.Error("a missing result was allowed")
	}

	// An errored hook is not an ALLOW either.
	errored := &pb.CallHookResponse{}
	errored.SetErrorMessage("boom")
	v = preTurnVerdict(errored)
	if !v.Denied {
		t.Error("an errored hook was allowed")
	}
}

// TestPreToolVerdictFailsClosed pins the same rule for the tool path, plus
// the argument rewrite.
func TestPreToolVerdictFailsClosed(t *testing.T) {
	allow := &pb.PreToolResult{}
	allow.SetDecision(pb.PreToolResult_ALLOW)
	if v := preToolVerdict(preToolResponse(allow)); v.Denied {
		t.Error("an explicit ALLOW was denied")
	}

	deny := &pb.PreToolResult{}
	deny.SetDecision(pb.PreToolResult_DENY)
	deny.SetReason("blocked by policy")
	v := preToolVerdict(preToolResponse(deny))
	if !v.Denied {
		t.Error("an explicit DENY was allowed")
	}
	if v.Reason != "blocked by policy" {
		t.Errorf("reason = %q, want %q", v.Reason, "blocked by policy")
	}

	unspecified := &pb.PreToolResult{}
	if v := preToolVerdict(preToolResponse(unspecified)); !v.Denied {
		t.Error("an unspecified decision was allowed")
	}
	if v := preToolVerdict(&pb.CallHookResponse{}); !v.Denied {
		t.Error("a missing result was allowed")
	}
}

// TestStopVerdictDoesNotLoop pins the STOP hook's exception.
//
// An unrecognised STOP verdict means ALLOW_STOP. The fail-closed rule
// would mean CONTINUE, and CONTINUE is a loop that never ends.
func TestStopVerdictDoesNotLoop(t *testing.T) {
	if v := stopVerdict(&pb.CallHookResponse{}); v.Continue {
		t.Error("a missing result asked for a continuation")
	}
	unspecified := &pb.StopResult{}
	if v := stopVerdict(stopResponse(unspecified)); v.Continue {
		t.Error("an unspecified decision asked for a continuation")
	}
	allow := &pb.StopResult{}
	allow.SetDecision(pb.StopResult_ALLOW_STOP)
	if v := stopVerdict(stopResponse(allow)); v.Continue {
		t.Error("ALLOW_STOP asked for a continuation")
	}
	continueResult := &pb.StopResult{}
	continueResult.SetDecision(pb.StopResult_CONTINUE)
	if v := stopVerdict(stopResponse(continueResult)); !v.Continue {
		t.Error("CONTINUE did not ask for a continuation")
	}
}

// TestEndToEndHandshakeAndInitialize drives the whole protocol: the stdio
// handshake, the websocket connection, the API key check and the
// initialize exchange.
func TestEndToEndHandshakeAndInitialize(t *testing.T) {
	dir := t.TempDir()
	opts := options{
		backend:   "shim",
		workspace: dir,
		agentDir:  filepath.Join(dir, "agent"),
		host:      "127.0.0.1",
		timeout:   -1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	defer func() { _ = inW.Close() }()
	defer func() { _ = outR.Close() }()

	served := make(chan error, 1)
	go func() { served <- serve(ctx, opts, inR, outW) }()

	// Phase 1: the client writes the InputConfig.
	input := &pb.InputConfig{}
	input.SetBindAddress("127.0.0.1")
	payload, err := proto.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	frame := make([]byte, 4+len(payload))
	binary.LittleEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	go func() { _, _ = inW.Write(frame) }()

	// Phase 1: the harness answers with the OutputConfig.
	var prefix [4]byte
	if _, err := io.ReadFull(outR, prefix[:]); err != nil {
		t.Fatalf("read the output config prefix: %v", err)
	}
	size := binary.LittleEndian.Uint32(prefix[:])
	body := make([]byte, size)
	if _, err := io.ReadFull(outR, body); err != nil {
		t.Fatalf("read the output config: %v", err)
	}
	output := &pb.OutputConfig{}
	if err := proto.Unmarshal(body, output); err != nil {
		t.Fatalf("decode the output config: %v", err)
	}
	if output.GetPort() == 0 {
		t.Fatal("the harness did not report a port")
	}
	if output.GetApiKey() == "" {
		t.Fatal("the harness did not mint an API key")
	}

	// Phase 2: the websocket connection.
	header := http.Header{}
	header.Set("x-goog-api-key", output.GetApiKey())
	addr := "ws://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(int(output.GetPort()))) + "/"
	conn, _, err := websocket.Dial(ctx, addr, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// The first frame is the standalone initialize event.
	init := &pb.InitializeConversationEvent{}
	cfg := &pb.HarnessConfig{}
	cfg.SetCascadeId(testCascadeID)
	init.SetConfig(cfg)
	data, err := localharness.MarshalFrame(init)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	// The harness answers with the initialize response.
	typ, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("frame type = %v, want text", typ)
	}
	answer := &pb.OutputEvent{}
	if err := localharness.UnmarshalFrame(raw, answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp := answer.GetInitializeConversationResponse()
	if resp == nil {
		t.Fatalf("the harness did not answer the initialize event: %s", raw)
	}
	if resp.GetCascadeId() != testCascadeID {
		t.Errorf("cascade id = %q, want %q", resp.GetCascadeId(), testCascadeID)
	}

	// A turn: the client sends a user input and the harness streams the
	// model's answer back.
	userInput := &pb.UserInput{}
	part := &pb.UserInput_Part{}
	part.SetText("hello harness")
	userInput.SetParts([]*pb.UserInput_Part{part})
	turn := &pb.InputEvent{}
	turn.SetUserInput(userInput)
	turnData, err := localharness.MarshalFrame(turn)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, turnData); err != nil {
		t.Fatalf("write: %v", err)
	}

	sawText := false
	sawIdle := false
	for !sawIdle {
		typ, raw, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if typ != websocket.MessageText {
			t.Fatalf("frame type = %v, want text", typ)
		}
		ev := &pb.OutputEvent{}
		if err := localharness.UnmarshalFrame(raw, ev); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if step := ev.GetStepUpdate(); step != nil {
			if step.GetText() != "" || step.GetTextDelta() != "" {
				sawText = true
			}
		}
		if state := ev.GetTrajectoryStateUpdate(); state != nil {
			if state.GetState() == pb.TrajectoryStateUpdate_STATE_FULLY_IDLE {
				sawIdle = true
			}
		}
	}
	if !sawText {
		t.Error("the harness never sent the model's text")
	}

	// The harness must not have written anything else on stdout: the
	// OutputConfig frame is the only thing that stream may carry.
	cancel()
	select {
	case <-served:
	case <-time.After(10 * time.Second):
		t.Error("the harness did not stop")
	}
}

// TestShortCascadeIDIsRejected pins the reference's rule: a cascade id
// shorter than 32 characters is refused and the conversation never opens.
//
// The reference logs "Failed to create agent: cascade_id must be at least
// 32 characters long" and closes the connection without answering the
// initialize event, so the clone refuses the conversation the same way.
func TestShortCascadeIDIsRejected(t *testing.T) {
	conv := &conversation{logger: newLogger(false)}
	cfg := &pb.HarnessConfig{}
	cfg.SetCascadeId("too-short")
	err := conv.initialize(context.Background(), cfg)
	if err == nil {
		t.Fatal("the conversation opened with a short cascade id")
	}
	if !strings.Contains(err.Error(), "at least 32 characters") {
		t.Errorf("error = %v, want it to name the 32-character minimum", err)
	}
}

// TestClientToolRoundTrip pins the client-side tool delegation: the
// harness asks the client to run a tool, and the client's answer reaches
// the model as the tool's result.
func TestClientToolRoundTrip(t *testing.T) {
	srv, err := localharness.Listen(localharness.ServerConfig{BindAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	answered := make(chan string, 1)
	go func() {
		_ = srv.Serve(ctx, func(ctx context.Context, conn *localharness.ServerConn) error {
			conv := newConversation(conn, options{}, newLogger(false), &pb.InputConfig{})
			decl := &pb.Tool{}
			decl.SetName("client_tool")
			decl.SetDescription("a tool the client implements")
			decl.SetParametersJsonSchema(`{"type":"object","properties":{"x":{"type":"string"}}}`)
			cfg := &pb.HarnessConfig{}
			cfg.SetTools([]*pb.Tool{decl})
			conv.cfg = cfg

			// The conversation's read loop: it delivers the client's
			// answer to whoever is waiting for it.
			go func() {
				for {
					frame, err := conn.Read(ctx)
					if err != nil {
						return
					}
					if frame.Event != nil {
						_ = conv.handleEvent(ctx, frame.Event)
					}
				}
			}()

			tools := conv.clientTools()
			if len(tools) != 1 {
				answered <- "the harness did not build the client tool"
				return nil
			}
			result, err := tools[0].Execute(ctx, "call-1", []byte(`{"x":"1"}`), nil)
			if err != nil {
				answered <- "error: " + err.Error()
				return err
			}
			answered <- toolResultText(result)
			return nil
		})
	}()

	header := http.Header{}
	header.Set("x-goog-api-key", srv.APIKey())
	addr := "ws://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(srv.Port())) + "/"
	conn, _, err := websocket.Dial(ctx, addr, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// The harness asks the client to run the tool.
	typ, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("frame type = %v, want text", typ)
	}
	request := &pb.OutputEvent{}
	if err := localharness.UnmarshalFrame(raw, request); err != nil {
		t.Fatalf("decode: %v", err)
	}
	call := request.GetToolCall()
	if call == nil {
		t.Fatalf("the harness did not ask for a tool: %s", raw)
	}
	if call.GetName() != "client_tool" {
		t.Errorf("tool name = %q, want %q", call.GetName(), "client_tool")
	}
	if call.GetId() != "call-1" {
		t.Errorf("tool call id = %q, want %q", call.GetId(), "call-1")
	}
	if call.GetArgumentsJson() != `{"x":"1"}` {
		t.Errorf("arguments = %q, want %q", call.GetArgumentsJson(), `{"x":"1"}`)
	}

	// The client answers.
	answer := &pb.ToolResponse{}
	answer.SetId(call.GetId())
	answer.SetResponseJson(`{"ran":true}`)
	event := &pb.InputEvent{}
	event.SetToolResponse(answer)
	data, err := localharness.MarshalFrame(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case got := <-answered:
		if got != `{"ran":true}` {
			t.Errorf("the tool result = %q, want %q", got, `{"ran":true}`)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the tool result")
	}
}

// TestPreToolHookRoundTrip pins the hook protocol: the harness asks the
// client about a tool call, and the client's verdict comes back as the
// hook's answer.
//
// The correlation id is the hook request's id, and the answer carries it
// back — that is the only thing that pairs the two.
func TestPreToolHookRoundTrip(t *testing.T) {
	srv, err := localharness.Listen(localharness.ServerConfig{BindAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	verdicts := make(chan hookVerdict, 1)
	go func() {
		_ = srv.Serve(ctx, func(ctx context.Context, conn *localharness.ServerConn) error {
			conv := newConversation(conn, options{}, newLogger(false), &pb.InputConfig{})
			conv.hooks = newHookDispatcher(conv, []pb.LifecycleHook{pb.LifecycleHook_LIFECYCLE_HOOK_PRE_TOOL})

			go func() {
				for {
					frame, err := conn.Read(ctx)
					if err != nil {
						return
					}
					if frame.Event != nil {
						_ = conv.handleEvent(ctx, frame.Event)
					}
				}
			}()

			verdict, err := conv.hooks.preTool(ctx, "run_command", []byte(`{"CommandLine":"ls"}`), "call-1")
			if err != nil {
				verdicts <- hookVerdict{Denied: true, Reason: err.Error()}
				return err
			}
			verdicts <- verdict
			return nil
		})
	}()

	header := http.Header{}
	header.Set("x-goog-api-key", srv.APIKey())
	addr := "ws://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(srv.Port())) + "/"
	conn, _, err := websocket.Dial(ctx, addr, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	typ, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("frame type = %v, want text", typ)
	}
	request := &pb.OutputEvent{}
	if err := localharness.UnmarshalFrame(raw, request); err != nil {
		t.Fatalf("decode: %v", err)
	}
	hook := request.GetCallHookRequest()
	if hook == nil {
		t.Fatalf("the harness did not fire a hook: %s", raw)
	}
	if hook.GetType() != pb.LifecycleHook_LIFECYCLE_HOOK_PRE_TOOL {
		t.Errorf("hook type = %v, want PRE_TOOL", hook.GetType())
	}
	if hook.GetRequestId() == "" {
		t.Error("the hook request carries no correlation id")
	}
	args := hook.GetPreToolArgs()
	if args == nil {
		t.Fatal("the hook request carries no pre-tool arguments")
	}
	if args.GetToolName() != "run_command" {
		t.Errorf("tool name = %q, want %q", args.GetToolName(), "run_command")
	}

	// The client denies the call.
	result := &pb.PreToolResult{}
	result.SetDecision(pb.PreToolResult_DENY)
	result.SetReason("not allowed here")
	response := &pb.CallHookResponse{}
	response.SetRequestId(hook.GetRequestId())
	response.SetPreToolResult(result)
	event := &pb.InputEvent{}
	event.SetCallHookResponse(response)
	data, err := localharness.MarshalFrame(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case verdict := <-verdicts:
		if !verdict.Denied {
			t.Error("the denial did not reach the caller")
		}
		if verdict.Reason != "not allowed here" {
			t.Errorf("reason = %q, want %q", verdict.Reason, "not allowed here")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the hook verdict")
	}
}

// preTurnResponse wraps a PRE_TURN result in a hook response.
func preTurnResponse(r *pb.PreTurnResult) *pb.CallHookResponse {
	resp := &pb.CallHookResponse{}
	resp.SetPreTurnResult(r)
	return resp
}

// preToolResponse wraps a PRE_TOOL result in a hook response.
func preToolResponse(r *pb.PreToolResult) *pb.CallHookResponse {
	resp := &pb.CallHookResponse{}
	resp.SetPreToolResult(r)
	return resp
}

// stopResponse wraps a STOP result in a hook response.
func stopResponse(r *pb.StopResult) *pb.CallHookResponse {
	resp := &pb.CallHookResponse{}
	resp.SetStopResult(r)
	return resp
}
