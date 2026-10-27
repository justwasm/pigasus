// hooks.go — the harness side of the lifecycle-hook protocol.
//
// # The direction
//
// The hook protocol runs **from the harness to the client**. The client
// declares, in its `HarnessConfig.enabled_hooks`, which hook *types* it has
// handlers for; the harness then calls each of them at the right moment
// and waits for the answer.
//
// The wire request is a `CallHookRequest` with the hook's type, a name (for
// diagnostics), a correlation id and the type's arguments. The answer is a
// `CallHookResponse` whose `result` oneof carries the type's verdict.
//
// The typed model lives in `lifecyclehooks`: this file uses its
// `Type`, its `Decision` and its `StructToMap` bridge, and it is the
// mirror image of that package's client-side dispatcher.
//
// # Fail-closed
//
// A hook that cannot be understood must not let the guarded action
// through:
//
//   - a hook call that fails, times out or comes back with an
//     `error_message` **denies** the guarded action;
//   - a PRE_TOOL / PRE_TURN verdict that is not an explicit ALLOW denies
//     it as well.
//
// 【反编译】 The reference's own wire layer is *more* permissive:
// `preToolResultFromProto` (0x101988380) derives `denied` from
// `decision == DENY`, so a `DECISION_UNSPECIFIED` arriving over the wire
// reads as "not denied" on the harness side, and `preTurnResultFromProto`
// (0x1019881c0) does the same. The reference's *inner* layer is the
// strict one: `checkCortexHookResults` (0x101971090) refuses to act on a
// decision it does not recognise and fails the step with
// "Unrecognized pre-tool hook decision %q; blocked execution for safety."
//
// This implementation follows the strict reading, because a hook the
// harness cannot interpret is exactly the case the reference's own inner
// layer refuses to guess at. See 存疑 in the package's cycle report.
//
// The STOP hook is the exception: an unrecognised verdict means ALLOW_STOP
// rather than CONTINUE, because the alternative is a loop that never ends.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"

	"github.com/justwasm/pigasus/lifecyclehooks"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// hookTimeout bounds one hook call.
//
// A client that never answers must not wedge the agent loop; the hook is
// treated as failed, and a failed hook denies.
const hookTimeout = 2 * time.Minute

// hookVerdict is the harness's reading of a hook's answer.
type hookVerdict struct {
	// Denied blocks the guarded action.
	Denied bool

	// Reason explains the denial. It reaches the model.
	Reason string

	// ModifiedArgs is the rewritten tool arguments. Only meaningful for
	// PRE_TOOL.
	ModifiedArgs json.RawMessage

	// Continue keeps the agent loop going. Only meaningful for STOP.
	Continue bool
}

// hookDispatcher fires the lifecycle hooks at the client.
type hookDispatcher struct {
	conv    *conversation
	enabled map[lifecyclehooks.Type]bool
}

// newHookDispatcher builds a dispatcher for the hook types the client
// enabled.
//
// The client advertises the *types* it has handlers for, not their names:
// the shipped SDK's `_get_enabled_hooks` maps each of its hook lists onto
// one enum value, and the client fans a single request out over all the
// handlers it registered for that type.
func newHookDispatcher(conv *conversation, enabled []pb.LifecycleHook) *hookDispatcher {
	set := make(map[lifecyclehooks.Type]bool, len(enabled))
	for _, hook := range enabled {
		t := lifecyclehooks.TypeFromProto(hook)
		if t.Valid() {
			set[t] = true
		}
	}
	return &hookDispatcher{conv: conv, enabled: set}
}

// enabled reports whether a hook type is on.
func (h *hookDispatcher) enabledType(t lifecyclehooks.Type) bool {
	if h == nil {
		return false
	}
	return h.enabled[t]
}

// sessionStart fires the ON_SESSION_START hook.
func (h *hookDispatcher) sessionStart(ctx context.Context) {
	if !h.enabledType(lifecyclehooks.TypeOnSessionStart) {
		return
	}
	if _, err := h.fire(ctx, lifecyclehooks.TypeOnSessionStart, nil); err != nil {
		h.conv.logger.Warn("harness: session-start hook failed", "error", err)
	}
}

