// Package lifecyclehooks implements the bidirectional lifecycle-hook
// protocol that Antigravity's closed-source `localharness` binary speaks.
//
// # What this is
//
// The `agy` CLI (and the `google-antigravity` SDKs built on top of it) can
// register *lifecycle hooks* — callbacks the agent runs at nine fixed points
// of its life. The harness binary owns the agent loop, so the hooks live on
// the **client** side and the harness reaches them over the wire:
//
//	harness --OutputEvent{call_hook_request{request_id,type,name,...}}--> client
//	client  --InputEvent{call_hook_response{request_id,...}}------------> harness
//
// The existing `internal/hooks` package is a different thing: a same-process,
// fire-and-forget bus for the TUI's `/btw` side-question (two events, one
// Decision type, no request/response correlation). This package is the
// *protocol* half: nine hook types, request/response correlation by
// `request_id`, per-type result payloads, argument rewriting, and the
// fail-closed rules the harness applies to a decision it cannot interpret.
//
// # Provenance
//
// 【一手】 The wire contract is the `localharness_pb2.py` descriptor embedded
// in the `google-antigravity` PyPI wheel (v0.1.22, macosx_11_0_arm64):
// `antigravity.localharness.CallHookRequest` / `CallHookResponse`, the nine
// `LifecycleHook` values, and the per-type result messages
// (`PreTurnResult`, `PreToolResult`, `OnToolErrorResult`, `StopResult`,
// `EmptyResult`). Recovered sources are committed at
// `out/localharness/proto/localharness.proto`.
//
// 【一手】 The dispatch semantics are the shipped Python SDK's
// `google/antigravity/hooks/hook_runner.py` (`dispatch_pre_turn`,
// `dispatch_pre_tool_call`, `dispatch_on_tool_error`, `dispatch_stop`) and
// `google/antigravity/connections/local/hook_router.py` (`_handle_*`), both
// read out of the same wheel.
//
// 【反编译】 The harness side of the same protocol, recovered from the binary
// (addresses are the Mach-O load addresses of `localharness`, 0x1xxxxxxxx):
//
//	0x101989810  localagent.runPreTurnHooks
//	             — walks the pre-turn hooks, returns on the first deny,
//	               wraps a hook failure as "pre-turn hook error: %w".
//	0x1019881c0  localagent.preTurnResultFromProto
//	0x101988380  localagent.preToolResultFromProto
//	             — the internal verdict is `denied = (decision == DENY)`;
//	               the reason travels with it.
//	0x10157b780  interactionstranslate.translatePreTurnDecision
//	0x101590cb0  interactionstranslate.translatePreToolDecision
//	             — a decision outside {UNSPECIFIED, ALLOW, DENY} is an
//	               error: "unknown PreTurnResult decision: %v".
//	0x101971090  localagent.checkCortexHookResults
//	             — the gate that turns a hook verdict into an outcome. A
//	               verdict it does not recognise is refused with
//	               "Unrecognized pre-tool hook decision %q; blocked
//	               execution for safety." and the step is failed.
//
// 【strings】 From the harness binary itself (see `out/localharness/`):
//
//	"hook failed for %s: %v"
//	"failed to fire pre-turn hook: %w"
//	"Unrecognized pre-tool hook decision %q; blocked execution for safety."
//	"unknown pre-tool hook decision %q"
//	"pre-tool hook %s not registered"
//	"post-tool hook %s not registered"
//	"stop hook %s not registered"
//	"OnSessionStart hook failed: %v"
//	"OnSessionEnd hook failed: %v"
//	"PostTurn hook failed for trajectory %s: %v"
//	"OnToolError hook failed for %s: %v"
//	"post-tool hooks failed for step %d"
//	"Turn execution denied by hook."
//	"Denied by pre-tool hook."
//	"tool call denied by pre-tool hook: %s"
//	"Stop hook blocked termination: %s"
//	"A pre-tool hook changed the arguments of this tool call before it ran."
//	"Call this tool to submit your final structured decision for the hook."
//
// 【推测】 Where the SDK and the binary disagree, or where neither says
// anything, this package follows the SDK and says so inline.
//
// # Hard constraints (硬约束)
//
//   - A hook the harness cannot interpret must never be treated as "allow".
//     The harness string "Unrecognized pre-tool hook decision %q; blocked
//     execution for safety." pins this: an unknown decision BLOCKS. The same
//     fail-closed rule is applied to PRE_TURN.
//   - `request_id` must be echoed verbatim on the response. The harness
//     correlates strictly; a mismatched id is an unmatched response.
//   - The client must never answer a hook it did not enable. The SDK only
//     advertises the hook types it actually has handlers for
//     (`HarnessConfig.enabled_hooks`), and the harness only calls those.
//
// # Limitations (存疑)
//
//   - The harness's own JSON-hook host (a `hooks.json` with `command` /
//     `prompt` handlers) is not modelled here; this package models the
//     *client-side* callback surface only.
//   - The harness's `Stop` hook can also be a "submit a structured decision"
//     tool flow; the binary string "Call this tool to submit your final
//     structured decision for the hook." suggests a second, tool-mediated
//     path that this package does not implement.
//   - The harness's own verdict mapping is *more* permissive than this
//     package: `preTurnResultFromProto` derives `denied` from
//     `decision == DENY`, so a DECISION_UNSPECIFIED arriving over the wire
//     reads as "not denied" on the harness side. This package refuses to
//     emit DECISION_UNSPECIFIED in the first place, because a decision the
//     harness has to guess at is exactly what `checkCortexHookResults`
//     refuses to act on. See the fail-closed rule above.
package lifecyclehooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Type is the lifecycle-hook type. The numeric values are identical to the
// wire enum `antigravity.localharness.LifecycleHook`, so the conversion in
// proto.go is a plain cast.
type Type int

