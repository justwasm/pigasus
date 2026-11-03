// document_test.go — the transcript's own mechanics.
//
// The host's tests cover the host's use of them; these cover the
// mechanics themselves: the range replacement, the blank row, the block
// index, the live step batches and the turn's finalisation.

package transcript

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/toolsteps"
)

func TestReplaceRange_ReplacesTheRows(t *testing.T) {
	lines := []string{"a", "b", "c", "d"}
	got := ReplaceRange(lines, 1, 3, []string{"x"})
	want := []string{"a", "x", "d"}
	if len(got) != len(want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rows = %q, want %q", got, want)
		}
	}
}

func TestReplaceRange_ClampsTheEndAndIgnoresAStartOutside(t *testing.T) {
	got := ReplaceRange([]string{"a", "b"}, 0, 99, []string{"x"})
	if len(got) != 1 || got[0] != "x" {
		t.Fatalf("rows = %q, want [x]", got)
	}
	got = ReplaceRange([]string{"a", "b"}, 5, 6, []string{"x"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("rows = %q, want the buffer untouched", got)
	}
}

func TestReplaceRange_KeepsTheTail(t *testing.T) {
	lines := []string{"a", "b", "c"}
	got := ReplaceRange(lines, 1, 2, []string{"x", "y", "z"})
	want := []string{"a", "x", "y", "z", "c"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %q, want %q", got, want)
	}
}

func TestBlankBefore_AppendsOneRowAfterContent(t *testing.T) {
	got := BlankBefore([]string{"a"})
	if len(got) != 2 || got[1] != "" {
		t.Fatalf("rows = %q, want a blank row after the content", got)
	}
	got = BlankBefore([]string{"a", ""})
	if len(got) != 2 {
		t.Fatalf("rows = %q, want no second blank row", got)
	}
	got = BlankBefore(nil)
	if len(got) != 0 {
		t.Fatalf("rows = %q, want an empty buffer", got)
	}
}

func TestBlankBefore_IgnoresTheStylingOfTheLastRow(t *testing.T) {
	got := BlankBefore([]string{"\x1b[31m\x1b[0m"})
	if len(got) != 1 {
		t.Fatalf("rows = %q, want no blank row after a styled blank", got)
	}
	got = BlankBefore([]string{"\x1b[31mred\x1b[0m"})
	if len(got) != 2 {
		t.Fatalf("rows = %q, want a blank row after the styled content", got)
	}
}

func TestDeduplicateBlocks_SortsAndDropsTheDuplicates(t *testing.T) {
	blocks := []Block{
		{Start: 4, Lines: 1, Steps: []toolsteps.Step{{Name: "b"}}},
		{Start: 1, Lines: 1, Steps: []toolsteps.Step{{Name: "a"}}},
		{Start: 1, Lines: 1, Steps: []toolsteps.Step{{Name: "a"}}},
	}
	got := DeduplicateBlocks(blocks)
	if len(got) != 2 {
		t.Fatalf("blocks = %d, want 2: %+v", len(got), got)
	}
	if got[0].Start != 1 || got[1].Start != 4 {
		t.Fatalf("blocks are not sorted by their start: %+v", got)
	}
}

func TestDeduplicateBlocks_KeepsTheBlockWithTheMetrics(t *testing.T) {
	blocks := []Block{
		{Start: 0, Lines: 1, Steps: []toolsteps.Step{{Name: "a"}}},
		{Start: 0, Lines: 2, Steps: []toolsteps.Step{{Name: "a"}, {Name: "b"}}, Metrics: toolsteps.TokenMetrics{Input: 7}},
	}
	got := DeduplicateBlocks(blocks)
	if len(got) != 1 {
		t.Fatalf("blocks = %d, want 1: %+v", len(got), got)
	}
	if got[0].Lines != 2 || !got[0].Metrics.HasTokens() {
		t.Fatalf("block = %+v, want the one that carries the metrics", got[0])
	}
}

func TestCollectLiveStepBatches_GroupsByBatchAndSkipsTheThoughts(t *testing.T) {
	runs := []LiveStepRun{
		{Start: 0, Lines: 1, Step: toolsteps.Step{Kind: toolsteps.KindThought, Name: "thinking"}},
		{Start: 1, Lines: 1, Step: toolsteps.Step{Kind: toolsteps.KindTool, Name: "a", Batch: 1}},
		{Start: 2, Lines: 1, Step: toolsteps.Step{Kind: toolsteps.KindTool, Name: "b", Batch: 1}},
		{Start: 3, Lines: 1, Step: toolsteps.Step{Kind: toolsteps.KindTool, Name: "c", Batch: 2}},
	}
	got := CollectLiveStepBatches(runs)
	if len(got) != 2 {
		t.Fatalf("batches = %d, want 2: %+v", len(got), got)
	}
	if got[0].Start != 1 || got[0].Last != 2 || len(got[0].Steps) != 2 {
		t.Fatalf("first batch = %+v, want the two steps of batch 1", got[0])
	}
	if got[1].Start != 3 || got[1].Last != 3 || len(got[1].Steps) != 1 {
		t.Fatalf("second batch = %+v, want the one step of batch 2", got[1])
	}
}

func TestFinaliseTurnReply_RendersTheStepsAndTheReply(t *testing.T) {
	step := toolsteps.Step{Kind: toolsteps.KindTool, Name: "run_command", Title: "Bash (echo one)"}
	doc := Document{
		Lines:     []string{"> ask"},
		LiveStart: 1,
	}
	finalised := doc.FinaliseTurnReply(toolsteps.TurnDoneMsg{
		Reply: "done",
		Steps: []toolsteps.Step{step},
	}, Options{Width: 80, Verbosity: toolsteps.VerbosityMedium})

	if finalised {
		t.Fatalf("a turn without live rows was reported as finalised")
	}
	if len(doc.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1: %+v", len(doc.Blocks), doc.Blocks)
	}
	if len(doc.Assistants) != 1 {
		t.Fatalf("assistant blocks = %d, want 1: %+v", len(doc.Assistants), doc.Assistants)
	}
	if doc.Assistants[0].Raw != "done" {
		t.Fatalf("assistant raw = %q, want %q", doc.Assistants[0].Raw, "done")
	}
	if !strings.Contains(strings.Join(doc.Lines, "\n"), "done") {
		t.Fatalf("the reply is missing from the rows: %q", doc.Lines)
	}
}

func TestFinaliseTurnReply_AnEmptyTurnDrawsNoReply(t *testing.T) {
	doc := Document{
		Lines:     []string{"> ask"},
		LiveStart: 1,
	}
	doc.FinaliseTurnReply(toolsteps.TurnDoneMsg{}, Options{Width: 80, EmptyTurn: true})
	if len(doc.Assistants) != 0 {
		t.Fatalf("assistant blocks = %d, want 0: %+v", len(doc.Assistants), doc.Assistants)
	}
	if len(doc.Blocks) != 0 {
		t.Fatalf("blocks = %d, want 0: %+v", len(doc.Blocks), doc.Blocks)
	}
}

func TestFinaliseLiveTurn_TurnsTheLiveRunsIntoBlocks(t *testing.T) {
	step := toolsteps.Step{Kind: toolsteps.KindTool, Name: "run_command", Title: "Bash (echo one)"}
	doc := Document{
		Lines:     []string{"> ask", "step"},
		LiveStart: 1,
		Runs:      []LiveStepRun{{Start: 1, Lines: 1, Step: step}},
	}
	finalised := doc.FinaliseLiveTurn(toolsteps.TurnDoneMsg{Steps: []toolsteps.Step{step}}, Options{Width: 80})
	if !finalised {
		t.Fatalf("a turn with a tool step was not finalised")
	}
	if len(doc.Runs) != 0 {
		t.Fatalf("live runs = %d, want 0: %+v", len(doc.Runs), doc.Runs)
	}
	if len(doc.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1: %+v", len(doc.Blocks), doc.Blocks)
	}
}

func TestFinaliseLiveTurn_AThoughtOnlyTurnFinalisesNothing(t *testing.T) {
	doc := Document{
		Lines:             []string{"> ask"},
		LiveStart:         1,
		Runs:              []LiveStepRun{{Start: 1, Lines: 1, Step: toolsteps.Step{Kind: toolsteps.KindThought}}},
		LiveThoughtRunIdx: 0,
	}
	finalised := doc.FinaliseLiveTurn(toolsteps.TurnDoneMsg{}, Options{Width: 80})
	if finalised {
		t.Fatalf("a thought-only turn was reported as finalised")
	}
	if len(doc.Runs) != 0 {
		t.Fatalf("live runs = %d, want 0: %+v", len(doc.Runs), doc.Runs)
	}
	if doc.LiveThoughtRunIdx != -1 {
		t.Fatalf("live thought run = %d, want -1", doc.LiveThoughtRunIdx)
	}
}
