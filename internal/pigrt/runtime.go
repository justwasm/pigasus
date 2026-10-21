// Package pigrt runs the clone's turns on PiG's coding runtime.
//
// Cycle 209 starts the migration AGENTS.md calls "PiG as the default agent
// runtime, jetski sunset gradually". Until now the clone owned its turn
// engine: internal/agent wrapped PiG's agent.Agent and internal/tools supplied
// a hand-recovered tool catalogue. PiG's coding.Runtime is a superset — it owns
// tools, the extension runner, the built-in MCP extension (a real MCP client
// with stdio/SSE/streamable-HTTP transports) and the resource loader that
// discovers skills. Delegating to it is how the clone gets MCP and skills
// without reimplementing either.
//
// What this package deliberately keeps from the clone:
//
//   - the transport. Model is a *ai.Model whose Provider is the clone's
//     piagentbridge.Provider (the agw OpenAI-SSE client), so the gateway, the
//     fingerprint and the *backend.HTTPError classification are unchanged;
//   - the config. AgentDir is the agy config home
//     (~/.gemini/antigravity-cli), and the MCP extension is pointed at the
//     agy mcp.json through LoadAgyMCP, not PiG's own file;
//   - the tool surface, when the caller passes ExtraTools.
//
// PiG's hot path never sees a backend.StreamChunk: the session emits
// agent.AgentEvent and ai.AssistantMessageEvent. Callers that need the clone's
// chunk vocabulary (the print and TUI emitters) translate at their own seam;
// see docs/cycles/cycle-209/.
package pigrt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"
	"github.com/MichaelKinsy/PiG/coding/extension"
	"github.com/MichaelKinsy/PiG/coding/extension/builtin"
	"github.com/MichaelKinsy/PiG/coding/extension/factoryload"
	"github.com/MichaelKinsy/PiG/coding/mcpext"

	"github.com/justwasm/pigasus/internal/agyprompt"
	"github.com/justwasm/pigasus/internal/retry"
	"github.com/justwasm/pigasus/internal/retrybudget"
)

// CompactionRecord summarises a single auto-compaction event the
// session reported via PiG's agent.CompactionStartEvent / agent.CompactionEndEvent.
//
// Cycle 227: PiG's coding.Session emits these events through its
// subscriber channel; the runtime keeps the most recent values so the
// print path can echo them on stdout and the TUI can show the spinner.
// StartedAt / EndedAt may be zero if the corresponding event was not
// observed before Run returned (compact aborted mid-flight, no
// CompactionEndEvent followed, etc.).
type CompactionRecord struct {
	// StartedAt is when the CompactionStartEvent fired (UTC).
	StartedAt time.Time
	// EndedAt is when the CompactionEndEvent fired (UTC). Zero while
	// compact is still running or aborted with no end event.
	EndedAt time.Time
	// Reason is the upstream Reason — "manual", "threshold", or "overflow".
	// The Start event carries the trigger; the End event carries the
	// outcome (often the same string).
	Reason string
	// TokensBefore is the estimated token count before compacting
	// (carried on the End event).
	TokensBefore int
	// EstimatedTokensAfter is the projected post-compact size (carried
	// on the End event). 0 when the upstream did not report it.
	EstimatedTokensAfter int
	// Aborted is true when the upstream end event flags a user /
	// overflow abort that was not followed by a retry.
	Aborted bool
}

// HasCompleted reports whether a CompactionEndEvent has been observed
// for this record.
func (r *CompactionRecord) HasCompleted() bool { return !r.EndedAt.IsZero() }

// Duration is the elapsed wall-clock duration of the most recently
// observed compact. Zero until EndedAt is set.
func (r *CompactionRecord) Duration() time.Duration {
	if r == nil || r.EndedAt.IsZero() || r.StartedAt.IsZero() {
		return 0
	}
	return r.EndedAt.Sub(r.StartedAt)
}

// EventFunc observes every agent event a turn produces. It is the hook a UI
// uses to render tool activity; the print path only needs TextFunc.
type EventFunc func(agent.AgentEvent)

