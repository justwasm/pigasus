// hooks_runtime.go — the host's side of the documented hooks.json
// runtime.
//
// # Where the runtime lives
//
// The runtime itself is internal/core/policy/hooks: it owns the six
// event names, the document shape, the loader, the decision vocabulary
// and the execution of a handler's command. It is in the core tier, so
// it cannot reach the app tier — and the host supplies the three things
// the core tier cannot know:
//
//   - the shell a handler's command runs through (the app tier's shell,
//     the one the reference runs its own commands through),
//   - the session's own facts for the payload (the conversation's id,
//     the mounted workspace, the model), and
//   - the places the six events fire from.
//
// # What fires where
//
// SessionStart fires once per session, before the first model
// invocation. PreInvocation and PostInvocation bracket a turn.
// PreToolUse and PostToolUse bracket a tool call. Stop fires when the
// turn ends.
//
// # What is not acted on
//
// The documented outputs the clone parses but does not act on are the
// injected steps (the trajectory is the session store's, and the
// injection is not wired into it) and the Stop hook's "continue" (the
// clone's turn ends when the turn engine returns). Both are reported in
// the cycle report.

package tui

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/core/backend"
	corehooks "github.com/justwasm/pigasus/internal/core/policy/hooks"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
)

// hookToolGate is the answer the documented PreToolUse hooks gave about
// one tool call.
//
// The hooks decide the call: their verdict replaces the policy's own
// verdict, and the policy decides only what the hooks left undecided.
type hookToolGate struct {
	// Denied is true when the hooks blocked the call.
	Denied bool
	// Approved is true when the hooks approved the call themselves.
	Approved bool
	// Reason is the text the model reads when the call is blocked.
	Reason string
	// Overwrite are the replacement arguments the hooks asked for.
	Overwrite map[string]json.RawMessage
}

// hookRuntime returns the session's documented hooks runtime.
//
// The documents are read once, when the session arms the runtime: the
// global one at ~/.gemini/config/hooks.json and the workspace one at
// <workspace>/.agents/hooks.json. A document that is not there is not
// an error — the session simply has no hooks.
func (m *RootModel) hookRuntime() *corehooks.Runtime {
	if m == nil {
		return nil
	}
	if m.session.hookRT != nil {
		return m.session.hookRT
	}
	workspace := m.hookWorkspace()
	m.session.hookRT = &corehooks.Runtime{
		Registry: corehooks.Load(workspace),
		Shell:    shell.DefaultShellArgv,
		WorkDir:  workspace,
	}
	return m.session.hookRT
}

// hookWorkspace is the workspace the session's hooks are read from.
func (m *RootModel) hookWorkspace() string {
	if m == nil {
		return ""
	}
	if m.session.cwd != "" {
		return m.session.cwd
	}
	return defaultWorkspaceForRootModel()
}

// hookInput builds the payload the documented handlers receive on stdin.
//
// The common fields are the session's own: the conversation's id, the
// mounted workspace and the model that handles the invocation. The
// transcript and the artifact directory are the reference's own paths
// under ~/.gemini/antigravity-cli — the clone does not write them, so
// they stay empty rather than name a file that is not there.
func (m *RootModel) hookInput() corehooks.Input {
	in := corehooks.Input{
		ConversationID: m.session.conversationID,
		ModelName:      m.turnModel(),
	}
	if ws := m.hookWorkspace(); ws != "" {
		in.WorkspacePaths = []string{ws}
	}
	return in
}

// fireHook dispatches one of the six documented events.
//
// The bus's own in-process handlers run first; the documented handlers
// run after them, through the runtime.
func (m *RootModel) fireHook(ctx context.Context, ev corehooks.Event, in corehooks.Input) corehooks.Outcome {
	if m == nil || m.session.hooks == nil {
		return corehooks.Outcome{}
	}
	out, err := m.session.hooks.FireDocumented(ctx, m.hookRuntime(), ev, in)
	if err != nil {
		out.Problems = append(out.Problems, err.Error())
	}
	return out
}

// hookSessionStart fires the SessionStart event.
//
// The event fires once per session, before the first model invocation.
func (m *RootModel) hookSessionStart(ctx context.Context) {
	if m == nil || m.session.hookSessionStarted {
		return
	}
	m.session.hookSessionStarted = true
	m.fireHook(ctx, corehooks.EventSessionStart, m.hookInput())
}

// hookPreInvocation fires the PreInvocation event.
//
// The clone's turn is the host's unit: the model's own invocations
// happen inside the turn engine, so the event fires once per turn.
func (m *RootModel) hookPreInvocation(ctx context.Context) {
	if m == nil {
		return
	}
	m.session.hookInvocationNum++
	in := m.hookInput()
	in.InvocationNum = m.session.hookInvocationNum
	loaded, calls, _ := m.trajectoryCounts()
	in.InitialNumSteps = loaded + calls
	m.fireHook(ctx, corehooks.EventPreInvocation, in)
}

// hookPostInvocation fires the PostInvocation event.
func (m *RootModel) hookPostInvocation(ctx context.Context) {
	if m == nil {
		return
	}
	in := m.hookInput()
	in.InvocationNum = m.session.hookInvocationNum
	loaded, calls, _ := m.trajectoryCounts()
	in.InitialNumSteps = loaded + calls
	m.fireHook(ctx, corehooks.EventPostInvocation, in)
}

