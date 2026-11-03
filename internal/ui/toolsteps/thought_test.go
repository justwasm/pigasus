// thought_test.go — the turn's thought row.

package toolsteps

import (
	"testing"
	"time"
)

func TestAppendThoughtStep_PutsTheThoughtInFront(t *testing.T) {
	steps := []Step{{Kind: KindTool, Name: "run_command"}}
	got := AppendThoughtStep(steps, "  thinking about it  ", 3*time.Second, 12)
	if len(got) != 2 {
		t.Fatalf("steps = %d, want 2: %+v", len(got), got)
	}
	if got[0].Kind != KindThought || got[0].Name != "thinking" {
		t.Fatalf("first step = %+v, want the thought", got[0])
	}
	if got[0].Detail != "thinking about it" {
		t.Fatalf("detail = %q, want the trimmed thought", got[0].Detail)
	}
	if got[0].Dur != 3*time.Second || got[0].Tokens != 12 {
		t.Fatalf("step = %+v, want the duration and the token count", got[0])
	}
	if got[1].Name != "run_command" {
		t.Fatalf("second step = %+v, want the tool step", got[1])
	}
}

func TestAppendThoughtStep_AnEmptyThoughtLeavesTheStepsAlone(t *testing.T) {
	steps := []Step{{Kind: KindTool, Name: "run_command"}}
	got := AppendThoughtStep(steps, "", time.Second, 0)
	if len(got) != 1 || got[0].Name != "run_command" {
		t.Fatalf("steps = %+v, want the tool step alone", got)
	}
}
