package lifecyclehooks

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

func TestTypeNames(t *testing.T) {
	cases := map[Type]string{
		TypeOnSessionStart: "ON_SESSION_START",
		TypeOnSessionEnd:   "ON_SESSION_END",
		TypePreTurn:        "PRE_TURN",
		TypePostTurn:       "POST_TURN",
		TypePreTool:        "PRE_TOOL",
		TypePostTool:       "POST_TOOL",
		TypeOnToolError:    "ON_TOOL_ERROR",
		TypeOnCompaction:   "ON_COMPACTION",
		TypeStop:           "STOP",
	}
	for typ, want := range cases {
		if got := typ.String(); got != want {
			t.Errorf("Type(%d).String() = %q, want %q", int(typ), got, want)
		}
		if got := typ.ProtoName(); got != "LIFECYCLE_HOOK_"+want {
			t.Errorf("Type(%d).ProtoName() = %q", int(typ), got)
		}
		if !typ.Valid() {
			t.Errorf("Type(%d) should be valid", int(typ))
		}
	}
	if TypeUnspecified.Valid() {
		t.Error("TypeUnspecified must not be valid")
	}
	if got := TypeUnspecified.String(); got != "TYPE(0)" {
		t.Errorf("TypeUnspecified.String() = %q", got)
	}
	// The wire enum and the local Type must stay numerically aligned.
	for i, typ := range AllTypes {
		if int(typ) != i+1 {
			t.Errorf("AllTypes[%d] = %d, want %d", i, int(typ), i+1)
		}
	}
}

