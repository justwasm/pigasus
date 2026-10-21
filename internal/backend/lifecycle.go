// Package backend — lifecycle.go: the recovered 1.3.5 backend
// lifecycle wrapper.
//
// Background. The cycle 25/40 backend package was a thin shim around
// `Client.Stream` — exactly the slice the openai-shim and anthropic-shim
// need to forward chunks. That was fine for the shims, but it skips the
// real lifecycle the v1.3.5 binary drives:
//
//   - NewServerBackend (0x8754d20) → constructs a 3-field struct
//     (settings, logger, opts) and seeds the conversation search index
//   - (*ServerBackend).StartConversation (0x875c020) → opens a fresh
//     conversation stream (handles the channel-1 setup, mTLS handshake,
//     retry-budget)
//   - Run (0x8754a60, on the retry wrapper) → kicks the body of the
//     turn: `cascader.Run(prompt)` walks the state machine, emits
//     chunks, and waits for the terminal frame
//   - Emit(...) → forwards a chunk to the printmode emitter
//     (textEmitter / jsonEmitter / streamJSONEmitter)
//   - Save (via cascade_run_state closeSubagentForwarder 0x76caa60) →
//     commits the final state to the on-disk store
//   - Close (0x87566c0) → tears down the conversation / subagent
//     forwarders / observer channels
//
// This file defines:
//
//   - Backend: the high-level lifecycle interface (Init / Run / Emit /
//     Save / Close). It wraps a Client and a Sink so callers can drive
//     a single turn end-to-end without knowing whether the underlying
//     transport is mock, agw.k0s.io, or a registry-backed real provider.
//   - Lifecycle: the concrete implementation. It tracks the same
//     init→execute→stream→save→done state machine the printmode
//     emitters and the cascade state machine use, so the
//     `Options.OnStateTransition` observer in agent.Loop can render the
//     real transitions on stderr.
//   - NewLifecycle: a constructor that picks the right Client based on
//     the model name (cycle 33 + 40 dispatch rules) and wires the
//     recovered `(*textEmitter)` as the default Sink.
//
// What it replaces:
//
//	The previous --print path in cmd/wagyu/main.go called
//	backend.Client.Stream directly, which only exposed the stream
//	surface. cmd/wagyu now constructs a Lifecycle once, calls
//	Init() on it, then for each turn calls Run(prompt) → for each
//	chunk the Lifecycle's Run loops over `client.Stream` and pushes
//	text through Emit() → finally Save() + Close(). The Cascade state
//	transitions the agent.Loop drives are mirrored 1:1 by the
//	Lifecycle so the two FSMs stay in sync (per the decompiled 1.3.5
//	call graph in docs/analysis-v1.3.5-official/backend-call-graph.md).
package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// LifecycleState is the 1:1 mirror of the recovered 1.3.5 printmode
// emitter state machine, exposed to callers that want to observe it
// (the agent.Loop OnStateTransition callback fires on every edge).
//
// Recovered symbols (see backend-call-graph.md §1):
//
//	EmitInit      -> 0x8d063e0
//	BeginTurn     -> 0x8d06400
//	(streaming)   ->  (one or more chunks)
//	EmitFinal     -> 0x8d06600
//	EmitError     -> 0x8d06560  (escape from any state)
type LifecycleState string

const (
	// LStateInit — Lifecycle just constructed, no work done yet.
	LStateInit LifecycleState = "init"
	// LStateReady — Init() ran; conversation channel open; awaiting prompt.
	LStateReady LifecycleState = "ready"
	// LStateExecute — Run() called; cascader walking state machine.
	LStateExecute LifecycleState = "execute"
	// LStateStream — at least one chunk has been emitted.
	LStateStream LifecycleState = "stream"
	// LStateSave — terminal chunks processed, persisting to store.
	LStateSave LifecycleState = "save"
	// LStateDone — all work complete (terminal happy path).
	LStateDone LifecycleState = "done"
	// LStateError — terminal failure path.
	LStateError LifecycleState = "error"
)