// Options configures one runtime. CWD and Model are required; AgentDir
// defaults to the agy config home.
type Options struct {
	// CWD is the working directory the session is anchored to.
	CWD string
	// AgentDir is the global config directory. Empty uses DefaultAgentHome()
	// — the reference's ~/.gemini/antigravity-cli.
	AgentDir string
	// Model is the LLM the session calls. REQUIRED. Build it from the clone's
	// provider with (*piagentbridge.Provider).Model(label) so the agw
	// transport stays in the loop.
	Model *ai.Model
	// SystemPrompt is the initial instruction block. Empty uses the
	// Antigravity CLI's system prompt (internal/agyprompt), which is what the
	// reference sends; set it explicitly to override that.
	SystemPrompt string
	// AgentBody is the Markdown body of a --agent custom agent. Non-empty
	// selects the custom-agent system prompt (agyprompt.BuildAgent) instead of
	// the default one when SystemPrompt is also empty.
	AgentBody string
	// CustomSubagents are the markdown agents discovered on this host. The
	// reference lists them in the default agent's <subagents> section; a
	// custom agent's prompt has no such section, so BuildAgent ignores them.
	CustomSubagents []agyprompt.Subagent
	// ConversationID identifies the conversation in the system prompt's
	// <user_information> section and in the artifact directory it names.
	// Empty mints a fresh UUID at New() time.
	ConversationID string
	// PigPrompt restores PiG's own coding prompt instead of the reference's.
	// It exists as an escape hatch for hosts that need PiG's tool-guidance
	// sections; the default is the reference behaviour.
	PigPrompt bool
	// ExtraTools are the clone's tools, appended to PiG's built-ins. Nil means
	// the session runs on PiG's built-in tools plus MCP.
	ExtraTools []agent.AgentTool
	// SkillPaths are directories PiG discovers skills from. Nil uses
	// AgySkillPaths(AgentDir); pass an explicit (possibly empty) slice to
	// override.
	SkillPaths []string
	// MCPConfigPath is the agy mcp.json to translate. Empty disables MCP
	// registration entirely.
	MCPConfigPath string
	// AlwaysLoadToolsByServer is the AGY 1.3.3 (line 42) dynamic
	// counterpart to the static per-server `alwaysLoad` slice in
	// mcp.json: tools listed here join the runtime's always-load
	// registration alongside the static hints, with dynamic
	// winning on a (server, tool) collision. Use this when the
	// (server, tool) pair was discovered out of band — a custom
	// MCP client, a `/mcp` panel toggle, a third-party plugin.
	//
	// The map is read once at New() time. Mutating it after New()
	// does not change the session's tool surface; rebuild the
	// session if the configuration has changed.
	//
	// The static always-load is read from the file at
	// MCPConfigPath independently, so this map may add entries a
	// server did not declare — those survive a merge as
	// Source="dynamic" and end up in AlwaysLoadedTools().
	AlwaysLoadToolsByServer map[string][]string
	// CreateTransport overrides how an MCP server's transport is built. Nil
	// uses PiG's default (stdio from command/args, streamable HTTP from url).
	// Tests use it to drive the extension over an in-process transport
	// instead of spawning a child process.
	CreateTransport mcpext.TransportFactory
	// NoContextFiles suppresses AGENTS.md/CLAUDE.md discovery.
	NoContextFiles bool
	// SkipBuiltinTools omits PiG's built-in coding tools (read, bash, edit,
	// write, …) while keeping ExtraTools, extension tools (MCP) and caller
	// hooks. The migration sets it so a session's tool surface is the
	// reference's own 60 names plus whatever MCP contributes — PiG's tools are
	// a later, deliberate step, not an accidental widening of the surface the
	// parity captures pin.
	SkipBuiltinTools bool
	// BeforeToolCall hooks run before each tool call and may block it. The
	// clone uses one to keep its recovered permission policy in front of every
	// call, including MCP tools PiG would otherwise run unattended.
	BeforeToolCall []agent.BeforeToolCallHook
	// ErrorSlot, when set, receives the original transport error of a failed
	// provider Stream so Run can report its type instead of PiG's flattened
	// message. See errors.go; combine it with RecordProvider.
	ErrorSlot *ErrorSlot
	// NoSession disables on-disk session persistence. Useful for one-shot
	// print runs that must not leave a conversation behind.
	NoSession bool
	// OpenURL and Notify surface the extension host's user-facing events (MCP
	// OAuth, load failures). Both may be nil.
	OpenURL func(url string)
	Notify  func(message, level string)
	// CompactEnabled, when non-nil, overrides PiG's default compaction
	// enable bit (true). Set to a *bool pointing at false to disable
	// auto-compaction entirely. When nil, the runtime uses PiG's default
	// (true) — PiG's coding/session.go:752 calls checkPromptCompaction
	// unconditionally on every Send, so "no override" still gives the
	// upstream behaviour.
	//
	// Cycle 227: AGY 1.4.0 always runs auto-compact driven by
	// cascade_config.checkpoint_config.max_token_limit / summaryCacheFillEffort,
	// and the clone's silent "default on" is the missing row in TODO §4.
	CompactEnabled *bool
	// CompactThreshold, when > 0, derives ReserveTokens as
	// `int((1 - threshold) * 200_000)`. A value of 0.5 yields 100 000
	// tokens of headroom; 0.92 (PiG's default) yields 16 000. The
	// settings-store value is rounded; the runtime only overrides
	// ReserveTokens, not KeepRecentTokens (it stays at PiG's 20 000).
	// 0 / negative / >= 1 leaves the runtime at the upstream default.
	CompactThreshold float64

	// Effort sets the agent's reasoning-effort (thinking) level for the
	// session. Empty keeps the model's default; "low"/"medium"/"high" and
	// any other ai.ThinkingLevel value PiG accepts ("xhigh", "max", "off",
	// "minimal") are forwarded through PiG's Session.SetThinkingLevel
	// after the MCP extensions bind, so the next Send uses the new level.
	//
	// Cycle effort-picker: the wagyu CLI accepts `--effort <level>` and a
	// `/effort <level>` slash command on top of the cycle-208 picker; the
	// picker takes effect through the same SetThinkingLevel path. PiG
	// clamps an over-range value against the active model's
	// ThinkingLevelMap, so an invalid level does not error — it silently
	// narrows to whatever the model supports.
	Effort string

	// AutoCont enables the cycle auto-continue-interrupt recovery. When
	// true, a stream-interrupt (StopReasonError / Aborted / Length) or
	// iteration-cap (agent.ErrMaxTurnsReached) is recovered by injecting
	// the verbatim AGY recovery copy and re-running via
	// Session.Agent().Continue(). The default zero value (false) keeps the
	// pre-cycle behaviour: a stream failure is surfaced as a typed error
	// the caller can match. The print path defaults this to true in
	// runPigPrintTurn so heads-up users see the same "auto-continuing"
	// tail the reference emits; tests and offline harnesses opt out.
	//
	// Cycle auto-continue-interrupt recovery depth is bounded by
	// AutoContMaxDepth (=3): three consecutive failures return
	// ErrAutoContMaxDepth and the caller is expected to surface a
	// user-input-required message. See auto_continue.go.
	AutoCont bool

	// RetryBudgetCap is the cumulative-wait cap on PiG's auto-retry
	// backoff per turn (agent.AutoRetryStartEvent). AGY 1.2.13's
	// changelog pins this at 30s: retries that fit inside the budget
	// run with the usual exponential backoff; the retry that would
	// overflow the budget is aborted via Session.AbortRetry() and the
	// underlying error surfaces to the user.
	//
	// Zero (the default) means retrybudget.DefaultPerTurnCap (=30s).
	// Negative disables the cap entirely — useful for tests that need
	// to drive the retry loop past the AGY limit without the observer
	// aborting the in-flight wait. PiG's own RetryPolicy.MaxDelayMs is
	// unchanged either way; this option is layered on top.
	//
	// Cycle retry-30s-cap-stays: see internal/retrybudget and
	// docs/cycles/retry-30s-cap-stays/.
	RetryBudgetCap time.Duration
}

