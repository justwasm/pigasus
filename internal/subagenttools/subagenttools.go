// Package subagenttools declares the AGY 1.4.0 subagent toolset as
// PiG-compatible AgentTools.
//
// AGY's advertised surface includes five subagent tools:
// `invoke_subagent`, `send_message`, `manage_subagents`,
// `define_subagent`, `generate_image` (see
// `internal/cli/testdata/changelog_reference.txt` line 513 / 234 / 213).
//
// Four are here; `generate_image` lives with the other image code and is
// still a stub. Since cycle 293 the result texts and the unknown-recipient /
// unknown-type / empty-registry paths are the reference's own (【一手】 agy
// 1.4.4):
//
//   - `define_subagent` stores a conversation-scoped definition (the store is
//     one per Tools() call) and returns the reference's success sentence.
//   - `invoke_subagent` consults that store, rejects a TypeName that is
//     neither a definition nor one of the built-ins (self, research,
//     image-generator) with `subagent "X" not found or not allowed to be
//     invoked`, and otherwise delegates to a fresh PiG coding.Session.
//   - `send_message` reports `recipient "X" not found` — delegations here are
//     synchronous, so no recipient is ever live by the time the parent can
//     message it.
//   - `manage_subagents` reports the reference's registry texts: `list` prints
//     `You have N active subagent(s):` followed by the marshalled states,
//     `kill` with explicit ConversationIds names the first id that is not an
//     active subagent, and kill/kill_all with nothing live prints
//     `No active subagents to kill.`
//
// The reference's invoke is asynchronous: it returns a launch acknowledgement
// immediately and delivers the subagent's answer later via send_message. The
// clone blocks and returns the answer in a JSON envelope; making that async is
// a feature, not a text fix, and is documented in the cycle-293 report.
//
// Why a fresh PiG `coding.Session` (not `agent.Agent` or a
// `runtime.Worker`): AGY's documented runtime model is a fresh agent
// per delegation (research/agy-subagents-vs-wagyu.md §Future-cycle
// map, cycle 228 row: "real LLM-driven delegation via PiG
// `coding.Agent` as sub-runtime"). PiG's `coding.Session` is the
// highest-level turn engine PiG ships; using it gives the subagent:
// MCP extensions, skill discovery, prompt-cache by session ID, and
// session persistence (`NoSession: true` for ephemeral). `Worker` is
// a lower-level batch executor that does not own a transcript, which
// is the opposite of what AGY's per-subagent transcript needs.
//
// Synchronous semantic: AGY's `invoke_subagent` returns the
// subagent's final answer as the parent tool's result; intermediate
// tool calls inside the subagent are NOT visible to the parent. This
// package's `Invoke` blocks until the subagent's session settles or
// ctx is cancelled, returning the assistant's concatenated text. A
// future cycle (229) will surface subagent progress to the parent's
// TUI — today the parent only sees a single tool-call -> tool-result
// pair.
//
// Transcript persistence: every successful delegation writes a JSONL
// transcript to `<AgentDir>/subagents/<id>.jsonl`. The on-disk format
// is intentionally PiG's `coding.Session` JSONL layout (PiG owns the
// session that writes it), so a future `/agents` UI can hand the file
// to PiG's resume machinery and recover the transcript byte-identical
// to the live one. Cycle 229 work, not this cycle.
package subagenttools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"
	"github.com/justwasm/pigasus/internal/taskregistry"
)

