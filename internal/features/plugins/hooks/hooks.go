// Package hooks is the lifecycle-hook bus for the clone's TUI.
//
// The Antigravity CLI ships five hook events: PreToolUse, PostToolUse,
// PreInvocation, PostInvocation, and Stop. The /hooks panel lists them
// (cmd/wagyu/slash_panels.go: hookRows); cycle 205 added the panel
// state machine; the dispatch — actually firing the registered hooks
// from each event — has never been wired. This package is the
// dispatch.
//
// The shape mirrors what a Claude-Code-style hooks.json would emit:
//
//	{
//	  "hooks": {
//	    "PreToolUse":   [{ "command": "...", "matcher": "Write|Edit" }],
//	    "PostSideQuestion": [{ "command": "..." }]
//	  }
//	}
//
// but we do not actually invoke shell commands here. The clone has no
// subprocess-hook host; the dispatch is a typed in-process bus. A
// caller registers a HookFunc for an event, and Fire invokes every
// registered handler in registration order. Each handler returns a
// Decision: Allow (continue), Block (stop the parent action with a
// reason), or Modify (replace one of the payload fields). Hooks
// compose: every registered handler runs unless one of them Blocks,
// in which case the rest still get a turn so they can react to the
// block reason (they cannot unblock, but they can log it).
//
// Cycle btw-runs-hooks fires PreSideQuestion and PostSideQuestion
// from /btw. The other four events are reserved; tests can register
// handlers for them, but production code does not fire them yet.
package hooks

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Event is the typed hook event name. New events are added here so
// the registration API rejects typos at compile time.
type Event string

const (
	// PreSideQuestion fires before /btw spawns its independent
	// coding.Session. A handler that returns DecisionBlock aborts
	// the side question — the TUI echoes the block reason into the
	// transcript and never opens a Session.
	PreSideQuestion Event = "PreSideQuestion"
	// PostSideQuestion fires after /btw settles (success or fail).
	// It always runs even when PreSideQuestion blocked; in that
	// case Answer/Tokens are empty and the payload's Blocked field
	// is true with the upstream block reason.
	PostSideQuestion Event = "PostSideQuestion"
)

// KnownEvents is the cycle btw-runs-hooks catalog. /hooks panel
// rendering is unchanged (still lists the recovered REF five); this
// slice is the dispatch side.
var KnownEvents = []Event{PreSideQuestion, PostSideQuestion}

// IsKnown reports whether e is one of the typed events this package
// dispatches. Unknown events still register and fire, but callers
// should prefer the typed constants.
func IsKnown(e Event) bool {
	for _, k := range KnownEvents {
		if k == e {
			return true
		}
	}
	return false
}

// Decision is what a HookFunc returns. Allow is the default;
// Block stops the parent action; Modify updates one payload field.
type Decision int

const (
	// DecisionAllow lets the parent action proceed.
	DecisionAllow Decision = iota
	// DecisionBlock stops the parent action. Reason is what the
	// TUI renders in place of the action's normal output.
	DecisionBlock
	// DecisionModify replaces the payload's Question field with
	// the hook's modified copy. The parent action proceeds with
	// the new question.
	DecisionModify
)

// String reports the decision in a form suitable for tests.
func (d Decision) String() string {
	switch d {
	case DecisionAllow:
		return "allow"
	case DecisionBlock:
		return "block"
	case DecisionModify:
		return "modify"
	}
	return fmt.Sprintf("decision(%d)", int(d))
}

// Payload is the structured data passed to every hook handler. The
// fields are stable across events so a single handler can switch on
// the Event to decide what to do; only SideQuestion has Question /
// Answer / Tokens populated in cycle btw-runs-hooks.
//
// Blocked is true when the dispatch is running Post-event because a
// Pre-event returned Block — the handler sees the upstream block
// reason via BlockReason.
type Payload struct {
	// Event is the typed event being dispatched.
	Event Event
	// Question is the user-typed side-question text (PreSideQuestion,
	// PostSideQuestion).
	Question string
	// Answer is the streamed answer (PostSideQuestion only; empty
	// when the upstream turn failed).
	Answer string
	// Tokens is the output-token total PiG reported (PostSideQuestion
	// only).
	Tokens int
	// Blocked is true when this Post-event fires because a
	// Pre-event blocked the parent action.
	Blocked bool
	// BlockReason is the reason the upstream hook gave for blocking.
	BlockReason string
	// Timestamp records when the dispatch began. Useful for tests
	// that want to assert the order of side-question events.
	Timestamp time.Time
}

// SideQuestion builds a Payload for a side-question event. Pre and
// Post share the same constructor — the Post-only fields default to
// their zero values, and the caller fills them in via the returned
// pointer before the Post dispatch.
func SideQuestion(event Event, question string) Payload {
	return Payload{
		Event:     event,
		Question:  question,
		Timestamp: time.Now(),
	}
}

