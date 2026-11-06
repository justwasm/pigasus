package subagenttools

import (
	"testing"
)

// TestUsedSteps — the steps are the assistant's content blocks, in
// order: a tool call by its name, a text block by its first line.
func TestUsedSteps(t *testing.T) {
	msgs := []SubagentMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "let me look"},
		{Role: "assistant", ToolName: "read_file", ToolCallID: "1"},
		{Role: "assistant", ToolName: "run_command", ToolCallID: "2"},
		{Role: "assistant", Content: "the answer\nsecond line"},
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
	msgs := []SubagentMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "   \n  "},
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
