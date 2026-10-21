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
//   - `manage_subagents` reports `No active subagents to kill.` for
//     kill/kill_all; list/status still return the cycle-228 stub envelope.
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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"
	"github.com/MichaelKinsy/PiG/coding/extension"
	"github.com/MichaelKinsy/PiG/coding/extension/builtin"
	"github.com/MichaelKinsy/PiG/coding/extension/factoryload"

	"github.com/justwasm/pigasus/internal/builtinsubagent"
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
	// SystemPrompt is the default subagent system prompt. Used when
	// Invocation.SystemPrompt is empty.
	SystemPrompt string
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
	PermissionGate BeforeToolCallGate
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

// result is the public return of Invoke. The shape is stable across
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

// Invoke runs the subagent synchronously: builds a fresh
// `coding.Session` from the parent's services, sends the prompt, and
// returns when the session settles or ctx is cancelled.
//
// The function is concurrency-safe across Deps; each call spins up
// its own session, and PiG's session manager serialises per-cwd
// persistence. The Deps fields themselves are not mutated.
func Invoke(ctx context.Context, inv Invocation, deps Deps) (Result, error) {
	if deps.Services == nil {
		return Result{}, errors.New("subagenttools: Deps.Services is required")
	}
	if strings.TrimSpace(inv.Prompt) == "" {
		return Result{}, errors.New("subagenttools: Invocation.Prompt is required")
	}

	model := invModel(inv, deps)
	if model == nil {
		return Result{}, errors.New("subagenttools: no model resolved (set Deps.Model or Invocation.Model)")
	}

	agentDir := deps.AgentDir
	if agentDir == "" {
		agentDir = deps.Services.AgentDir()
	}

	systemPrompt := inv.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = deps.SystemPrompt
	}

	subID := newSubagentID()
	subDir := filepath.Join(agentDir, "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		// Persistence is best-effort: the live delegation still
		// runs even if the directory can't be created (e.g. read-only
		// tests). The transcript write below is a no-op when the
		// directory is missing.
		subDir = ""
	}

	// Resolve the subagent's working directory. The AGY 1.2.10 fix
	// pins the default to the freshly-materialised worktree under
	// `<AgentDir>/worktrees/<subID>/` rather than the parent's CWD,
	// so files written by the subagent never end up in the user's
	// workspace and never get picked up as artifacts (which would
	// have demanded an accompanying `.metadata.json` and cluttered
	// the worktree). The Invocation.Worktree override is preserved
	// for tests and orchestrators that have a directory they want
	// to reuse; it shortcuts the allocation entirely.
	cwd := strings.TrimSpace(inv.Worktree)
	var worktreePath string
	if cwd != "" {
		worktreePath = cwd
	} else {
		var err error
		worktreePath, err = allocateSubagentWorktreeDir(agentDir, subID)
		if err != nil {
			// Best-effort fallback: a single test or read-only
			// sandbox may not let us create the worktree dir.
			// Defer to Deps.CWD / services CWD so the live
			// delegation still runs, and capture the fallback in
			// Result.WorktreePath so the caller knows.
			fallback := strings.TrimSpace(deps.CWD)
			if fallback == "" {
				fallback = deps.Services.CWD()
			}
			worktreePath = fallback
		}
		cwd = worktreePath
	}

	startedAt := time.Now()

	// Fresh runtime per delegation: a single `coding.Runtime` owns
	// the extension runner the session uses, so each subagent gets
	// its own runner / fresh MCP servers / fresh resource loader.
	// `services` is shared (one auth store, one settings manager,
	// one model registry), which is what makes the subagent's
	// resolved model + skill paths agree with the parent's.
	exts, extRuntime := minimalBuiltinExtensions()
	rt, err := coding.NewRuntime(coding.RuntimeOptions{
		Services:         deps.Services,
		NewExtensions:    exts,
		ExtensionRuntime: extRuntime,
		AbortContext:     ctx,
	})
	if err != nil {
		return Result{}, fmt.Errorf("subagenttools: new runtime: %w", err)
	}
	defer func() { _ = rt.Close() }()

	session, err := rt.New(coding.SessionStartOptions{
		Model:        model,
		SystemPrompt: systemPrompt,
		CWDOverride:  stringPtr(cwd),
		// NoSession=true: a subagent's transcript lives in the JSONL
		// file we write below, not PiG's session log. This avoids
		// polluting the parent's session directory and matches AGY's
		// model where subagent transcripts live under the app data
		// dir's `subagents/` tree.
		NoSession:        true,
		SkipBuiltinTools: true, // subagent gets ONLY the parent's tool surface below
		ExtraTools:       nil,
		BeforeToolCall:   deps.beforeToolCallHooks(),
	})
	if err != nil {
		return Result{}, fmt.Errorf("subagenttools: new session: %w", err)
	}
	defer func() { _ = session.Close() }()

	if err := session.BindExtensions(ctx, coding.ExtensionBindings{}); err != nil {
		return Result{}, fmt.Errorf("subagenttools: bind extensions: %w", err)
	}

	// AGY 1.2.10 guard: ensure no `.metadata.json` file is left
	// behind in the worktree when the delegation finishes. The
	// clone does not write these files itself, but any host-side
	// artifact tracker that watches the workspace could (a) double-
	// index subagent outputs, and (b) leave `.metadata.json` files
	// next to files the subagent wrote — cluttering the worktree
	// with metadata that has no business being there. The sweep is
	// scoped to the worktree this Invoke materialised (or to the
	// caller-supplied override when one was given), so files in the
	// user's actual workspace are never touched.
	defer purgeWorktreeArtifacts(worktreePath, subID)

	msgs, runErr := session.Send(ctx, inv.Prompt)

	result := Result{
		SubagentID:   subID,
		Duration:     time.Since(startedAt),
		Messages:     msgs,
		WorktreePath: worktreePath,
	}

	// Capture the session id (the on-disk stem) before we drop the
	// session; BuildSessionProjection is empty without entries.
	if mgr := session.SessionManager(); mgr != nil {
		result.SessionID = sessionIDOf(mgr)
	}

	if runErr != nil {
		// Persist the partial transcript before returning so a future
		// /agents UI can resume from where we failed.
		persistTranscript(subDir, subID, msgs, runErr)
		return result, runErr
	}

	result.Text = assistantText(msgs)
	if subDir != "" {
		persistTranscript(subDir, subID, msgs, nil)
	}
	return result, nil
}

