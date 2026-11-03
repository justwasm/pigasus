// results_test.go — the raw tool outputs a set of steps carries, pinned.

package toolsteps

import "testing"

// The raw outputs are the tool steps' own: a thought step carries none,
// and the results come back in the order the steps were given.
func TestToolResults(t *testing.T) {
	steps := []Step{
		{Kind: KindThought, Name: "thinking", Result: "not a tool output"},
		{Kind: KindTool, Name: "fetch", Result: "alpha"},
		{Kind: KindTool, Name: "shell", Result: "beta"},
	}
	got := ToolResults(steps)
	want := []string{"alpha", "beta"}
	if len(got) != len(want) {
		t.Fatalf("ToolResults() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ToolResults()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// A step set without a tool step has no output at all.
func TestToolResults_None(t *testing.T) {
	if got := ToolResults([]Step{{Kind: KindThought, Result: "x"}}); len(got) != 0 {
		t.Errorf("ToolResults() = %q, want none", got)
	}
	if got := ToolResults(nil); len(got) != 0 {
		t.Errorf("ToolResults(nil) = %q, want none", got)
	}
}

// A running tool step has no output yet, so it contributes an empty one —
// the token count reads it as zero.
func TestToolResults_RunningStep(t *testing.T) {
	got := ToolResults([]Step{{Kind: KindTool, Name: "shell", Running: true}})
	if len(got) != 1 || got[0] != "" {
		t.Errorf("ToolResults() = %q, want one empty output", got)
	}
}