// The nine hook types, in wire-enum order.
const (
	// TypeUnspecified is the zero value and is never a valid hook type.
	TypeUnspecified Type = 0
	// TypeOnSessionStart fires once when the session starts.
	TypeOnSessionStart Type = 1
	// TypeOnSessionEnd fires once when the session ends.
	TypeOnSessionEnd Type = 2
	// TypePreTurn fires before a turn starts and can deny it.
	TypePreTurn Type = 3
	// TypePostTurn fires after a turn ends.
	TypePostTurn Type = 4
	// TypePreTool fires before a tool call and can deny it or rewrite its
	// arguments.
	TypePreTool Type = 5
	// TypePostTool fires after a tool call completes.
	TypePostTool Type = 6
	// TypeOnToolError fires when a tool call fails and can replace the
	// error text the model sees.
	TypeOnToolError Type = 7
	// TypeOnCompaction fires when the harness compacts the context.
	TypeOnCompaction Type = 8
	// TypeStop fires when the root trajectory goes idle and can force the
	// agent loop to continue.
	TypeStop Type = 9
)

// AllTypes lists every hook type in wire-enum order.
var AllTypes = []Type{
	TypeOnSessionStart,
	TypeOnSessionEnd,
	TypePreTurn,
	TypePostTurn,
	TypePreTool,
	TypePostTool,
	TypeOnToolError,
	TypeOnCompaction,
	TypeStop,
}

// String returns the short type name used in log lines and test failures.
func (t Type) String() string {
	switch t {
	case TypeOnSessionStart:
		return "ON_SESSION_START"
	case TypeOnSessionEnd:
		return "ON_SESSION_END"
	case TypePreTurn:
		return "PRE_TURN"
	case TypePostTurn:
		return "POST_TURN"
	case TypePreTool:
		return "PRE_TOOL"
	case TypePostTool:
		return "POST_TOOL"
	case TypeOnToolError:
		return "ON_TOOL_ERROR"
	case TypeOnCompaction:
		return "ON_COMPACTION"
	case TypeStop:
		return "STOP"
	}
	return fmt.Sprintf("TYPE(%d)", int(t))
}

// ProtoName returns the wire enum name, e.g. "LIFECYCLE_HOOK_PRE_TOOL".
func (t Type) ProtoName() string {
	switch t {
	case TypeUnspecified:
		return "LIFECYCLE_HOOK_UNSPECIFIED"
	}
	return "LIFECYCLE_HOOK_" + t.String()
}

// Valid reports whether t is one of the nine dispatchable hook types.
func (t Type) Valid() bool {
	for _, k := range AllTypes {
		if k == t {
			return true
		}
	}
	return false
}

// Decision is the verdict a deciding hook returns.
//
// The wire protocol splits it across three per-type enums —
// `PreTurnResult.Decision` and `PreToolResult.Decision` carry ALLOW /
// DENY, `StopResult.Decision` carries ALLOW_STOP / CONTINUE. This package
// uses one superset; the bridge in proto.go narrows it back down.
type Decision int

