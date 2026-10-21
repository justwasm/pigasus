// empty_step.go — the "empty assistant turn" classifier.
//
// Cycle stall-empty-step-cleanup (AGY 1.3.6 changelog line 5, second
// half): "a stalled request no longer leaves an empty response step
// behind." The fix lives at the step-append path: when a model request
// is cancelled (stall-cancel middleware, user-Esc, normal error path)
// before any content streams, the loop's tail must not grow the
// conversation by an empty assistant step.
//
// "Empty" means: no text content AND no tool calls AND no reasoning
// content. A turn that produced only thinking (no text, no tools) is
// still classified as empty — a model that thinks but never responds
// is just as much an empty step as a turn that aborted on the wire
// before its first byte. A turn that produced only tool calls (no
// text, no reasoning) is NOT empty — the calls are content, the
// conversation's next turn needs the tool-result pair they imply.
//
// Trimming whitespace guards against two real cases:
//
//   - the gateway's leading-newline SSE framing, which frequently
//     delivers a single "\n" as the first delta;
//   - the agent.Sanitizer stripping the last think tag at a chunk
//     boundary (see internal/agent/sanitize.go), which can leave a
//     trailing space in the reasoning buffer the caller passes in.
//
// Without the trim, either case would count " " as content and the
// classification would still emit an empty step.
//
// This helper is the unit-level classifier. The integration site is
// cmd/wagyu's turnDoneMsg case in tui_run.go — it imports this
// function and gates `m.session = append(...)` on
// `!IsEmptyAssistantTurn(...)`. Tests live in empty_step_test.go; they
// pin the matrix the changelog row advertises (cancel / error / Esc /
// stall / partial / tool-call-only / normal).
package agent

import (
	"strings"

	"github.com/justwasm/pigasus/internal/backend"
)

// IsEmptyAssistantTurn reports whether an assistant turn is empty for
// the purposes of recording it in the conversation step list.
//
// A turn is empty when all three of the following hold:
//
//	content is empty (after trimming whitespace),
//	toolCalls has zero entries,
//	reasoning is empty (after trimming whitespace).
//
// The function does not look at the agent's StopReason or any error
// the upstream may have folded into the assistant message — the
// caller decides whether a non-empty turn that ended on error is
// still worth recording. A turn that streamed text and then was
// cancelled mid-stream still classifies as non-empty (its content
// makes the answer recoverable), so the partial-output policy the
// --print-timeout path relies on survives this gate.
//
// Tool calls are detected by slice length, not by any field inside
// them: a model that emits a tool call with empty arguments is still
// asking to act, and the conversation's next turn needs the result
// that the runner produces for it.
func IsEmptyAssistantTurn(content string, toolCalls []backend.ToolCall, reasoning string) bool {
	return strings.TrimSpace(content) == "" && len(toolCalls) == 0 && strings.TrimSpace(reasoning) == ""
}