// sessionEnd fires the ON_SESSION_END hook.
func (h *hookDispatcher) sessionEnd(ctx context.Context) {
	if !h.enabledType(lifecyclehooks.TypeOnSessionEnd) {
		return
	}
	if _, err := h.fire(ctx, lifecyclehooks.TypeOnSessionEnd, nil); err != nil {
		h.conv.logger.Warn("harness: session-end hook failed", "error", err)
	}
}

// preTurn fires the PRE_TURN hook and reads its verdict.
func (h *hookDispatcher) preTurn(ctx context.Context, prompt string) (hookVerdict, error) {
	if !h.enabledType(lifecyclehooks.TypePreTurn) {
		return hookVerdict{}, nil
	}
	resp, err := h.fire(ctx, lifecyclehooks.TypePreTurn, func(req *pb.CallHookRequest) {
		args := &pb.PreTurnArgs{}
		args.SetTrajectoryId(h.conv.trajectoryID)
		if prompt != "" {
			input := &pb.UserInput{}
			part := &pb.UserInput_Part{}
			part.SetText(prompt)
			input.SetParts([]*pb.UserInput_Part{part})
			args.SetUserInput(input)
		}
		req.SetPreTurnArgs(args)
	})
	if err != nil {
		return hookVerdict{Denied: true, Reason: "pre-turn hook failed: " + err.Error()}, err
	}
	return preTurnVerdict(resp), nil
}

// preTurnVerdict reads a PRE_TURN answer.
//
// The mapping is fail-closed: anything that is not an explicit ALLOW denies
// the turn. See the package comment for the evidence.
func preTurnVerdict(resp *pb.CallHookResponse) hookVerdict {
	if msg := resp.GetErrorMessage(); msg != "" {
		return hookVerdict{Denied: true, Reason: "pre-turn hook failed: " + msg}
	}
	result := resp.GetPreTurnResult()
	if result == nil {
		return hookVerdict{Denied: true, Reason: unrecognizedDecision(lifecyclehooks.TypePreTurn, "none")}
	}
	switch result.GetDecision() {
	case pb.PreTurnResult_ALLOW:
		return hookVerdict{}
	case pb.PreTurnResult_DENY:
		return hookVerdict{Denied: true, Reason: result.GetReason()}
	default:
		return hookVerdict{Denied: true, Reason: unrecognizedDecision(lifecyclehooks.TypePreTurn, result.GetDecision().String())}
	}
}

// postTurn fires the POST_TURN hook.
func (h *hookDispatcher) postTurn(ctx context.Context, responseText string) {
	if !h.enabledType(lifecyclehooks.TypePostTurn) {
		return
	}
	if _, err := h.fire(ctx, lifecyclehooks.TypePostTurn, func(req *pb.CallHookRequest) {
		args := &pb.PostTurnArgs{}
		args.SetResponseText(responseText)
		args.SetTrajectoryId(h.conv.trajectoryID)
		req.SetPostTurnArgs(args)
	}); err != nil {
		h.conv.logger.Warn("harness: post-turn hook failed", "error", err)
	}
}

// preTool fires the PRE_TOOL hook and reads its verdict.
//
// A denial blocks the call; a `modified_args` result replaces the call's
// arguments.
func (h *hookDispatcher) preTool(ctx context.Context, toolName string, args json.RawMessage, callID string) (hookVerdict, error) {
	if !h.enabledType(lifecyclehooks.TypePreTool) {
		return hookVerdict{}, nil
	}
	resp, err := h.fire(ctx, lifecyclehooks.TypePreTool, func(req *pb.CallHookRequest) {
		toolArgs := &pb.PreToolArgs{}
		toolArgs.SetToolName(toolName)
		toolArgs.SetArgumentsJson(string(args))
		toolArgs.SetCallId(callID)
		toolArgs.SetTrajectoryId(h.conv.trajectoryID)
		toolArgs.SetStepIndex(h.conv.currentStepIndex())
		req.SetPreToolArgs(toolArgs)
	})
	if err != nil {
		return hookVerdict{Denied: true, Reason: "pre-tool hook failed: " + err.Error()}, err
	}
	return preToolVerdict(resp), nil
}

