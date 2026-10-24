package pigrt

import (
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"
)

// CompactionRecord summarises a single auto-compaction event the
// session reported via PiG's agent.CompactionStartEvent / agent.CompactionEndEvent.
type CompactionRecord struct {
	// StartedAt is when the CompactionStartEvent fired (UTC).
	StartedAt time.Time
	// EndedAt is when the CompactionEndEvent fired (UTC). Zero while
	// compact is still running or aborted with no end event.
	EndedAt time.Time
	// Reason is the upstream Reason — "manual", "threshold", or "overflow".
	Reason string
	// TokensBefore is the estimated token count before compacting.
	TokensBefore int
	// EstimatedTokensAfter is the projected post-compact size.
	EstimatedTokensAfter int
	// Aborted is true when the upstream end event flags an abort.
	Aborted bool
}

// HasCompleted reports whether a CompactionEndEvent has been observed for this record.
func (r *CompactionRecord) HasCompleted() bool { return !r.EndedAt.IsZero() }

// Duration is the elapsed wall-clock duration of the most recently observed compact.
func (r *CompactionRecord) Duration() time.Duration {
	if r == nil || r.EndedAt.IsZero() || r.StartedAt.IsZero() {
		return 0
	}
	return r.EndedAt.Sub(r.StartedAt)
}

// EventFunc observes every agent event a turn produces.
type EventFunc func(agent.AgentEvent)

// drain consumes the session's event channel and routes text deltas to the
// run's callback. It also records the most recent auto-compaction event.
func (r *Runtime) drain() {
	defer close(r.drainDone)
	for ev := range r.session.Events() {
		if coding.AcknowledgeEvent(ev) {
			continue
		}
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

// recordCompactionEvent stamps r.lastCompact from a PiG compaction event.
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

// LastCompact returns a snapshot of the most recently observed compaction record.
func (r *Runtime) LastCompact() CompactionRecord {
	r.compactMu.Lock()
	defer r.compactMu.Unlock()
	return r.lastCompact
}

// IsCompacting reports whether a CompactionStartEvent has been observed without a matching CompactionEndEvent.
func (r *Runtime) IsCompacting() bool {
	r.compactMu.Lock()
	defer r.compactMu.Unlock()
	return r.compactActive
}

// InjectForTest feeds an agent.AgentEvent through the runtime's compaction bookkeeping.
func (r *Runtime) InjectForTest(ev agent.AgentEvent) {
	r.recordCompactionEvent(ev)
}

// chainEventFuncs returns an EventFunc that invokes first, then second, on the same event.
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
