// Package supervisor provides SessionSupervisor, which encapsulates and manages
// the conversation transcript, implicit trajectory tracking, and durable trajectory state.
//
// By moving these lifecycle states out of the TUI runMode God Object, session state
// can be tested in isolation and reused across TUI, Headless, and Server runtimes.
package supervisor

import (
	"sync"

	cumulativeprompt "github.com/justwasm/pigasus/internal/app/prompt/cumulative"
	"github.com/justwasm/pigasus/internal/app/prompt/implicit"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/rewind"
	"github.com/justwasm/pigasus/internal/core/backend"
)

// Supervisor coordinates active conversation messages, implicit shell trajectories,
// and conversation persistence tracking.
type Supervisor struct {
	mu sync.RWMutex

	// session contains the chat turn messages in order.
	session []backend.Message

	// implicitMsgs holds messages converted from the implicit trajectory (! shell commands).
	implicitMsgs []backend.Message
	// pendingCheckpoints are the checkpoints a turn recorded but has
	// not inserted into the call list yet. Their insertion points are
	// the call counts that preceded them.
	pendingCheckpoints     []convstore.Step
	pendingCheckpointAfter []int

	// implicitCursor records how many implicit trajectory steps were processed.
	implicitCursor int

	// Trajectory persistence tracking
	trajLoaded    []convstore.Step
	trajCalls     []convstore.Step
	trajSeededLen int

	// CWD for implicit trajectory workspace scoping
	cwd string
}

// New creates a new SessionSupervisor for the given workspace directory.
func New(cwd string) *Supervisor {
	return &Supervisor{
		cwd: cwd,
	}
}

// SetCWD updates the workspace directory for implicit trajectory scoping.
func (s *Supervisor) SetCWD(cwd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cwd = cwd
}

// Messages returns a copy of the active session messages.
func (s *Supervisor) Messages() []backend.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.session) == 0 {
		return nil
	}
	out := make([]backend.Message, len(s.session))
	copy(out, s.session)
	return out
}

// Len returns the count of active session messages.
func (s *Supervisor) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.session)
}

// Append adds one or more messages to the session.
func (s *Supervisor) Append(msgs ...backend.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = append(s.session, msgs...)
}

// MessageAt returns the message at index i. The second result is false
// when i is out of range, so callers never have to bracket the read with
// a separate Len call.
func (s *Supervisor) MessageAt(i int) (backend.Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if i < 0 || i >= len(s.session) {
		return backend.Message{}, false
	}
	return s.session[i], true
}

// LastMessage returns the newest message in the session.
func (s *Supervisor) LastMessage() (backend.Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.session) == 0 {
		return backend.Message{}, false
	}
	return s.session[len(s.session)-1], true
}

// SetMessageContent replaces the content of the message at index i. It is
// the write side of a streaming reply, whose final text replaces the
// placeholder the first chunk created. It reports whether the index was
// in range.
func (s *Supervisor) SetMessageContent(i int, content string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= len(s.session) {
		return false
	}
	s.session[i].Content = content
	return true
}

// AppendMessageContent appends delta to the content of the message at
// index i. It reports whether the index was in range.
func (s *Supervisor) AppendMessageContent(i int, delta string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= len(s.session) {
		return false
	}
	s.session[i].Content += delta
	return true
}

// Truncate keeps the first n messages and drops the rest. A negative n is
// treated as zero, so a truncation never leaves a negative length behind.
func (s *Supervisor) Truncate(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < 0 {
		n = 0
	}
	if n >= len(s.session) {
		return
	}
	s.session = s.session[:n]
	if s.trajSeededLen > n {
		s.trajSeededLen = n
	}
}

// Replace replaces the entire active session with the provided messages.
func (s *Supervisor) Replace(msgs []backend.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(msgs) == 0 {
		s.session = nil
		s.trajSeededLen = 0
		return
	}
	s.session = make([]backend.Message, len(msgs))
	copy(s.session, msgs)
	if s.trajSeededLen > len(s.session) {
		s.trajSeededLen = len(s.session)
	}
}

// Clear resets the active conversation session and trajectory tracking.
func (s *Supervisor) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = nil
	s.trajLoaded = nil
	s.trajCalls = nil
	s.trajSeededLen = 0
}

// ImplicitMessages returns a copy of messages converted from the implicit trajectory.
func (s *Supervisor) ImplicitMessages() []backend.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.implicitMsgs) == 0 {
		return nil
	}
	out := make([]backend.Message, len(s.implicitMsgs))
	copy(out, s.implicitMsgs)
	return out
}

// WireMessages gathers implicit shell messages and active conversation messages
// into a single slice suitable for model submission.
func (s *Supervisor) WireMessages() []backend.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := len(s.implicitMsgs) + len(s.session)
	if total == 0 {
		return nil
	}
	msgs := make([]backend.Message, 0, total)
	msgs = append(msgs, s.implicitMsgs...)
	msgs = append(msgs, s.session...)
	return msgs
}

