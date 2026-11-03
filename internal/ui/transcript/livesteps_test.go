package transcript

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
)

// plainRows are the document's rows without the styling.
func plainRows(lines []string) string {
	return diffaction.StripANSI(strings.Join(lines, "\n"))
}

// liveOpts are the render facts the live-step mechanics work with.
func liveOpts() Options {
	return Options{Width: 72, Verbosity: toolsteps.VerbosityMedium}
}

// runningStep is a call that is in flight.
func runningStep(title string) toolsteps.Step {
	return toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "run_command",
		Title:   "Bash(" + title + ")",
		Summary: "Ran (" + title + ")",
		Running: true,
	}
}

// finishedStep is a call that has ended.
func finishedStep(title, detail string) toolsteps.Step {
	return toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "run_command",
		Title:   "Bash(" + title + ")",
		Summary: "Ran (" + title + ")",
		Detail:  detail,
	}
}

func TestAppendRunningStep_StartsTheFirstBatch(t *testing.T) {
	doc := Document{}
	step := runningStep("echo one")

	batch, brk := doc.AppendRunningStep(&step, 0, false, liveOpts())

	if batch != 1 {
		t.Fatalf("batch = %d, want 1", batch)
	}
	if brk {
		t.Fatalf("the batch break was consumed by a step that did not break it")
	}
	if step.Batch != 1 {
		t.Fatalf("the step's batch = %d, want 1", step.Batch)
	}
	if len(doc.Runs) != 1 {
		t.Fatalf("runs = %d, want 1: %+v", len(doc.Runs), doc.Runs)
	}
	if doc.Runs[0].Start != 0 || doc.Runs[0].Lines != len(doc.Lines) {
		t.Fatalf("run = %+v, want the rows at 0..%d", doc.Runs[0], len(doc.Lines))
	}
	if len(doc.Lines) == 0 {
		t.Fatalf("the running call painted no rows")
	}
}

func TestAppendRunningStep_JoinsTheBatchAndRepaintsItsRows(t *testing.T) {
	doc := Document{}
	first := runningStep("echo one")
	second := runningStep("echo two")

	batch, _ := doc.AppendRunningStep(&first, 0, false, liveOpts())
	if batch != 1 {
		t.Fatalf("the first call's batch = %d, want 1", batch)
	}
	batch, _ = doc.AppendRunningStep(&second, batch, false, liveOpts())
	if batch != 1 {
		t.Fatalf("the second call's batch = %d, want 1: it joins the batch", batch)
	}
	if len(doc.Runs) != 2 {
		t.Fatalf("runs = %d, want 2: %+v", len(doc.Runs), doc.Runs)
	}
	if doc.Runs[1].Start != doc.Runs[0].Start {
		t.Fatalf("the joined run starts at %d, want the batch's own start %d",
			doc.Runs[1].Start, doc.Runs[0].Start)
	}
	if doc.Runs[0].Lines == 0 || doc.Runs[0].Lines != doc.Runs[1].Lines {
		t.Fatalf("the batch's runs have different heights: %d and %d",
			doc.Runs[0].Lines, doc.Runs[1].Lines)
	}
	if !strings.Contains(plainRows(doc.Lines), "Ran 2 commands (echo one · echo two)") {
		t.Fatalf("the batch's rows were not repainted with the second call:\n%s",
			plainRows(doc.Lines))
	}
}

func TestAppendRunningStep_ABreakStartsANewBatch(t *testing.T) {
	doc := Document{}
	first := runningStep("echo one")
	if batch, _ := doc.AppendRunningStep(&first, 0, false, liveOpts()); batch != 1 {
		t.Fatalf("the first call's batch = %d, want 1", batch)
	}

	// A reply separated the batches.
	second := runningStep("echo two")
	batch, brk := doc.AppendRunningStep(&second, 1, true, liveOpts())

	if batch != 2 {
		t.Fatalf("the batch = %d, want 2: the reply separated the batches", batch)
	}
	if brk {
		t.Fatalf("the batch break was not consumed")
	}
	if second.Batch != 2 {
		t.Fatalf("the step's batch = %d, want 2", second.Batch)
	}
	if doc.Runs[1].Start == doc.Runs[0].Start {
		t.Fatalf("the new batch's run starts at the old batch's start")
	}
}

func TestFinishLiveStep_ReplacesTheRunningRows(t *testing.T) {
	doc := Document{}
	running := runningStep("echo one")
	if batch, _ := doc.AppendRunningStep(&running, 0, false, liveOpts()); batch != 1 {
		t.Fatalf("the call's batch = %d, want 1", batch)
	}

	done := finishedStep("echo one", "one")
	if !doc.FinishLiveStep(&done, liveOpts()) {
		t.Fatalf("the finished call did not replace the running one")
	}
	if done.Batch != 1 {
		t.Fatalf("the finished step's batch = %d, want 1", done.Batch)
	}
	if len(doc.Runs) != 1 {
		t.Fatalf("runs = %d, want 1: the running row was replaced, not appended", len(doc.Runs))
	}
	if doc.Runs[0].Step.Running {
		t.Fatalf("the run still carries the running step: %+v", doc.Runs[0].Step)
	}
	if doc.Runs[0].Lines != len(doc.Lines) {
		t.Fatalf("the run's height = %d, want the block's own height %d",
			doc.Runs[0].Lines, len(doc.Lines))
	}
	if !strings.Contains(plainRows(doc.Lines), "one") {
		t.Fatalf("the finished call's own detail is missing:\n%s", plainRows(doc.Lines))
	}
}

func TestFinishLiveStep_WithoutARunningPredecessorIsNotAReplacement(t *testing.T) {
	doc := Document{}
	done := finishedStep("echo one", "one")

	if doc.FinishLiveStep(&done, liveOpts()) {
		t.Fatalf("a call with no running predecessor reported a replacement")
	}
	if len(doc.Runs) != 0 {
		t.Fatalf("the call was recorded as a run: %+v", doc.Runs)
	}
}

func TestAppendLiveStep_AppendsARunOfItsOwn(t *testing.T) {
	doc := Document{}
	step := finishedStep("echo one", "one")

	batch, _ := doc.AppendLiveStep(&step, 0, false, liveOpts())

	if batch != 1 {
		t.Fatalf("batch = %d, want 1", batch)
	}
	if len(doc.Runs) != 1 || doc.Runs[0].Start != 0 {
		t.Fatalf("runs = %+v, want one run at 0", doc.Runs)
	}
	if len(doc.Lines) != doc.Runs[0].Lines {
		t.Fatalf("the run's height = %d, want the block's own height %d",
			doc.Runs[0].Lines, len(doc.Lines))
	}
}
