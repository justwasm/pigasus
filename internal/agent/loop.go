// Package agent defines a shared agent loop that both the TUI and the HTTP
// shims (openai-shim, anthropic-shim) drive into. The loop is intentionally
// transport-agnostic: they only interact with it via `Send` (in), `Handler`
// (out), and `Done` (signal). The actual byte stream coming back from the
// upstream backend is kept inside this package.
//
// Design notes
// ----------------
//   - Single goroutine per loop iteration; no shared mutable state across
//     turns. The TUI and shims can each hold a long-lived Loop instance and
//     call Send concurrently without locks, because Backend.Client.Stream is
//     itself concurrency-safe (it's called from a per-loop goroutine, never shared
//     across turns).
//   - Streaming chunks arrive on a channel from backend.Stream; we forward them
//     to a handler closure the caller supplied. The handler is called inline
//     with the goroutine, so callers needing ordering guarantees (the shims)
//     don't need to add their own synchronisation.
//   - Errors and "done" markers are merged into the same chunk type so the
//     handler doesn't need a second channel.
//   - Auto-compaction events (PiG's agent.CompactionStartEvent /
//     agent.CompactionEndEvent) fire on the loop's own event channel since
//     cycle 227 and surface through Options.OnCompactionStart /
//     Options.OnCompactionEnd. The TUI passes closures that push tea.Msg
//     updates; the print path leaves both nil and lets cmd/wagyu's
//     pigrt observer handle the echo instead.
package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/piagentbridge"
)

// Loop is a per-conversation driver. Construct once, call Send for each user
// message, and read chunks via the handler set in Options.Handler.
type Loop struct {
	backend backend.Client
	opts    Options

	mu       sync.Mutex // guards in-flight turn count
	inFlight int
	closed   bool
	closeCh  chan struct{}

	// cascade is the recovered state machine observer. Set lazily on the
	// first Send from opts.OnStateTransition; nil when the caller does
	// not care about state transitions.
	cascade *Cascade

	// compactMu guards the lastCompact record. CompactionStart/End
	// events arrive on the loop's drain goroutine while Send is in
	// flight, so the drain and the caller (which reads LastCompact
	// between Sends) race on the same memory. The mutex keeps the race
	// benign — the caller always gets a coherent snapshot.
	compactMu   sync.Mutex
	lastCompact compactRecord
	// compactActive is true after a CompactionStartEvent has been
	// observed without a matching CompactionEndEvent. The TUI's status
	// row reads this every render.
	compactActive bool
}