// Runtime is one configured session. It is not safe for concurrent Run calls:
// a Session owns a single turn queue.
type Runtime struct {
	opts     Options
	services *coding.AgentSessionServices
	rt       *coding.Runtime
	session  *coding.Session
	loader   *coding.DefaultResourceLoader

	// alwaysLoad is the merged always-load hint list the runtime
	// parsed at New() time. The list is alphabetically sorted by
	// (server, tool) — see MergeAlwaysLoadHints — so a caller
	// diffing two runtimes sees a stable order. The list is owned
	// by the runtime and never mutated after New returns;
	// AlwaysLoadedTools / AlwaysLoadByServer return defensive
	// copies so a caller cannot hold a reference into it
	// alongside a future New on the same value (not that the
	// concurrency rules permit that anyway).
	alwaysLoad []AlwaysLoadHint
	// retryBudget enforces the per-turn auto-retry wait cap (cycle
	// retry-30s-cap-stays). It is nil when Options.RetryBudgetCap is
	// negative — the run path skips the budget observer entirely and
	// the session's retries run unbounded as in pre-cycle behaviour.
	retryBudget *retrybudget.Budget

	mu      sync.Mutex
	onText  func(string)
	onEvent EventFunc

	// compactMu guards the lastCompact record and the active boolean.
	// CompactionStart and CompactionEnd events arrive on the session's
	// event channel while Run is in flight, so the drain (which observes
	// them) and the caller (which reads lastCompact between RunWith
	// calls) race on the same memory. Read/write through CompactRecord
	// / SetCompactRecord so the race is benign.
	compactMu   sync.Mutex
	lastCompact CompactionRecord
	// compactActive is true while a CompactionStartEvent has been
	// observed without a matching CompactionEndEvent.
	compactActive bool

	drainDone chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// DefaultAgentHome is the reference's config directory:
// $ANTIGRAVITY_HOME, else $HOME/.gemini/antigravity-cli. It mirrors
// auth.AntigravityConfigHome without importing internal/auth.
func DefaultAgentHome() string {
	if h := os.Getenv("ANTIGRAVITY_HOME"); h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".gemini", "antigravity-cli")
	}
	return filepath.Join(os.TempDir(), ".gemini", "antigravity-cli")
}