const (
	// DecisionUnspecified means "the hook said nothing". For the deciding
	// hook types the harness treats this as unrecognised and blocks.
	DecisionUnspecified Decision = iota
	// DecisionAllow lets the guarded action proceed.
	DecisionAllow
	// DecisionDeny blocks the guarded action. Reason is shown to the model.
	DecisionDeny
	// DecisionAllowStop lets the agent loop end. Stop hooks only.
	DecisionAllowStop
	// DecisionContinue forces the agent loop to keep going. Stop hooks only.
	DecisionContinue
)

// String returns the decision name used in logs and tests.
func (d Decision) String() string {
	switch d {
	case DecisionAllow:
		return "ALLOW"
	case DecisionDeny:
		return "DENY"
	case DecisionAllowStop:
		return "ALLOW_STOP"
	case DecisionContinue:
		return "CONTINUE"
	}
	return "DECISION_UNSPECIFIED"
}

// PreTurnArgs is the payload of a PRE_TURN hook.
type PreTurnArgs struct {
	// UserInput is the prompt the turn is about to run with. It is a
	// multi-modal `genai.Content` on the wire; the SDK renders it as text
	// for the hook, which is what this field carries.
	UserInput string
	// TrajectoryID identifies the trajectory the turn belongs to.
	TrajectoryID string
}

// PostTurnArgs is the payload of a POST_TURN hook.
type PostTurnArgs struct {
	// ResponseText is the model's response text for the completed turn.
	ResponseText string
	// TrajectoryID identifies the trajectory the turn belongs to.
	TrajectoryID string
}

// PreToolArgs is the payload of a PRE_TOOL hook.
type PreToolArgs struct {
	// ToolName is the wire name of the tool being called.
	ToolName string
	// ArgumentsJSON is the tool's arguments as a JSON document.
	ArgumentsJSON string
	// ServerName is set for MCP tool calls so a policy can match on the
	// server as well as the tool.
	ServerName string
	// CallID correlates the call with the eventual tool response.
	CallID string
	// TrajectoryID identifies the trajectory the call belongs to.
	TrajectoryID string
	// StepIndex is the step the call belongs to.
	StepIndex int
}

// PostToolArgs is the payload of a POST_TOOL hook.
type PostToolArgs struct {
	// ToolName is the wire name of the tool that ran.
	ToolName string
	// Result is the tool's output. Empty when Error is set.
	Result string
	// Error is the tool's error text. Empty on success.
	Error string
	// ServerName is set for MCP tool calls.
	ServerName string
	// CallID correlates the call with the tool response.
	CallID string
	// TrajectoryID identifies the trajectory the call belongs to.
	TrajectoryID string
	// StepIndex is the step the call belongs to.
	StepIndex int
}

// OnToolErrorArgs is the payload of an ON_TOOL_ERROR hook.
type OnToolErrorArgs struct {
	// ToolName is the wire name of the tool that failed.
	ToolName string
	// ErrorMessage is the failure text.
	ErrorMessage string
	// ServerName is set for MCP tool calls.
	ServerName string
	// CallID correlates the call with the tool response.
	CallID string
	// TrajectoryID identifies the trajectory the call belongs to.
	TrajectoryID string
	// StepIndex is the step the call belongs to.
	StepIndex int
}

// OnCompactionArgs is the payload of an ON_COMPACTION hook.
type OnCompactionArgs struct {
	// TrajectoryID identifies the compacted trajectory.
	TrajectoryID string
	// StepIndex is the step the compaction was recorded at.
	StepIndex int
	// Summary is the compaction summary the harness produced.
	Summary string
}

// StopArgs is the payload of a STOP hook.
type StopArgs struct {
	// ResponseText is the model text that ended the turn.
	ResponseText string
	// TrajectoryID identifies the trajectory that went idle.
	TrajectoryID string
	// ContinuationCount is how many times the loop has already been
	// continued by a stop hook.
	ContinuationCount int
	// StopReason is the harness's reason for stopping, as the wire enum
	// name (e.g. "STOP_REASON_MAX_TOOL_CALLS_EXCEEDED") or the empty
	// string.
	StopReason string
	// ErrorMessage is set when the turn ended because of an error.
	ErrorMessage string
}

