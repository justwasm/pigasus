// Package subagentquestions owns the per-subagent pending-question
// store that the cycle subagent-question-picker feature reads from
// and writes to.
//
// AGY 1.3.6 changelog line 6:
//
//	"Fixed questions asked by subagents and workflow agents being
//	 lost, which left the subagent waiting forever: the alert now
//	 reads `<agent> has a question` and shows it, and Ctrl+K or `a`
//	 in the subagent's view opens a question picker that sends your
//	 answer to that agent. Headless `-p` runs skip these questions
//	 so the subagent can continue."
//
// The data model this commit ships is the storage layer the TUI's
// alert + picker read. The caller (the parent's ask_question tool
// runner, in either the TUI or the print path) is responsible for
// translating a tool call into an Enqueue/Answer pair; the picker
// translates a selection into an Answer.
//
// One Manager instance is shared by the parent session and every
// subagent the parent owns. Each subagent is identified by a stable
// human-readable string — the subagent_id the subagent's
// `invoke_subagent` result surfaces (cycle 228's
// subagenttools.Result.SubagentID, e.g. "subagent-1761234567-3").
// The parent itself is keyed as the empty string, so the parent's
// own ask_question calls use the same data model without a special
// case.
//
// Concurrency: every method takes the package's mutex so a
// parent's ask_question call and a picker's drain can race
// without corrupting the pending slices. Operations are O(N) over
// the per-subagent list, which is bounded by user patience rather
// than code shape; the picker never has to scan more than a handful
// of entries at once.
package subagentquestions

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// State values a Question carries during its lifetime.
//
//   - StatePending: queued; the user has not yet seen it.
//   - StateAnswered: the user (or a headless skip) supplied an
//     answer; the subagent can read it via Answered(id).
//   - StateSkipped: the headless -p path answered on the user's
//     behalf with the literal text "(skipped)" so the subagent
//     continues without a real reply.
const (
	StatePending  = "pending"
	StateAnswered = "answered"
	StateSkipped  = "skipped"
)

// ParentKey is the SubagentID value the parent session uses for its
// own ask_question calls. Empty string distinguishes the parent
// from any real subagent (whose IDs the subagenttools package
// mints as "subagent-<unix-nano>-<counter>").
const ParentKey = ""

// SkipAnswer is the literal the headless -p path reports back to
// the subagent when the question is auto-skipped. The model can
// branch on this text without a separate state field; the
// subagent's loop sees "(skipped)" and continues without waiting.
const SkipAnswer = "(skipped)"

// Question is one ask_question the user (or headless skip) has yet
// to answer.
//
// Fields:
//
//   - ID: stable across the queue; pickers select by ID, drain
//     returns it for the caller to match on.
//   - SubagentID: the parent ("") or the named subagent that
//     asked. The picker's title row uses this string.
//   - Prompt: the literal question text the model's ask_question
//     call passed. The picker renders this verbatim.
//   - Options: optional structured choices (ask_question's
//     "options" array). Empty when the question is freeform.
//   - CreatedAt: monotonic enough for the alert order.
//   - State: pending / answered / skipped.
//   - Answer: the user's typed answer or SkipAnswer.
//   - AnsweredAt: zero while pending.
type Question struct {
	ID         string
	SubagentID string
	Prompt     string
	Options    []string
	CreatedAt  time.Time
	State      string
	Answer     string
	AnsweredAt time.Time

	chMu      sync.Mutex  // guards lazy creation of answerCh + Answer/State writes
	answerCh  chan string // closed on Answer; callers Wait on it
	closeOnce sync.Once   // guards the single close(answerCh)
}

// Wait blocks until the question transitions out of StatePending
// (answered or skipped). It returns the Answer field's final value
// when the wait completes. The second return value is true when the
// wait observed the transition (i.e. an Answer or Skip landed while
// Wait was running, or had landed just before Wait was called and
// was recorded on the channel close); false when the question is
// nil.
//
// A successful wait keeps the Question in the Manager; the caller
// is expected to look it up by ID after Wait returns to read the
// Answer field's final value if it needs to re-check.
//
// The wait is one-shot: callers that need the same value
// repeatedly should cache the return and not re-Wait — the
// underlying channel is closed after the first Answer.
func (q *Question) Wait() (answer string, ok bool) {
	if q == nil {
		return "", false
	}
	// Lazily create the answer channel under the chMu mutex
	// (separate from the once that guards the close). This lets
	// Wait and setAnswer both run their respective setup
	// paths independently — only the close itself is once.
	q.chMu.Lock()
	if q.answerCh == nil {
		q.answerCh = make(chan string)
	}
	ch := q.answerCh
	q.chMu.Unlock()
	ans, open := <-ch
	if !open {
		// answerCh was closed (Answer set the value before
		// Wait was called). Re-read the Answer field for the
		// value; channel close does not carry data.
		return q.Answer, true
	}
	return ans, true
}