// New builds the services, the extension runner and one session, and issues
// session_start so the MCP servers connect before the first prompt. The caller
// must Close the Runtime.
func New(ctx context.Context, opts Options) (*Runtime, error) {
	if opts.Model == nil {
		return nil, errors.New("pigrt: Options.Model is required")
	}
	if opts.CWD == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("pigrt: resolve cwd: %w", err)
		}
		opts.CWD = wd
	}
	if opts.AgentDir == "" {
		opts.AgentDir = DefaultAgentHome()
	}

	// Cycle 227: build the SettingsManager the session will consult on
	// every checkPromptCompaction call. PiG's coding/session.go:752
	// reads ReserveTokens via SettingsManager.GetCompactionReserveTokens;
	// without an override the runtime inherits the caller's settings.json
	// (often absent), which is the row TODO §4 calls out.
	//
	// We have to construct the *icodingagent.CompactionSettingsJSON
	// field via JSON unmarshal — PiG's CompactionSettingsJSON lives in
	// `internal/codingagent` and is not reachable from outside the
	// module, but the Settings struct has JSON tags that round-trip the
	// value cleanly. Decoded values carry through every SettingsManager
	// getter PiG exposes (GetCompactionEnabled, GetCompactionReserveTokens,
	// GetCompactionKeepRecentTokens, GetModelCompactionSettings).
	settingsManager := buildDefaultSettingsManager(opts)
	services, err := coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{
		CWD:             opts.CWD,
		AgentDir:        opts.AgentDir,
		SettingsManager: settingsManager,
	})
	if err != nil {
		return nil, fmt.Errorf("pigrt: services: %w", err)
	}

	exts, extRuntime, err := builtinExtensions(opts)
	if err != nil {
		services.Close()
		return nil, err
	}

	rt, err := coding.NewRuntime(coding.RuntimeOptions{
		Services:         services,
		NewExtensions:    exts,
		ExtensionRuntime: extRuntime,
		AbortContext:     ctx,
	})
	if err != nil {
		services.Close()
		return nil, fmt.Errorf("pigrt: runtime: %w", err)
	}

	skillPaths := opts.SkillPaths
	if skillPaths == nil {
		skillPaths = AgySkillPaths(opts.AgentDir, opts.CWD)
	}
	loader := coding.NewDefaultResourceLoader(coding.DefaultResourceLoaderOptions{
		CWD:      opts.CWD,
		AgentDir: opts.AgentDir,
		// SettingsManager is the one Services built, so project trust and the
		// settings-configured resource paths agree with the session.
		SettingsManager: services.SettingsManager(),
		// NoSkills keeps discovery to exactly the agy roots: without it the
		// loader would also read `.pig/skills` and add a second, empty
		// catalogue that the reference does not have.
		NoSkills:             true,
		AdditionalSkillPaths: skillPaths,
		NoContextFiles:       opts.NoContextFiles,
	})
	if err := loader.Reload(); err != nil {
		_ = rt.Close()
		services.Close()
		return nil, fmt.Errorf("pigrt: resource loader: %w", err)
	}

	// The reference sends the Antigravity system prompt on every request. An
	// empty SystemPrompt means "the reference's prompt", not "PiG's default":
	// the latter is a different product's preamble and a visible parity break.
	// The skill list comes from the loader so it matches the skills the model
	// can actually read.
	systemPrompt := opts.SystemPrompt
	if systemPrompt == "" && !opts.PigPrompt {
		conversationID := opts.ConversationID
		if conversationID == "" {
			conversationID = agyprompt.NewConversationID()
		}
		promptOpts := agyprompt.DefaultOptions(opts.CWD, opts.AgentDir, conversationID)
		promptOpts.Skills = agyprompt.DiscoverSkills(skillPaths)
		promptOpts.Subagents = append(promptOpts.Subagents, opts.CustomSubagents...)
		if opts.AgentBody != "" {
			systemPrompt = agyprompt.BuildAgent(opts.AgentBody, promptOpts)
		} else {
			systemPrompt = agyprompt.Build(promptOpts)
		}
		opts.ConversationID = conversationID
	}

	session, err := rt.New(coding.SessionStartOptions{
		Model:            opts.Model,
		SystemPrompt:     systemPrompt,
		ExtraTools:       opts.ExtraTools,
		ResourceLoader:   loader,
		NoSession:        opts.NoSession,
		SkipBuiltinTools: opts.SkipBuiltinTools,
		BeforeToolCall:   opts.BeforeToolCall,
	})
	if err != nil {
		_ = rt.Close()
		services.Close()
		return nil, fmt.Errorf("pigrt: session: %w", err)
	}

	r := &Runtime{
		opts:       opts,
		services:   services,
		rt:         rt,
		session:    session,
		loader:     loader,
		alwaysLoad: loadAlwaysLoadHints(opts),
		drainDone:  make(chan struct{}),
	}
	// Cycle retry-30s-cap-stays: cap the per-turn auto-retry wait.
	// Negative RetryBudgetCap skips the cap (preserves pre-cycle
	// behaviour); zero / positive sets the cap, with zero defaulting
	// to retrybudget.DefaultPerTurnCap (=30s).
	switch {
	case opts.RetryBudgetCap < 0:
		r.retryBudget = nil
	case opts.RetryBudgetCap == 0:
		r.retryBudget = retrybudget.New(retrybudget.DefaultPerTurnCap)
	default:
		r.retryBudget = retrybudget.New(opts.RetryBudgetCap)
	}
	// The session's event channel is buffered and sendEvent blocks: an unread
	// Events() stalls the forwarding goroutine once the buffer fills. The
	// drain owns the channel for the session's lifetime.
	go r.drain()

	// session_start is what connects the MCP servers and admits the tools they
	// register; NewSession only binds the extension core.
	if err := session.BindExtensions(ctx, coding.ExtensionBindings{}); err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("pigrt: bind extensions: %w", err)
	}
	// Apply the requester-supplied reasoning-effort level (cycle
	// effort-picker). PiG's SetThinkingLevel clamps to the active model's
	// ThinkingLevelMap, so a level the model does not support narrows to
	// the closest valid one instead of erroring — matching agy's
	// behaviour on the (model, effort) pair. An empty Effort leaves the
	// model's own default in place.
	if opts.Effort != "" {
		level := ai.ModelThinkingLevel(opts.Effort)
		_ = session.SetThinkingLevel(level)
	}
	return r, nil
}

// builtinExtensions assembles PiG's built-in extension set, with the MCP
// extension reading the agy mcp.json instead of its own.
//
// PiG 0.5.1 turned a built-in from a pre-populated extension.Extension into a
// factory that registers into an extension.API, so the door out is now
// factoryload.LoadExtensionFromFactory. The registration state the factories
// write to (MCP servers, providers, virtual models) is handed back to the
// caller: RuntimeOptions.ExtensionRuntime must receive the same value, or the
// session's runner builds its own and the MCP servers registered here never
// reach session_start.
func builtinExtensions(opts Options) ([]extension.Extension, *extension.ExtensionRuntime, error) {
	var o builtin.Options
	o.ConfigureMcp(opts.AgentDir, codingConfigDirName(), opts.OpenURL)
	// Override the loader: agy's file, agy's schema. Set after ConfigureMcp so
	// it wins; LoadAgyMCP closes over the path.
	path := opts.MCPConfigPath
	o.Mcp.LoadConfig = func(context.Context) mcpext.LoadedMcpConfig {
		return LoadAgyMCP(path)
	}
	if opts.CreateTransport != nil {
		o.Mcp.CreateTransport = opts.CreateTransport
	}
	if opts.Notify != nil {
		o.Mcp.OpenURL = opts.OpenURL
	}

	extRuntime := extension.CreateExtensionRuntime()
	var exts []extension.Extension
	for _, entry := range builtin.All(o) {
		if entry.Name == "mcp" && path == "" {
			// No config: skip the extension entirely rather than let it read
			// PiG's own mcp.json, which belongs to a different tool.
			continue
		}
		ext, err := factoryload.LoadExtensionFromFactory(entry.Factory, opts.CWD, nil, extRuntime, entry.Path())
		if err != nil {
			return nil, nil, fmt.Errorf("pigrt: builtin extension %q: %w", entry.Name, err)
		}
		exts = append(exts, ext)
	}
	return exts, extRuntime, nil
}

// codingConfigDirName is the per-project config directory name. The clone does
// not use PiG's project conventions, but the extension needs a non-empty value
// to build its (unused) project path.
func codingConfigDirName() string { return ".pig" }

