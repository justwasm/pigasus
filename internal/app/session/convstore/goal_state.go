// Package convstore goal_state.go — Goal accumulator and stop hook support.
package convstore

import (
	"fmt"
	"strings"
	"time"
)

// GoalState represents the state of a goal within a conversation.
// In agy (google3/third_party/jetski/cortex_pb/cortex.proto GoalState),
// the state values are:
//   0: UNSPECIFIED
//   1: ACTIVE
//   2: COMPLETED
//   3: CANCELLED
const (
	GoalStateUnspecified = 0
	GoalStateActive      = 1
	GoalStateCompleted   = 2
	GoalStateCancelled   = 3
)

// Tokens and identifiers recovered from agy reference binary.
const (
	GoalCompleteToken  = "<!-- GOAL_COMPLETE -->"
	GoalCancelledToken = "<!-- GOAL_CANCELLED -->"
	GoalStopHookName   = "goal_stop"
)

// GoalState stores runtime tracking information for an active /goal.
type GoalState struct {
	GoalText          string    `json:"goal_text"`
	StartedAt         time.Time `json:"started_at"`
	StartStepIndex    int       `json:"start_step_index"`
	ContinuationCount int       `json:"continuation_count"`
	State             int       `json:"state"`
}

// NewGoalState creates an active GoalState for the given goal request.
func NewGoalState(goalText string, startStepIndex int) *GoalState {
	return &GoalState{
		GoalText:          strings.TrimSpace(goalText),
		StartedAt:         time.Now(),
		StartStepIndex:    startStepIndex,
		ContinuationCount: 0,
		State:             GoalStateActive,
	}
}

// IsActive returns whether the goal is currently in the active state.
func (g *GoalState) IsActive() bool {
	return g != nil && g.State == GoalStateActive
}

// FormatDuration formats an elapsed duration matching agy's formatDuration (0x101c72250):
// - if hours > 0: "%dh %dm"
// - else if minutes > 0: "%dm %ds"
// - else: "%ds"
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSec := int(d.Round(time.Second).Seconds())
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	s := totalSec % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// GoalReminderFormat is the exact 962-byte verification prompt format string
// recovered from agy 1.4.12 binary at 0x37e05ee.
const GoalReminderFormat = `You are still working toward the user's goal. Do not stop until the task is fully complete.

Time spent so far: %s
Continuation: #%d

Before concluding, verify your work:
1. Re-read the original request and identify every concrete deliverable.
2. Build a checklist that maps every explicit requirement and deliverable to concrete evidence.
3. For each deliverable, confirm it is done by checking actual output: file contents, test results, build logs, etc.
4. Wanting to be done or having spent effort is not the same as being done. Only real evidence counts.
5. If anything is unfinished or unverified, keep going. If anything is uncertain, then spend more time to validate.

Once every deliverable is genuinely complete, include %s in your response to signal you are finished.

If the user explicitly asked to stop or cancel this goal, include %s in your response to cancel the goal.

If there is still work to do, take the next step now.

Original request: %s`

// CheckOutcome inspects the assistant's reply and updates the state if completed or cancelled.
func (g *GoalState) CheckOutcome(reply string) (completed bool, cancelled bool) {
	if g == nil {
		return false, false
	}
	completed, cancelled = CheckGoalOutcome(reply)
	if completed {
		g.State = GoalStateCompleted
	} else if cancelled {
		g.State = GoalStateCancelled
	}
	return completed, cancelled
}

// BuildReminderMessage formats the verification reminder for this goal.
func (g *GoalState) BuildReminderMessage() string {
	if g == nil {
		return ""
	}
	return BuildGoalReminderMessage(g.GoalText, g.StartedAt, g.ContinuationCount)
}

// BuildGoalReminderMessage formats the verification reminder injected by NewGoalStopHook.
func BuildGoalReminderMessage(goalText string, startedAt time.Time, continuationCount int) string {
	durationStr := FormatDuration(time.Since(startedAt))
	return fmt.Sprintf(
		GoalReminderFormat,
		durationStr,
		continuationCount,
		GoalCompleteToken,
		GoalCancelledToken,
		goalText,
	)
}

// CheckGoalOutcome inspects the assistant's reply for goal completion or cancellation tokens.
func CheckGoalOutcome(reply string) (completed bool, cancelled bool) {
	if strings.Contains(reply, GoalCompleteToken) {
		return true, false
	}
	if strings.Contains(reply, GoalCancelledToken) {
		return false, true
	}
	return false, false
}