// Options configure a Loop. Only Backend and Handler are required; the rest
// default to sane values.
type Options struct {
	// Backend is the upstream Client. Exactly one of Backend and Provider
	// is required.
	Backend backend.Client
	// Provider, when set, is used instead of the Provider that would
	// otherwise be derived from Backend. It is an override, not the only
	// way to reach the native path: when Backend is a client that can
	// describe itself (internal/backend.AgwK0sClient.AgwConfig), the Loop
	// derives the matching piagentbridge.Provider and serves the turn from
	// it, so cmd/wagyu needs no change and keeps the exact wire behaviour
	// — the ExternalModel remap, the SystemFingerprint, the User-Agent, and
	// the *backend.HTTPError that cmd/wagyu/api_error.go classifies. See
	// docs/cycles/cycle-183.
	Provider *piagentbridge.Provider
	// Handler is invoked once per upstream chunk. Required; must not be nil.
	Handler Handler
	// SystemPrompt is prepended to every StreamRequest, model `system` role.
	SystemPrompt string
	// DefaultModel is used when the caller doesn't override per turn.
	DefaultModel string
	// MaxTurns caps how many outstanding turns can be in flight at once
	// (default: 1 — strictly serial turns, easiest semantics for shims).
	MaxTurns int
	// OnStateTransition, if non-nil, is invoked from Send at every
	// Cascade state edge (init -> read -> plan -> execute -> stream ->
	// save -> done). This is the recovery hook that lets the --print
	// CLI surface the decompiled `printmode.(*textEmitter)` state
	// machine without re-implementing the model side. The callback runs
	// synchronously in the Send goroutine, so it must be cheap.
	OnStateTransition OnTransitionFn

	// Tools, when non-empty, is the advertised tool set sent on every
	// turn. The model may answer with tool calls instead of text.
	Tools []backend.ToolDecl

	// ToolRunner executes model-requested tool calls. When nil, the loop
	// is single-turn: tool calls are still surfaced to the handler but
	// never executed, preserving the pre-cycle-97 behaviour.
	ToolRunner ToolRunner

	// ExtraAgentTools are appended to the PiG agent's tool surface after
	// Tools are wrapped via ToolRunner. Use this for tools that drive
	// their own LLM turn (e.g. internal/subagenttools) and therefore don't
	// fit the clone's backend-decl/runner shape — they already implement
	// piagent.AgentTool, so wrapping them through ToolRunner would
	// double-dispatch. Cycle 228.
	ExtraAgentTools []piagent.AgentTool

	// MaxToolIterations caps the model -> tool -> model round trips in one
	// Send (default: defaultMaxToolIterations).
	MaxToolIterations int

	// OnCompactionStart, when non-nil, fires from the loop's drain
	// goroutine on every PiG agent.CompactionStartEvent PiG emits
	// during the turn. The signature matches the simplest read of
	// "compact is starting" — a reason string ("manual" | "threshold"
	// | "overflow") and the wall-clock start time. The print path
	// leaves this nil and lets cmd/wagyu's pigrt observer echo instead;
	// the TUI sets it to push a tea.Msg that updates runMode.lastCompact.
	//
	// Cycle 227: TODO §4 long-running task continuity row. Before the
	// cycle the TUI was silent on auto-compact — a long task could
	// hang the chat thread for ~30 s with no UI feedback.
	OnCompactionStart func(reason string, startedAt time.Time)
	// OnCompactionEnd is the matching end-of-compact hook. The summary
	// field is the upstream-generated summary the LLM emitted; tokensBefore
	// and tokensAfter are the projected context-window deltas. Any of
	// them may be zero / empty on an aborted compact.
	OnCompactionEnd func(reason string, endedAt time.Time, summary string, tokensBefore, tokensAfter int, aborted bool)

	// Effort is the reasoning-effort level the loop forwards to PiG's
	// agent before each Send. Empty keeps the model's default; any
	// ai.ThinkingLevel value PiG accepts ("low", "medium", "high", "xhigh",
	// "max", "off", "minimal") is passed through and clamped against the
	// active model's ThinkingLevelMap.
	//
	// Cycle effort-picker: the wagyu TUI wires --effort and /effort through
	// this seam so a single Send sees the level the user picked, regardless
	// of whether the picker Enter or the /effort <level> shortcut set it.
	Effort string

	// AutoCont enables the cycle auto-continue-interrupt recovery. When
	// true, a stream-interrupt (StopReasonError / Aborted / Length) or
	// iteration-cap (agent.ErrMaxTurnsReached) is recovered by injecting
	// the verbatim AGY recovery copy and re-running the turn. The default
	// zero value (false) keeps the pre-cycle behaviour: a stream failure
	// is surfaced as a typed error the caller can match. The TUI sets this
	// to true in cmd/wagyu/tui_run.go so users see the same
	// "auto-continuing" tail the reference emits; tests and offline
	// harnesses opt out.
	//
	// Cycle auto-continue-interrupt recovery depth is bounded by
	// AutoContMaxDepth (=3): three consecutive failures return
	// ErrAutoContMaxDepth and the caller is expected to surface a
	// user-input-required message. See auto_continue.go.
	AutoCont bool

	// AutoContBanner, when set, is invoked once before each auto-continue
	// turn (i.e. on each recovery attempt past the first). The TUI uses
	// this to flash a transient "↻ auto-continuing…" status line; keeping
	// it a callback means the agent package does not pull in lipgloss or
	// colorprofile. The callback runs synchronously inside Send, so it
	// must be cheap.
	AutoContBanner func(depth, maxDepth int)
}

// compactRecord mirrors pigrt.CompactionRecord on the Loop side. The
// field shapes match exactly so a Loop and a pigrt.Runtime can be
// interchanged transparently in cmd/wagyu — they store the same wall
// clock data PiG's agent.Compaction{Start,End}Event carried.
type compactRecord struct {
	StartedAt            time.Time
	EndedAt              time.Time
	Reason               string
	Summary              string
	TokensBefore         int
	EstimatedTokensAfter int
	Aborted              bool
}