// drain consumes the session's event channel and routes text deltas to the
// run's callback. It also records the most recent auto-compaction event
// on r.lastCompact (cycle 227: TODO §4 long-running task continuity row).
func (r *Runtime) drain() {
	defer close(r.drainDone)
	for ev := range r.session.Events() {
		if coding.AcknowledgeEvent(ev) {
			continue
		}
		// Cycle 227: CompactionStart/End events drive the runtime's
		// lastCompact record before they reach the caller's onEvent
		// hook. The hook still fires (it sees the same event) so the
		// caller's higher-level observer can render the spinner or
		// echo on stdout. We do the bookkeeping here because the
		// session emits the events regardless of whether the caller
		// attached an observer.
		r.recordCompactionEvent(ev)
		r.mu.Lock()
		onText, onEvent := r.onText, r.onEvent
		r.mu.Unlock()
		if onEvent != nil {
			onEvent(ev)
		}
		if onText == nil {
			continue
		}
		update, ok := ev.(agent.MessageUpdateEvent)
		if !ok {
			continue
		}
		if delta, ok := update.AssistantMessageEvent.(ai.TextDeltaEvent); ok && delta.Delta != "" {
			onText(delta.Delta)
		}
	}
}

// recordCompactionEvent stamps r.lastCompact from a PiG compaction
// event. Start overwrites the open record's StartedAt; End stamps
// EndedAt and the post-compaction token totals. End with no open
// start (a manual compact that fires its events before Run attaches a
// callback) is dropped — without the start the duration / spinner
// model is wrong, and the runtime cannot tie the end to a record.
func (r *Runtime) recordCompactionEvent(ev agent.AgentEvent) {
	switch event := ev.(type) {
	case agent.CompactionStartEvent:
		r.compactMu.Lock()
		r.lastCompact = CompactionRecord{
			StartedAt: time.Now().UTC(),
			Reason:    event.Reason,
		}
		r.compactActive = true
		r.compactMu.Unlock()
	case agent.CompactionEndEvent:
		r.compactMu.Lock()
		if r.compactActive {
			r.lastCompact.EndedAt = time.Now().UTC()
			r.lastCompact.Reason = event.Reason
			r.lastCompact.TokensBefore = event.TokensBefore
			r.lastCompact.EstimatedTokensAfter = event.EstimatedTokensAfter
			r.lastCompact.Aborted = event.Aborted
			r.compactActive = false
		}
		r.compactMu.Unlock()
	}
}

// LastCompact returns a snapshot of the most recently observed
// compaction record. The caller's copy is detached from the runtime
// state; subsequent events update the runtime's record but not the
// returned value.
func (r *Runtime) LastCompact() CompactionRecord {
	r.compactMu.Lock()
	defer r.compactMu.Unlock()
	return r.lastCompact
}

// IsCompacting reports whether a CompactionStartEvent has been
// observed without a matching CompactionEndEvent.
func (r *Runtime) IsCompacting() bool {
	r.compactMu.Lock()
	defer r.compactMu.Unlock()
	return r.compactActive
}

// InjectForTest feeds an agent.AgentEvent through the runtime's
// compaction bookkeeping without going through the session's event
// channel. It exists so cycle 227's tests can drive a deterministic
// start/end pair without depending on PiG's session-internal helpers
// that are not exported in v0.4.1 (the cycle-227 spec calls out
// Session.CheckPromptCompaction as observable, but the manual
// "publish" path is private).
//
// Production code MUST NOT call this — the agent.EventFunc onEvent
// hook and PiG's own event bus are the only paths a turn produces
// these events. The helper is documented as test-only and routed
// through the same recordCompactionEvent function the real drain
// uses, so it is functionally equivalent.
func (r *Runtime) InjectForTest(ev agent.AgentEvent) {
	r.recordCompactionEvent(ev)
}

// Run sends one prompt and returns the messages the turn produced. onText, when
// non-nil, receives the assistant's text deltas as they stream.
func (r *Runtime) Run(ctx context.Context, prompt string, onText func(string)) ([]agent.AgentMessage, error) {
	return r.RunWith(ctx, prompt, onText, nil)
}

