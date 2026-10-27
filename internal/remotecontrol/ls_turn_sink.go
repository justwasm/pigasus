// Package remotecontrol — ls_turn_sink.go: the sink that publishes a
// turn onto the trajectory while the model is still talking.
//
// The Web UI renders a conversation from the trajectory, so a turn that
// is only written when it ends is a turn the user watches nothing
// happen through. This sink writes every delta onto the trajectory as
// it arrives: the answer grows in place, the tool calls appear one by
// one, and the thinking becomes visible before the first word of the
// answer.
//
// 【一手】 the Web UI's planner-response renderer reads the thinking
// off the step (`rawThinking || thinking`) and the "Thought for Ns"
// label off `thinkingDuration.seconds`, and the live label counts
// from `metadata.viewableAt`. A step that is published only at the end
// of the turn shows all three at once — or none of them.
package remotecontrol

import (
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/convstore"
)

// lsStepStatusGenerating is exa.cortex_pb.CortexStepStatus.GENERATING —
// the step is still being written.
const lsStepStatusGenerating = 8

// lsTrajectorySink publishes a turn's steps onto the trajectory.
//
// One model message is one planner-response step: a tool call ends the
// message that asked for it, so the next message starts a new step.
// That is the order the reference's own trajectory has — the answer,
// then the tool call it triggered, then the next answer.
type lsTrajectorySink struct {
	inner EventSink
	traj  *lsTrajectory

	mu        sync.Mutex
	replyStep int
	reply     strings.Builder
	thinking  strings.Builder
	viewable  time.Time
	thinkFrom time.Time
	thinkTo   time.Time
	tools     int
	// stored holds the turn's steps in the shape the conversation
	// store keeps them. The store's rows are the reference's own, so
	// the turn is appended to them in the order the reference uses:
	// the answer, then the tool calls it triggered, then the next
	// answer.
	stored []convstore.Step
}

// newLSTrajectorySink returns a sink that writes onto traj and
// forwards every event to inner.
func newLSTrajectorySink(traj *lsTrajectory, inner EventSink) *lsTrajectorySink {
	return &lsTrajectorySink{traj: traj, inner: inner, replyStep: -1}
}

// OnState forwards the state frame.
func (s *lsTrajectorySink) OnState(cascade *Cascade, frame StateFrame) {
	if s.inner != nil {
		s.inner.OnState(cascade, frame)
	}
}

// OnTextDelta publishes the answer as it grows.
func (s *lsTrajectorySink) OnTextDelta(cascade *Cascade, delta string) {
	if s.inner != nil {
		s.inner.OnTextDelta(cascade, delta)
	}
	if delta == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reply.WriteString(delta)
	s.publishLocked(lsStepStatusGenerating, 0)
}

// OnThought publishes the thinking as it grows.
//
// The thinking is published before the first word of the answer, so
// the Web UI's collapsible panel opens on the model's reasoning and
// not on the answer.
func (s *lsTrajectorySink) OnThought(cascade *Cascade, text string) {
	if text == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.thinkFrom.IsZero() {
		s.thinkFrom = now
	}
	s.thinkTo = now
	s.thinking.WriteString(text)
	s.publishLocked(lsStepStatusGenerating, 0)
}

// OnToolCall publishes the tool call as it completes.
//
// The tool call ends the message that asked for it, so the reply step
// is closed before the tool-call step is appended.
func (s *lsTrajectorySink) OnToolCall(cascade *Cascade, call ToolCall) {
	if s.inner != nil {
		s.inner.OnToolCall(cascade, call)
	}
	if call.Name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools++
	s.closeReplyLocked(lsStepStatusDone, s.thinkingDurationLocked())
	s.traj.Append(lsToolCallStep(call.Name, string(call.Args), call.Result))
	s.stored = append(s.stored, convstore.Step{
		Type:       convstore.StepToolCall,
		Status:     lsStepStatusDone,
		ToolName:   call.Name,
		ToolArgs:   string(call.Args),
		ToolResult: call.Result,
	})
}

// OnError forwards the error.
func (s *lsTrajectorySink) OnError(cascade *Cascade, err error) {
	if s.inner != nil {
		s.inner.OnError(cascade, err)
	}
}

// OnUserInteraction forwards the interaction request.
func (s *lsTrajectorySink) OnUserInteraction(cascade *Cascade, intr *PendingInteraction) {
	if s.inner != nil {
		s.inner.OnUserInteraction(cascade, intr)
	}
}

