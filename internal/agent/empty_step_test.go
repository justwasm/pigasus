// empty_step_test.go — cover IsEmptyAssistantTurn.
//
// The tests pin the matrix the AGY 1.3.6 changelog row "a stalled
// request no longer leaves an empty response step behind" advertises:
//
//	Scenario                                     Expected
//	---------                                     --------
//	cancelled request (no content streamed)      empty == true  (no step)
//	error-path Send (no content streamed)        empty == true  (no step)
//	normal Send (text streamed)                  empty == false (step)
//	user-Esc mid-stream (no content streamed)    empty == true  (no step)
//	stall-detected (5-min middleware fires)      empty == true  (no step)
//	partial-content (some chunks, then cancel)   empty == false (partial step)
//	tool-call-only assistant turn                empty == false (step)
//
// Plus boundary coverage: whitespace-only content / reasoning, an
// empty-but-present tool-call slice, and a tool-call slice with one
// empty entry.
package agent

import (
	"encoding/json"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

// TestIsEmptyAssistantTurn_Matrix drives every row in the doc table
// above through the helper and checks the boolean the call site
// would feed into its `if !empty { record }` gate.
func TestIsEmptyAssistantTurn_Matrix(t *testing.T) {
	oneToolCall := []backend.ToolCall{{
		ID:        "c1",
		Name:      "read_file",
		Arguments: json.RawMessage(`{"path":"/tmp/x"}`),
	}}

	cases := []struct {
		name      string
		content   string
		toolCalls []backend.ToolCall
		reasoning string
		wantEmpty bool
	}{
		// The empty-class rows — every entry here is the kind of
		// turn a stall-cancel, error, or Esc hit before any byte
		// streamed, and that the cycle must NOT record.
		{
			name:      "cancelled request no content",
			content:   "",
			toolCalls: nil,
			reasoning: "",
			wantEmpty: true,
		},
		{
			name:      "error-path Send no content",
			content:   "",
			toolCalls: nil,
			reasoning: "",
			wantEmpty: true,
		},
		{
			name:      "user-Esc mid-stream no content",
			content:   "",
			toolCalls: nil,
			reasoning: "",
			wantEmpty: true,
		},
		{
			name:      "stall-detected middleware no content",
			content:   "",
			toolCalls: nil,
			reasoning: "",
			wantEmpty: true,
		},
		{
			name:      "whitespace-only content",
			content:   " \n\t",
			toolCalls: nil,
			reasoning: "",
			wantEmpty: true,
		},
		{
			name:      "whitespace-only reasoning",
			content:   "",
			toolCalls: nil,
			reasoning: "   \n",
			wantEmpty: true,
		},
		// The non-empty-class rows — every entry here is content
		// the cycle MUST preserve, even when the turn ended on an
		// error path.
		{
			name:      "normal Send streamed text",
			content:   "It is now.",
			toolCalls: nil,
			reasoning: "",
			wantEmpty: false,
		},
		{
			name:      "partial-content assistant after cancel",
			content:   "I was about to say that",
			toolCalls: nil,
			reasoning: "",
			wantEmpty: false,
		},
		{
			name:      "tool-call assistant with no content",
			content:   "",
			toolCalls: oneToolCall,
			reasoning: "",
			wantEmpty: false,
		},
		{
			name:      "tool-call + reasoning but no content",
			content:   "",
			toolCalls: oneToolCall,
			reasoning: "let me look at the file",
			wantEmpty: false,
		},
		{
			name:      "reasoning-only assistant turn",
			content:   "",
			toolCalls: nil,
			reasoning: "thinking through the bug",
			wantEmpty: false,
		},
		{
			name:      "text + tool calls",
			content:   "looking at it now",
			toolCalls: oneToolCall,
			reasoning: "",
			wantEmpty: false,
		},
		{
			name:      "text + reasoning",
			content:   "answer",
			toolCalls: nil,
			reasoning: "thought",
			wantEmpty: false,
		},
		{
			name:      "text + tool calls + reasoning",
			content:   "answer",
			toolCalls: oneToolCall,
			reasoning: "thought",
			wantEmpty: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsEmptyAssistantTurn(tc.content, tc.toolCalls, tc.reasoning)
			if got != tc.wantEmpty {
				t.Errorf("IsEmptyAssistantTurn(content=%q, calls=%d, reasoning=%q) = %v, want %v",
					tc.content, len(tc.toolCalls), tc.reasoning, got, tc.wantEmpty)
			}
		})
	}
}