// hookStop fires the Stop event.
//
// The event fires when the execution loop terminates, which in the clone
// is the end of the turn.
func (m *RootModel) hookStop(ctx context.Context) {
	if m == nil {
		return
	}
	in := m.hookInput()
	in.ExecutionNum = m.session.hookInvocationNum
	in.FullyIdle = m.hookIdle()
	m.fireHook(ctx, corehooks.EventStop, in)
}

// hookIdle reports whether every background task has finished.
//
// The Stop payload's fullyIdle flag says whether the agent is
// completely finished: a background command or an asynchronous task
// that is still running makes it false.
func (m *RootModel) hookIdle() bool {
	if m == nil || m.tasks.reg == nil {
		return true
	}
	return m.tasks.reg.CountRunning() == 0
}

// hookToolGate asks the documented PreToolUse hooks about one tool
// call.
//
// The hooks decide the call. Their verdict replaces the policy's own
// verdict — a hook that approves the call approves it, and a hook that
// blocks it blocks it — and the policy decides only what the hooks left
// undecided.
//
// The decision "allow" is the delegation the documentation names: the
// call goes to the default policy. The decision "ask" respects the
// user's own "always allow" grants, and "force_ask" does not.
func (m *RootModel) hookToolGate(ctx context.Context, call backend.ToolCall) hookToolGate {
	rt := m.hookRuntime()
	if rt == nil || rt.Registry.Len() == 0 {
		return hookToolGate{}
	}
	in := m.hookInput()
	in.ToolCall = &corehooks.ToolCall{Name: call.Name, Args: call.Arguments}
	out := m.fireHook(ctx, corehooks.EventPreToolUse, in)
	if out.Ran == 0 {
		// No handler ran: the policy decides.
		return hookToolGate{}
	}
	gate := hookToolGate{Overwrite: out.Overwrite}
	target := toolrunner.ToolTarget(call)
	names := tools.PermissionNames(call.Name)
	policy := permissions.Policy{}
	if m.session.policy != nil {
		policy = *m.session.policy
	}
	switch out.Decision {
	case corehooks.DecisionAllow:
		// The decision delegates to the default policy.
		return gate
	case corehooks.DecisionAsk:
		// The decision respects the user's "always allow" grants.
		if policy.HasGrant(target, names...) {
			return gate
		}
		gate.Denied = true
		gate.Reason = hookDenialReason(call, target, out.Reason)
		return gate
	case corehooks.DecisionForceAsk:
		gate.Denied = true
		gate.Reason = hookDenialReason(call, target, out.Reason)
		return gate
	case corehooks.DecisionDeny:
		gate.Denied = true
		gate.Reason = hookDenialReason(call, target, out.Reason)
		return gate
	case corehooks.DecisionAutoApprove:
		if policy.HookVerdict(out.Decision, target, names...) == permissions.Allow {
			gate.Approved = true
			return gate
		}
		gate.Denied = true
		gate.Reason = hookDenialReason(call, target, out.Reason)
		return gate
	case corehooks.DecisionDenyUnlessPriorGrant:
		if policy.HookVerdict(out.Decision, target, names...) == permissions.Allow {
			gate.Approved = true
			return gate
		}
		gate.Denied = true
		gate.Reason = hookDenialReason(call, target, out.Reason)
		return gate
	}
	// An answer the documentation does not name is a prompt: the call
	// does not run.
	gate.Denied = true
	gate.Reason = hookDenialReason(call, target, out.Reason)
	return gate
}

// hookPostToolUse fires the PostToolUse event after a tool call.
//
// The event sees the call that ran and the error it failed with.
func (m *RootModel) hookPostToolUse(ctx context.Context, call backend.ToolCall, err error) {
	rt := m.hookRuntime()
	if rt == nil || rt.Registry.Len() == 0 {
		return
	}
	in := m.hookInput()
	in.ToolCall = &corehooks.ToolCall{Name: call.Name, Args: call.Arguments}
	if err != nil {
		in.Error = err.Error()
	}
	m.fireHook(ctx, corehooks.EventPostToolUse, in)
}

// hookDenialReason is the text the model reads when the hooks blocked
// the call.
//
// The hook's own reason is the explanation the documentation promises;
// without one the clone falls back to the reference's own denial text.
func hookDenialReason(call backend.ToolCall, target, reason string) string {
	if strings.TrimSpace(reason) != "" {
		return reason
	}
	return permissions.DeniedToolResult(call.Name, target)
}

// mergeHookArgs merges the hooks' replacement arguments into the call's
// own arguments.
//
// The merge is shallow, the way the documentation names it: a key the
// hook named replaces the call's own value, and the keys it did not
// name are kept.
func mergeHookArgs(args json.RawMessage, overwrite map[string]json.RawMessage) json.RawMessage {
	if len(overwrite) == 0 {
		return args
	}
	obj := map[string]json.RawMessage{}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &obj); err != nil {
			obj = map[string]json.RawMessage{}
		}
	}
	for k, v := range overwrite {
		obj[k] = v
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return args
	}
	return out
}

// approvedPolicy is the policy a hook's approval runs under.
//
// The hooks approved the call, so the policy's own prompt does not
// decide it any more. The terminal sandbox still does: it is checked
// before the policy, and it is not the policy's business.
func approvedPolicy(p *permissions.Policy) *permissions.Policy {
	if p == nil {
		return nil
	}
	eff := *p
	eff.SkipPermissions = true
	return &eff
}
