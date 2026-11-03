package toolsteps

import "testing"

func TestBatchSteps_KeepsTheCallsOfTheBatchAndLeavesTheThoughts(t *testing.T) {
	steps := []Step{
		{Kind: KindThought, Name: "thinking"},
		{Kind: KindTool, Name: "a", Batch: 1},
		{Kind: KindTool, Name: "b", Batch: 1},
		{Kind: KindTool, Name: "c", Batch: 2},
	}

	got := BatchSteps(steps, 1)

	if len(got) != 2 {
		t.Fatalf("steps = %d, want 2: %+v", len(got), got)
	}
	if got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("steps = %+v, want the calls of batch 1 in paint order", got)
	}
}

func TestBatchSteps_AnUnknownBatchHasNoSteps(t *testing.T) {
	steps := []Step{{Kind: KindTool, Name: "a", Batch: 1}}

	if got := BatchSteps(steps, 7); got != nil {
		t.Fatalf("steps = %+v, want none", got)
	}
}