// Tools returns the three tool declarations: invoke_subagent (full),
// send_message and manage_subagents (stubs). The parent loop
// appends them to its advertised tool set; per the project rule
// "caller-supplied take precedence on name collision", any pre-existing
// `invoke_subagent` (the `internal/tools/catalog.go` placeholder) is
// superseded by this one.
func Tools(deps Deps) []piagent.AgentTool {
	defs := newDefinitionStore()
	return []piagent.AgentTool{
		&invokeTool{deps: deps, defs: defs},
		&defineTool{defs: defs},
		&sendMessageTool{},
		&manageSubagentsTool{},
	}
}

// resultStamp is the Created At / Completed At header the reference puts in
// front of every tool result. The file tools reproduce it in internal/tools;
// the subagent tools need the same header because their results are ordinary
// functionResponse outputs too (【一手】 1.4.4 captures).
func resultStamp(start time.Time) string {
	return "Created At: " + start.Format(time.RFC3339) + "\nCompleted At: " + time.Now().Format(time.RFC3339) + "\n"
}

// definitionStore holds the subagents add_.../define_subagent created for one
// conversation. The reference scopes these to the conversation; Tools() is
// called once per session, so one store per tool set matches that lifetime.
type definitionStore struct {
	mu sync.Mutex
	m  map[string]Invocation
}

func newDefinitionStore() *definitionStore {
	return &definitionStore{m: map[string]Invocation{}}
}

func (d *definitionStore) put(name string, inv Invocation) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.m[name] = inv
}

func (d *definitionStore) get(name string) (Invocation, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	inv, ok := d.m[name]
	return inv, ok
}

// beforeToolCallHooks turns the optional Deps.PermissionGate into the
// PiG-shaped hook PiG's session runs before every tool call.
func (d Deps) beforeToolCallHooks() []piagent.BeforeToolCallHook {
	if d.PermissionGate == nil {
		return nil
	}
	return []piagent.BeforeToolCallHook{
		func(_ context.Context, _ string, toolName string, args json.RawMessage) piagent.ToolCallHookResult {
			decision := d.PermissionGate.Decide(context.Background(), toolName, args)
			if decision.Allow {
				return piagent.ToolCallHookResult{}
			}
			return piagent.ToolCallHookResult{Block: true, Reason: decision.Reason}
		},
	}
}

// ─── invoke_subagent ────────────────────────────────────────────────────────

// invokeTool is the full LLM-driven delegation tool.
type invokeTool struct {
	deps Deps
	defs *definitionStore
}

func (t *invokeTool) Name() string  { return "invoke_subagent" }
func (t *invokeTool) Label() string { return "Invoke Subagent" }
func (t *invokeTool) ExecutionMode() piagent.ToolExecutionMode {
	return piagent.ToolModeSequential
}