// Sink is the recovered printmode emitter abstraction. The three
// concrete emitters in 1.3.5 (textEmitter / jsonEmitter /
// streamJSONEmitter) all implement the same 4-method contract:
//
//	EmitInit(ctx, model string) error
//	BeginTurn(turnID int64) error
//	Emit(text string) error
//	EmitFinal() error
//	EmitError(err error) error
//
// The default impl is textEmitter (writes to w directly, the way
// `antigravity --print` does). jsonEmitter / streamJSONEmitter live in
// the cmd/wagyu package because their wire shape depends on the
// output destination (stdout vs HTTP).
type Sink interface {
	// EmitInit signals the start of a new conversation. Mirrors
	// `(*textEmitter).EmitInit` at 0x8d063e0.
	EmitInit(ctx context.Context, model string) error
	// BeginTurn signals the start of a new turn. Mirrors
	// `(*textEmitter).BeginTurn` at 0x8d06400.
	BeginTurn(turnID int64) error
	// Emit forwards one streamed chunk. The recovered code path calls
	// this on every StreamChunk.Text (cycle 33+40 mock path, and the
	// agw_k0s SSE parser).
	Emit(text string) error
	// EmitFinal signals the end of a successful turn. Mirrors
	// `(*textEmitter).EmitFinal` at 0x8d06600.
	EmitFinal() error
	// EmitError signals an error. Mirrors `(*textEmitter).EmitError`
	// at 0x8d06560. May be called from any state.
	EmitError(err error) error
}

// Backend is the high-level lifecycle interface. cmd/wagyu's
// runPrintMode now drives this instead of the raw Client.Stream; the
// agent.Loop OnStateTransition observer sees one transition per
// interface call (Init/Run/Emit/Save/Close), matching the decompiled
// 1.3.5 emit FSM.
//
// The split between Client and Backend is intentional: Client keeps
// its single-method surface (it's what the HTTP shims need; changing
// the shim wire format is out of scope for cycle 41), while Backend
// adds the lifecycle hooks the v1.3.5 printmode+backend actually use.
type Backend interface {
	// Init opens a conversation channel and selects the auth provider
	// (mirrors `(*ServerBackend).activeCascadeConfig` at 0x8757760 +
	// the proto's GetAuthProviderType at 0x6c368c0). Returns the model
	// that was selected; the caller may override by setting
	// StreamRequest.Model later.
	Init(ctx context.Context, model string) (string, error)

	// Run kicks the body of a turn: it opens a Stream on the
	// underlying Client, walks the chunks, and pushes them through
	// the configured Sink. The return value is the final StreamChunk
	// (always has Done=true) or an error.
	//
	// In the decompiled code this is `(*retry).Run` at 0x8754a60
	// wrapping `cascader.Run` from cortex/cascade_run_state.
	Run(ctx context.Context, prompt string) error

	// Emit is an explicit escape hatch for callers that want to push a
	// single chunk through the sink without going through the full
	// Run path (e.g. error messages, banner text). The lifecycle
	// transitions to LStateStream after the first successful Emit.
	Emit(text string) error

	// Save persists the final conversation state. Mirrors the
	// commit-side of `(*ServerBackend).closeSubagentForwarder` at
	// 0x76caa60 (cascade_run_state) and the store's
	// `commitSubagentStates` at 0x8aa0b20. The cycle 41 default
	// implementation writes nothing (the recovered on-disk store
	// is owned by internal/store/manager.go); a richer impl can
	// hook a real storage layer here.
	Save() error

	// Close tears down the conversation. Mirrors `(*ServerBackend).Close`
	// at 0x87566c0. Idempotent.
	Close() error
}

// LifecycleConfig controls how a Lifecycle is wired.
type LifecycleConfig struct {
	// Client is the underlying stream client. Required.
	Client Client
	// Sink is where chunks are written. If nil, a default textSink
	// that writes to os.Stdout is used.
	Sink Sink
	// SystemPrompt is prepended to every StreamRequest.
	SystemPrompt string
	// DefaultModel is used when Init is called with an empty model.
	DefaultModel string
	// MaxTurns caps how many Run() calls may be in flight at once
	// (default 1, matching the cycle 40 agent.Loop default).
	MaxTurns int
	// OnStateTransition, if non-nil, is invoked at every edge of the
	// LifecycleState machine. Mirrors the OnStateTransition callback
	// in agent.Options; both fire on the same goroutine so the chain
	// is observable in lockstep.
	OnStateTransition func(from, to LifecycleState, note string)
	// Saver is the optional persistence hook called by Save().
	// If nil, Save is a no-op (the recovered cycle 41 default — the
	// full on-disk store is wired in a later cycle).
	Saver Saver
}

// Saver is the persistence interface LifecycleConfig.Saver satisfies.
// We keep it separate from the Backend interface so the cycle 41
// default is no-op and a later cycle can wire a real store without
// changing the Backend surface.
type Saver interface {
	// Save commits the conversation state. The conversationID is the
	// Init()-generated id; the full transcript is the last emitted
	// text + the final StreamChunk. Error is non-nil only on storage
	// failure.
	Save(conversationID string, transcript string) error
}