// RunWith is Run with an event observer attached for the duration of the turn.
func (r *Runtime) RunWith(ctx context.Context, prompt string, onText func(string), onEvent EventFunc) ([]agent.AgentMessage, error) {
	// Cycle retry-30s-cap-stays: reset the per-turn retry budget at the
	// top of RunWith so each user turn starts with a fresh 30s window.
	// Reset is a no-op when RetryBudgetCap is disabled (retryBudget is
	// nil) — both TryConsume and Reset handle a nil receiver.
	if r.retryBudget != nil {
		r.retryBudget.Reset()
	}
	// Chain the budget observer after the caller's observer: the
	// caller's hook (e.g. printCompactionObserver) sees every event
	// first and renders it, then the budget observer decides whether
	// to abort the in-flight retry. Both fire on every event because
	// they switch on disjoint event types in practice, but chaining
	// keeps the contract "user's onEvent is the last thing to see an
	// event" intact — the budget observer's AbortRetry side-effect is
	// invisible to it.
	chained := onEvent
	if r.retryBudget != nil {
		chained = chainEventFuncs(onEvent, retrybudget.Observer(r.retryBudget, r))
	}
	r.mu.Lock()
	r.onText, r.onEvent = onText, chained
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.onText, r.onEvent = nil, nil
		r.mu.Unlock()
	}()
	msgs, err := r.session.Send(ctx, prompt)
	// Send returns once the run settles, but the drain consumes events on its
	// own goroutine. A text delta emitted just before the run ended can still be
	// in flight when Send returns, and clearing onText below would drop it —
	// which truncates a print turn's stdout. FlushEvents writes a barrier into
	// the same FIFO the drain reads and waits for it to acknowledge, so it
	// returns only after every event up to now has reached onText.
	if r.session != nil {
		_ = r.session.FlushEvents(ctx)
	}
	// A cancelled context is the caller's own failure and outranks whatever the
	// provider reported on the way out; internal/agent's sendPig orders it the
	// same way. --print-timeout depends on it: a deadline becomes a partial
	// output success, not an error.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return msgs, ctxErr
	}
	// Cycle auto-continue-interrupt: when the runtime was built with
	// Options.AutoCont, a stream-interrupt (StopReasonError/Aborted/Length)
	// or iteration-cap (agent.ErrMaxTurnsReached) is recovered by injecting
	// the verbatim AGY recovery copy and calling Agent.ContinueMessages(). The
	// function recurses up to AutoContMaxDepth; beyond that it returns
	// ErrAutoContMaxDepth. The d=0 seed here means the depth ceiling is
	// re-armed per user prompt. See auto_continue.go for the trigger matrix
	// and the recovery copy catalogue.
	if r.opts.AutoCont {
		recovered, rcErr := r.maybeAutoContinue(ctx, msgs, err, 0)
		if rcErr != nil {
			// a recovery attempt itself failed (depth ceiling hit, or the
			// continued turn streamed another error we couldn't classify).
			// Surface it below alongside the existing typed-error recovery.
			return recovered, rcErr
		}
		msgs, err = recovered, nil
	}
	// PiG folds a provider failure into an assistant message and returns a nil
	// Go error beside it (agent_loop.go:494), so Send can report success for a
	// turn that actually failed. Recover the failure from the messages, and
	// prefer the recorded original: *backend.HTTPError is what api_error.go
	// classifies for the AGY_ERROR envelope and exit code 3.
	if failure := turnFailure(msgs); failure != nil {
		if r.opts.ErrorSlot != nil {
			if typed := r.opts.ErrorSlot.Err(); typed != nil {
				return msgs, typed
			}
		}
		return msgs, failure
	}
	if err != nil && r.opts.ErrorSlot != nil {
		// PiG flattens the provider error to its message; report the recorded
		// original so the caller can still match on type (see errors.go).
		if typed := r.opts.ErrorSlot.Err(); typed != nil {
			return msgs, typed
		}
	}
	return msgs, err
}

// turnFailure reports the failure PiG encoded in the turn's messages: the last
// assistant message ending in error or abort. A clean stop on the last
// assistant message means the turn succeeded — a rate limit PiG retried in
// place leaves the earlier attempts out of the projection, and a recovered
// attempt must not be reported as a failure.
func turnFailure(msgs []agent.AgentMessage) error {
	for i := len(msgs) - 1; i >= 0; i-- {
		message := msgs[i].Assistant
		if message == nil {
			continue
		}
		switch message.StopReason {
		case ai.StopReasonError, ai.StopReasonAborted:
			text := message.ErrorMessage
			if text == "" {
				text = "agent: upstream stream failed"
			}
			return errors.New(text)
		default:
			return nil
		}
	}
	return nil
}

// Session exposes the underlying PiG session for callers that need more than
// Run (commands, steering, model swaps).
func (r *Runtime) Session() *coding.Session { return r.session }

// AbortRetry cancels the in-flight auto-retry backoff timer without
// cancelling the session itself. It is the Aborter half of the
// retrybudget.Observer contract: when the budget is about to overflow,
// the observer calls AbortRetry() so the upcoming attempt does not
// start and the original error surfaces as the turn's final error.
//
// It is safe to call before New has wired the session, on a nil
// receiver, or when no retry is in flight — each path is a no-op
// rather than a panic, so a half-wired retry budget cannot crash a
// print run.
//
// Cycle retry-30s-cap-stays: see internal/retrybudget/observer.go.
func (r *Runtime) AbortRetry() {
	if r == nil || r.session == nil {
		return
	}
	r.session.AbortRetry()
}

// RetryBudget returns the per-turn retry budget (or nil when the cap
// is disabled via Options.RetryBudgetCap < 0). Callers use it to
// render "X / 30s" progress markers or to assert on the cap from
// integration tests; the print path does not currently use it.
func (r *Runtime) RetryBudget() *retrybudget.Budget { return r.retryBudget }

// Services exposes the underlying PiG services container for callers that
// need to build things against the same registry / settings / agent dir the
// session uses. Cycle 228's subagenttools.Deps.Services expects this — the
// subagent's runtime is spun up from the parent's services so model registry,
// skill paths and auth.json agree.
func (r *Runtime) Services() *coding.AgentSessionServices { return r.services }

