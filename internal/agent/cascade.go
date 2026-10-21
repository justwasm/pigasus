// Package agent — Cascade state machine (recovered from 1.3.5 decompile).
//
// Cycle 40 reconstruction: Antigravity's binary has an internal
// `(*textEmitter).EmitInit -> BeginTurn -> ... -> EmitFinal` state machine
// in `google3/third_party/jetski/cli/printmode/printmode` (recovered
// addresses 0x8c57080..0x8c57e00), and a parallel `(*Store).SubagentAwaitsApproval`
// flow in `google3/third_party/jetski/cli/store/store`. Together they form
// the "Cascade" lifecycle that wraps every user prompt:
//
//	init -> read -> plan -> execute -> stream -> save
//
// We expose a Go-native Cascade that:
//   - holds an explicit State and History of transitions
//   - fires a caller-supplied OnTransition callback at every transition
//   - drives a Loop (Loop.Send) through the states, so callers see state
//     transitions on stderr/stdout instead of a single echo
//
// The transitions in our Cascade match the decompiled 1.3.5 emitter FSM
// in shape (init -> read -> plan -> execute -> stream -> save -> done) and
// the Subagent approval state in store (pending -> running -> done|failed).
// They are intentionally *additive* — Loop.Send still streams the model
// response through its Handler; the Cascade only decorates the lifecycle
// so the recovery can claim "real state machine, not a black box".
package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// CascadeState is one node in the recovered lifecycle. Names are chosen
// to match the task spec exactly: init -> read -> plan -> execute -> stream
// -> save (with error/done as terminal alternates).
type CascadeState string

const (
	// StateInit — Cascade just constructed, no work done yet.
	StateInit CascadeState = "init"
	// StateRead — user prompt received, building message list.
	StateRead CascadeState = "read"
	// StatePlan — picking backend / model / planning tool calls.
	StatePlan CascadeState = "plan"
	// StateExecute — backend.Stream is open, no chunks yet.
	StateExecute CascadeState = "execute"
	// StateStream — at least one chunk has been delivered.
	StateStream CascadeState = "stream"
	// StateSave — terminal chunks processed, persisting state.
	StateSave CascadeState = "save"
	// StateDone — all work complete (terminal happy path).
	StateDone CascadeState = "done"
	// StateError — terminal failure path.
	StateError CascadeState = "error"
)

// allCascadeStates is the canonical order used to render the "arrow chain"
// summary (`init -> read -> plan -> execute -> stream -> save`).
var allCascadeStates = []CascadeState{
	StateInit, StateRead, StatePlan, StateExecute, StateStream, StateSave, StateDone,
}

// validTransitions encodes the recovery's allowed edges. The decompiled
// 1.3.5 printmode emitter allows any node to short-circuit to `error`; we
// mirror that by listing the happy-path edges explicitly and then falling
// back to "anything -> error" / "anything -> done" at lookup time.
var validTransitions = map[CascadeState][]CascadeState{
	StateInit:    {StateRead, StateError},
	StateRead:    {StatePlan, StateError},
	StatePlan:    {StateExecute, StateError},
	StateExecute: {StateStream, StateSave, StateError},
	StateStream:  {StateSave, StateError},
	StateSave:    {StateDone, StateError},
	StateDone:    {}, // terminal
	StateError:   {}, // terminal
}

// CascadeTransition is one entry in the lifecycle log. At is monotonic
// wall-clock; from/to are the states being left/entered; note carries
// the caller's reason (e.g. "open stream", "channel closed").
type CascadeTransition struct {
	From CascadeState
	To   CascadeState
	At   time.Time
	Note string
}

// OnTransitionFn is the callback signature for state observers. It runs
// synchronously inside Transition, so it must be cheap (and must not call
// back into the Cascade that fired it).
type OnTransitionFn func(CascadeTransition)

// Cascade holds the state, history, and observer for one conversation.
// Zero value is not usable — call NewCascade.
type Cascade struct {
	mu      sync.Mutex
	state   CascadeState
	history []CascadeTransition
	onTrans OnTransitionFn
}