// defaultMaxToolIterations bounded the tool-call loop so a model that kept
// asking to act could not spin forever. Cycle 279 set it to 0 (= no cap),
// which is PiG's own upstream default and the reference's posture:
// agent/agent.go:747 documents MaxTurns 0 as "unlimited, as upstream, which has
// no turn cap", and a real A/B on the same 30-step task showed agy running 11
// model turns where the clone stopped at 8.
//
// The value 8 was inherited from the clone's pre-PiG hand-rolled loop (one Send
// with no recovery). On a long-horizon turn it is not a safety net, it is a
// bug: the turn dies mid-task, auto-continue replays the *user* transcript
// without the tool history, and the model starts the work over from scratch
// until the retry budget is exhausted.
//
// What bounds a runaway turn instead: the user's Esc, the per-tool timeout, and
// the 5-minute SSE idle watcher. A caller that really wants a budget can still
// set Options.MaxToolIterations.
const defaultMaxToolIterations = 0

// ToolRunner executes one model-requested tool call and returns the textual
// result fed back to the model as a `tool` role message. A returned error is
// reported to the model as text (the loop never aborts on a tool failure).
type ToolRunner interface {
	Run(ctx context.Context, call backend.ToolCall) (string, error)
}

// Handler receives StreamChunks from the upstream call. The same chunk is
// delivered to every registered sink; the simplest impl is a closure that
// writes to an http.ResponseWriter and flushes.
//
// Returning a non-nil error stops the in-flight stream and surfaces the error
// to the caller of Send.
type Handler func(chunk backend.StreamChunk) error

// New returns a Loop with MaxTurns defaulted to 1.
func New(opts Options) (*Loop, error) {
	if opts.Backend == nil && opts.Provider == nil {
		return nil, errors.New("agent: Backend or Provider is required")
	}
	if opts.Handler == nil {
		return nil, errors.New("agent: Handler is required")
	}
	if opts.MaxTurns <= 0 {
		opts.MaxTurns = 1
	}
	return &Loop{
		backend: opts.Backend,
		opts:    opts,
		closeCh: make(chan struct{}),
	}, nil
}

// Send kicks off one turn: takes the user's `input`, forwards it to the
// upstream backend, and dispatches every chunk to Options.Handler. It returns
// once the upstream stream is fully consumed (terminal chunk observed) or
// when ctx is canceled. If MaxTurns is exceeded, Send blocks until a turn
// slot is free.
//
// Send is safe to call from multiple goroutines.
//
// Cycle 40 Cascade integration: each Send walks the recovered Cascade
// state machine (init -> read -> plan -> execute -> stream -> save -> done).
// The transitions are observed by Options.OnStateTransition when set, so
// callers (notably the --print CLI) can render them on stderr/stdout. The
// transitions are best-effort: an unreachable transition (e.g. forgetting
// to enter `execute` before `stream`) returns an error from Cascade.Transition
// which we log via the observer and continue; the streaming path is the
// source of truth for actual work.
func (l *Loop) Send(ctx context.Context, input TurnInput) error {
	if err := l.acquireSlot(ctx); err != nil {
		return err
	}
	defer l.releaseSlot()

	// Lazy-init the cascade so callers that don't care about state
	// transitions pay nothing. Reset it on every Send so a long-lived
	// Loop re-observes the lifecycle per turn.
	if l.cascade == nil {
		l.cascade = NewCascade(l.opts.OnStateTransition)
	} else {
		l.cascade.Reset()
		if l.opts.OnStateTransition != nil {
			l.cascade.SetObserver(l.opts.OnStateTransition)
		}
	}
	cascadeTransition(l.cascade, StateRead, "received user prompt")

	// Since cycle 183 the model -> tool -> model cycle is PiG's
	// (github.com/MichaelKinsy/PiG/agent); see pig.go. Nothing above this
	// line changed: the cascade is still this package's, and every chunk the
	// handler receives is still a backend.StreamChunk translated back out of
	// PiG's event stream.
	//
	// Cycle auto-continue-interrupt: when AutoCont is set, the public Send
	// dispatches to sendPigAuto, which calls sendPig and recovers from a
	// stream-interrupt or iteration-cap by appending the AGY recovery
	// copy and re-running. The two paths share the same PiG plumbing — only
	// the failure-handling tail differs.
	if l.opts.AutoCont {
		return l.sendPigAuto(ctx, input)
	}
	return l.sendPig(ctx, input)
}