// Request is one inbound hook call.
type Request struct {
	// Type is the hook type being invoked.
	Type Type
	// Name is the hook's name as the harness knows it. The harness uses
	// it for diagnostics ("hook %s has no caller configured"), not for
	// routing: routing is by Type.
	Name string
	// RequestID correlates the request with its response and must be
	// echoed verbatim.
	RequestID string
	// ClientID identifies the client that owns the conversation.
	ClientID string

	// PreTurn is set for TypePreTurn.
	PreTurn *PreTurnArgs
	// PostTurn is set for TypePostTurn.
	PostTurn *PostTurnArgs
	// PreTool is set for TypePreTool.
	PreTool *PreToolArgs
	// PostTool is set for TypePostTool.
	PostTool *PostToolArgs
	// OnToolError is set for TypeOnToolError.
	OnToolError *OnToolErrorArgs
	// OnCompaction is set for TypeOnCompaction.
	OnCompaction *OnCompactionArgs
	// Stop is set for TypeStop.
	Stop *StopArgs
}

// Response is the answer to one hook call.
//
// Exactly one of the payload fields is meaningful for a given Type, and the
// bridge in proto.go picks it:
//
//	PRE_TURN            → Decision, Reason
//	PRE_TOOL            → Decision, Reason, ModifiedArgs
//	ON_TOOL_ERROR       → CustomErrorMessage
//	STOP                → Decision, Reason
//	everything else     → nothing (EmptyResult)
type Response struct {
	// RequestID is the correlation id. The dispatcher fills it in from the
	// request; a handler may override it but must not.
	RequestID string
	// Decision is the verdict. See the per-type rules on Manager.Handle.
	Decision Decision
	// Reason is the human-readable explanation. It reaches the model on
	// a deny and the harness log on a stop.
	Reason string
	// ModifiedArgs is the rewritten tool argument map. Only meaningful
	// for TypePreTool; when set, the manager has already merged it over
	// the original arguments, so it is the complete argument set.
	ModifiedArgs map[string]any
	// CustomErrorMessage replaces the error text the model sees. Only
	// meaningful for TypeOnToolError.
	CustomErrorMessage string
	// Err is set when a handler failed. The bridge turns a non-nil Err
	// into the wire's `error_message` field instead of a result payload,
	// exactly as the shipped SDK does.
	Err error
}

// Handler is the in-process hook callback.
//
// A handler that returns a non-nil error fails the hook. For the deciding
// types (PRE_TURN, PRE_TOOL) the failure is fail-closed: the guarded action
// is denied.
type Handler func(ctx context.Context, req Request) (Response, error)

// Manager owns the registered handlers for one client session.
//
// The zero value is not usable; call NewManager.
type Manager struct {
	mu       sync.RWMutex
	handlers map[Type][]entry
	fired    map[Type]int
}

type entry struct {
	name string
	h    Handler
}

// NewManager returns an empty Manager.
func NewManager() *Manager {
	return &Manager{
		handlers: make(map[Type][]entry),
		fired:    make(map[Type]int),
	}
}

// Register adds h to the dispatch list for t.
//
// name identifies the hook in diagnostics and in Unregister; it must not be
// empty. Registering the same (type, name) twice is allowed — the SDK's
// HookRunner appends to a list too — and both handlers run, in registration
// order.
func (m *Manager) Register(t Type, name string, h Handler) error {
	if !t.Valid() {
		return fmt.Errorf("lifecyclehooks: cannot register unknown hook type %d", int(t))
	}
	if name == "" {
		return errors.New("lifecyclehooks: hook name must not be empty")
	}
	if h == nil {
		return fmt.Errorf("lifecyclehooks: hook %q for %s has no caller configured", name, t)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[t] = append(m.handlers[t], entry{name: name, h: h})
	return nil
}

// Unregister removes the first handler registered for (t, name) and reports
// whether one was found.
func (m *Manager) Unregister(t Type, name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.handlers[t]
	for i, e := range list {
		if e.name == name {
			m.handlers[t] = append(list[:i:i], list[i+1:]...)
			return true
		}
	}
	return false
}

// Has reports whether at least one handler is registered for t.
func (m *Manager) Has(t Type) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.handlers[t]) > 0
}

// EnabledTypes lists the hook types that have at least one handler, in
// wire-enum order. This is what the client advertises to the harness through
// `HarnessConfig.enabled_hooks`: the SDK only enables the types it can
// actually service.
func (m *Manager) EnabledTypes() []Type {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Type, 0, len(AllTypes))
	for _, t := range AllTypes {
		if len(m.handlers[t]) > 0 {
			out = append(out, t)
		}
	}
	return out
}

// Names lists the registered hook names for t, in registration order.
func (m *Manager) Names(t Type) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := m.handlers[t]
	out := make([]string, 0, len(list))
	for _, e := range list {
		out = append(out, e.name)
	}
	return out
}

