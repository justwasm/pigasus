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
//     transport is mock, the configured gateway, or a registry-backed real provider.
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
	"os"
	"strings"
	"sync"
	"time"
)

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

func (l *Lifecycle) openTurnStream(ctx context.Context, turnID int64, prompt string) (<-chan StreamChunk, error) {
	if err := l.sink.BeginTurn(turnID); err != nil {
		l.transition(fmt.Sprintf("BeginTurn failed: %s", err), LStateError)
		_ = l.sink.EmitError(err)
		return nil, fmt.Errorf("backend.Lifecycle: BeginTurn: %w", err)
	}

	stream, err := l.client.Stream(ctx, StreamRequest{
		Model:        l.cfg.DefaultModel,
		SystemPrompt: l.cfg.SystemPrompt,
		Messages:     []Message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		l.transition(fmt.Sprintf("open stream failed: %s", err), LStateError)
		_ = l.sink.EmitError(err)
		return nil, fmt.Errorf("backend.Lifecycle: open stream: %w", err)
	}
	return stream, nil
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

	turnID, err := l.startTurn(prompt)
	if err != nil {
		return err
	}

	stream, err := l.openTurnStream(ctx, turnID, prompt)
	if err != nil {
		return err
	}

	return l.consumeStream(ctx, stream)
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

// Compile-time guarantee Lifecycle implements Backend.
var _ Backend = (*Lifecycle)(nil)