// TestIsEmptyAssistantTurn_ToolCallWithEmptyArgs pins the contract: a
// model that emits a tool call with empty Arguments is still asking
// to act. The classifier must not unwrap the slice and check Name /
// Arguments — every non-zero entry counts. This guards against a
// future refactor that tries to be clever and demotes the gate to
// "non-empty Name", which would silently leak empty assistant steps
// when the model emits `{"name": "read_file"}` with no body.
func TestIsEmptyAssistantTurn_ToolCallWithEmptyArgs(t *testing.T) {
	calls := []backend.ToolCall{{
		Name:      "read_file",
		Arguments: nil,
	}}
	if IsEmptyAssistantTurn("", calls, "") {
		t.Error("tool call with empty Arguments must still count as content")
	}
}

// TestIsEmptyAssistantTurn_NilAndEmptySlicesAgree pins that nil and
// []ToolCall{} produce the same answer, so a caller that builds its
// slice with `make([]ToolCall, 0)` and a caller that leaves it nil
// (the most common shape, since most code paths don't preallocate
// the slice) get the same boolean. Without this, every site that
// calls IsEmptyAssistantTurn would need to remember to normalise.
func TestIsEmptyAssistantTurn_NilAndEmptySlicesAgree(t *testing.T) {
	nilCase := IsEmptyAssistantTurn("", nil, "")
	emptyCase := IsEmptyAssistantTurn("", []backend.ToolCall{}, "")
	if nilCase != emptyCase {
		t.Errorf("nil slice (=%v) and empty slice (=%v) disagree", nilCase, emptyCase)
	}
	if !nilCase {
		t.Error("both nil and empty should classify as empty (no content, no calls)")
	}
}

// TestIsEmptyAssistantTurn_ContentOnly counts as non-empty. This is
// the partial-output case the --print-timeout path depends on: a
// turn that streamed some text and then hit --print-timeout is a
// partial-output success, and the assistant message carries whatever
// made it through. The classifier must not require reasoning or
// tool calls for "not empty".
func TestIsEmptyAssistantTurn_ContentOnly(t *testing.T) {
	if IsEmptyAssistantTurn("partial answer", nil, "") {
		t.Error("text-only assistant turn must not be classified as empty")
	}
}

// TestIsEmptyAssistantTurn_ReasoningOnly counts as non-empty. This
// is the edge case the spec calls out: a model that thinks but never
// produces text content is still worth recording as a step, so the
// next turn's context has the reasoning the model spent quota on.
// A previous draft of the helper required content or tool calls;
// that draft would have re-introduced the very empty step the
// changelog row says to delete (a turn that ends on a think-only
// response with no follow-up text is observably "empty" from the
// conversation database's perspective).
func TestIsEmptyAssistantTurn_ReasoningOnly(t *testing.T) {
	if IsEmptyAssistantTurn("", nil, "spent 3s thinking about the bug") {
		t.Error("reasoning-only assistant turn must not be classified as empty")
	}
}

// TestIsEmptyAssistantTurn_TrimHandlesSSELeadingNewline pins the
// gateway's leading-newline SSE framing: the gateway transport delivers
// "\n" as the very first delta on every stream, and the recovered
// sanitizer keeps that byte in `full`. A classification that
// required content == "" instead of strings.TrimSpace(content) == ""
// would treat that newline as content and skip the gate, leaking an
// empty step on every single request.
//
// The bug this test pins: an earlier prototype used `content == ""`,
// which is byte-cheap but wrong; the trim is load-bearing.
func TestIsEmptyAssistantTurn_TrimHandlesSSELeadingNewline(t *testing.T) {
	for _, content := range []string{"\n", " ", "\n\n", " \t \n"} {
		if !IsEmptyAssistantTurn(content, nil, "") {
			t.Errorf("whitespace-only content %q must classify as empty", content)
		}
	}
}