// Fired returns how many times a hook of type t has been dispatched. Tests
// use it to assert a hook ran exactly once.
func (m *Manager) Fired(t Type) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.fired[t]
}

// Handle dispatches req to the handlers registered for req.Type and returns
// the answer to send back to the harness.
//
// # Per-type rules
//
// PRE_TURN and PRE_TOOL are *deciding* hooks. Handlers run in registration
// order; the first handler that returns DecisionDeny wins and the rest are
// not run. Otherwise the result is DecisionAllow.
//
// For PRE_TOOL, a handler may also return ModifiedArgs. The manager merges
// each handler's map over the accumulated arguments — later handlers win on
// a per-key basis — and hands the accumulated arguments to the next handler,
// so a later hook sees what the earlier hooks changed. When any handler
// modified the arguments the response carries the **complete** merged map,
// not a delta.
//
// ON_TOOL_ERROR is a *transform* hook: the first handler that returns a
// non-empty CustomErrorMessage wins.
//
// STOP is a *deciding* hook with inverted polarity: the first handler that
// returns DecisionContinue wins; otherwise the result is DecisionAllowStop.
//
// ON_SESSION_START, ON_SESSION_END, POST_TURN, POST_TOOL and ON_COMPACTION
// are *inspect* hooks: every handler runs and none of them can change the
// outcome.
//
// # Failure
//
// A handler error fails the hook. For the deciding types the failure is
// fail-closed — the guarded action is denied with the error text as the
// reason — because the harness blocks on a decision it cannot interpret
// ("Unrecognized pre-tool hook decision %q; blocked execution for safety.").
// For every other type the error is reported through Response.Err and the
// default decision stands.
//
// A handler that returns DecisionUnspecified for a deciding type is treated
// the same way: the action is denied. An unspecified decision from a STOP
// hook degrades to DecisionAllowStop, so a malformed stop hook cannot pin
// the agent in an endless loop.
//
// Handle never returns a nil Response, and never panics on a malformed
// request: a request whose payload does not match its type is dispatched
// with a nil payload pointer, and the handler is expected to cope.
func (m *Manager) Handle(ctx context.Context, req Request) Response {
	m.mu.Lock()
	list := append([]entry(nil), m.handlers[req.Type]...)
	m.fired[req.Type]++
	m.mu.Unlock()

	resp := Response{RequestID: req.RequestID, Decision: defaultDecision(req.Type)}
	if len(list) == 0 {
		return resp
	}

	switch req.Type {
	case TypePreTurn:
		return m.handlePreTurn(ctx, req, list, resp)
	case TypePreTool:
		return m.handlePreTool(ctx, req, list, resp)
	case TypeStop:
		return m.handleStop(ctx, req, list, resp)
	case TypeOnToolError:
		return m.handleOnToolError(ctx, req, list, resp)
	default:
		return m.handleInspect(ctx, req, list, resp)
	}
}

// defaultDecision is the answer for a hook type with no handlers, and the
// starting point for the inspect path.
func defaultDecision(t Type) Decision {
	if t == TypeStop {
		return DecisionAllowStop
	}
	return DecisionAllow
}

// handlePreTurn implements the deciding PRE_TURN dispatch.
//
// The SDK's `HookRunner.dispatch_pre_turn` is the model: it walks the
// registered hooks in order and returns as soon as one of them does not
// allow the turn; if every hook allows, the turn proceeds.
//
// A hook that answers with something other than ALLOW or DENY has not
// expressed a verdict. The SDK cannot express that state — its `HookResult`
// carries an explicit `allow` boolean — so the wire value would be
// DECISION_UNSPECIFIED, which is a decision the harness has to interpret.
// This dispatcher therefore fails closed and denies the turn instead of
// inventing an allow.
func (m *Manager) handlePreTurn(ctx context.Context, req Request, list []entry, resp Response) Response {
	for _, e := range list {
		if err := ctx.Err(); err != nil {
			resp.Decision = DecisionDeny
			resp.Reason = "hook dispatch canceled: " + err.Error()
			resp.Err = err
			return resp
		}
		out, err := e.h(ctx, req)
		if err != nil {
			resp.Decision = DecisionDeny
			resp.Reason = hookFailureReason(e.name, req.Type, err)
			resp.Err = err
			return resp
		}
		switch out.Decision {
		case DecisionDeny:
			resp.Decision = DecisionDeny
			resp.Reason = out.Reason
			return resp
		case DecisionAllow:
			// Keep going: a later hook may still deny the turn.
		default:
			resp.Decision = DecisionDeny
			resp.Reason = fmt.Sprintf("Unrecognized pre-turn hook decision %q; blocked execution for safety.", out.Decision)
			return resp
		}
	}
	return resp
}