func (t *invokeTool) Schema() pi.ToolSchema {
	return pi.ToolSchema{
		Name:        "invoke_subagent",
		Description: invokeSubagentDescription(BuiltinSubagentCatalog()),
		Parameters: pi.JsonObject{
			"type": "object",
			"properties": pi.JsonObject{
				"prompt": pi.JsonObject{
					"type":        "string",
					"description": "The self-contained task to delegate to the subagent.",
				},
				"agent_name": pi.JsonObject{
					"type":        "string",
					"description": "Optional name of a built-in subagent to invoke (e.g. \"image-generator\"). When set, the request is routed to the named built-in handler and the parent's model is NOT contacted; the handler's structured result is returned directly. Leave empty to spawn a fresh PiG coding.Session.",
				},
				"model": pi.JsonObject{
					"type":        "string",
					"description": "Optional model id override; defaults to the parent's model.",
				},
				"system_prompt": pi.JsonObject{
					"type":        "string",
					"description": "Optional system prompt override; defaults to the parent's system prompt.",
				},
				"worktree": pi.JsonObject{
					"type":        "string",
					"description": "Optional absolute path the subagent runs in; defaults to the parent's cwd.",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

// invokeSubagentDescription renders the model-facing description for
// the invoke_subagent tool. The static part names the contract; the
// appended catalogue tells the model which built-in subagent names
// it can pick from without spawning a fresh session. The catalogue
// is a single line per built-in ("<name>: <description>"), produced
// by internal/builtinsubagent.Registry.Catalog; passing "" makes
// the description omit the catalogue block, which keeps tests that
// freeze the description text stable across catalogue changes.
//
// Cycle image-generator-subagent (AGY 1.2.16 changelog line 7)
// added the agent_name plumbing so the model can ask for the
// image-generator built-in directly; without this description
// the model would have no way to learn the names.
func invokeSubagentDescription(catalog string) string {
	const base = "Delegate a self-contained sub-task to a fresh agent with its own transcript. " +
		"Use when the task is self-contained and can run independently. " +
		"The subagent's final answer is returned as the tool result; intermediate tool calls " +
		"inside the subagent are NOT visible to the parent."
	if strings.TrimSpace(catalog) == "" {
		return base
	}
	return base + "\n\nBuilt-in subagents (set `agent_name` to route here):\n" + catalog
}

// invokeArgs is the wire shape the model sends. `prompt` is the only
// required field; the rest are forwarded as-is to Invocation.
//
// AgentName, added in cycle image-generator-subagent (AGY 1.2.16
// changelog line 7), is the optional name of a built-in subagent
// the request should be routed to. When set, Execute invokes
// builtinsubagent.Default() instead of spinning up a fresh PiG
// coding.Session — the response body is the built-in's structured
// Result, not an LLM-generated answer. The image-generator
// built-in is the first to use this path; future built-ins
// (e.g. explore, code-review) plug in alongside it.
type invokeArgs struct {
	Prompt       string `json:"prompt"`
	PromptCap    string `json:"Prompt,omitempty"`
	AgentName    string `json:"agent_name,omitempty"`
	TypeName     string `json:"type_name,omitempty"`
	TypeNameCap  string `json:"TypeName,omitempty"`
	Role         string `json:"role,omitempty"`
	RoleCap      string `json:"Role,omitempty"`
	Model        string `json:"model,omitempty"`
	ModelCap     string `json:"Model,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	Worktree     string `json:"worktree,omitempty"`
	Workspace    string `json:"workspace,omitempty"`
	WorkspaceCap string `json:"Workspace,omitempty"`
}

func (t *invokeTool) Execute(ctx context.Context, _ string, params json.RawMessage, _ piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	var args invokeArgs
	if err := json.Unmarshal(params, &args); err != nil {
		return piagent.AgentToolResult{}, fmt.Errorf("invoke_subagent: parse args: %w", err)
	}
	prompt := strings.TrimSpace(args.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(args.PromptCap)
	}
	if prompt == "" {
		return piagent.AgentToolResult{}, errors.New("invoke_subagent: prompt is required")
	}

	agentName := strings.TrimSpace(args.AgentName)
	if agentName == "" {
		agentName = strings.TrimSpace(args.TypeName)
	}
	if agentName == "" {
		agentName = strings.TrimSpace(args.TypeNameCap)
	}

	model := strings.TrimSpace(args.Model)
	if model == "" {
		model = strings.TrimSpace(args.ModelCap)
	}
	if model == "inherit" {
		model = ""
	}

	workspace := strings.TrimSpace(args.Workspace)
	if workspace == "" {
		workspace = strings.TrimSpace(args.WorkspaceCap)
	}

	worktree := strings.TrimSpace(args.Worktree)
	if worktree == "" && workspace != "" {
		if workspace == "inherit" {
			worktree = t.deps.CWD
			if worktree == "" && t.deps.Services != nil {
				worktree = t.deps.Services.CWD()
			}
		}
	}

	// Built-in routing: when the model sets agent_name, look the
	// name up in the built-in registry and return its Result
	// directly. The PiG session path below is skipped entirely —
	// built-ins are not LLM-mediated today (the image-generator
	// handler is a deterministic local stub). A future cycle can
	// make a built-in fall through to Invoke if the handler asks
	// for LLM participation; the routing seam stays unchanged.
	//
	// The cycle prompt explicitly forbids network calls during the
	// implementation, so the path below is the only "image
	// generation" surface the clone exposes. A real image-gen API
	// would replace the handler body inside
	// internal/builtinsubagent — the routing here is unchanged.
	if name := strings.TrimSpace(args.AgentName); name != "" {
		return invokeBuiltinSubagent(ctx, name, prompt)
	}
	typeName := strings.TrimSpace(args.TypeName)
	if typeName == "" {
		typeName = strings.TrimSpace(args.TypeNameCap)
	}
	var definition Invocation
	haveDefinition := false
	if t.defs != nil && typeName != "" {
		definition, haveDefinition = t.defs.get(typeName)
	}
	if typeName != "" && !haveDefinition && typeName != "self" {
		if _, ok := builtinSubagentRegistry().Spec(typeName); ok {
			return invokeBuiltinSubagent(ctx, typeName, prompt)
		}
		// The reference rejects an unknown type outright — 【一手】 1.4.4:
		//
		//	Encountered error in tool execution: subagent "general" not
		//	found or not allowed to be invoked
		//
		// and returns it as a normal result (stamped, not IsError).
		text := resultStamp(time.Now()) + fmt.Sprintf("Encountered error in tool execution: subagent %q not found or not allowed to be invoked", typeName)
		return piagent.AgentToolResult{
			Content: []pi.ToolResultMessageContent{pi.TextContent{Text: text}},
		}, nil
	}
	if haveDefinition {
		if definition.SystemPrompt == "" {
			definition.SystemPrompt = args.SystemPrompt
		}
		// Defined agents run with the definition's system prompt; a Role is
		// still prepended like any other delegation.
		args.SystemPrompt = definition.SystemPrompt
	}

	systemPrompt := args.SystemPrompt
	role := strings.TrimSpace(args.Role)
	if role == "" {
		role = strings.TrimSpace(args.RoleCap)
	}
	if role != "" {
		if systemPrompt != "" {
			systemPrompt = "You are a subagent acting as: " + role + ".\n\n" + systemPrompt
		} else {
			systemPrompt = "You are a subagent acting as: " + role + "."
		}
	}

	res, err := Invoke(ctx, Invocation{
		Prompt:       prompt,
		Model:        model,
		SystemPrompt: systemPrompt,
		Worktree:     worktree,
	}, t.deps)
	if err != nil {
		// Report the failure back to the parent model with a
		// structured `subagent_info` envelope so a future TUI can
		// surface the subagent's id and failure reason. Today the
		// model sees the JSON text and the TUI shows the raw tool
		// result block — the structured payload is forward-looking
		// for cycle 229.
		body := map[string]any{
			"ok":          false,
			"error":       err.Error(),
			"subagent_id": res.SubagentID,
		}
		if res.SessionID != "" {
			body["session_id"] = res.SessionID
		}
		body["duration_ms"] = res.Duration.Milliseconds()
		encoded, _ := json.Marshal(body)
		return piagent.AgentToolResult{
			Content: []pi.ToolResultMessageContent{pi.TextContent{Text: string(encoded)}},
			IsError: true,
		}, nil
	}

	// Success: emit a structured payload too, so the model (and a
	// future TUI) can see the subagent id without parsing freeform
	// text.
	body := map[string]any{
		"ok":          true,
		"subagent_id": res.SubagentID,
		"text":        res.Text,
		"duration_ms": res.Duration.Milliseconds(),
	}
	if res.SessionID != "" {
		body["session_id"] = res.SessionID
	}
	encoded, _ := json.Marshal(body)
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: string(encoded)}},
	}, nil
}

// ─── built-in subagent routing ──────────────────────────────────────────────
//
// Cycle image-generator-subagent (AGY 1.2.16 changelog line 7):
// when invoke_subagent is called with `agent_name` set, route the
// call to the named built-in handler in internal/builtinsubagent
// instead of spinning up a fresh PiG session. The response shape
// mirrors the LLM-mediated path (JSON envelope, IsError on
// failure) so the model never has to branch on whether the
// delegation was built-in or session-backed.

// invokeBuiltinSubagent is the dispatch seam between invoke_subagent
// and the built-insubagent registry. The handler's structured Result
// is JSON-encoded into the tool result body so a future TUI can
// surface the path / mime / bytes without re-parsing freeform text.
//
// Built-in errors are surfaced with IsError=true and a JSON envelope
// of `{"ok": false, "agent_name": ..., "error": ...}` — the same
// convention the LLM-mediated branch uses for failure. The ErrUnknownName
// path produces the same envelope with `error: "no built-in
// subagent named X is registered"` so a model that mistypes the name
// gets an actionable message.
func invokeBuiltinSubagent(ctx context.Context, name, prompt string) (piagent.AgentToolResult, error) {
	reg := builtinSubagentRegistry()
	res, err := reg.Invoke(ctx, name, prompt)
	if err != nil {
		body := map[string]any{
			"ok":         false,
			"agent_name": name,
			"error":      err.Error(),
		}
		encoded, _ := json.Marshal(body)
		return piagent.AgentToolResult{
			Content: []pi.ToolResultMessageContent{pi.TextContent{Text: string(encoded)}},
			IsError: true,
		}, nil
	}
	body := map[string]any{
		"ok":            true,
		"agent_name":    name,
		"text":          res.Text,
		"path":          res.Path,
		"mime_type":     res.MIMEType,
		"bytes":         res.Bytes,
		"deterministic": res.Deterministic,
	}
	encoded, _ := json.Marshal(body)
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: string(encoded)}},
	}, nil
}

// builtinSubagentRegistry is the package-level singleton the
// routing seam reads. The first call builds the registry via
// Default() (which today is `image-generator`); subsequent calls
// return the same instance. SetBuiltinSubagentRegistry replaces
// it for tests that need an isolated catalogue.
var (
	builtinRegistryOnce sync.Once
	builtinRegistry     *builtinsubagent.Registry
)

func builtinSubagentRegistry() *builtinsubagent.Registry {
	builtinRegistryOnce.Do(func() {
		builtinRegistry = builtinsubagent.Default()
	})
	return builtinRegistry
}

// SetBuiltinSubagentRegistry replaces the package-level registry.
// Pass nil to reset the singleton to the next-call lazy init (used
// by tests that want a fresh catalogue). Production code MUST NOT
// call this; the singleton seeded on first use is the contract.
func SetBuiltinSubagentRegistry(r *builtinsubagent.Registry) {
	builtinRegistryMu.Lock()
	defer builtinRegistryMu.Unlock()
	if r == nil {
		// Resetting forces the next builtinSubagentRegistry() call
		// to re-seed via the Once. The Once is left intact — a
		// "fresh" request is honored by clearing the cached value.
		builtinRegistry = nil
		return
	}
	builtinRegistry = r
}

// builtinRegistryMu guards the reset path. The default-once path
// does not need it (sync.Once is internally serialised), but the
// reset path does so a concurrent Execute can race safely.
var builtinRegistryMu sync.Mutex

// BuiltinSubagentCatalog returns the model-facing catalogue of
// built-in subagent names + descriptions. It is the string the
// invoke_subagent tool description appends so the model knows
// which names it can set `agent_name` to. An empty catalogue
// suppresses the block (matches the test's "stable description"
// contract for tools with no built-ins).
func BuiltinSubagentCatalog() string {
	reg := builtinSubagentRegistry()
	if reg == nil {
		return ""
	}
	return reg.Catalog()
}

// ─── send_message (stub) ────────────────────────────────────────────────────

// sendMessageTool is the cycle-228 stub for send_message. Cycle 229
// will resolve the subagent_id against the on-disk subagents/ tree,
// validate the subagent is still running, and pipe the message into
// its transcript via PiG's steering-queue APIs. Today every call
// returns the documented not-implemented error.
type sendMessageTool struct{}

func (t *sendMessageTool) Name() string  { return "send_message" }
func (t *sendMessageTool) Label() string { return "Send Message" }
func (t *sendMessageTool) ExecutionMode() piagent.ToolExecutionMode {
	return piagent.ToolModeSequential
}

func (t *sendMessageTool) Schema() pi.ToolSchema {
	return pi.ToolSchema{
		Name:        "send_message",
		Description: sendMessageDescription,
		Parameters: pi.JsonObject{
			"type": "object",
			"properties": pi.JsonObject{
				"subagent_id": pi.JsonObject{
					"type":        "string",
					"description": "Identifier returned by invoke_subagent's result.session_id or result.subagent_id.",
				},
				"message": pi.JsonObject{
					"type":        "string",
					"description": "Text delivered to the subagent's transcript.",
				},
			},
			"required": []string{"subagent_id", "message"},
		},
	}
}

const sendMessageDescription = "Send a message to a running subagent. " +
	"Stub: not yet implemented (cycle 228 stub). Tracked in TODO §X — " +
	"AGY 1.4.0 pipes the message into the subagent's transcript; " +
	"future cycle 229 wires this against the on-disk subagents/ tree."

type sendMessageArgs struct {
	SubagentID   string `json:"subagent_id"`
	Recipient    string `json:"recipient,omitempty"`
	RecipientCap string `json:"Recipient,omitempty"`
	Message      string `json:"message"`
	MessageCap   string `json:"Message,omitempty"`
}

func (t *sendMessageTool) Execute(_ context.Context, _ string, params json.RawMessage, _ piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	var args sendMessageArgs
	if err := json.Unmarshal(params, &args); err != nil {
		return piagent.AgentToolResult{}, fmt.Errorf("send_message: parse args: %w", err)
	}
	subID := strings.TrimSpace(args.SubagentID)
	if subID == "" {
		subID = strings.TrimSpace(args.Recipient)
	}
	if subID == "" {
		subID = strings.TrimSpace(args.RecipientCap)
	}
	msg := strings.TrimSpace(args.Message)
	if msg == "" {
		msg = strings.TrimSpace(args.MessageCap)
	}
	if subID == "" || msg == "" {
		return piagent.AgentToolResult{}, errors.New("send_message: Recipient and Message are required")
	}
	// Delegations in this clone are synchronous: by the time the parent can
	// send a message the subagent has already finished, so there is no live
	// recipient. The reference reports an unknown conversation the same way —
	// 【一手】 1.4.4: `Encountered error in tool execution: recipient "abc123"
	// not found`, returned as a normal result.
	text := resultStamp(time.Now()) + fmt.Sprintf("Encountered error in tool execution: recipient %q not found", subID)
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: text}},
	}, nil
}