// Invocation is the LLM-driven delegation request from the parent
// tool call. All fields except Prompt are optional; empty values fall
// back to the Deps defaults or to a sane PiG-built-in value.
type Invocation struct {
	// Prompt is the user message sent to the subagent as its first turn.
	// Required.
	Prompt string
	// Model is the wire model id (e.g. "claude-sonnet-4-6"). Empty uses
	// Deps.Model.
	Model string
	// SystemPrompt is the subagent's system instruction. Empty uses
	// Deps.SystemPrompt, then PiG's default coding prompt plus
	// discovered resources.
	SystemPrompt string
	// Worktree, when non-empty, is the absolute path the subagent's
	// session is anchored to — a power-user override for tests and
	// custom orchestrators that already have a directory they want to
	// reuse. Empty (the typical case) routes Invoke down the
	// worktree-isolation path: a fresh per-subagent directory is
	// created under `<AgentDir>/worktrees/<subagent-id>/` and the
	// subagent runs there, never in the parent's CWD. Cycle 229 will
	// upgrade that materialised directory to a real `git worktree add`
	// (same place, just with .git metadata inside); today's cycle
	// focuses on WHERE the worktree lives so files written there are
	// never picked up as artifacts in the user's workspace.
	Worktree string
	// Tools are the tools the subagent's own session may call, on top of
	// the ones its extensions contribute. Nil (the default) gives the
	// subagent no tools at all — the plain delegation shape this package
	// shipped with.
	//
	// Cycle 311 uses the seam for the reference's browser agent: the
	// browser subagent's whole point is that it drives the browser, so
	// its session carries the browser-control tools (see
	// internal/browsersubagent).
	Tools []piagent.AgentTool
	// Grant is the tool authorization the subagent runs under. It is
	// what define_subagent's three switches recorded, and it is what
	// the invoke tool turns into the subagent's tool set.
	//
	// A zero Grant is the reference's base set: the research tools and
	// the messaging tool.
	Grant Grant
	// TypeName is the subagent type the call named ("self", "research",
	// or a definition's own name). The reference's list entry reports
	// it, and it is the type's own name the role falls back to.
	TypeName string
	// Role is the role the call asked for ("A 2-5 word description of
	// the subagent's role"). Empty means the type's own name.
	Role string
	// InheritPrompt marks a delegation that inherits the parent's own
	// system prompt as it is — the reference's `self` type, "inherits
	// the parent agent's full configuration including tools, system
	// prompt, and model". The task instruction is not added to such a
	// prompt: the prompt *is* the parent's.
	InheritPrompt bool
}

// Deps is the cross-call state Invoke needs: PiG services (for the
// shared registry / settings / extension runner), the model + provider
// the subagent calls, and the AgentDir / CWD defaults.
//
// The provider and model can be left zero; Invoke falls back to
// `coding.Services`-resolved values for the model id and a no-op
// provider if none is configured (which only the test fakes do).
type Deps struct {
	// Services is the PiG services container from the parent runtime.
	// Required.
	Services *coding.AgentSessionServices
	// Model is the default model the subagent calls. Used when
	// Invocation.Model is empty.
	Model *pi.Model
	// Provider, when set, is recorded for diagnostics only — the
	// model's own Provider is what the subagent's session uses. Nil is
	// fine.
	Provider pi.Provider
	// SystemPrompt is the parent's own system prompt, resolved when a
	// delegation needs it. Used when Invocation.SystemPrompt is empty.
	//
	// The parent hands its own prompt in here: the reference's `self`
	// type "inherits the parent agent's full configuration including
	// tools, system prompt, and model", so a `self` delegation runs on
	// the parent's text. A delegation that names its own type runs on
	// that type's own prompt and never reads this field.
	//
	// It is a function because building the prompt is not free (the
	// reference's prompt walks the skills directories), and a turn that
	// spawns no subagent should not pay for it.
	SystemPrompt func() string
	// Catalogue is the parent's own tool catalogue, adapted to PiG's
	// tool interface.
	//
	// A subagent's tool set is drawn from it: the reference's grants
	// say *which* of the parent's tools a subagent may call, not that
	// the subagent gets new ones. Nil gives every subagent no
	// catalogue tools — only the messaging tool (and, with the subagent
	// grant, define_subagent and invoke_subagent).
	Catalogue []piagent.AgentTool
	// CWD is a fallback used only when the worktree-isolation
	// allocation fails (read-only tests, sandboxes that block
	// mkdir). In normal operation Invoke never reads this field:
	// subagent sessions are anchored to a fresh directory under
	// `<AgentDir>/worktrees/<subID>/` so files written by the
	// subagent never collide with the user's workspace.
	CWD string
	// AgentDir is the parent agent's config directory. Subagent
	// transcripts are persisted under `<AgentDir>/subagents/`. Empty
	// defaults to Services.AgentDir().
	AgentDir string
	// PermissionGate, when non-nil, is consulted for every tool call
	// the subagent issues. A denied call is reported to the subagent's
	// model as the reference's denial text (matching the parent's
	// `permissionRunner` semantics).
	//
	// It is a factory rather than a gate because the sandbox that guards
	// a subagent's commands has to allow the subagent's own worktree,
	// and the worktree is only known once the delegation has been
	// launched. The factory is called with the directory the subagent
	// runs in.
	//
	// The policy behind it is the conversation's one policy: a rule the
	// user wrote for the parent governs the subagent too, and the tool
	// is judged under both of its names (see
	// internal/tools.PermissionNames).
	PermissionGateFor func(worktree string) BeforeToolCallGate
	// Registry is the conversation's live-subagent registry.
	//
	// It is what makes a delegation asynchronous: the child is reachable
	// by its conversation id while it runs, it can message the parent,
	// and the parent can message it. Nil turns every recipient into the
	// reference's "not found" answer.
	Registry *Registry
	// ConversationID is the id of the conversation that owns this tool
	// set — the parent's own id. A subagent's messaging tool addresses
	// the parent by it, and the parent's messages carry it as the
	// sender.
	ConversationID string
	// Definitions is the conversation's subagent definition store. The
	// reference scopes a definition to the conversation
	// ("Define a subagent for the duration of this conversation"), so
	// the store belongs to the conversation, not to the tool set. A
	// resumed conversation passes the store the rehydration pass
	// filled in; a fresh one leaves it nil and Tools creates it.
	Definitions *Store
	// Tasks is the optional task registry to register running subagents into.
	Tasks *taskregistry.Registry
}