// nopSaver is the default Saver when LifecycleConfig.Saver is nil.
type nopSaver struct{}

func (nopSaver) Save(conversationID string, transcript string) error { return nil }

// Lifecycle is the concrete Backend implementation. The struct fields
// are private; callers drive it through the Backend interface.
type Lifecycle struct {
	cfg        LifecycleConfig
	client     Client
	sink       Sink
	saver      Saver
	mu         sync.Mutex // guards state, inFlight, convID, transcript
	state      LifecycleState
	inFlight   int
	convID     string
	transcript string
	turnID     int64
	closed     bool
	usage      *UsageTracker // cycle 45 — accumulated prompt/completion/total
}

// NewLifecycle constructs a Lifecycle wrapping cfg.Client. The cfg.Sink
// defaults to a textSink(os.Stdout); cfg.Saver defaults to a no-op.
// cfg.MaxTurns defaults to 1 (cycle 40 strict-serial semantics). The
// state machine starts in LStateInit and is advanced by the first
// method call (Init → LStateReady).
func NewLifecycle(cfg LifecycleConfig) (*Lifecycle, error) {
	if cfg.Client == nil {
		return nil, errors.New("backend.NewLifecycle: Client is required")
	}
	if cfg.Sink == nil {
		cfg.Sink = NewTextSink(os.Stdout)
	}
	if cfg.Saver == nil {
		cfg.Saver = nopSaver{}
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 1
	}
	return &Lifecycle{
		cfg:    cfg,
		client: cfg.Client,
		sink:   cfg.Sink,
		saver:  cfg.Saver,
		state:  LStateInit,
		usage:  NewUsageTracker(),
	}, nil
}

// Init implements Backend.Init. The conversation id is generated from
// wall-clock + a counter (the recovered 1.3.5 server backend uses a
// ULID; this is the same shape, just uint64-derived).
func (l *Lifecycle) Init(ctx context.Context, model string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return "", errors.New("backend.Lifecycle: Init after Close")
	}
	if strings.TrimSpace(model) == "" {
		model = l.cfg.DefaultModel
	}
	if strings.TrimSpace(model) == "" {
		return "", errors.New("backend.Lifecycle: model is required")
	}
	// LStateInit -> LStateReady. Mirrors EmitInit in the printmode FSM.
	l.transitionLocked(LStateReady, "Init completed: model="+model)
	if err := l.sink.EmitInit(ctx, model); err != nil {
		l.transitionLocked(LStateError, "EmitInit failed: "+err.Error())
		return "", fmt.Errorf("backend.Lifecycle: EmitInit: %w", err)
	}
	l.turnID = 0
	l.convID = fmt.Sprintf("conv-%d", time.Now().UnixNano())
	return model, nil
}