// handlePreTool implements the deciding PRE_TOOL dispatch.
func (m *Manager) handlePreTool(ctx context.Context, req Request, list []entry, resp Response) Response {
	merged := map[string]any{}
	if req.PreTool != nil && req.PreTool.ArgumentsJSON != "" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(req.PreTool.ArgumentsJSON), &parsed); err == nil && parsed != nil {
			merged = parsed
		}
	}
	modified := false

	for _, e := range list {
		if err := ctx.Err(); err != nil {
			resp.Decision = DecisionDeny
			resp.Reason = "hook dispatch canceled: " + err.Error()
			resp.Err = err
			return resp
		}
		// The next handler sees the accumulated arguments.
		if req.PreTool != nil {
			clone := *req.PreTool
			if encoded, err := json.Marshal(merged); err == nil {
				clone.ArgumentsJSON = string(encoded)
			}
			req.PreTool = &clone
		}
		out, err := e.h(ctx, req)
		if err != nil {
			resp.Decision = DecisionDeny
			resp.Reason = hookFailureReason(e.name, req.Type, err)
			resp.Err = err
			return resp
		}
		switch out.Decision {
		case DecisionDeny:
			resp.Decision = DecisionDeny
			resp.Reason = out.Reason
			return resp
		case DecisionAllow:
			if len(out.ModifiedArgs) > 0 {
				for k, v := range out.ModifiedArgs {
					merged[k] = v
				}
				modified = true
			}
		default:
			// An unspecified or nonsensical decision is unrecognised.
			resp.Decision = DecisionDeny
			resp.Reason = fmt.Sprintf("Unrecognized pre-tool hook decision %q; blocked execution for safety.", out.Decision)
			return resp
		}
	}
	if modified {
		resp.ModifiedArgs = merged
	}
	return resp
}

// handleStop implements the STOP dispatch: the first CONTINUE wins.
func (m *Manager) handleStop(ctx context.Context, req Request, list []entry, resp Response) Response {
	for _, e := range list {
		if err := ctx.Err(); err != nil {
			resp.Err = err
			return resp
		}
		out, err := e.h(ctx, req)
		if err != nil {
			// A failing stop hook must not pin the loop: the turn ends.
			resp.Err = err
			resp.Reason = hookFailureReason(e.name, req.Type, err)
			return resp
		}
		switch out.Decision {
		case DecisionContinue:
			resp.Decision = DecisionContinue
			resp.Reason = out.Reason
			return resp
		case DecisionAllowStop:
			return resp
		default:
			// Unspecified: degrade to ALLOW_STOP.
			return resp
		}
	}
	return resp
}

// handleOnToolError implements the transform dispatch: the first non-empty
// custom message wins.
func (m *Manager) handleOnToolError(ctx context.Context, req Request, list []entry, resp Response) Response {
	for _, e := range list {
		if err := ctx.Err(); err != nil {
			resp.Err = err
			return resp
		}
		out, err := e.h(ctx, req)
		if err != nil {
			resp.Err = err
			resp.Reason = hookFailureReason(e.name, req.Type, err)
			return resp
		}
		if out.CustomErrorMessage != "" {
			resp.CustomErrorMessage = out.CustomErrorMessage
			return resp
		}
	}
	return resp
}

// handleInspect runs every handler and lets none of them change the
// outcome. The first error is recorded and the remaining handlers still run,
// so a broken observer cannot silence the others.
func (m *Manager) handleInspect(ctx context.Context, req Request, list []entry, resp Response) Response {
	for _, e := range list {
		if err := ctx.Err(); err != nil {
			resp.Err = err
			return resp
		}
		if _, err := e.h(ctx, req); err != nil && resp.Err == nil {
			resp.Err = err
			resp.Reason = hookFailureReason(e.name, req.Type, err)
		}
	}
	return resp
}

// hookFailureReason renders the failure text the harness logs. The binary
// string is "hook failed for %s: %v".
func hookFailureReason(name string, t Type, err error) string {
	if name == "" {
		return fmt.Sprintf("hook failed for %s: %v", t, err)
	}
	return fmt.Sprintf("hook failed for %s: %v", name, err)
}

// SortNames returns names sorted lexicographically. It exists so callers
// that render a hook inventory get a stable order without importing sort.
func SortNames(names []string) []string {
	out := append([]string(nil), names...)
	sort.Strings(out)
	return out
}
