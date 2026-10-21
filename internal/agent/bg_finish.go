// Package agent — bg_finish.go: cycle 259 (AGY 1.4.0), the agent-loop
// gap that lets the clone keep its focus after a background command
// finishes or is canceled.
//
// AGY 1.4.0 release note (changelog_reference.txt:23):
//
//	"Fixed the agent losing track of its place in the conversation
//	 after a background command finishes or is canceled — it now
//	 stays focused on the original task and continues with the next
//	 step after re-reading recent context."
//
// Root cause (recovered from the binary): when a bg command
// finishes, the result lands in the transcript as a generic tool
// reply with no reminder of what the agent was doing when the bg
// task was kicked off. The clone's Loop had no seam to capture
// the original task + plan step pointer at bg-start time, so the
// post-finish turn either asked the model to re-plan from scratch
// or jumped past the planned next step. Cycle 167's
// `Background-step cache eviction` row in feature_diff_table.json
// closed the symptom on the panel side (the live-steps invalidation
// stops tasks / file changes / session cost from rolling back); this
// cycle closes the agent-loop side — the model itself keeps the
// original task in mind and resumes from the step it was on.
//
// BgFinishCoordinator is the new seam. It pins the agent's
// original task + plan step pointer at SetPlan time and, when the
// bg task transitions to a terminal state (finished / failed /
// canceled), emits a task-notification transcript chunk that
// contains BOTH the bg result AND a one-line "you were working on:
// <original task>" reminder. The plan index is preserved across
// the interrupt — it does NOT advance — and the OnReorient hook
// gives the caller a place to re-feed recent transcript context
// so the agent's next Send has the same ground truth as before the
// bg task.
//
// All methods are safe for concurrent use. The mutex serialises
// plan / index reads with the bg-finish notification so a late
// reader cannot observe a half-written notification. The recent
// transcript tail has its own mutex because RecordTranscriptChunk
// can be called from a different goroutine than NotifyBgFinished
// (the TUI's streaming goroutine vs. the bg-finish goroutine in
// internal/tools/support.go).
//
// Zero third-party deps: just stdlib. The research-tool half of the
// repo is stdlib-only (AGENTS.md), and the cycle's hard constraint
// pins it for this package too.
package agent

import (
	"strconv"
	"strings"
	"sync"
)

// PolicyMode labels the user's agent policy selection across a bg
// interrupt. Cycle 259's contract: the value is captured at SetPlan
// time and PolicyMode() reads it back unchanged after the bg task
// finishes, so a tool call from /plan mode does not silently fall
// back to /auto. The agent package defines its own enum so the
// coordinator does not need to import cmd/wagyu's permissions
// types; cmd/wagyu maps PolicyMode <-> permissions.ModeXxx at the
// seam.
type PolicyMode string

const (
	// PolicyDefault is the unset / "first run" mode.
	PolicyDefault PolicyMode = "default"
	// PolicyAuto is the auto-approve mode.
	PolicyAuto PolicyMode = "auto"
	// PolicyPlan is the read-only plan mode.
	PolicyPlan PolicyMode = "plan"
)

// BgFinishNotification is what NotifyBgFinished emits to OnBgFinish.
// TaskID is the bg task's id; Status is "finished" / "failed" /
// "canceled"; Output is the captured stdout/stderr text (may be
// empty). OriginalTask and PlanIndex are the values the coordinator
// had at bg-start time so the formatter can include the reminder
// line even when the bg output was empty (the AGY 1.4.0 contract:
// the fix must work even if the bg command's final output was
// empty).
type BgFinishNotification struct {
	TaskID       string
	Status       string
	Output       string
	OriginalTask string
	PlanIndex    int
	TotalSteps   int
}

// BgFinishCoordinator pins the agent's focus state across the
// lifecycle of one background command. One coordinator is owned by
// the runMode (or pigrt.Runtime); SetPlan / RecordTranscriptChunk /
// NotifyBgFinished are called in that order:
//
//  1. The user prompt arrives; SetPlan(task, steps, policy) records
//     the task + plan + policy and resets the index to 0.
//  2. The model asks to run a command in the background;
//     NotifyBgStarted is implicit (no separate state is needed —
//     the first NotifyBgFinished arms itself off the values
//     captured in step 1).
//  3. As transcript chunks arrive, RecordTranscriptChunk pushes
//     them onto the recent tail so the OnReorient hook can
//     re-feed them after a bg finish.
//  4. The bg command finishes or is canceled;
//     NotifyBgFinished(taskID, status, output) emits the
//     task-notification chunk and fires the OnReorient hook.
type BgFinishCoordinator struct {
	mu sync.Mutex

	// Plan state. SetPlan replaces the whole plan; AdvancePlan
	// advances the index; OriginalTask / CurrentPlanIndex /
	// PlanSteps are read by tests and by the loop to render the
	// "you were working on" reminder.
	originalTask string
	planSteps    []string
	currentIndex int

	// Policy mode. Set at SetPlan time and read by tests to
	// confirm a bg interrupt does not silently demote /plan to
	// /auto. The agent package owns the enum; cmd/wagyu maps to
	// permissions.ModeXxx at the seam.
	policy PolicyMode

	// OnBgFinish is invoked synchronously inside NotifyBgFinished
	// with the formatted task-notification transcript chunk.
	// Tests pin the chunk text; production callers feed it into
	// the transcript as a tool reply.
	OnBgFinish func(notification BgFinishNotification, transcriptChunk string)

	// OnReorient fires after OnBgFinish when the bg status is
	// terminal. The hook receives the recent transcript tail the
	// caller wants the loop to re-feed into the next Send — the
	// AGY 1.4.0 release note's "re-reading recent context"
	// recovery. nil is fine; the coordinator only fires it when
	// set.
	OnReorient func(taskID string, recentTranscript []string)

	// recentTranscript tail. RecordTranscriptChunk pushes onto
	// it; RecentTranscript reads it. The cap is bounded so a long
	// session does not grow without limit.
	recentMu      sync.Mutex
	recentTail    []string
	recentTailCap int
}