// setAnswer transitions the question out of StatePending. The
// answerCh close is wrapped in a sync.Once so a double-Answer (a
// race between the picker and an external resolver) never
// deadlocks Wait. The second caller sees its Answer silently
// overwritten; both return the same final value because the
// Manager's setter wins.
func (q *Question) setAnswer(answer string, state string) {
	// Set the answer and state under chMu so a concurrent Wait
	// that just grabbed the channel pointer sees the final
	// value too. The close itself is once-guarded.
	q.chMu.Lock()
	q.Answer = answer
	q.State = state
	q.AnsweredAt = time.Now()
	if q.answerCh == nil {
		q.answerCh = make(chan string)
	}
	ch := q.answerCh
	q.chMu.Unlock()
	q.closeOnce.Do(func() { close(ch) })
}

// Manager owns the per-subagent pending question lists. One
// instance lives on the runMode (TUI) and on the print-pig
// runtime; both modes call Enqueue/Answer on the same Manager
// when their ask_question tool fires.
//
// The internal store is a map from SubagentID to a slice of
// pending Question pointers. The slices are append-only: a
// answered question stays in the slice for the picker's
// "answered" tab and for the agent's transcript replay, but its
// State tells the caller whether the question is still waiting.
type Manager struct {
	mu         sync.Mutex
	pending    map[string][]*Question
	idCounter  uint64
	skipInHead bool // true for the print path: every Enqueue is auto-skipped
}

// NewManager constructs a Manager with no questions queued. The
// headless flag controls whether Enqueue auto-skips (true for
// print mode — see the AGY 1.3.6 changelog line "Headless -p runs
// skip these questions so the subagent can continue").
func NewManager(headless bool) *Manager {
	return &Manager{
		pending:    make(map[string][]*Question),
		skipInHead: headless,
	}
}

// Headless reports whether the Manager was constructed with the
// headless flag. The TUI uses this to render a different alert
// when questions will auto-skip, but the flag's only behavioural
// effect is on Enqueue.
func (m *Manager) Headless() bool {
	return m == nil || m.skipInHead
}

// Enqueue records a new ask_question call. In headless mode the
// question is immediately transitioned to StateSkipped with the
// SkipAnswer literal, and the returned Question carries the
// skip value in its Answer field (so Wait returns SkipAnswer
// without blocking). In interactive mode the question stays in
// StatePending until Answer or Skip is called.
//
// subAgentID is the subagent's stable ID (ParentKey for the
// parent session itself). prompt is the literal question the
// model passed. options is the structured-choice list — empty
// for freeform questions; the picker renders options verbatim.
//
// The returned Question is registered with the Manager; the
// caller's only responsibility is to Wait on it (or read the
// Answer field later by ID). The ID is unique within the
// Manager; multiple subagents sharing a Manager never collide.
func (m *Manager) Enqueue(subAgentID, prompt string, options []string) *Question {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.idCounter++
	q := &Question{
		ID:         fmt.Sprintf("q-%d-%d", time.Now().UnixNano(), m.idCounter),
		SubagentID: subAgentID,
		Prompt:     prompt,
		Options:    append([]string(nil), options...),
		CreatedAt:  time.Now(),
		State:      StatePending,
	}
	m.pending[subAgentID] = append(m.pending[subAgentID], q)
	if m.skipInHead {
		// Lock is held; setAnswer doesn't need it because the
		// Question is not yet shared with another goroutine.
		q.setAnswer(SkipAnswer, StateSkipped)
	}
	return q
}

// Answer records the user's reply for question id and transitions
// it to StateAnswered. The matching Question's Wait (if any
// caller is blocked on it) unblocks with the answer string. A
// nil Manager or an unknown id is a no-op.
//
// The subAgentID parameter is the caller's own scope guard: the
// picker keys answers by (subAgentID, id) so a stale question
// from a sibling subagent cannot be answered from the wrong
// picker's session. Pass ParentKey for parent questions.
func (m *Manager) Answer(subAgentID, id, answer string) error {
	if m == nil {
		return errors.New("subagentquestions: nil manager")
	}
	m.mu.Lock()
	q, ok := m.findLocked(subAgentID, id)
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("subagentquestions: unknown question id %q for subagent %q", id, subAgentID)
	}
	if q.State != StatePending {
		// A re-Answer or a Skip-then-Answer race. Ignore the
		// second call so the first reply sticks.
		return nil
	}
	q.setAnswer(answer, StateAnswered)
	return nil
}