// BeforeToolCallGate is the optional policy hook a subagent's tool
// batch runs through. It mirrors `piagent.BeforeToolCallHook` but is
// declared here so this package does not have to be threaded through
// `internal/permissions` (which would drag in the full permission
// stack for a feature that mostly only the TUI wires today).
type BeforeToolCallGate interface {
	Decide(ctx context.Context, toolName string, args json.RawMessage) GateDecision
}

// GateDecision is what the gate returns for one tool call. Allow lets
// it run; Block returns a denial message that the subagent's session
// reports back to the model in place of a result.
type GateDecision struct {
	Allow bool
	// Reason is the denial text the model sees when Allow is false.
	Reason string
}

// Result is the public return of Invoke. The shape is stable across
// the package so a caller can marshal it as the parent tool's result.
type Result struct {
	// Text is the subagent's final assistant text — the concatenation
	// of every assistant text block in the turn that ended with
	// StopReason == "stop" or "toolUse". Empty when the subagent
	// failed.
	Text string
	// SessionID is the new session's PiG-assigned id (the
	// `<ts>_<id>.jsonl` filename stem). Empty when persistence is
	// disabled (NoSession). send_message/manage_subagents will hand
	// back this id once cycle 229 wires them.
	SessionID string
	// SubagentID is a stable handle for this delegation: a short
	// human-readable tag like "subagent-<unix-nano>-<counter>".
	SubagentID string
	// Messages is the raw agent messages the subagent's session
	// produced. Useful for logs and tests; cycle 229 will surface
	// them to the parent TUI.
	Messages []piagent.AgentMessage
	// Duration is the wall-clock time the delegation took.
	Duration time.Duration
	// WorktreePath is the absolute path the subagent's session was
	// anchored to. Empty only when Invoke was called with
	// Invocation.Worktree == "" AND the worktree-isolation allocation
	// failed (a fatal error in that case — the call never ran). When
	// Invocation.Worktree was set explicitly, the value echoes back
	// the override; otherwise it is the freshly-created path under
	// `<AgentDir>/worktrees/<subagent-id>/`.
	WorktreePath string
}

// Tools returns the four tool declarations: invoke_subagent, define_subagent,
// send_message and manage_subagents. The parent loop appends them to its
// advertised tool set; per the project rule "caller-supplied take precedence
// on name collision", any pre-existing `invoke_subagent` (the
// `internal/tools/catalog.go` placeholder) is superseded by this one.
//
// Deps.Definitions is the conversation's definition store. When it is nil,
// Tools creates a fresh one — the behaviour of a conversation that has not
// been resumed. A caller that resumes a conversation passes the store the
// rehydration pass filled (see internal/rehydration).
func Tools(deps Deps) []piagent.AgentTool {
	defs := deps.Definitions
	if defs == nil {
		defs = NewStore()
	}
	return []piagent.AgentTool{
		&invokeTool{deps: deps, defs: defs},
		&defineTool{defs: defs},
		&sendMessageTool{registry: deps.Registry, owner: deps.ConversationID},
		&manageSubagentsTool{registry: deps.Registry, owner: deps.ConversationID},
	}
}

