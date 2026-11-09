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

	"github.com/justwasm/pigasus/actor"
	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/core/backend"
)

// Invocation is the LLM-driven delegation request from the parent
// tool call. All fields except Prompt are optional; empty values fall
// back to the Deps defaults or to a sane built-in value.
type Invocation struct {
	// Prompt is the user message sent to the subagent as its first turn.
	// Required.
	Prompt string
	// Model is the wire model id (e.g. "claude-sonnet-4-6"). Empty uses
	// Deps.Model.
	Model string
	// SystemPrompt is the subagent's system instruction. Empty uses
	// Deps.SystemPrompt.
	SystemPrompt string
	// Worktree, when non-empty, is the absolute path the subagent's
	// session is anchored to. Empty routes Invoke down the
	// worktree-isolation path: a fresh per-subagent directory is
	// created under `<AgentDir>/worktrees/<subagent-id>/`.
	Worktree string
	// Tools are the tools the subagent's own session may call.
	Tools []actor.Tool
	// Grant is the tool authorization the subagent runs under. It is
	// what define_subagent's three switches recorded, and it is what
	// the invoke tool turns into the subagent's tool set.
	Grant Grant
	// TypeName is the subagent type the call named ("self", "research",
	// or a definition's own name).
	TypeName string
	// Role is the role the call asked for.
	Role string
	// InheritPrompt marks a delegation that inherits the parent's own
	// system prompt as it is.
	InheritPrompt bool
}

// Deps is the cross-call state Invoke needs: the model + provider
// the subagent calls, and the AgentDir / CWD defaults.
type Deps struct {
	// Model is the default model the subagent calls. Used when
	// Invocation.Model is empty.
	Model string
	// Effort is the reasoning effort the subagent inherits from the
	// parent session. It is displayed with the model (AGY 1.5.3); the
	// actor transport derives its own generation settings from the model.
	Effort string
	// Provider is the actor.Provider the subagent calls.
	Provider actor.Provider
	// Backend is the checkpoint policy's summariser transport. Nil disables
	// automatic checkpointing for the subagent.
	Backend backend.Client
	// SystemPrompt is the parent's own system prompt, resolved when a
	// delegation needs it. Used when Invocation.SystemPrompt is empty.
	SystemPrompt func() string
	// Catalogue is the parent's own tool catalogue.
	Catalogue []actor.Tool
	// CWD is a fallback used only when the worktree-isolation
	// allocation fails.
	CWD string
	// AgentDir is the parent agent's config directory. Subagent
	// transcripts are persisted under `<AgentDir>/subagents/`.
	AgentDir string
	// PermissionGate, when non-nil, is consulted for every tool call
	// the subagent issues.
	PermissionGateFor func(worktree string) BeforeToolCallGate
	// Registry is the conversation's live-subagent registry.
	Registry *Registry
	// ConversationID is the id of the conversation that owns this tool
	// set — the parent's own id.
	ConversationID string
	// Definitions is the conversation's subagent definition store.
	Definitions *Store
	// Tasks is the optional task registry to register running subagents into.
	Tasks *taskregistry.Registry
	// Questions is the optional subagent question store.
	Questions *subagentquestions.Manager
	// OnAsk is called when a question is enqueued by a subagent.
	OnAsk func(scope string)
}

// BeforeToolCallGate is the optional policy hook a subagent's tool
// batch runs through.
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
	// Text is the subagent's final assistant text.
	Text string
	// SessionID is the new session's assigned id.
	SessionID string
	// SubagentID is a stable handle for this delegation.
	SubagentID string
	// Messages is the transcript messages the subagent's session produced.
	Messages []SubagentMessage
	// Duration is the wall-clock time the delegation took.
	Duration time.Duration
	// WorktreePath is the absolute path the subagent's session was anchored to.
	WorktreePath string
}

// Tools returns the four tool declarations: invoke_subagent, define_subagent,
// send_message and manage_subagents.
func Tools(deps Deps) []actor.Tool {
	defs := deps.Definitions
	if defs == nil {
		defs = NewStore()
	}
	return []actor.Tool{
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
// its store back from the trajectory (see internal/app/session/rehydration).
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

// Compile-time assertion: all four tools implement actor.Tool.
var (
	_ actor.Tool = (*invokeTool)(nil)
	_ actor.Tool = (*defineTool)(nil)
	_ actor.Tool = (*sendMessageTool)(nil)
	_ actor.Tool = (*manageSubagentsTool)(nil)
)