// Skip is the headless path's Enqueue-time shortcut's match on
// the interactive side. Calling Skip on an already-skipped or
// answered question is a no-op. It exists so the picker can
// offer "Skip this question" alongside an answer prompt.
func (m *Manager) Skip(subAgentID, id string) error {
	if m == nil {
		return errors.New("subagentquestions: nil manager")
	}
	m.mu.Lock()
	q, ok := m.findLocked(subAgentID, id)
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("subagentquestions: unknown question id %q for subagent %q", id, subAgentID)
	}
	if q.State != StatePending {
		return nil
	}
	q.setAnswer(SkipAnswer, StateSkipped)
	return nil
}

// List returns every Question (in insertion order) queued against
// subAgentID. The slice is a defensive copy so the caller can
// iterate without holding the Manager's lock.
//
// The returned slice is mixed-state (pending + answered +
// skipped) so the picker can render both "pending" and
// "history" tabs off the same data.
func (m *Manager) List(subAgentID string) []*Question {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	src := m.pending[subAgentID]
	out := make([]*Question, len(src))
	copy(out, src)
	return out
}

// Pending returns just the StatePending questions for subAgentID,
// in insertion order. The picker uses this for its main list.
func (m *Manager) Pending(subAgentID string) []*Question {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	src := m.pending[subAgentID]
	out := make([]*Question, 0, len(src))
	for _, q := range src {
		if q.State == StatePending {
			out = append(out, q)
		}
	}
	return out
}

// PendingCount is the O(1) Pending(subAgentID) shortcut used by
// the status row's "· N pending questions" badge.
func (m *Manager) PendingCount(subAgentID string) int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, q := range m.pending[subAgentID] {
		if q.State == StatePending {
			n++
		}
	}
	return n
}

// TotalPending sums StatePending counts across every subagent.
// The parent status row uses this for the global "1 subagent
// has a question" alert; a per-subagent breakdown is available
// through AllPending.
func (m *Manager) TotalPending() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, qs := range m.pending {
		for _, q := range qs {
			if q.State == StatePending {
				n++
			}
		}
	}
	return n
}

// AllPending returns every pending question grouped by subagent
// ID. The outer map is a fresh copy on every call. The
// picker's parent-side alert uses this to render "<agent> has a
// question" rows for every subagent with at least one pending
// question.
func (m *Manager) AllPending() map[string][]*Question {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string][]*Question, len(m.pending))
	for sub, qs := range m.pending {
		for _, q := range qs {
			if q.State == StatePending {
				out[sub] = append(out[sub], q)
			}
		}
	}
	return out
}

// PendingSubagents returns the IDs of subagents that currently
// have at least one pending question. The parent alert uses
// this to render the "<agent> has a question" flash; the order
// is map iteration order (intentionally non-deterministic — the
// alert sorts by CreatedAt before rendering).
func (m *Manager) PendingSubagents() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for sub, qs := range m.pending {
		for _, q := range qs {
			if q.State == StatePending {
				out = append(out, sub)
				break
			}
		}
	}
	return out
}

// Drain returns and removes every pending question for subAgentID.
// The picker uses this after the user picks a question and
// answers it (or skips it): the question leaves the pending
// bucket so the global alert stops counting it, but the answered
// question is preserved in Manager.List(subAgentID) for the
// "history" tab.
//
// The returned slice is in insertion order; an empty slice means
// "no pending questions, picker should not open".
func (m *Manager) Drain(subAgentID string) []*Question {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	src := m.pending[subAgentID]
	out := make([]*Question, 0, len(src))
	kept := src[:0]
	for _, q := range src {
		if q.State == StatePending {
			out = append(out, q)
			continue
		}
		kept = append(kept, q)
	}
	m.pending[subAgentID] = kept
	return out
}

// findLocked is the lock-held Question lookup. Callers must hold
// m.mu; the function never blocks.
func (m *Manager) findLocked(subAgentID, id string) (*Question, bool) {
	for _, q := range m.pending[subAgentID] {
		if q.ID == id {
			return q, true
		}
	}
	return nil, false
}