// preToolVerdict reads a PRE_TOOL answer.
//
// The mapping is fail-closed: anything that is not an explicit ALLOW denies
// the call. A `modified_args` result replaces the call's arguments; it is
// carried even when the decision is a denial, so the caller can log what
// the hook wanted.
func preToolVerdict(resp *pb.CallHookResponse) hookVerdict {
	verdict := hookVerdict{}
	if msg := resp.GetErrorMessage(); msg != "" {
		return hookVerdict{Denied: true, Reason: "pre-tool hook failed: " + msg}
	}
	result := resp.GetPreToolResult()
	if result == nil {
		return hookVerdict{Denied: true, Reason: unrecognizedDecision(lifecyclehooks.TypePreTool, "none")}
	}
	if modified := result.GetModifiedArgs(); modified != nil {
		if fields := lifecyclehooks.StructToMap(modified); len(fields) > 0 {
			if encoded, err := json.Marshal(fields); err == nil {
				verdict.ModifiedArgs = encoded
			}
		}
	}
	switch result.GetDecision() {
	case pb.PreToolResult_ALLOW:
		return verdict
	case pb.PreToolResult_DENY:
		verdict.Denied = true
		verdict.Reason = result.GetReason()
		return verdict
	default:
		verdict.Denied = true
		verdict.Reason = unrecognizedDecision(lifecyclehooks.TypePreTool, result.GetDecision().String())
		return verdict
	}
}

// postTool fires the POST_TOOL hook.
func (h *hookDispatcher) postTool(ctx context.Context, e piagent.ToolExecutionEndEvent) {
	if !h.enabledType(lifecyclehooks.TypePostTool) {
		return
	}
	if _, err := h.fire(ctx, lifecyclehooks.TypePostTool, func(req *pb.CallHookRequest) {
		args := &pb.PostToolArgs{}
		args.SetToolName(e.ToolName)
		args.SetResult(toolResultText(e.Result))
		if e.IsError {
			args.SetError(toolResultText(e.Result))
		}
		args.SetCallId(e.ToolCallID)
		args.SetTrajectoryId(h.conv.trajectoryID)
		req.SetPostToolArgs(args)
	}); err != nil {
		h.conv.logger.Warn("harness: post-tool hook failed", "error", err)
	}
}

// onToolError fires the ON_TOOL_ERROR hook.
//
// The hook may replace the error text the model sees. The replacement is
// not applied to the running call — PiG has already produced the error
// result by the time the end event fires — so the custom message is
// logged and the next turn's error surfaces it.
//
// 【推测】 The reference applies the custom message to the step it is
// building; the clone's runtime owns the error result, so the harness can
// only observe it.
func (h *hookDispatcher) onToolError(ctx context.Context, e piagent.ToolExecutionEndEvent) {
	if !h.enabledType(lifecyclehooks.TypeOnToolError) {
		return
	}
	if _, err := h.fire(ctx, lifecyclehooks.TypeOnToolError, func(req *pb.CallHookRequest) {
		args := &pb.OnToolErrorArgs{}
		args.SetToolName(e.ToolName)
		args.SetErrorMessage(toolResultText(e.Result))
		args.SetCallId(e.ToolCallID)
		args.SetTrajectoryId(h.conv.trajectoryID)
		req.SetOnToolErrorArgs(args)
	}); err != nil {
		h.conv.logger.Warn("harness: on-tool-error hook failed", "error", err)
	}
}

// onCompaction fires the ON_COMPACTION hook.
func (h *hookDispatcher) onCompaction(ctx context.Context) {
	if !h.enabledType(lifecyclehooks.TypeOnCompaction) {
		return
	}
	if _, err := h.fire(ctx, lifecyclehooks.TypeOnCompaction, func(req *pb.CallHookRequest) {
		args := &pb.OnCompactionArgs{}
		args.SetTrajectoryId(h.conv.trajectoryID)
		args.SetStepIndex(h.conv.currentStepIndex())
		req.SetOnCompactionArgs(args)
	}); err != nil {
		h.conv.logger.Warn("harness: compaction hook failed", "error", err)
	}
}

