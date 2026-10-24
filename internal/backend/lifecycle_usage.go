package backend

import (
	"sync"
)

// UsageTracker accumulates prompt / completion / total token
// counts across a session. Zero value is ready to use.
//
// UsageTracker is the recovered "battle" counter that aggregates
// per-turn prompt + completion token counts across a session.
// The v1.3.5 binary emits a `[usage] prompt=N completion=N
// total=N fp=...` line on every terminal stream frame; the
// recovered lifecycle captures the same frame (the gateway
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