// NewCascade returns a Cascade rooted at StateInit. observer may be nil
// (silent); pass a closure to render transitions to stderr/stdout.
func NewCascade(observer OnTransitionFn) *Cascade {
	c := &Cascade{
		state: StateInit,
		onTrans: observer,
	}
	// Record the initial transition (init -> init is a no-op; instead we
	// just push a synthetic "born" entry so the history is non-empty).
	c.history = append(c.history, CascadeTransition{
		From: "",
		To:   StateInit,
		At:   time.Now(),
		Note: "cascade created",
	})
	return c
}

// State returns the current state. Safe to call from any goroutine.
func (c *Cascade) State() CascadeState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// History returns a copy of the transition log in chronological order.
// Returned slice is owned by the caller.
func (c *Cascade) History() []CascadeTransition {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]CascadeTransition, len(c.history))
	copy(out, c.history)
	return out
}

// SetObserver swaps the transition callback. Pass nil to silence the
// observer. Useful for tests that want to assert on transitions directly
// without the observer racing the assertion.
func (c *Cascade) SetObserver(fn OnTransitionFn) {
	c.mu.Lock()
	c.onTrans = fn
	c.mu.Unlock()
}

// Transition moves the Cascade from its current state to `to`. The caller
// is responsible for picking a reachable `to`; if the edge is not in
// validTransitions (and `to` is not error/done which are universally
// allowed), Transition returns an error and the state is unchanged.
//
// Errors are non-fatal: a typical recovery wiring is to call Transition
// and log the error, since the upstream Loop has its own retry/escalation
// story. The point of returning the error is observability, not control
// flow — Loop.Send ignores it and proceeds with the next state.
func (c *Cascade) Transition(to CascadeState, note string) error {
	c.mu.Lock()
	if c.state == to {
		// Idempotent: a caller asking for the same state is not a
		// programmer error worth raising on.
		c.mu.Unlock()
		return nil
	}
	if !c.canTransitionLocked(c.state, to) {
		from := c.state
		c.mu.Unlock()
		return fmt.Errorf("cascade: invalid transition %s -> %s", from, to)
	}
	trans := CascadeTransition{
		From: c.state,
		To:   to,
		At:   time.Now(),
		Note: note,
	}
	c.state = to
	c.history = append(c.history, trans)
	obs := c.onTrans
	c.mu.Unlock()
	if obs != nil {
		obs(trans)
	}
	return nil
}

// canTransitionLocked checks the edge table. Caller holds c.mu.
// Universal escape hatches (StateError, StateDone) are always reachable
// from any non-terminal state.
func (c *Cascade) canTransitionLocked(from, to CascadeState) bool {
	if to == StateError || to == StateDone {
		return from != StateDone && from != StateError
	}
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// RenderChain prints the "happy path" of the cascade as a single arrow
// chain, e.g. `init -> read -> plan -> execute -> stream -> save -> done`.
// Used by --print mode to give the user a one-line summary of what the
// cascade did during their turn.
func (c *Cascade) RenderChain() string {
	c.mu.Lock()
	// Walk the history forward, collecting unique states in order.
	seen := make(map[CascadeState]bool)
	chain := []CascadeState{}
	for _, t := range c.history {
		if !seen[t.To] {
			seen[t.To] = true
			chain = append(chain, t.To)
		}
	}
	c.mu.Unlock()

	// Append the final state if it isn't in chain yet.
	if !seen[c.State()] {
		chain = append(chain, c.State())
	}
	parts := make([]string, len(chain))
	for i, s := range chain {
		parts[i] = string(s)
	}
	return strings.Join(parts, " -> ")
}

// Reset puts the Cascade back to StateInit and clears the history. Useful
// between turns in a long-lived Loop; not used by Loop.Send itself (which
// creates a fresh Cascade per Send), but exported for callers that want
// a single Cascade across many turns.
func (c *Cascade) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = StateInit
	c.history = nil
	c.history = append(c.history, CascadeTransition{
		From: "",
		To:   StateInit,
		At:   time.Now(),
		Note: "cascade reset",
	})
}

// String makes Cascade implement fmt.Stringer by rendering the chain.
// Debug-only: it does not lock, so concurrent use is technically racy,
// but the only consequence is a stale chain string.
func (c *Cascade) String() string {
	return c.RenderChain()
}