// stop fires the STOP hook and reads its verdict.
//
// A CONTINUE verdict keeps the agent loop going; the harness then runs the
// turn again.
func (h *hookDispatcher) stop(ctx context.Context, responseText string, continuations int) (hookVerdict, error) {
	if !h.enabledType(lifecyclehooks.TypeStop) {
		return hookVerdict{}, nil
	}
	resp, err := h.fire(ctx, lifecyclehooks.TypeStop, func(req *pb.CallHookRequest) {
		args := &pb.StopArgs{}
		args.SetResponseText(responseText)
		args.SetTrajectoryId(h.conv.trajectoryID)
		args.SetContinuationCount(int32(continuations))
		req.SetStopArgs(args)
	})
	if err != nil {
		return hookVerdict{}, err
	}
	return stopVerdict(resp), nil
}

// stopVerdict reads a STOP answer.
//
// The STOP hook is the one exception to the fail-closed rule: an
// unrecognised verdict means ALLOW_STOP, because the alternative —
// CONTINUE — is a loop that never ends.
func stopVerdict(resp *pb.CallHookResponse) hookVerdict {
	if msg := resp.GetErrorMessage(); msg != "" {
		return hookVerdict{}
	}
	result := resp.GetStopResult()
	if result == nil {
		return hookVerdict{}
	}
	switch result.GetDecision() {
	case pb.StopResult_CONTINUE:
		return hookVerdict{Continue: true, Reason: result.GetReason()}
	default:
		return hookVerdict{}
	}
}

// fire sends one hook request and waits for the answer.
//
// A hook that is not enabled is not fired at all: the client only
// advertised the types it has handlers for.
func (h *hookDispatcher) fire(ctx context.Context, t lifecyclehooks.Type, build func(*pb.CallHookRequest)) (*pb.CallHookResponse, error) {
	if h == nil {
		return nil, nil
	}
	req := &pb.CallHookRequest{}
	req.SetRequestId(h.conv.nextRequestID())
	req.SetType(pb.LifecycleHook(t))
	req.SetName(hookName(t))
	req.SetClientId(h.clientID())
	if build != nil {
		build(req)
	}
	ev := &pb.OutputEvent{}
	ev.SetCallHookRequest(req)
	hookCtx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()
	answer, err := h.conv.await(hookCtx, req.GetRequestId(), func() error {
		return h.conv.send(hookCtx, ev)
	})
	if err != nil {
		return nil, err
	}
	resp := answer.GetCallHookResponse()
	if resp == nil {
		return nil, fmt.Errorf("harness: the client answered a %s hook with no response", t)
	}
	return resp, nil
}

// clientID identifies the client the conversation belongs to.
func (h *hookDispatcher) clientID() string {
	if h == nil || h.conv == nil {
		return ""
	}
	return h.conv.trajectoryID
}

// hookName is the wire name of a hook type.
//
// The SDK routes on the type and only logs the name, so the name is the
// SDK's own spelling of the hook ("pre_tool", "on_compaction", …).
func hookName(t lifecyclehooks.Type) string {
	switch t {
	case lifecyclehooks.TypeOnSessionStart:
		return "on_session_start"
	case lifecyclehooks.TypeOnSessionEnd:
		return "on_session_end"
	case lifecyclehooks.TypePreTurn:
		return "pre_turn"
	case lifecyclehooks.TypePostTurn:
		return "post_turn"
	case lifecyclehooks.TypePreTool:
		return "pre_tool"
	case lifecyclehooks.TypePostTool:
		return "post_tool"
	case lifecyclehooks.TypeOnToolError:
		return "on_tool_error"
	case lifecyclehooks.TypeOnCompaction:
		return "on_compaction"
	case lifecyclehooks.TypeStop:
		return "stop"
	default:
		return "unknown"
	}
}

// unrecognizedDecision renders the reference's refusal message.
//
// 【strings】 "Unrecognized pre-tool hook decision %q; blocked execution for
// safety." is a verbatim string-table hit of the reference binary.
func unrecognizedDecision(t lifecyclehooks.Type, decision string) string {
	return fmt.Sprintf("Unrecognized %s hook decision %q; blocked execution for safety.", hookName(t), decision)
}