// Run implements Backend.Run. The call:
//
//  1. opens a Stream on l.client (the call shape that maps to
//     `(*ServerBackend).StartConversation` at 0x875c020 in the
//     decompiled 1.3.5 code),
//  2. emits a BeginTurn event into the sink,
//  3. loops over the StreamChunk channel, calling sink.Emit for every
//     non-empty text chunk,
//  4. on the terminal chunk (Done=true), advances the state machine
//     to LStateSave then LStateDone, calls sink.EmitFinal, and
//     returns.
//
// Errors at any point funnel through sink.EmitError and end the
// state machine in LStateError.
func (l *Lifecycle) Run(ctx context.Context, prompt string) error {
	if err := l.acquireTurnSlot(ctx); err != nil {
		return err
	}
	defer l.releaseTurnSlot()

	if strings.TrimSpace(prompt) == "" {
		return errors.New("backend.Lifecycle: Run: prompt is required")
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return errors.New("backend.Lifecycle: Run after Close")
	}
	if l.state == LStateInit {
		l.mu.Unlock()
		return errors.New("backend.Lifecycle: Run before Init")
	}
	l.turnID++
	turnID := l.turnID
	l.transitionLocked(LStateExecute, fmt.Sprintf("Run started: turn=%d", turnID))
	l.mu.Unlock()

	if err := l.sink.BeginTurn(turnID); err != nil {
		l.transition(fmt.Sprintf("BeginTurn failed: %s", err), LStateError)
		_ = l.sink.EmitError(err)
		return fmt.Errorf("backend.Lifecycle: BeginTurn: %w", err)
	}

	stream, err := l.client.Stream(ctx, StreamRequest{
		Model:        l.cfg.DefaultModel,
		SystemPrompt: l.cfg.SystemPrompt,
		Messages:     []Message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		l.transition(fmt.Sprintf("open stream failed: %s", err), LStateError)
		_ = l.sink.EmitError(err)
		return fmt.Errorf("backend.Lifecycle: open stream: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			l.transition(fmt.Sprintf("ctx canceled: %s", ctx.Err()), LStateError)
			_ = l.sink.EmitError(ctx.Err())
			return ctx.Err()
		case chunk, ok := <-stream:
			if !ok {
				// Channel closed without a Done marker; synthesize one.
				l.transition("channel closed without done marker", LStateSave)
				_ = l.sink.EmitFinal()
				l.transition("turn complete (synthesized done)", LStateDone)
				return nil
			}
			if chunk.Error != nil {
				l.transition(fmt.Sprintf("chunk error: %s", chunk.Error), LStateError)
				_ = l.sink.EmitError(chunk.Error)
				return chunk.Error
			}
			if chunk.Text != "" {
				l.transition(fmt.Sprintf("chunk %d bytes", len(chunk.Text)), LStateStream)
				if err := l.sink.Emit(chunk.Text); err != nil {
					l.transition(fmt.Sprintf("sink.Emit failed: %s", err), LStateError)
					return fmt.Errorf("backend.Lifecycle: sink.Emit: %w", err)
				}
				l.appendTranscriptLocked(chunk.Text)
			}
			if chunk.Done {
				// Cycle 45 (Issue #7 fix) — capture terminal
				// chunk usage into the Lifecycle's UsageTracker.
				// The v1.3.5 binary populates chunk.Meta on
				// the terminal frame (the OpenAI usage block
				// is mapped 1:1 by client.go); the recovered
				// agw_k0s SSE parser does the same. Add
				// updates the running totals the UsageBattle
				// panel reads via Lifecycle.Usage().
				if chunk.Meta != nil && l.usage != nil {
					l.usage.Add(
						chunk.Meta.PromptTokens,
						chunk.Meta.CompletionTokens,
						chunk.Meta.TotalTokens,
					)
				}
				l.transition("received done marker", LStateSave)
				_ = l.sink.EmitFinal()
				l.transition("turn complete", LStateDone)
				return nil
			}
		}
	}
}

// Emit implements Backend.Emit. Useful for callers that want to push
// banner text or error messages through the same sink without going
// through the full Run path. After the first successful Emit the
// state machine is in LStateStream.
func (l *Lifecycle) Emit(text string) error {
	if text == "" {
		return nil
	}
	if err := l.sink.Emit(text); err != nil {
		l.transition(fmt.Sprintf("sink.Emit failed: %s", err), LStateError)
		return err
	}
	l.mu.Lock()
	l.appendTranscriptLocked(text)
	l.mu.Unlock()
	l.transition("explicit Emit call", LStateStream)
	return nil
}

// Save implements Backend.Save. The default Saver is a no-op; the
// recovered on-disk store lives in internal/store/manager.go and a
// richer saver can be plugged in by setting LifecycleConfig.Saver.
//
// Cycle trim-trailing-blank-messages: the transcript handed to the
// saver is run through TrimTrailingBlankLines, so the trailing
// "\n\n" the OpenAI / Anthropic / Gemini streaming APIs commonly
// emit on the final chunk do not leak into the persisted
// conversation transcript. Without this pass the saved DB grows
// one empty row per turn and successive transcript diffs are
// polluted with phantom whitespace deltas.
func (l *Lifecycle) Save() error {
	l.mu.Lock()
	convID := l.convID
	transcript := TrimTrailingBlankLines(l.transcript)
	state := l.state
	l.mu.Unlock()
	if state == LStateInit {
		return errors.New("backend.Lifecycle: Save before Init")
	}
	l.transition(fmt.Sprintf("Save: conv=%s", convID), LStateSave)
	if err := l.saver.Save(convID, transcript); err != nil {
		l.transition(fmt.Sprintf("Save failed: %s", err), LStateError)
		return fmt.Errorf("backend.Lifecycle: Save: %w", err)
	}
	l.transition("Save: ok", LStateDone)
	return nil
}

// Close implements Backend.Close. Idempotent. Mirrors the decompiled
// `(*ServerBackend).Close` at 0x87566c0 (a thunk that zeros the
// 3-field struct).
func (l *Lifecycle) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.state != LStateError {
		l.transitionLocked(LStateDone, "Close")
	}
	return nil
}

