package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

func (l *Lifecycle) startTurn(prompt string) (int64, error) {
	if strings.TrimSpace(prompt) == "" {
		return 0, errors.New("backend.Lifecycle: Run: prompt is required")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, errors.New("backend.Lifecycle: Run after Close")
	}
	if l.state == LStateInit {
		return 0, errors.New("backend.Lifecycle: Run before Init")
	}
	l.turnID++
	turnID := l.turnID
	l.transitionLocked(LStateExecute, fmt.Sprintf("Run started: turn=%d", turnID))
	return turnID, nil
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