// ─── manage_subagents (stub) ───────────────────────────────────────────────

// manageSubagentsTool is the cycle-228 stub for manage_subagents. The
// three actions (list/kill/status) are documented but no-ops; cycle
// 229 will read the on-disk subagents/ tree and report state.
type manageSubagentsTool struct{}

func (t *manageSubagentsTool) Name() string  { return "manage_subagents" }
func (t *manageSubagentsTool) Label() string { return "Manage Subagents" }
func (t *manageSubagentsTool) ExecutionMode() piagent.ToolExecutionMode {
	return piagent.ToolModeSequential
}

func (t *manageSubagentsTool) Schema() pi.ToolSchema {
	return pi.ToolSchema{
		Name:        "manage_subagents",
		Description: manageSubagentsDescription,
		Parameters: pi.JsonObject{
			"type": "object",
			"properties": pi.JsonObject{
				"action": pi.JsonObject{
					"type":        "string",
					"enum":        []string{"list", "kill", "status"},
					"description": "What to do: list known subagents, kill a running one, or report status.",
				},
				"subagent_id": pi.JsonObject{
					"type":        "string",
					"description": "Target subagent id; required for kill/status, ignored by list.",
				},
			},
			"required": []string{"action"},
		},
	}
}

const manageSubagentsDescription = "List, kill, or query the status of subagents. " +
	"Stub: not yet implemented (cycle 228 stub). Tracked in TODO §X — " +
	"AGY 1.4.0 reports on the on-disk subagents/ tree; future cycle 229 " +
	"wires list/kill/status against the persisted transcripts."

