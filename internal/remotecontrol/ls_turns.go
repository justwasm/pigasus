// Package remotecontrol — ls_turns.go: the turn view of a
// trajectory.
//
// A trajectory is a list of steps; a *turn* is what the user sees as
// one exchange: the message they sent and the reply that followed
// it. The conversation methods answer the turns of a conversation
// (GetTrajectoryTurns) and a summary of every conversation
// (GetAllCascadeTrajectories).
//
// 【反编译】 exa.jetski_cortex_pb.TurnSummary: start_step_index=1,
// first_step=2 (a gemini_coder.Step), response_preview=3 (repeated
// string).
//
// 【反编译】 exa.jetski_cortex_pb.CascadeTrajectorySummary:
// summary=1, step_count=2, last_modified_time=3, trajectory_id=4,
// status=5, created_time=6, last_user_input_time=7,
// last_user_input_step_index=8, trajectory_type=9.
package remotecontrol

import (
	"time"
)

// lsTurnPreviewLimit is how much of a reply the preview carries.
//
// The preview is a hint, not the reply: the workbench shows it in the
// collapsed view of a turn.
const lsTurnPreviewLimit = 200

// lsTrajectoryTurns is the turn list of a trajectory.
//
// The steps that ask for are the turns that contain them: an empty
// list asks for every turn. A trajectory with no user message has no
// turn — the steps before the first message are not an exchange.
func lsTrajectoryTurns(steps []map[string]any, want []uint32) []map[string]any {
	spans := lsGroupTurns(steps)
	out := make([]map[string]any, 0, len(spans))
	seen := map[int]bool{}
	for _, span := range spans {
		if len(want) > 0 && !lsSpanHasStep(span, want) {
			continue
		}
		if seen[span.start] {
			continue
		}
		seen[span.start] = true
		out = append(out, lsBuildTurn(steps, span))
	}
	return out
}

// lsTurnSpan is the range of steps one turn covers.
type lsTurnSpan struct {
	start int
	end   int
}

// lsGroupTurns splits a trajectory's steps into turns.
//
// A turn starts at a user-input step and runs until the next one.
func lsGroupTurns(steps []map[string]any) []lsTurnSpan {
	out := []lsTurnSpan{}
	start := -1
	for i, step := range steps {
		if lsStepIsUserInput(step) {
			if start >= 0 {
				out = append(out, lsTurnSpan{start: start, end: i})
			}
			start = i
		}
	}
	if start >= 0 {
		out = append(out, lsTurnSpan{start: start, end: len(steps)})
	}
	return out
}

// lsSpanHasStep reports whether a turn covers one of the step indices
// the caller asked for.
func lsSpanHasStep(span lsTurnSpan, want []uint32) bool {
	for _, idx := range want {
		if int(idx) >= span.start && int(idx) < span.end {
			return true
		}
	}
	return false
}

// lsBuildTurn is the summary of the steps in a turn.
func lsBuildTurn(steps []map[string]any, span lsTurnSpan) map[string]any {
	turn := map[string]any{
		"startStepIndex": span.start,
		"firstStep":      steps[span.start],
	}
	previews := []string{}
	for _, step := range steps[span.start:span.end] {
		if text := lsStepResponseText(step); text != "" {
			previews = append(previews, lsTruncateText(text, lsTurnPreviewLimit))
		}
	}
	turn["responsePreview"] = previews
	return turn
}

// lsStepIsUserInput reports whether a step is the user's message.
func lsStepIsUserInput(step map[string]any) bool {
	v, ok := step["type"].(int)
	return ok && v == lsStepTypeUserInput
}

// lsStepResponseText is the text a step carries as its answer.
//
// The planner-response step carries it in `plannerResponse.response`;
// the other steps carry none.
func lsStepResponseText(step map[string]any) string {
	planner, ok := step["plannerResponse"].(map[string]any)
	if !ok {
		return ""
	}
	text, _ := planner["response"].(string)
	return text
}

// lsTruncateText cuts a text to a rune count, with an ellipsis when
// it had to cut.
func lsTruncateText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

// lsCascadeSummary is the summary of one conversation.
//
// 【反编译】 exa.jetski_cortex_pb.CascadeTrajectorySummary — the
// summary is the conversation's title, the step count the number of
// steps it holds, and the times when it started and last changed.
func lsCascadeSummary(snap lsSnapshot) map[string]any {
	summary := map[string]any{
		"summary":      lsConversationSummary(snap),
		"stepCount":    len(snap.Steps),
		"trajectoryId": snap.TrajectoryID,
		"status":       lsRunStatusName(snap.Running),
	}
	if !snap.CreatedAt.IsZero() {
		summary["createdTime"] = snap.CreatedAt.UTC().Format(time.RFC3339)
	}
	if !snap.UpdatedAt.IsZero() {
		summary["lastModifiedTime"] = snap.UpdatedAt.UTC().Format(time.RFC3339)
	}
	if idx, ok := lsLastUserInputIndex(snap.Steps); ok {
		summary["lastUserInputStepIndex"] = idx
	}
	return summary
}

// lsConversationSummary is the one-line label of a conversation: the
// text of the user's first message.
func lsConversationSummary(snap lsSnapshot) string {
	for _, step := range snap.Steps {
		if !lsStepIsUserInput(step) {
			continue
		}
		if text := lsStepUserInputText(step); text != "" {
			return lsTruncateText(text, lsTurnPreviewLimit)
		}
	}
	return ""
}

// lsStepUserInputText is the text of a user-input step.
func lsStepUserInputText(step map[string]any) string {
	input, ok := step["userInput"].(map[string]any)
	if !ok {
		return ""
	}
	items, ok := input["items"].([]any)
	if !ok {
		return ""
	}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := m["text"].(string); ok && text != "" {
			return text
		}
	}
	return ""
}

// lsLastUserInputIndex is the index of the last user message of a
// trajectory.
func lsLastUserInputIndex(steps []map[string]any) (int, bool) {
	for i := len(steps) - 1; i >= 0; i-- {
		if lsStepIsUserInput(steps[i]) {
			return i, true
		}
	}
	return 0, false
}