// SkillNames returns the names of the skills the resource loader discovered,
// sorted. It is what a caller reports (or diffs against the reference's skill
// list) — the prompt injection itself is PiG's job.
func (r *Runtime) SkillNames() []string {
	if r.loader == nil {
		return nil
	}
	result := r.loader.GetSkills()
	out := make([]string, 0, len(result.Skills))
	for _, s := range result.Skills {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}

// SkillDiagnostics returns the resource loader's per-path diagnostics, so a
// caller can see a skill that failed to parse instead of silently missing it.
func (r *Runtime) SkillDiagnostics() []extension.ResourceDiagnostic {
	if r.loader == nil {
		return nil
	}
	return r.loader.GetSkills().Diagnostics
}

// AgentDir is the resolved config directory.
func (r *Runtime) AgentDir() string { return r.opts.AgentDir }

// SystemPrompt returns the prompt the session sends. It is the reference's
// Antigravity prompt unless the caller supplied one or set PigPrompt.
func (r *Runtime) SystemPrompt() string {
	if r.session == nil {
		return ""
	}
	return r.session.SystemPrompt()
}

// ConversationID returns the id the system prompt's <user_information>
// section names, including one minted at New() time.
func (r *Runtime) ConversationID() string { return r.opts.ConversationID }

// AlwaysLoadedTools is the canonical `mcp__<server>__<tool>` name of
// every tool the runtime parsed out of the AGY 1.3.3 always-load
// config (per-server static `alwaysLoad` from mcp.json, plus any
// dynamic `_meta.antigravity/alwaysLoad` flags a caller registered
// through Options.AlwaysLoadToolsByServer). Names are sorted, so a
// caller diffing two runtimes sees a stable order. The returned
// slice is a copy — a caller can mutate it without affecting the
// runtime or future calls.
//
// `nil` is returned when no MCP config was supplied, the file was
// missing, no server carried an alwaysLoad hint, or the dynamic
// channel is empty. The empty-but-no-error case (a config file
// with zero servers) is the same nil — the runtime's MCP layer is
// the only place that tracks "registered but empty", and a nil
// return is the simplest "no work to do" signal for a caller.
func (r *Runtime) AlwaysLoadedTools() []string {
	if r == nil || len(r.alwaysLoad) == 0 {
		return nil
	}
	out := make([]string, len(r.alwaysLoad))
	for i, hint := range r.alwaysLoad {
		out[i] = MCPName(hint.Server, hint.Tool)
	}
	return out
}

// AlwaysLoadByServer groups the canonical tool names by their
// server prefix. The returned map is freshly allocated; mutating it
// does not affect the runtime. The map preserves a sorted slice
// per server so the caller's iteration order is stable.
func (r *Runtime) AlwaysLoadByServer() map[string][]string {
	if r == nil || len(r.alwaysLoad) == 0 {
		return nil
	}
	keys := make([]string, 0)
	group := make(map[string][]string)
	for _, hint := range r.alwaysLoad {
		if _, ok := group[hint.Server]; !ok {
			keys = append(keys, hint.Server)
		}
		group[hint.Server] = append(group[hint.Server], MCPName(hint.Server, hint.Tool))
	}
	sort.Strings(keys)
	out := make(map[string][]string, len(group))
	for _, server := range keys {
		names := group[server]
		sort.Strings(names)
		out[server] = names
	}
	return out
}

// AlwaysLoadHints returns the parsed hints (server, tool, source)
// the runtime knows about. This is the lower-level accessor: tests
// and diagnostics read it to assert which side (the static hint
// from mcp.json, or the dynamic `_meta` flag PiG would surface
// through Options) supplied a given tool.
func (r *Runtime) AlwaysLoadHints() []AlwaysLoadHint {
	if r == nil || len(r.alwaysLoad) == 0 {
		return nil
	}
	out := make([]AlwaysLoadHint, len(r.alwaysLoad))
	copy(out, r.alwaysLoad)
	return out
}

// loadAlwaysLoadHints reads the MCP config (if one is configured)
// once and merges the static always-load hints with whatever the
// caller supplied through Options.AlwaysLoadToolsByServer. The
// configuration file is read independently of LoadAgyMCP: PiG's
// shape-discarding bridge (mcpconfig.go) only carries transport
// fields, while the always-load reader needs the per-server
// `alwaysLoad` JSON slice. Two reads of the same file are fine on
// the small mcp.json inputs we expect; the cost is one syscall
// and 4 KB at most.
func loadAlwaysLoadHints(opts Options) []AlwaysLoadHint {
	var static []AlwaysLoadHint
	if opts.MCPConfigPath != "" {
		doc, err := LoadAgyMCPFile(opts.MCPConfigPath)
		if err == nil {
			static = AlwaysLoadHintsFromConfig(doc.Servers)
		}
		// A present-but-bad file surfaces in LoadAgyMCP.Errors on the
		// PiG side; we do not duplicate the diagnostic here because
		// the static hints are best-effort and a missing entry never
		// blocks session startup.
	}
	var dynamic []AlwaysLoadHint
	for server, tools := range opts.AlwaysLoadToolsByServer {
		for _, tool := range tools {
			dynamic = append(dynamic, AlwaysLoadHint{
				Server: server,
				Tool:   tool,
				Source: "dynamic",
			})
		}
	}
	return MergeAlwaysLoadHints(static, dynamic)
}

// Close tears the session, runtime and services down in order, then waits for
// the drain goroutine to observe the closed event channel.
func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		// Bound the wait: a session whose turn is mid-flight Close blocks on
		// its own teardown, and a stuck server must not hang process exit.
		done := make(chan struct{})
		go func() {
			defer close(done)
			if r.session != nil {
				_ = r.session.Close()
			}
			if r.rt != nil {
				_ = r.rt.Close()
			}
			if r.services != nil {
				r.services.Close()
			}
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			r.closeErr = errors.New("pigrt: close timed out")
		}
		<-r.drainDone
	})
	return r.closeErr
}

// UsageOf sums the token usage of the assistant messages a turn produced.
// ok is false when the turn carried no usage at all (the reference omits the
// usage block in that case).
func UsageOf(msgs []agent.AgentMessage) (ai.Usage, bool) {
	var total ai.Usage
	found := false
	for _, m := range msgs {
		if m.Assistant == nil || m.Assistant.Usage == nil {
			continue
		}
		found = true
		u := *m.Assistant.Usage
		total.Input += u.Input
		total.Output += u.Output
		total.CacheRead += u.CacheRead
		total.CacheWrite += u.CacheWrite
		total.TotalTokens += u.TotalTokens
	}
	return total, found
}

// compactContextWindow is the canonical 200 K context window AGY /
// Gemini-class models share. The threshold derivation in
// buildDefaultSettingsManager reduces this by the caller's
// percentage so a 0.50 threshold yields 100 000 tokens of headroom —
// mirroring the cycle-227 spec `int((1-threshold) * 200_000)`.
const compactContextWindow = 200_000