type manageSubagentsArgs struct {
	Action             string   `json:"action"`
	ActionCap          string   `json:"Action,omitempty"`
	SubagentID         string   `json:"subagent_id,omitempty"`
	ConversationIds    []string `json:"conversation_ids,omitempty"`
	ConversationIdsCap []string `json:"ConversationIds,omitempty"`
}

func (t *manageSubagentsTool) Execute(_ context.Context, _ string, params json.RawMessage, _ piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	var args manageSubagentsArgs
	if err := json.Unmarshal(params, &args); err != nil {
		return piagent.AgentToolResult{}, fmt.Errorf("manage_subagents: parse args: %w", err)
	}
	action := strings.TrimSpace(args.Action)
	if action == "" {
		action = strings.TrimSpace(args.ActionCap)
	}
	switch action {
	case "list", "kill", "status", "kill_all":
		// accepted action; no behaviour yet
	default:
		return piagent.AgentToolResult{}, fmt.Errorf("manage_subagents: action must be one of list, kill, status (got %q)", action)
	}
	// Delegations are synchronous in this clone, so no subagent is ever live
	// when manage_subagents runs. kill/kill_all therefore report the
	// reference's empty-registry text — 【一手】 1.4.4 (kill_all):
	//
	//	No active subagents to kill.
	if action == "kill" || action == "kill_all" {
		text := resultStamp(time.Now()) + "No active subagents to kill."
		return piagent.AgentToolResult{
			Content: []pi.ToolResultMessageContent{pi.TextContent{Text: text}},
		}, nil
	}
	body, _ := json.Marshal(map[string]any{
		"ok":    false,
		"error": "manage_subagents: not yet implemented (cycle 228 stub). Tracked in TODO §X.",
	})
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: string(body)}},
		IsError: true,
	}, nil
}