// State returns the current LifecycleState. Exposed for tests and
// for the agent.Loop Cascade observer.
func (l *Lifecycle) State() LifecycleState {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state
}

// Transcript returns the accumulated text emitted so far, with
// any trailing whitespace / blank-line run stripped. Useful for
// tests and for the Save() path — both consumers expect the
// trimmed shape, matching what `agent.SanitizeText` produces for
// the on-screen reply so the bytes that hit the screen and the
// bytes that hit the saved DB agree.
func (l *Lifecycle) Transcript() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return TrimTrailingBlankLines(l.transcript)
}

// ConvID returns the conversation id set by Init.
func (l *Lifecycle) ConvID() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.convID
}

// Usage returns the Lifecycle's UsageTracker. Cycle 45
// (Issue #7 fix) — the UsageTracker accumulates prompt /
// completion / total tokens across all turns driven by this
// Lifecycle. The TUI's UsageBattle panel reads Snapshot() via
// this accessor; the cascade [usage] line emitted on every
// terminal frame in cmd/wagyu/tui_live.go is parsed by
// the panel into Add() calls on the same tracker. Returns
// nil only if NewLifecycle was bypassed (which the public
// API does not allow).
func (l *Lifecycle) Usage() *UsageTracker {
	return l.usage
}

// UsageTrackerOwner interface assertion. The TUI panel
// accepts a UsageTracker via this interface so the package
// doesn't import internal/backend; the production wiring
// passes *Lifecycle, which satisfies UsageTrackerOwner.
var _ UsageTrackerOwner = (*Lifecycle)(nil)

// transition is a non-locking wrapper for state transitions that
// happens outside l.mu (caller must hold the lock OR be in a
// goroutine-safe path; we lock here too because the state machine
// is read by State() and we want the transition + the note to be
// atomic from the observer's perspective).
func (l *Lifecycle) transition(note string, to LifecycleState) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.transitionLocked(to, note)
}

// transitionLocked is the actual state-mutator; must be called with
// l.mu held.
func (l *Lifecycle) transitionLocked(to LifecycleState, note string) {
	from := l.state
	l.state = to
	if l.cfg.OnStateTransition != nil {
		l.cfg.OnStateTransition(from, to, note)
	}
}

func (l *Lifecycle) appendTranscriptLocked(s string) {
	l.transcript += s
}

func (l *Lifecycle) acquireTurnSlot(ctx context.Context) error {
	for {
		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			return errors.New("backend.Lifecycle: closed")
		}
		if l.inFlight < l.cfg.MaxTurns {
			l.inFlight++
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func (l *Lifecycle) releaseTurnSlot() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight > 0 {
		l.inFlight--
	}
}

// ---- default Sink impl: textSink ---------------------------------------

// textSink is the default Sink that mirrors the decompiled
// (*textEmitter) at 0x8d063e0..0x8d06600. It writes a banner on
// EmitInit, a turn-marker on BeginTurn, the text directly on Emit, a
// newline on EmitFinal, and a `[error] ...` line on EmitError.
//
// This is what the `cmd/wagyu --print` path needs; the JSON
// variant lives in cmd/wagyu (out of scope for this file).
type textSink struct {
	w  io.Writer
	mu sync.Mutex // writer is concurrent-safe per Chunk callback
}

// NewTextSink returns a Sink that writes to w. Returns a usable
// Sink even if w is nil (writes go to /dev/null in that case so the
// caller can still drive the lifecycle in tests).
func NewTextSink(w io.Writer) Sink {
	if w == nil {
		w = io.Discard
	}
	return &textSink{w: w}
}

// EmitInit prints a one-line banner identifying the model. The shape
// matches what `antigravity --print` prints today; the cycle 41
// refactor just adds it as a lifecycle event instead of a special
// case in runPrintMode.
func (t *textSink) EmitInit(_ context.Context, model string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := fmt.Fprintf(t.w, "[init] model=%s\n", model)
	return err
}

// BeginTurn prints the turn number on its own line. The 1.3.5 binary
// uses this to drive the TUI's `step=5/5 turn=N` counter; the cycle 41
// textSink just echoes it for the `--print` path.
func (t *textSink) BeginTurn(turnID int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := fmt.Fprintf(t.w, "[turn %d] ", turnID)
	return err
}

// Emit writes the chunk text directly to the writer.
func (t *textSink) Emit(text string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := io.WriteString(t.w, text)
	return err
}

// EmitFinal writes a trailing newline and a separator so the next
// turn's [turn N] is on its own line.
func (t *textSink) EmitFinal() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := io.WriteString(t.w, "\n")
	return err
}