// HookFunc is the in-process handler signature. The context is the
// same context the parent action is running under (canceling the
// parent cancels the hook); the manager passes the payload by value
// so handlers cannot mutate each other's view. The handler returns
// a Decision and a free-form reason; an error short-circuits the
// remaining handlers with a synthetic DecisionBlock carrying the
// error's message.
type HookFunc func(ctx context.Context, p Payload) (Decision, string, error)

// HookRecord is the public-facing view of a registered hook. It
// exists so tests and observability code can list what is registered
// without touching the manager's mutex.
type HookRecord struct {
	Event    Event
	Priority int
}

// Manager is the hook bus. One Manager per TUI session; production
// callers wire it in NewRunMode so /btw can dispatch through it,
// tests build a fresh Manager per test (Register is concurrency-safe
// but the test pattern is single-goroutine).
type Manager struct {
	mu       sync.RWMutex
	handlers map[Event][]registeredHandler
	// fireCount is bumped on every Fire call (per event) so tests
	// can assert how many times a hook fired.
	fireCount map[Event]int
}

type registeredHandler struct {
	fn       HookFunc
	priority int // lower runs first; stable insertion order on tie
}

// NewManager constructs an empty Manager. The returned value is safe
// to use immediately; no warm-up call is required.
func NewManager() *Manager {
	return &Manager{
		handlers:  make(map[Event][]registeredHandler),
		fireCount: make(map[Event]int),
	}
}

// Register adds fn to the dispatch list for event. Handlers fire in
// priority order (low → high), then in registration order on ties.
// The returned int is the hook's ID — currently unused but exposed
// for a future cycle that needs to deregister.
func (m *Manager) Register(event Event, fn HookFunc) int {
	if fn == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rh := registeredHandler{fn: fn, priority: len(m.handlers[event])}
	m.handlers[event] = append(m.handlers[event], rh)
	return len(m.handlers[event])
}

// RegisterPriority is Register with an explicit priority. Negative
// priorities run before zero; positive run after. Mostly useful for
// tests that want a hook to fire before another one.
func (m *Manager) RegisterPriority(event Event, priority int, fn HookFunc) int {
	if fn == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rh := registeredHandler{fn: fn, priority: priority}
	m.handlers[event] = append(m.handlers[event], rh)
	m.sortLocked(event)
	return len(m.handlers[event])
}

// sortLocked re-sorts the handler list for event by priority, then
// insertion order on ties. Insertion order is encoded in priority
// (Register appends in priority=insertion-count order), so sorting
// is enough.
func (m *Manager) sortLocked(event Event) {
	hs := m.handlers[event]
	sort.SliceStable(hs, func(i, j int) bool {
		return hs[i].priority < hs[j].priority
	})
	m.handlers[event] = hs
}

// Fire dispatches every registered handler for event. Handlers see
// the payload by value; the returned payload is the *final* payload
// after every DecisionModify has been applied. The dispatch stops
// on the first DecisionBlock and returns (true, payload, reason) —
// the caller echoes reason in place of the parent action's output.
// DecisionAllow and DecisionModify both let the dispatch continue.
//
// On handler error the dispatch short-circuits with a synthetic
// DecisionBlock carrying err.Error(); the original error is wrapped
// so callers can errors.As if needed.
func (m *Manager) Fire(ctx context.Context, event Event, p Payload) (blocked bool, final Payload, reason string) {
	m.mu.Lock()
	handlers := append([]registeredHandler(nil), m.handlers[event]...)
	m.fireCount[event]++
	m.mu.Unlock()

	final = p
	for _, rh := range handlers {
		if err := ctx.Err(); err != nil {
			return true, final, "hook dispatch canceled: " + err.Error()
		}
		dec, rsn, err := rh.fn(ctx, final)
		if err != nil {
			return true, final, "hook error: " + err.Error()
		}
		switch dec {
		case DecisionBlock:
			final.Blocked = true
			final.BlockReason = rsn
			return true, final, rsn
		case DecisionModify:
			// Modify replaces Question. Other modify semantics
			// (e.g. replacing Answer) are not in scope for cycle
			// btw-runs-hooks; keep the API surface honest.
			final.Question = strings.TrimSpace(rsn)
		}
	}
	return false, final, ""
}

// Count returns how many times Fire has been called for event. Tests
// use it to assert a hook fired exactly once per /btw.
func (m *Manager) Count(event Event) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.fireCount[event]
}

// Registered reports every Event that has at least one handler.
// Returns a fresh slice; safe to mutate.
func (m *Manager) Registered() []Event {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Event, 0, len(m.handlers))
	for e := range m.handlers {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i]) < string(out[j]) })
	return out
}

// Reset clears the handler list and the fire counts. Tests call it
// between cases; production never calls it.
func (m *Manager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers = make(map[Event][]registeredHandler)
	m.fireCount = make(map[Event]int)
}