// Finish closes the turn: the last reply step is marked done and the
// thinking duration is stamped on it.
//
// The result carries the tool calls the turn made. The ones the sink
// already published are not repeated; the ones it never saw — a
// backend that does not stream — are appended now.
func (s *lsTrajectorySink) Finish(res MessageResult, duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reply.Len() == 0 && res.Reply != "" {
		s.reply.WriteString(lsStripThink(res.Reply))
	}
	for _, tc := range lsUnseenToolCalls(res.ToolCalls, s.tools) {
		if tc.Name == "" {
			continue
		}
		s.traj.Append(lsToolCallStep(tc.Name, string(tc.Args), tc.Result))
		s.stored = append(s.stored, convstore.Step{
			Type:       convstore.StepToolCall,
			Status:     lsStepStatusDone,
			ToolName:   tc.Name,
			ToolArgs:   string(tc.Args),
			ToolResult: tc.Result,
		})
	}
	s.closeReplyLocked(s.finishStatus(), duration)
}

// finishStatus is the status the turn's last step ends with.
//
// A turn the user stopped ends interrupted: the Web UI shows the badge
// "INTERRUPTED" instead of a finished answer that stops mid-sentence.
//
// The trajectory's flag is still set when the sink is finished with —
// the trajectory clears it only after the turn returned (see
// lsTrajectory.FinishTurn).
func (s *lsTrajectorySink) finishStatus() int {
	if s.traj != nil && s.traj.Interrupted() {
		return lsStepStatusInterrupted
	}
	return lsStepStatusDone
}

// lsUnseenToolCalls returns the tool calls the sink did not publish
// yet.
func lsUnseenToolCalls(calls []ToolCall, seen int) []ToolCall {
	if seen < 0 || seen >= len(calls) {
		return nil
	}
	return calls[seen:]
}

// closeReplyLocked marks the current reply step done and starts a new
// one for the next message.
func (s *lsTrajectorySink) closeReplyLocked(status int, duration time.Duration) {
	if s.replyStep < 0 {
		return
	}
	s.publishLocked(status, duration)
	thinking := strings.TrimSpace(s.thinking.String())
	if reply := strings.TrimSpace(s.reply.String()); reply != "" {
		s.stored = append(s.stored, convstore.Step{
			Type:   convstore.StepModelResponse,
			Status: lsStepStatusDone,
			Text:   reply,
			// The thinking and its duration go into the stored blob
			// as well: the reference stores both, and the Web UI
			// renders the "Thought for Ns" panel from them. A step
			// without them shows the answer alone once the page is
			// reloaded and the history comes from the store.
			Thinking:         thinking,
			ThinkingDuration: duration,
		})
	}
	s.replyStep = -1
	s.reply.Reset()
	s.thinking.Reset()
	s.viewable = time.Time{}
	s.thinkFrom = time.Time{}
	s.thinkTo = time.Time{}
}

// StoredSteps returns the turn's steps in the shape the conversation
// store keeps them, in the order the reference stores them.
//
// The user's own message is not part of it: the caller adds it, so
// that a turn whose model never answered still leaves the message
// behind.
func (s *lsTrajectorySink) StoredSteps() []convstore.Step {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]convstore.Step, len(s.stored))
	copy(out, s.stored)
	return out
}

// thinkingDurationLocked is how long the current message's thinking
// took.
func (s *lsTrajectorySink) thinkingDurationLocked() time.Duration {
	if s.thinkFrom.IsZero() || s.thinkTo.IsZero() {
		return 0
	}
	return s.thinkTo.Sub(s.thinkFrom)
}

// publishLocked writes the current reply step onto the trajectory —
// creating it the first time and replacing it afterwards.
//
// A zero duration leaves the thinking duration off the step: the Web
// UI counts the live label from `metadata.viewableAt` and only reads
// the duration once the step is done.
func (s *lsTrajectorySink) publishLocked(status int, duration time.Duration) {
	if s.traj == nil {
		return
	}
	reply := s.reply.String()
	thinking := strings.TrimSpace(s.thinking.String())
	if reply == "" && thinking == "" {
		return
	}
	if s.viewable.IsZero() {
		s.viewable = time.Now()
	}
	step := lsPlannerResponseStepAt(status, reply, thinking, duration, s.viewable)
	if s.replyStep < 0 {
		s.replyStep = s.traj.Append(step)
		return
	}
	s.traj.Update(s.replyStep, step)
}