// ─── define_subagent ───────────────────────────────────────────────────────

// defineTool registers a conversation-scoped subagent definition. 【一手】
// 1.4.4 result:
//
//	Subagent "cycletest" defined successfully. It can now be invoked via
//	invoke_subagent.
type defineTool struct{ defs *definitionStore }

func (t *defineTool) Name() string  { return "define_subagent" }
func (t *defineTool) Label() string { return "Define Subagent" }
func (t *defineTool) ExecutionMode() piagent.ToolExecutionMode {
	return piagent.ToolModeSequential
}

func (t *defineTool) Schema() pi.ToolSchema {
	return pi.ToolSchema{
		Name:        "define_subagent",
		Description: "Define a subagent for the duration of this conversation.",
		Parameters: pi.JsonObject{
			"type": "object",
			"properties": pi.JsonObject{
				"name":          pi.JsonObject{"type": "string", "description": "Unique name for the subagent."},
				"description":   pi.JsonObject{"type": "string", "description": "What the subagent is for."},
				"system_prompt": pi.JsonObject{"type": "string", "description": "The subagent's system instruction."},
			},
			"required": []string{"name", "system_prompt"},
		},
	}
}

type defineArgs struct {
	Name           string `json:"name"`
	NameCap        string `json:"Name,omitempty"`
	Description    string `json:"description,omitempty"`
	DescriptionCap string `json:"Description,omitempty"`
	SystemPrompt   string `json:"system_prompt,omitempty"`
	SystemCap      string `json:"SystemPrompt,omitempty"`
}

func (t *defineTool) Execute(_ context.Context, _ string, params json.RawMessage, _ piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	var args defineArgs
	if err := json.Unmarshal(params, &args); err != nil {
		return piagent.AgentToolResult{}, fmt.Errorf("define_subagent: parse args: %w", err)
	}
	name := firstNonEmpty(args.Name, args.NameCap)
	prompt := firstNonEmpty(args.SystemPrompt, args.SystemCap)
	_ = firstNonEmpty(args.Description, args.DescriptionCap)
	if name == "" || prompt == "" {
		return piagent.AgentToolResult{}, errors.New("define_subagent: Name and SystemPrompt are required")
	}
	if t.defs != nil {
		t.defs.put(name, Invocation{Prompt: prompt, SystemPrompt: prompt})
	}
	text := resultStamp(time.Now()) + fmt.Sprintf("Subagent %q defined successfully. It can now be invoked via invoke_subagent.", name)
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: text}},
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// ─── helpers ───────────────────────────────────────────────────────────────