// buildDefaultSettingsManager assembles the SettingsManager the
// runtime hands to coding.NewServices. PiG's coding/session.go
// consults SettingsManager().GetCompactionEnabled() and
// GetCompactionReserveTokens() at every Send (checkPromptCompaction),
// so without this override the clone silently inherits whatever the
// caller's settings.json carries — usually nothing.
//
// The default enabled flag and token budget match PiG's documented
// defaults (internal/codingagent/compaction/compaction.go:46
// DefaultCompactionSettings). The caller can override via
// Options.CompactEnabled (a *bool to distinguish "unset" from "false"
// — AGENTS.md's hard-research constraint calls this out) and
// Options.CompactThreshold (a 0–1 ratio mapped onto the 200 K
// context window).
//
// Cycle retry-30s-cap added a `retry` section to the same payload.
// PiG's coding/assistant_retry.go reads
// SettingsManager.GetRetrySettings() at every Send and applies:
// MaxRetries (default 3), BaseDelayMs (default 2000), MaxDelayMs
// (default 60 000). With no override the agent loop would wait up to
// 60 s between retries — twice the AGY 1.3.4 cap. We pin MaxRetries,
// BaseDelayMs, and MaxAgentDelayMs to retry.MaxAttempts (5),
// retry.InitialIntervalMs (1000), retry.MaxIntervalMs (30 000) so
// the agent-loop cap matches the provider-retry cap set by init() in
// retry.go. The numbers come from one source (internal/retry), so the
// two surfaces cannot drift.
//
// When the caller passes no overrides, the manager is equivalent to
// the upstream default — every test that already passes (cycle 209
// through cycle 226) keeps its byte-for-byte behaviour.
//
// Construction goes through JSON because Settings.Compaction and
// Settings.Retry are *icodingagent.CompactionSettingsJSON /
// *icodingagent.RetrySettingsJSON respectively, and the icodingagent
// package is internal to PiG: it is not importable from this
// module's compilation graph. The Settings type IS exposed (as
// `coding.Settings`), and its JSON tags round-trip the values
// cleanly into the unexported concrete types via reflection.
// This indirection is load-bearing — without it the override is
// unreachable and the clone's TODO §4 row never closes.
func buildDefaultSettingsManager(opts Options) *coding.SettingsManager {
	enabled := true
	if opts.CompactEnabled != nil {
		enabled = *opts.CompactEnabled
	}

	reserve := compactionDefaultReserve
	if opts.CompactThreshold > 0 && opts.CompactThreshold < 1 {
		if derived := int((1 - opts.CompactThreshold) * float64(compactContextWindow)); derived > 0 {
			reserve = derived
		}
	}

	// json.Marshal keeps the literal identical to what Settings would
	// produce from a hand-typed literal in the upstream tests, so a
	// failing test can copy-paste this block back into the upstream
	// test for parity. Numbers go through strconv to avoid scientific
	// notation on 200000.00 — PiG's CompactionSettingsJSON validates
	// the string but a small JSON formatter prefers 2e+05 for the
	// reserve which JSON-decodes the same way; we still prefer the
	// explicit form. The retry section follows the same convention;
	// MaxIntervalMs (30 000) and InitialIntervalMs (1 000) keep the
	// JSON numerically aligned with retry.go's constants so a code
	// review can diff the payload against internal/retry in one eye.
	payload := fmt.Sprintf(
		`{"compaction":{"enabled":%s,"reserveTokens":%s,"keepRecentTokens":%s},`+
			`"retry":{"maxRetries":%d,"baseDelayMs":%d,"maxAgentDelayMs":%d}}`,
		strconv.FormatBool(enabled),
		strconv.FormatInt(int64(reserve), 10),
		strconv.Itoa(compactionDefaultKeepRecent),
		retry.MaxAttempts,
		retry.InitialIntervalMs,
		retry.MaxIntervalMs,
	)
	var settings coding.Settings
	if err := json.Unmarshal([]byte(payload), &settings); err != nil {
		// The payload is built from constants; reaching this branch is a
		// programmer error rather than user input. Fall back to the
		// upstream default Settings (Compaction/Retry nil → defaults apply),
		// which preserves the pre-cycle-227 behaviour (the pre-change
		// retry defaults are still 60 s / 3 retries from PiG, not the
		// clone's 30 s / 5 retries; that's the one regression we
		// accept when the JSON ever round-trips incorrectly).
		return coding.NewInMemorySettingsManager(coding.Settings{})
	}
	return coding.NewInMemorySettingsManager(settings)
}

// compactionDefault{Reserve,KeepRecent} mirrors PiG's
// DefaultCompactionSettings (compaction/compaction.go:46). Pulled out
// so the cycle-227 spec's "PiG defaults when no overrides" stays a
// one-line declaration instead of two magic numbers buried in the
// helper.
const (
	compactionDefaultReserve    = 16384
	compactionDefaultKeepRecent = 20000
)

// chainEventFuncs returns an EventFunc that invokes first, then second,
// on the same event. A nil first / second is permitted and short-circuits
// to the other. Used by RunWith to layer the retry-budget observer behind
// the caller's onEvent without exposing chain-plumbing through Options.
//
// Both functions are called synchronously on the drain goroutine — the
// budget observer's AbortRetry call therefore happens before the drain
// pulls the next event off the channel, which is the order PiG expects
// (the abort races with the timer NewTimer creates in prepareRetry, and
// either ordering is correct because both paths route through
// wait.abort()).
func chainEventFuncs(first, second EventFunc) EventFunc {
	switch {
	case first == nil:
		return second
	case second == nil:
		return first
	}
	return func(ev agent.AgentEvent) {
		first(ev)
		second(ev)
	}
}