// resultStamp is the Created At / Completed At header the reference puts in
// front of every tool result. The file tools reproduce it in internal/tools;
// the subagent tools need the same header because their results are ordinary
// functionResponse outputs too (【一手】 1.4.4 captures).
func resultStamp(start time.Time) string {
	return "Created At: " + start.Format(time.RFC3339) + "\nCompleted At: " + time.Now().Format(time.RFC3339) + "\n"
}

// Store holds the subagents define_subagent created for one
// conversation. The reference scopes these to the conversation; a store
// is therefore conversation-scoped, not turn-scoped: the same store
// serves every turn of a conversation, and a resumed conversation gets
// its store back from the trajectory (see internal/rehydration).
//
// Before the rehydration pass existed, Tools() created one store per
// call — and the TUI calls Tools() once per turn, so a definition made
// in one turn was gone by the next. Deps.Definitions now carries the
// conversation's store into the tool set.
type Store struct {
	mu sync.Mutex
	m  map[string]Invocation
	// killed holds the conversation ids the trajectory says were
	// killed. The clone has no live subagents to resurrect, so the
	// set exists to answer "was this one killed?" — the question
	// `manage_subagents` asks.
	killed map[string]bool
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{m: map[string]Invocation{}, killed: map[string]bool{}}
}

// Put records a subagent definition under name.
func (d *Store) Put(name string, inv Invocation) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.m == nil {
		d.m = map[string]Invocation{}
	}
	d.m[name] = inv
}

// Get returns the definition stored under name.
func (d *Store) Get(name string) (Invocation, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	inv, ok := d.m[name]
	return inv, ok
}

// Names returns the names of every stored definition, sorted.
func (d *Store) Names() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, 0, len(d.m))
	for name := range d.m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Kill records that the subagent with this conversation id was killed.
func (d *Store) Kill(conversationID string) {
	if conversationID == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.killed == nil {
		d.killed = map[string]bool{}
	}
	d.killed[conversationID] = true
}

// Killed reports whether the subagent with this conversation id was
// killed.
func (d *Store) Killed(conversationID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.killed[conversationID]
}

// parentPrompt returns the parent's system prompt, or "" when the
// caller does not know one.
func (d Deps) parentPrompt() string {
	if d.SystemPrompt == nil {
		return ""
	}
	return d.SystemPrompt()
}

// beforeToolCallHooks turns the optional Deps.PermissionGateFor into the
// PiG-shaped hook PiG's session runs before every tool call.
//
// The gate is built for the directory the subagent runs in, so the sandbox
// behind it can allow the subagent's own worktree.
func (d Deps) beforeToolCallHooks(worktree string) []piagent.BeforeToolCallHook {
	if d.PermissionGateFor == nil {
		return nil
	}
	gate := d.PermissionGateFor(worktree)
	if gate == nil {
		return nil
	}
	return []piagent.BeforeToolCallHook{
		func(_ context.Context, _ string, toolName string, args json.RawMessage) piagent.ToolCallHookResult {
			decision := gate.Decide(context.Background(), toolName, args)
			if decision.Allow {
				return piagent.ToolCallHookResult{}
			}
			return piagent.ToolCallHookResult{Block: true, Reason: decision.Reason}
		},
	}
}

// ─── subagent id generation ───────────────────────────────────────────────

var (
	subagentCounterMu sync.Mutex
	subagentCounter   uint64
)

func newSubagentID() string {
	subagentCounterMu.Lock()
	subagentCounter++
	n := subagentCounter
	subagentCounterMu.Unlock()
	return fmt.Sprintf("subagent-%d-%d", time.Now().UnixNano(), n)
}

// Compile-time assertion: all four tools implement piagent.AgentTool.
var (
	_ piagent.AgentTool = (*invokeTool)(nil)
	_ piagent.AgentTool = (*defineTool)(nil)
	_ piagent.AgentTool = (*sendMessageTool)(nil)
	_ piagent.AgentTool = (*manageSubagentsTool)(nil)
)