// CollectImplicit scans disk for any newly appended implicit trajectory steps.
func (s *Supervisor) CollectImplicit() {
	s.mu.Lock()
	defer s.mu.Unlock()

	steps, err := implicit.Open(convstore.StoreDir()).Steps(implicit.WorkspaceURI(s.cwd))
	if err != nil {
		return
	}
	if len(steps) < s.implicitCursor {
		s.implicitMsgs = nil
		s.implicitCursor = 0
	}
	if len(steps) == s.implicitCursor {
		return
	}
	for _, cm := range cumulativeprompt.Convert(steps, cumulativeprompt.Options{From: s.implicitCursor}) {
		s.implicitMsgs = append(s.implicitMsgs, backend.Message{
			Role:    cm.Role,
			Content: cm.Content,
		})
	}
	s.implicitCursor = len(steps)
}

// RewindPopTail pops the last user-assistant pair using rewind policy.
func (s *Supervisor) RewindPopTail() rewind.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := rewind.PopTail(s.session)
	s.session = r.Session
	if s.trajSeededLen > len(s.session) {
		s.trajSeededLen = len(s.session)
	}
	return r
}

// RewindToStep rewinds the session up to the given index.
func (s *Supervisor) RewindToStep(index int) rewind.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := rewind.ToStep(s.session, index)
	s.session = r.Session
	if s.trajSeededLen > len(s.session) {
		s.trajSeededLen = len(s.session)
	}
	return r
}

// RewindSteps returns the available rewind steps for the current session.
func (s *Supervisor) RewindSteps() []rewind.Step {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return rewind.Steps(s.session)
}

// RecordTrajectoryCalls adds executed tool calls to the durable trajectory.
func (s *Supervisor) RecordTrajectoryCalls(steps []convstore.Step) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trajCalls = append(s.trajCalls, steps...)
}

// SetSeededConversation records the conversation steps adopted upon
// resumption. Adopting a trajectory replaces the calls the previous
// conversation made: they belong to the old transcript, not the new one.
func (s *Supervisor) SetSeededConversation(steps []convstore.Step, seededLen int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trajLoaded = steps
	s.trajCalls = nil
	s.trajSeededLen = seededLen
}

// InsertTrajectoryCall inserts a step into the call list at the given
// position. A checkpoint belongs where the turn that recorded it ran, not
// at the end of the list.
func (s *Supervisor) InsertTrajectoryCall(at int, step convstore.Step) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.insertTrajectoryCallLocked(at, step)
}

func (s *Supervisor) insertTrajectoryCallLocked(at int, step convstore.Step) {
	if at < 0 {
		at = 0
	}
	if at > len(s.trajCalls) {
		at = len(s.trajCalls)
	}
	s.trajCalls = append(s.trajCalls, convstore.Step{})
	copy(s.trajCalls[at+1:], s.trajCalls[at:])
	s.trajCalls[at] = step
}

// QueueCheckpoint records a checkpoint and the call count that preceded
// it. The turn flushes the queue once its calls are recorded.
func (s *Supervisor) QueueCheckpoint(step convstore.Step, after int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingCheckpoints = append(s.pendingCheckpoints, step)
	s.pendingCheckpointAfter = append(s.pendingCheckpointAfter, after)
}

// FlushCheckpoints inserts every queued checkpoint at its recorded call
// count and clears the queue.
func (s *Supervisor) FlushCheckpoints() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, cp := range s.pendingCheckpoints {
		s.insertTrajectoryCallLocked(s.pendingCheckpointAfter[i]+i, cp)
	}
	s.pendingCheckpoints = nil
	s.pendingCheckpointAfter = nil
}

// TrajectoryFacts builds the durable Trajectory struct for persistence.
func (s *Supervisor) TrajectoryFacts() convstore.Trajectory {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return convstore.Trajectory{
		Loaded:    s.trajLoaded,
		Calls:     s.trajCalls,
		Session:   s.session,
		SeededLen: s.trajSeededLen,
	}
}

// TrajectoryCounts returns the count of loaded steps and current calls.
func (s *Supervisor) TrajectoryCounts() (loaded int, calls int, seededLen int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.trajLoaded), len(s.trajCalls), s.trajSeededLen
}

// Save writes the conversation to disk using convstore.
func (s *Supervisor) Save(conversationID, autoTitle string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if conversationID == "" || len(s.session) == 0 {
		return nil
	}
	facts := convstore.Trajectory{
		Loaded:    s.trajLoaded,
		Calls:     s.trajCalls,
		Session:   s.session,
		SeededLen: s.trajSeededLen,
	}
	steps := convstore.BuildSteps(facts)
	if len(steps) == 0 {
		return nil
	}
	if err := convstore.SaveConversation(conversationID, s.cwd, steps); err != nil {
		return err
	}
	if autoTitle != "" {
		_ = convstore.WriteSummariesTitle(conversationID, autoTitle)
		_ = convstore.WriteCacheTitle(conversationID, autoTitle)
	}
	return nil
}