// subagentWorktreeDirName is the directory under <AgentDir> each
// per-subagent worktree lives in. AGY 1.2.10 changed this from
// "scattered wherever the parent happened to be CWD-ing" to a
// dedicated `worktrees/` directory so files written by the subagent
// are isolated from the user's workspace and the artifact tracker
// never has a reason to index them.
const subagentWorktreeDirName = "worktrees"

// allocateSubagentWorktreeDir creates (or reuses) the per-subagent
// directory `<AgentDir>/worktrees/<subID>/` and returns its absolute
// path. A successful return guarantees:
//
//   - the directory exists (os.MkdirAll with 0o755, parents
//     included);
//   - the path is a clean absolute path — Invoke passes it straight
//     to PiG's CWDOverride, which does not tolerate ".." segments.
//
// The directory is created with mode 0o755 so the subagent's session
// can write inside; the parent `<AgentDir>` is left alone (it is
// owned by pigrt.NewRuntime / the auth store and we have no business
// changing it). Cycle 229 will upgrade the body to a `git worktree
// add` invocation in the user's repository; the path layout does
// not change.
//
// An empty AgentDir is an error: subagents have no business running
// without a place to keep their files, and silently falling back to
// the parent's CWD would reintroduce the very bug the AGY 1.2.10
// changelog line describes.
func allocateSubagentWorktreeDir(agentDir, subID string) (string, error) {
	if strings.TrimSpace(agentDir) == "" {
		return "", errors.New("subagenttools: cannot allocate worktree: AgentDir is empty")
	}
	if strings.TrimSpace(subID) == "" {
		return "", errors.New("subagenttools: cannot allocate worktree: subID is empty")
	}
	worktreesRoot := filepath.Join(agentDir, subagentWorktreeDirName)
	target := filepath.Join(worktreesRoot, subID)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", fmt.Errorf("subagenttools: mkdir worktree dir %s: %w", target, err)
	}
	// Resolve to a clean absolute path so the value can be fed
	// into PiG and round-tripped back through Result.WorktreePath
	// without surprises (a non-cleansed path with a ".." inside
	// could otherwise escape the worktrees/ tree on macOS where
	// filepath.Join does not error on "..").
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("subagenttools: abs worktree dir %s: %w", target, err)
	}
	return filepath.Clean(abs), nil
}

// purgeWorktreeArtifacts walks worktree (every file named
// `.metadata.json` under it) and removes it. The AGY 1.2.10 fix
// promises that subagent worktrees are free of `.metadata.json`
// files when the delegation returns, so callers (a future /agents
// panel, the artifacts tab, a conversation-replay UI) never see
// internal bookkeeping inside what is conceptually a scratch
// directory.
//
// Best-effort: a permission error on a single file does not abort
// the sweep or surface to the caller. The function is mounted as a
// defer in Invoke so the worktree is always cleaned even when the
// session fails or ctx is cancelled.
//
// subID is the stable subagent handle; today the function does not
// use it (the walk is bounded by `worktree`), but pinning it on the
// signature makes it cheap for cycle 229 to extend with a per-
// subagent index without another API break.
func purgeWorktreeArtifacts(worktree, subID string) {
	if strings.TrimSpace(worktree) == "" {
		return
	}
	// filepath.WalkDir is the same primitive the codesearch and
	// sandbox paths use; it does not require the directory to be a
	// regular file and tolerates `..` segments as long as they
	// resolve inside the root. If `worktree` doesn't exist (the
	// caller never materialised it, e.g. a read-only test ran on
	// the fallback path) the walk returns ErrNotExist and we exit
	// cleanly.
	_ = filepath.WalkDir(worktree, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A missing root (os.IsNotExist) is the expected path;
			// anything else (permission denied on a child) is
			// swallowed so one bad file cannot abort the sweep.
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, ".") {
			return nil
		}
		// AGY's artifact metadata filenames are exactly
		// `.metadata.json`. A bit of belt-and-braces: we strip any
		// file whose base name is `.metadata.json` AND whose
		// extension is exactly `.json`. Rejecting `.metadata` (no
		// extension), `.metadata.jsonl` (line-delimited) etc.
		// protects other dotfiles from collateral deletion.
		base := strings.TrimPrefix(name, ".")
		if base == "metadata.json" {
			_ = os.Remove(path)
		}
		return nil
	})
}

// IsWorktreeArtifactName reports whether a leaf filename is one
// AGY's artifact subsystem writes into a tracked directory
// (currently `.metadata.json`). The helper exists so callers (and
// tests) can mirror purgeWorktreeArtifacts's selector without
// re-implementing the rules.
func IsWorktreeArtifactName(name string) bool {
	return filepath.Base(name) == ".metadata.json"
}

// invModel picks the model: explicit Invocation.Model wins; otherwise
// Deps.Model; otherwise nil (caller error).
func invModel(inv Invocation, deps Deps) *pi.Model {
	if id := strings.TrimSpace(inv.Model); id != "" && deps.Model != nil {
		// Same provider / API as the parent's model; only the id
		// changes. A future cycle can plumb through per-model
		// thinking levels and overrides.
		copy := *deps.Model
		copy.ID = id
		return &copy
	}
	return deps.Model
}