func TestRegisterValidation(t *testing.T) {
	m := NewManager()
	if err := m.Register(TypeUnspecified, "x", func(context.Context, Request) (Response, error) {
		return Response{}, nil
	}); err == nil {
		t.Error("registering an unknown type must fail")
	}
	if err := m.Register(TypePreTool, "", func(context.Context, Request) (Response, error) {
		return Response{}, nil
	}); err == nil {
		t.Error("registering with an empty name must fail")
	}
	if err := m.Register(TypePreTool, "x", nil); err == nil {
		t.Error("registering a nil handler must fail")
	}
	if err := m.Register(TypePreTool, "x", func(context.Context, Request) (Response, error) {
		return Response{}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if !m.Has(TypePreTool) {
		t.Error("Has(TypePreTool) should be true")
	}
	if m.Has(TypeStop) {
		t.Error("Has(TypeStop) should be false")
	}
	if !m.Unregister(TypePreTool, "x") {
		t.Error("Unregister should report the removal")
	}
	if m.Unregister(TypePreTool, "x") {
		t.Error("Unregister of a missing hook should report false")
	}
}

func TestNoHandlersUsesDefaultDecision(t *testing.T) {
	m := NewManager()
	resp := m.Handle(context.Background(), Request{Type: TypePreTool, RequestID: "r1"})
	if resp.Decision != DecisionAllow {
		t.Errorf("pre-tool default = %v, want ALLOW", resp.Decision)
	}
	if resp.RequestID != "r1" {
		t.Errorf("request id = %q, want r1", resp.RequestID)
	}
	resp = m.Handle(context.Background(), Request{Type: TypeStop})
	if resp.Decision != DecisionAllowStop {
		t.Errorf("stop default = %v, want ALLOW_STOP", resp.Decision)
	}
}

func TestPreTurnDenyShortCircuits(t *testing.T) {
	m := NewManager()
	second := false
	if err := m.Register(TypePreTurn, "deny", func(context.Context, Request) (Response, error) {
		return Response{Decision: DecisionDeny, Reason: "nope"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(TypePreTurn, "never", func(context.Context, Request) (Response, error) {
		second = true
		return Response{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	resp := m.Handle(context.Background(), Request{Type: TypePreTurn})
	if resp.Decision != DecisionDeny {
		t.Errorf("decision = %v, want DENY", resp.Decision)
	}
	if resp.Reason != "nope" {
		t.Errorf("reason = %q", resp.Reason)
	}
	if second {
		t.Error("the second hook must not run after a deny")
	}
	if m.Fired(TypePreTurn) != 1 {
		t.Errorf("fired = %d, want 1", m.Fired(TypePreTurn))
	}
}

func TestPreTurnUnspecifiedDecisionFailsClosed(t *testing.T) {
	m := NewManager()
	if err := m.Register(TypePreTurn, "vague", func(context.Context, Request) (Response, error) {
		return Response{Decision: DecisionUnspecified}, nil
	}); err != nil {
		t.Fatal(err)
	}
	resp := m.Handle(context.Background(), Request{Type: TypePreTurn})
	if resp.Decision != DecisionDeny {
		t.Errorf("decision = %v, want DENY (fail closed)", resp.Decision)
	}
	if resp.Reason == "" {
		t.Error("a fail-closed deny must carry a reason")
	}
}

func TestPreTurnHandlerErrorFailsClosed(t *testing.T) {
	m := NewManager()
	if err := m.Register(TypePreTurn, "boom", func(context.Context, Request) (Response, error) {
		return Response{}, errors.New("explode")
	}); err != nil {
		t.Fatal(err)
	}
	resp := m.Handle(context.Background(), Request{Type: TypePreTurn})
	if resp.Decision != DecisionDeny {
		t.Errorf("decision = %v, want DENY", resp.Decision)
	}
	if resp.Err == nil {
		t.Error("the error must be reported")
	}
}

func TestPreToolModifiedArgsMergeCumulatively(t *testing.T) {
	m := NewManager()
	var seenBySecond string
	if err := m.Register(TypePreTool, "first", func(_ context.Context, req Request) (Response, error) {
		return Response{
			Decision:     DecisionAllow,
			ModifiedArgs: map[string]any{"path": "/tmp/a", "extra": 1},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(TypePreTool, "second", func(_ context.Context, req Request) (Response, error) {
		seenBySecond = req.PreTool.ArgumentsJSON
		return Response{
			Decision:     DecisionAllow,
			ModifiedArgs: map[string]any{"path": "/tmp/b"},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	req := Request{Type: TypePreTool}
	req.PreTool = &PreToolArgs{ToolName: "read_file", ArgumentsJSON: `{"path":"/orig","keep":true}`}
	resp := m.Handle(context.Background(), req)
	if resp.Decision != DecisionAllow {
		t.Fatalf("decision = %v, want ALLOW", resp.Decision)
	}
	// The second hook sees the arguments the first hook produced, not the
	// originals. It cannot see its own modification — that is only known
	// after it returns.
	want := `{"extra":1,"keep":true,"path":"/tmp/a"}`
	if seenBySecond != want {
		t.Errorf("second hook saw %s, want %s", seenBySecond, want)
	}
	// The response carries the complete merged map, not a delta.
	encoded, err := json.Marshal(resp.ModifiedArgs)
	if err != nil {
		t.Fatalf("marshal modified args: %v", err)
	}
	if got := string(encoded); got != `{"extra":1,"keep":true,"path":"/tmp/b"}` {
		t.Errorf("modified args = %s", got)
	}
}

func TestPreToolUnmodifiedLeavesModifiedArgsNil(t *testing.T) {
	m := NewManager()
	if err := m.Register(TypePreTool, "allow", func(context.Context, Request) (Response, error) {
		return Response{Decision: DecisionAllow}, nil
	}); err != nil {
		t.Fatal(err)
	}
	req := Request{Type: TypePreTool}
	req.PreTool = &PreToolArgs{ArgumentsJSON: `{"a":1}`}
	resp := m.Handle(context.Background(), req)
	if resp.ModifiedArgs != nil {
		t.Errorf("modified args = %#v, want nil", resp.ModifiedArgs)
	}
}

func TestPreToolDenyWins(t *testing.T) {
	m := NewManager()
	ran := false
	if err := m.Register(TypePreTool, "deny", func(context.Context, Request) (Response, error) {
		return Response{Decision: DecisionDeny, Reason: "blocked"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(TypePreTool, "later", func(context.Context, Request) (Response, error) {
		ran = true
		return Response{Decision: DecisionAllow}, nil
	}); err != nil {
		t.Fatal(err)
	}
	resp := m.Handle(context.Background(), Request{Type: TypePreTool})
	if resp.Decision != DecisionDeny || resp.Reason != "blocked" {
		t.Errorf("resp = %+v", resp)
	}
	if ran {
		t.Error("hooks after a deny must not run")
	}
}

func TestOnToolErrorFirstCustomMessageWins(t *testing.T) {
	m := NewManager()
	if err := m.Register(TypeOnToolError, "first", func(context.Context, Request) (Response, error) {
		return Response{CustomErrorMessage: "use the other flag"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(TypeOnToolError, "second", func(context.Context, Request) (Response, error) {
		return Response{CustomErrorMessage: "ignored"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	resp := m.Handle(context.Background(), Request{Type: TypeOnToolError})
	if resp.CustomErrorMessage != "use the other flag" {
		t.Errorf("custom message = %q", resp.CustomErrorMessage)
	}
}

func TestOnToolErrorWithoutCustomMessage(t *testing.T) {
	m := NewManager()
	if err := m.Register(TypeOnToolError, "quiet", func(context.Context, Request) (Response, error) {
		return Response{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	resp := m.Handle(context.Background(), Request{Type: TypeOnToolError})
	if resp.CustomErrorMessage != "" {
		t.Errorf("custom message = %q, want empty", resp.CustomErrorMessage)
	}
}

func TestStopContinueWins(t *testing.T) {
	m := NewManager()
	if err := m.Register(TypeStop, "keep", func(context.Context, Request) (Response, error) {
		return Response{Decision: DecisionContinue, Reason: "do the rest"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(TypeStop, "stop", func(context.Context, Request) (Response, error) {
		return Response{Decision: DecisionAllowStop}, nil
	}); err != nil {
		t.Fatal(err)
	}
	resp := m.Handle(context.Background(), Request{Type: TypeStop})
	if resp.Decision != DecisionContinue {
		t.Errorf("decision = %v, want CONTINUE", resp.Decision)
	}
	if resp.Reason != "do the rest" {
		t.Errorf("reason = %q", resp.Reason)
	}
}

func TestStopUnspecifiedDegradesToAllowStop(t *testing.T) {
	m := NewManager()
	if err := m.Register(TypeStop, "vague", func(context.Context, Request) (Response, error) {
		return Response{Decision: DecisionUnspecified}, nil
	}); err != nil {
		t.Fatal(err)
	}
	resp := m.Handle(context.Background(), Request{Type: TypeStop})
	if resp.Decision != DecisionAllowStop {
		t.Errorf("decision = %v, want ALLOW_STOP", resp.Decision)
	}
}

func TestStopHandlerErrorDoesNotPinTheLoop(t *testing.T) {
	m := NewManager()
	if err := m.Register(TypeStop, "boom", func(context.Context, Request) (Response, error) {
		return Response{}, errors.New("explode")
	}); err != nil {
		t.Fatal(err)
	}
	resp := m.Handle(context.Background(), Request{Type: TypeStop})
	if resp.Decision != DecisionAllowStop {
		t.Errorf("decision = %v, want ALLOW_STOP", resp.Decision)
	}
	if resp.Err == nil {
		t.Error("the error must be reported")
	}
}

func TestInspectHooksAllRunAndDoNotBlock(t *testing.T) {
	m := NewManager()
	ran := 0
	if err := m.Register(TypePostTool, "a", func(context.Context, Request) (Response, error) {
		ran++
		return Response{}, errors.New("first failed")
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(TypePostTool, "b", func(context.Context, Request) (Response, error) {
		ran++
		return Response{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	resp := m.Handle(context.Background(), Request{Type: TypePostTool})
	if ran != 2 {
		t.Errorf("handlers ran %d times, want 2", ran)
	}
	if resp.Err == nil {
		t.Error("the first error must be recorded")
	}
	if resp.Decision != DecisionAllow {
		t.Errorf("decision = %v, want ALLOW", resp.Decision)
	}
}

func TestEnabledTypes(t *testing.T) {
	m := NewManager()
	if err := m.Register(TypeStop, "s", func(context.Context, Request) (Response, error) {
		return Response{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(TypePreTool, "p", func(context.Context, Request) (Response, error) {
		return Response{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	got := m.EnabledTypes()
	want := []Type{TypePreTool, TypeStop}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("enabled types = %v, want %v", got, want)
	}
	if names := m.Names(TypePreTool); !reflect.DeepEqual(names, []string{"p"}) {
		t.Errorf("names = %v", names)
	}
}

func TestCancelledContextStopsDispatch(t *testing.T) {
	m := NewManager()
	if err := m.Register(TypePreTool, "p", func(context.Context, Request) (Response, error) {
		return Response{Decision: DecisionAllow}, nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resp := m.Handle(ctx, Request{Type: TypePreTool})
	if resp.Decision != DecisionDeny {
		t.Errorf("decision = %v, want DENY on a cancelled context", resp.Decision)
	}
}

// --- wire bridge ---

func TestRequestFromProtoPreTool(t *testing.T) {
	args := &pb.PreToolArgs{}
	args.SetToolName("mcp__server__do_thing")
	args.SetArgumentsJson(`{"a":1}`)
	args.SetServerName("server")
	args.SetCallId("call-1")
	args.SetTrajectoryId("traj-1")
	args.SetStepIndex(7)

	req := &pb.CallHookRequest{}
	req.SetType(pb.LifecycleHook_LIFECYCLE_HOOK_PRE_TOOL)
	req.SetName("my-hook")
	req.SetRequestId("req-9")
	req.SetClientId("client-1")
	req.SetPreToolArgs(args)

	got := RequestFromProto(req)
	if got.Type != TypePreTool {
		t.Errorf("type = %v", got.Type)
	}
	if got.Name != "my-hook" || got.RequestID != "req-9" || got.ClientID != "client-1" {
		t.Errorf("identity fields = %+v", got)
	}
	if got.PreTool == nil {
		t.Fatal("pre-tool payload missing")
	}
	if got.PreTool.ToolName != "mcp__server__do_thing" || got.PreTool.ServerName != "server" ||
		got.PreTool.CallID != "call-1" || got.PreTool.TrajectoryID != "traj-1" || got.PreTool.StepIndex != 7 {
		t.Errorf("pre-tool payload = %+v", got.PreTool)
	}
	if got.PreTool.ArgumentsJSON != `{"a":1}` {
		t.Errorf("arguments = %q", got.PreTool.ArgumentsJSON)
	}
}

func TestRequestFromProtoStop(t *testing.T) {
	args := &pb.StopArgs{}
	args.SetResponseText("done")
	args.SetTrajectoryId("traj")
	args.SetContinuationCount(2)
	args.SetStopReason(pb.TrajectoryStateUpdate_STOP_REASON_MAX_TOOL_CALLS_EXCEEDED)
	args.SetErrorMessage("boom")

	req := &pb.CallHookRequest{}
	req.SetType(pb.LifecycleHook_LIFECYCLE_HOOK_STOP)
	req.SetStopArgs(args)

	got := RequestFromProto(req)
	if got.Stop == nil {
		t.Fatal("stop payload missing")
	}
	if got.Stop.ContinuationCount != 2 {
		t.Errorf("continuation count = %d", got.Stop.ContinuationCount)
	}
	if got.Stop.StopReason != "STOP_REASON_MAX_TOOL_CALLS_EXCEEDED" {
		t.Errorf("stop reason = %q", got.Stop.StopReason)
	}
	if got.Stop.ErrorMessage != "boom" {
		t.Errorf("error message = %q", got.Stop.ErrorMessage)
	}
}

func TestRequestFromProtoNil(t *testing.T) {
	if got := RequestFromProto(nil); got.Type != TypeUnspecified {
		t.Errorf("nil request = %+v", got)
	}
}

func TestBuildResponsePerType(t *testing.T) {
	t.Run("pre-turn allow", func(t *testing.T) {
		out := BuildResponse(TypePreTurn, Response{RequestID: "r", Decision: DecisionAllow})
		if out.GetRequestId() != "r" {
			t.Errorf("request id = %q", out.GetRequestId())
		}
		if out.GetPreTurnResult().GetDecision() != pb.PreTurnResult_ALLOW {
			t.Errorf("decision = %v", out.GetPreTurnResult().GetDecision())
		}
	})
	t.Run("pre-turn deny", func(t *testing.T) {
		out := BuildResponse(TypePreTurn, Response{Decision: DecisionDeny, Reason: "because"})
		if out.GetPreTurnResult().GetDecision() != pb.PreTurnResult_DENY {
			t.Errorf("decision = %v", out.GetPreTurnResult().GetDecision())
		}
		if out.GetPreTurnResult().GetReason() != "because" {
			t.Errorf("reason = %q", out.GetPreTurnResult().GetReason())
		}
	})
	t.Run("pre-tool with modified args", func(t *testing.T) {
		out := BuildResponse(TypePreTool, Response{
			Decision:     DecisionAllow,
			ModifiedArgs: map[string]any{"path": "/x", "n": 3.0},
		})
		res := out.GetPreToolResult()
		if res.GetDecision() != pb.PreToolResult_ALLOW {
			t.Errorf("decision = %v", res.GetDecision())
		}
		back := StructToMap(res.GetModifiedArgs())
		if back["path"] != "/x" {
			t.Errorf("modified args = %#v", back)
		}
		if back["n"] != 3.0 {
			t.Errorf("modified args = %#v", back)
		}
	})
	t.Run("on-tool-error custom message", func(t *testing.T) {
		out := BuildResponse(TypeOnToolError, Response{CustomErrorMessage: "try again"})
		if out.GetOnToolErrorResult().GetCustomErrorMessage() != "try again" {
			t.Errorf("custom message = %q", out.GetOnToolErrorResult().GetCustomErrorMessage())
		}
	})
	t.Run("on-tool-error without message is empty", func(t *testing.T) {
		out := BuildResponse(TypeOnToolError, Response{})
		if out.GetEmptyResult() == nil {
			t.Error("expected an empty result")
		}
	})
	t.Run("stop continue", func(t *testing.T) {
		out := BuildResponse(TypeStop, Response{Decision: DecisionContinue, Reason: "more"})
		if out.GetStopResult().GetDecision() != pb.StopResult_CONTINUE {
			t.Errorf("decision = %v", out.GetStopResult().GetDecision())
		}
	})
	t.Run("stop allow", func(t *testing.T) {
		out := BuildResponse(TypeStop, Response{Decision: DecisionAllowStop})
		if out.GetStopResult().GetDecision() != pb.StopResult_ALLOW_STOP {
			t.Errorf("decision = %v", out.GetStopResult().GetDecision())
		}
	})
	t.Run("inspect is empty", func(t *testing.T) {
		out := BuildResponse(TypePostTurn, Response{})
		if out.GetEmptyResult() == nil {
			t.Error("expected an empty result")
		}
	})
	t.Run("error becomes error_message", func(t *testing.T) {
		out := BuildResponse(TypePreTurn, Response{RequestID: "r", Err: errors.New("boom")})
		if out.GetErrorMessage() == "" {
			t.Error("expected an error message")
		}
		if out.GetPreTurnResult() != nil {
			t.Error("a failed hook must not carry a result payload")
		}
	})
}

func TestMapStructRoundTrip(t *testing.T) {
	in := map[string]any{
		"s":   "text",
		"b":   true,
		"n":   1.5,
		"nil": nil,
		"o":   map[string]any{"k": "v"},
		"l":   []any{"a", 1.0, false},
	}
	out := MapToStruct(in)
	if len(out.GetFields()) != len(in) {
		t.Fatalf("fields = %d, want %d", len(out.GetFields()), len(in))
	}
	back := StructToMap(out)
	if !reflect.DeepEqual(back, in) {
		t.Errorf("round trip = %#v, want %#v", back, in)
	}
}

func TestRenderUserInput(t *testing.T) {
	part := &pb.UserInput_Part{}
	part.SetText("hello")
	media := &pb.UserInput_Media{}
	media.SetMimeType("image/png")
	mediaPart := &pb.UserInput_Part{}
	mediaPart.SetMedia(media)

	in := &pb.UserInput{}
	in.SetParts([]*pb.UserInput_Part{part, mediaPart})

	got := RenderUserInput(in)
	if got != "hello\n[media: image/png]" {
		t.Errorf("rendered = %q", got)
	}
	if RenderUserInput(nil) != "" {
		t.Error("nil input must render empty")
	}
}

func TestFormatStepID(t *testing.T) {
	if got := FormatStepID("traj", 3); got != "traj/3" {
		t.Errorf("step id = %q", got)
	}
	if got := FormatStepID("", 3); got != "3" {
		t.Errorf("step id = %q", got)
	}
}
