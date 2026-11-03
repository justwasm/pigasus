// session_steps_test.go — the cycle-317 tests for the subagent's
// trajectory steps.
//
// The /tasks detail view renders the steps under the reference's
// "Trajectory — <name> (<N> steps)" heading (【反编译】
// model.SubagentDetailModel.buildContentLines, the literal at
// 0x10358a89d). The steps themselves come from the subagent's own
// message stream: one per assistant content block.
package subagenttools

import (
	"testing"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
)

// TestUsedSteps — the steps are the assistant's content blocks, in
// order: a tool call by its name, a text block by its first line.
func TestUsedSteps(t *testing.T) {
	msgs := []piagent.AgentMessage{
		{User: &piagent.UserMessage{}},
		{Assistant: &piagent.AssistantMessage{Content: []pi.AssistantContentBlock{
			pi.TextContent{Text: "let me look"},
			pi.ToolCall{ID: "1", Name: "read_file"},
		}}},
		{Assistant: &piagent.AssistantMessage{Content: []pi.AssistantContentBlock{
			pi.ToolCall{ID: "2", Name: "run_command"},
			pi.TextContent{Text: "the answer\nsecond line"},
		}}},
	}
	want := []string{"let me look", "read_file", "run_command", "the answer"}
	got := usedSteps(msgs)
	if len(got) != len(want) {
		t.Fatalf("usedSteps = %q, want %q", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("step %d = %q, want %q", i, got[i], w)
		}
	}
}

// TestUsedStepsEmpty — a run that said nothing has no steps.
func TestUsedStepsEmpty(t *testing.T) {
	msgs := []piagent.AgentMessage{
		{User: &piagent.UserMessage{}},
		{Assistant: &piagent.AssistantMessage{Content: []pi.AssistantContentBlock{
			pi.TextContent{Text: "   \n  "},
			pi.ToolCall{ID: "1"},
		}}},
	}
	if got := usedSteps(msgs); len(got) != 0 {
		t.Errorf("usedSteps = %q, want none", got)
	}
}

// TestFirstLine — the first non-empty line of a text block.
func TestFirstLine(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		"   ":               "",
		"\n\n  hello\n":     "hello",
		"one\ntwo":          "one",
		"  spaced  \nnext":  "spaced",
		"\n\n\n":            "",
		"trailing spaces  ": "trailing spaces",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}