// cascadeTransition is a small helper that swallows the (rare) edge-error
// from Cascade.Transition. The error path means the caller asked for an
// unreachable state, which is a programmer error in the recovery — we
// don't want to fail the whole Send because of it.
func cascadeTransition(c *Cascade, to CascadeState, note string) {
	if c == nil {
		return
	}
	_ = c.Transition(to, note)
}

// Close releases the loop. In-flight sends are NOT canceled — they run to
// completion. After Close returns, further Calls to Start
// (none in the public API today) will fail. Close is idempotent.
func (l *Loop) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	close(l.closeCh)
	return nil
}

// Cascade returns the recovered state machine for the most recent Send.
// Returns nil if Send has not been called yet. The returned Cascade is
// shared (read-only by convention); do not call its mutators.
func (l *Loop) Cascade() *Cascade {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cascade
}

// LastCompact returns a snapshot of the most recently observed
// compaction record (cycle 227). The caller's copy is detached from
// the Loop's state; subsequent events update the Loop's record but
// not the returned value.
func (l *Loop) LastCompact() compactRecord {
	l.compactMu.Lock()
	defer l.compactMu.Unlock()
	return l.lastCompact
}

// IsCompacting reports whether a CompactionStartEvent has been
// observed on this Loop without a matching CompactionEndEvent.
func (l *Loop) IsCompacting() bool {
	l.compactMu.Lock()
	defer l.compactMu.Unlock()
	return l.compactActive
}

// recordCompact stamps l.lastCompact from a PiG agent.AgentEvent. Start
// overwrites the open record's StartedAt; End stamps EndedAt and the
// post-compaction token totals. End with no open start (a manual
// compact that fires its events before the drain attaches) is dropped
// — without the start the duration / spinner model is wrong.
func (l *Loop) recordCompact(ev piagent.AgentEvent) {
	switch event := ev.(type) {
	case piagent.CompactionStartEvent:
		start := time.Now().UTC()
		l.compactMu.Lock()
		l.lastCompact = compactRecord{
			StartedAt: start,
			Reason:    event.Reason,
		}
		l.compactActive = true
		l.compactMu.Unlock()
		if cb := l.opts.OnCompactionStart; cb != nil {
			cb(event.Reason, start)
		}
	case piagent.CompactionEndEvent:
		end := time.Now().UTC()
		l.compactMu.Lock()
		if l.compactActive {
			l.lastCompact.EndedAt = end
			l.lastCompact.Reason = event.Reason
			l.lastCompact.Summary = event.Summary
			l.lastCompact.TokensBefore = event.TokensBefore
			l.lastCompact.EstimatedTokensAfter = event.EstimatedTokensAfter
			l.lastCompact.Aborted = event.Aborted
			l.compactActive = false
		}
		l.compactMu.Unlock()
		if cb := l.opts.OnCompactionEnd; cb != nil {
			cb(event.Reason, end, event.Summary, event.TokensBefore, event.EstimatedTokensAfter, event.Aborted)
		}
	}
}

// acquireSlot blocks until a turn slot is free or ctx is canceled.
func (l *Loop) acquireSlot(ctx context.Context) error {
	for {
		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			return errors.New("agent: loop closed")
		}
		if l.inFlight < l.opts.MaxTurns {
			l.inFlight++
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (l *Loop) releaseSlot() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight > 0 {
		l.inFlight--
	}
}

// safeCall invokes handler, recovering from any panic so a buggy handler
// can't take down the agent goroutine.
func safeCall(h Handler, c backend.StreamChunk) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("agent: handler panic: %v", r)
		}
	}()
	return h(c)
}

// ---- TurnInput ---------------------------------------------------------

// TurnInput is the per-turn payload. Model, MaxTokens and Temperature are
// optional — when Model is empty the Loop's DefaultModel kicks in. Messages
// is the conversation context the upstream sees (excluding SystemPrompt,
// which the Loop prepends automatically).
type TurnInput struct {
	Model       string            `json:"model,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
	Messages    []backend.Message `json:"messages"`
}

// ModelOr returns m.Model if non-empty, else fallback.
func (t TurnInput) ModelOr(fallback string) string {
	if t.Model != "" {
		return t.Model
	}
	return fallback
}