// EmitError writes `[error] ...` followed by a newline. Mirrors the
// printmode emitter's error framing.
func (t *textSink) EmitError(err error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err == nil {
		return nil
	}
	_, werr := fmt.Fprintf(t.w, "[error] %v\n", err)
	return werr
}

// Compile-time guarantee textSink implements Sink.
var _ Sink = (*textSink)(nil)

// Compile-time guarantee Lifecycle implements Backend.
var _ Backend = (*Lifecycle)(nil)

// ---- UsageTracker (prompt/completion/total token accumulator) ----------
//
// UsageTracker is the recovered "battle" counter that aggregates
// per-turn prompt + completion token counts across a session.
// The v1.3.5 binary emits a `[usage] prompt=N completion=N
// total=N fp=...` line on every terminal stream frame; the
// recovered lifecycle captures the same frame (the agw_k0s
// StreamChunk.Meta block is mapped 1:1) and feeds the numbers
// into a UsageTracker via Add. The TUI's UsageBattle panel then
// reads Snapshot() to render the cumulative battle total.
//
// Cycle 45 (Issue #7 fix) — the cycle-43 UsageBattle panel was a
// placeholder that hardcoded 4 fake tool names. Cycle 45 wires
// the panel to a real UsageTracker + AgentSlot list so the
// battle line in the view reflects the actual prompt/completion
// tokens spent across the whole session.
//
// The tracker is goroutine-safe (mu guards the counters) so it
// can be shared between the agent loop's goroutine and the
// Bubbletea runtime. Reset is a separate op (used by the
// UsageBattle 'r' key) so partial-session totals are not lost
// when the user toggles the active slot.

// UsageTracker accumulates prompt / completion / total token
// counts across a session. Zero value is ready to use.
type UsageTracker struct {
	mu               sync.Mutex
	PromptTokens     int // sum of prompt tokens across all Add() calls
	CompletionTokens int // sum of completion tokens
	TotalTokens      int // sum of total tokens
	Events           int // number of Add() calls (debug / observability)
}

// NewUsageTracker returns a fresh UsageTracker with all
// counters at zero.
func NewUsageTracker() *UsageTracker {
	return &UsageTracker{}
}

// Add accumulates a single prompt/completion/total triple. The
// total is computed as prompt+completion if the caller didn't
// supply one (the cycle 42 close-gap evidence shows the v1.3.5
// binary always reports all three, but we accept the partial
// case for robustness). Goroutine-safe.
func (t *UsageTracker) Add(prompt, completion, total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.PromptTokens += prompt
	t.CompletionTokens += completion
	if total > 0 {
		t.TotalTokens += total
	} else {
		t.TotalTokens += prompt + completion
	}
	t.Events++
}

// Snapshot returns a copy of the current counters. The values
// are atomic from the caller's perspective (read under the
// same mutex that guards Add). The returned struct is a value
// type so callers can stash it without further locking.
func (t *UsageTracker) Snapshot() UsageSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return UsageSnapshot{
		PromptTokens:     t.PromptTokens,
		CompletionTokens: t.CompletionTokens,
		TotalTokens:      t.TotalTokens,
		Events:           t.Events,
	}
}

// Reset zeros all counters. Cycle 45 — the UsageBattle 'r' key
// calls this on the active slot's tracker. After Reset, Add
// restarts from zero and Snapshot returns zeros.
func (t *UsageTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.PromptTokens = 0
	t.CompletionTokens = 0
	t.TotalTokens = 0
	t.Events = 0
}

// UsageSnapshot is the immutable view of a UsageTracker at a
// point in time. Returned by Snapshot. Mirrors the cycle 42
// close-gap evidence shape: prompt / completion / total / events.
type UsageSnapshot struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Events           int
}

// UsageTrackerOwner is the minimal interface the UsageTracker
// needs from the Lifecycle. The cycle 45 wiring uses this
// interface so the Lifecycle can hand a real UsageTracker to
// the TUI's UsageBattle panel without creating a back-edge
// from internal/tui to internal/backend (the panel accepts a
// UsageTracker by interface and the real implementation is
// constructed in the orchestrator).
type UsageTrackerOwner interface {
	// Usage returns the Lifecycle's UsageTracker. Nil if the
	// Lifecycle was constructed without one (cycle 41 default
	// — no usage tracking).
	Usage() *UsageTracker
}