// defaultRecentTailCap bounds the recent transcript tail so a long
// session does not accumulate without limit. 16 chunks is enough
// to cover the bg interrupt window (the bg result + the model turn
// that kicked it off + the model turn that resumed) without
// dragging the full history through OnReorient.
const defaultRecentTailCap = 16

// NewBgFinishCoordinator returns an empty coordinator with the
// default recent-transcript tail cap (16 chunks). Callers may set
// the OnBgFinish / OnReorient hooks before the first bg task.
func NewBgFinishCoordinator() *BgFinishCoordinator {
	return &BgFinishCoordinator{
		policy:        PolicyDefault,
		recentTailCap: defaultRecentTailCap,
	}
}

// SetPlan pins the original task + plan + policy and resets the
// pointer. Calling SetPlan again resets the pointer to 0 so the
// next user turn starts from the first step. The policy mode is
// captured at the same time; the AGY 1.4.0 release note says the
// focus is preserved, so a bg interrupt must not demote /plan to
// /auto. An empty policy is ignored (the previous value sticks)
// so a caller can pass the zero value when it does not have one
// to set.
func (c *BgFinishCoordinator) SetPlan(originalTask string, steps []string, policy PolicyMode) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.originalTask = originalTask
	c.planSteps = append([]string(nil), steps...)
	c.currentIndex = 0
	if policy != "" {
		c.policy = policy
	}
}

// AdvancePlan moves the pointer to the next step. Returns false
// when the plan is already at the end; callers must not advance
// past the final step. NotifyBgFinished does NOT call this — the
// post-bg turn picks up the same step the agent was on before the
// interrupt, and AdvancePlan is the model's job once it actually
// takes that next step.
func (c *BgFinishCoordinator) AdvancePlan() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.currentIndex+1 >= len(c.planSteps) {
		return false
	}
	c.currentIndex++
	return true
}

// CurrentPlanIndex returns the current plan pointer (0-based).
// Cycle 259 invariants (pinned by TestBgFinish_PreservesPlanPointer
// and TestBgFinish_CanceledPreservesState):
//
//   - SetPlan resets to 0.
//   - AdvancePlan moves forward by 1.
//   - NotifyBgFinished does NOT advance — the post-bg turn must
//     pick up the same step the agent was on before the interrupt,
//     regardless of whether the bg task finished normally or was
//     canceled.
func (c *BgFinishCoordinator) CurrentPlanIndex() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentIndex
}

// PlanSteps returns a copy of the recorded plan. The slice header
// is fresh; callers can mutate it without affecting the
// coordinator.
func (c *BgFinishCoordinator) PlanSteps() []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.planSteps...)
}

// OriginalTask returns the task the agent was working on when the
// bg command started. Empty when SetPlan has not been called.
func (c *BgFinishCoordinator) OriginalTask() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.originalTask
}