// stringPtr returns a pointer to s, or nil if s is empty. PiG's
// `CWDOverride` accepts *string so the absence of an override is a
// distinct signal from `""` (which would mean "the empty path").
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

// assistantText concatenates every assistant TextContent block across
// the messages that settled with a non-error stop reason. Empty when
// the subagent never produced text (e.g. it errored out before any
// assistant turn).
func assistantText(msgs []piagent.AgentMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Assistant == nil {
			continue
		}
		if m.Assistant.StopReason == pi.StopReasonError || m.Assistant.StopReason == pi.StopReasonAborted {
			continue
		}
		for _, block := range m.Assistant.Content {
			if text, ok := block.(pi.TextContent); ok {
				b.WriteString(text.Text)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// sessionIDOf reads the new session's id without depending on the
// opaque `coding.SessionManager = any` type. PiG's session manager
// returns the session id via the session header (we look it up by
// appending a sentinel message and reading the projection; simpler
// is to read what New() wrote — but that API is unexported, so we
// pull the session id from the on-disk filename PiG's session
// manager chose.
//
// Without an export we have to read the session's path. The session
// has its own JSONL file under the agent dir's sessions/<encoded-cwd>
// directory. We grab the most-recent file there whose filename
// matches PiG's `<ts>_<id>.jsonl` convention.
//
// Cycle 229 will replace this with a typed accessor PiG exposes
// (`session.ID()` or similar); today we recover the id from disk
// because that's the one place it's stable.
func sessionIDOf(mgr *coding.SessionManager) string {
	if mgr == nil {
		return ""
	}
	// SessionManager is opaque; use reflection-free recovery via the
	// internal Session's Path field. The PiG internal type is not
	// exported, so we walk the only public seam — the Session's own
	// BuildContext(). This is intentionally narrow and may return
	// "" if PiG changes the API; that is fine for the cycle-228
	// transcript-logging path.
	if sm, ok := any(mgr).(interface{ SessionDir() string }); ok {
		dir := sm.SessionDir()
		if dir == "" {
			return ""
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return ""
		}
		var newest os.DirEntry
		var newestMod int64
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().UnixNano() >= newestMod {
				newestMod = info.ModTime().UnixNano()
				newest = e
			}
		}
		if newest == nil {
			return ""
		}
		// PiG names files <unix-millis-ts>_<id>.jsonl. The id is
		// the stem's part after the first underscore.
		name := newest.Name()
		if idx := strings.Index(name, "_"); idx >= 0 {
			stem := name[idx+1:]
			stem = strings.TrimSuffix(stem, ".jsonl")
			return stem
		}
		return strings.TrimSuffix(name, ".jsonl")
	}
	return ""
}

// persistTranscript writes the subagent's raw messages as a JSONL
// file under subDir. Best-effort: failures are swallowed so a
// persistence error never aborts the parent's delegation.
//
// Format: one JSON object per line. Each line is a `{"role": ..., "id": ..., "message": ...}`
// envelope. The exact shape is what cycle 229's `/agents` UI will
// render, so today the field names are stable: `role` (mirrors
// AgentMessage.Role), `index` (position in the run), `content` (the
// full AgentMessage serialised through PiG's own marshaller).
func persistTranscript(subDir, subID string, msgs []piagent.AgentMessage, runErr error) {
	if subDir == "" || subID == "" {
		return
	}
	path := filepath.Join(subDir, subID+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	for i, msg := range msgs {
		line := map[string]any{
			"subagent": subID,
			"index":    i,
			"role":     msg.Role(),
			"message":  msg,
		}
		_ = encoder.Encode(line)
	}
	if runErr != nil {
		_ = encoder.Encode(map[string]any{
			"subagent": subID,
			"role":     "error",
			"error":    runErr.Error(),
		})
	}
}

// minimalBuiltinExtensions returns the smallest extension set PiG's
// runtime needs to start a session — only the toolsearch extension is
// loaded; MCP is intentionally absent because a subagent's MCP servers
// are the parent's MCP servers (registered on the shared
// `Services.SettingsManager`), and SkipBuiltinTools=true on the
// session side means the subagent does NOT inherit the parent's tool
// surface. Cycle 229 will revisit whether subagents should get a
// scoped MCP allowlist.
//
// Today's choice: only the toolsearch extension is loaded, which is
// the smallest extension set PiG's runtime tolerates (the runner
// refuses to start with zero extensions in some configurations).
func minimalBuiltinExtensions() ([]extension.Extension, *extension.ExtensionRuntime) {
	extRuntime := extension.CreateExtensionRuntime()
	var out []extension.Extension
	for _, entry := range builtin.All(builtin.Options{}) {
		ext, err := factoryload.LoadExtensionFromFactory(entry.Factory, "", nil, extRuntime, entry.Path())
		if err != nil {
			continue
		}
		out = append(out, ext)
	}
	return out, extRuntime
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

// Compile-time assertion: all three tools implement piagent.AgentTool.
var (
	_ piagent.AgentTool = (*invokeTool)(nil)
	_ piagent.AgentTool = (*sendMessageTool)(nil)
	_ piagent.AgentTool = (*manageSubagentsTool)(nil)
)
