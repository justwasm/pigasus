// Package remotecontrol — ls_steps.go: the proto-JSON steps the
// Antigravity Web UI renders.
//
// A step is a `gemini_coder.Step` in proto-JSON form: the type, the
// status, the one message the step carries (the user's input, the
// model's reply, a tool call) and the metadata the frontend reads.
//
// 【一手】 2026-10-26, from the CLI web bundle (cli-1.4.7-main.js):
// the Web UI's planner-response renderer reads the reasoning off the
// step (`rawThinking || thinking`), the answer off `response`, and the
// "Thought for Ns" label off `thinkingDuration.seconds`. The duration
// is a google.protobuf.Duration, which proto-JSON encodes as a string
// ("1.500s") — the frontend's protobuf-es decoder rejects the object
// form, and a rejected frame costs the whole trajectory stream, not
// just the field.
package remotecontrol

import (
	"fmt"
	"time"
)

// lsUserInputStep builds the gemini_coder.Step the Web UI renders as
// the user's message. The step index the frontend reserved for its
// optimistic copy is the trajectory length at send time, so the
// server's step lands on the same index.
func lsUserInputStep(text string) map[string]any {
	return map[string]any{
		"type":   lsStepTypeUserInput,
		"status": lsStepStatusDone,
		"userInput": map[string]any{
			"items": []any{map[string]any{"text": text}},
		},
		"metadata": map[string]any{
			"source": lsStepSourceUserExplicit,
		},
	}
}

// lsPlannerResponseStep builds the assistant's reply step. The Web
// UI renders a planner-response step whose `response` is non-empty as
// a text block, and whose `thinking` is non-empty as the collapsible
// "Thought for Ns" panel above it.
//
// The thinking fields are the reference's own message fields
// (exa.cortex_pb.CortexStepPlannerResponse: thinking = 3,
// thinking_duration = 11).
func lsPlannerResponseStep(text string, thinking string, thinkingDuration time.Duration) map[string]any {
	return lsPlannerResponseStepAt(lsStepStatusDone, text, thinking, thinkingDuration, time.Time{})
}

// lsPlannerResponseStepAt builds the reply step with an explicit
// status and the time the step became visible.
//
// The status is GENERATING while the model is still talking and DONE
// once the message is over. `viewableAt` is the metadata timestamp
// the Web UI's live "Thinking for Ns" label counts from.
func lsPlannerResponseStepAt(status int, text string, thinking string, thinkingDuration time.Duration, viewableAt time.Time) map[string]any {
	planner := map[string]any{"response": text}
	if thinking != "" {
		planner["thinking"] = thinking
		if thinkingDuration > 0 {
			planner["thinkingDuration"] = lsDurationJSON(thinkingDuration)
		}
	}
	metadata := map[string]any{
		"source": lsStepSourceModel,
	}
	if !viewableAt.IsZero() {
		metadata["viewableAt"] = lsTimestampJSON(viewableAt)
	}
	return map[string]any{
		"type":            lsStepTypePlannerResponse,
		"status":          status,
		"plannerResponse": planner,
		"metadata":        metadata,
	}
}

// lsDurationJSON renders a duration in the proto-JSON form of
// google.protobuf.Duration: a string of seconds with 0, 3, 6 or 9
// fractional digits and a trailing "s".
func lsDurationJSON(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int64(d / time.Second)
	nanos := int64(d % time.Second)
	switch {
	case nanos == 0:
		return fmt.Sprintf("%ds", secs)
	case nanos%int64(time.Millisecond) == 0:
		return fmt.Sprintf("%d.%03ds", secs, nanos/int64(time.Millisecond))
	case nanos%int64(time.Microsecond) == 0:
		return fmt.Sprintf("%d.%06ds", secs, nanos/int64(time.Microsecond))
	default:
		return fmt.Sprintf("%d.%09ds", secs, nanos)
	}
}

// lsToolCallStep builds the generic step the Web UI renders as a tool
// call. The tool's name and arguments ride in the step metadata
// (exa.codeium_common_pb.ChatToolCall) because that is where the
// Web UI's renderer looks for them.
func lsToolCallStep(name string, argsJSON string, result string) map[string]any {
	step := map[string]any{
		"type":   lsStepTypeGeneric,
		"status": lsStepStatusDone,
		"generic": map[string]any{
			"args": map[string]any{},
			"result": map[string]any{
				"result": result,
			},
		},
		"metadata": map[string]any{
			"source": lsStepSourceModel,
			"toolCall": map[string]any{
				"id":            name,
				"name":          name,
				"argumentsJson": argsJSON,
			},
		},
	}
	return step
}