// PolicyMode returns the policy mode the user picked. Cycle 259
// keeps this stable across a bg interrupt — a user in /plan mode
// who started a long build still sees /plan mode after the build
// completes. Pin: TestBgFinish_DoesNotLoseFocus.
func (c *BgFinishCoordinator) PolicyMode() PolicyMode {
	if c == nil {
		return PolicyDefault
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.policy
}

// RecordTranscriptChunk appends a transcript chunk to the recent
// tail. The tail is capped at recentTailCap entries; the oldest
// chunk is dropped when the cap is exceeded so a long session
// does not grow without limit. An empty chunk is silently dropped
// so a stub Handler that only emits ToolCalls / Errors does not
// leave a hole in the tail.
func (c *BgFinishCoordinator) RecordTranscriptChunk(chunk string) {
	if c == nil {
		return
	}
	if chunk == "" {
		return
	}
	c.recentMu.Lock()
	defer c.recentMu.Unlock()
	c.recentTail = append(c.recentTail, chunk)
	if cap := c.recentTailCap; cap > 0 && len(c.recentTail) > cap {
		// Drop the oldest chunks so the tail stays bounded.
		drop := len(c.recentTail) - cap
		c.recentTail = append(c.recentTail[:0], c.recentTail[drop:]...)
	}
}

// RecentTranscript returns a copy of the recent tail. OnReorient
// receives this slice so the loop can re-feed the chunks the
// model lost track of during the bg interrupt.
func (c *BgFinishCoordinator) RecentTranscript() []string {
	if c == nil {
		return nil
	}
	c.recentMu.Lock()
	defer c.recentMu.Unlock()
	return append([]string(nil), c.recentTail...)
}

// SetRecentTailCap adjusts the cap. Negative values are ignored
// (the existing cap sticks). Tests use this to exercise the
// bounded-tail contract without filling 16 slots.
func (c *BgFinishCoordinator) SetRecentTailCap(n int) {
	if c == nil || n <= 0 {
		return
	}
	c.recentMu.Lock()
	defer c.recentMu.Unlock()
	c.recentTailCap = n
	if len(c.recentTail) > n {
		drop := len(c.recentTail) - n
		c.recentTail = append(c.recentTail[:0], c.recentTail[drop:]...)
	}
}

// NotifyBgFinished is called when a bg task transitions to a
// terminal state. It returns the formatted task-notification
// transcript chunk the caller should append to the conversation,
// and fires OnBgFinish / OnReorient so the loop can re-feed recent
// transcript context. The plan index is NOT advanced here.
//
// status values: "finished", "failed", "canceled". Anything else
// is normalised to "finished" so a malformed producer does not
// leak a typo into the transcript.
func (c *BgFinishCoordinator) NotifyBgFinished(taskID, status, output string) (BgFinishNotification, string) {
	if c == nil {
		return BgFinishNotification{}, ""
	}
	switch status {
	case "finished", "failed", "canceled":
	default:
		status = "finished"
	}
	c.mu.Lock()
	n := BgFinishNotification{
		TaskID:       taskID,
		Status:       status,
		Output:       output,
		OriginalTask: c.originalTask,
		PlanIndex:    c.currentIndex,
		TotalSteps:   len(c.planSteps),
	}
	steps := append([]string(nil), c.planSteps...)
	c.mu.Unlock()

	chunk := FormatBgFinishNotification(n, steps)
	if c.OnBgFinish != nil {
		c.OnBgFinish(n, chunk)
	}
	if c.OnReorient != nil {
		recent := c.RecentTranscript()
		c.OnReorient(taskID, recent)
	}
	return n, chunk
}

// FormatBgFinishNotification builds the task-notification transcript
// chunk that combines the bg result with the agent's focus reminder.
// The shape is:
//
//	<task-notification>
//	task-id: <id>
//	status: <status>
//	output:
//	<output or (empty)>
//	you were working on: <originalTask>
//	next plan step (#<index+1>/<total>): <planStep or "(none)">
//	</task-notification>
//
// The "you were working on:" line is the reminder the AGY 1.4.0
// release note pins: it tells the model what the user's task was,
// independent of the bg result, so a 0-byte output (or a totally
// silent finished command) does not leave the model guessing.
//
// The "next plan step" line is the pointer the agent should resume
// from. It is computed against the plan as it stood at bg-start
// time; the index has not advanced, so the line reads the same step
// the agent was on.
//
// The function is package-level (no receiver) so callers and tests
// can build the chunk without instantiating a coordinator.
func FormatBgFinishNotification(n BgFinishNotification, planSteps []string) string {
	var sb strings.Builder
	sb.WriteString("<task-notification>\n")
	sb.WriteString("task-id: ")
	sb.WriteString(n.TaskID)
	sb.WriteByte('\n')
	sb.WriteString("status: ")
	sb.WriteString(n.Status)
	sb.WriteString("\noutput:\n")
	if strings.TrimSpace(n.Output) == "" {
		sb.WriteString("(empty)\n")
	} else {
		sb.WriteString(n.Output)
		if !strings.HasSuffix(n.Output, "\n") {
			sb.WriteByte('\n')
		}
	}
	sb.WriteString("you were working on: ")
	sb.WriteString(n.OriginalTask)
	sb.WriteByte('\n')
	if n.TotalSteps > 0 && n.PlanIndex >= 0 && n.PlanIndex < n.TotalSteps {
		sb.WriteString("next plan step (#")
		sb.WriteString(strconv.Itoa(n.PlanIndex + 1))
		sb.WriteByte('/')
		sb.WriteString(strconv.Itoa(n.TotalSteps))
		sb.WriteString("): ")
		sb.WriteString(planSteps[n.PlanIndex])
		sb.WriteByte('\n')
	} else {
		sb.WriteString("next plan step: (none)\n")
	}
	sb.WriteString("</task-notification>\n")
	return sb.String()
}
