// livesteps.go — the live-step batch mechanic.
//
// The host owns the turn's batch bookkeeping: the number of the
// current batch and whether a reply separated the batches. This file
// owns the arithmetic of a batch — whether a step continues the batch
// or starts a new one, which rows the batch occupies, how tall the
// batch's block is, and what the batch's runs and the transcript's
// indexes become afterwards.
//
// The host hands over a Document and takes it back. The render facts
// arrive as Options, exactly as they do for the repaint engine.
//
// Stdlib-only plus internal/ui/toolsteps. No dependency on the TUI
// host.
package transcript

import (
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
)

// blockRenderOptions are the transcript's render facts as the step
// model's own rendering options.
//
// The live rows carry no token metrics: the metrics belong to a
// finished turn, not to the rows a call paints while it runs.
func blockRenderOptions(opts Options) toolsteps.RenderOptions {
	return toolsteps.RenderOptions{
		Expanded:  opts.Expanded,
		Width:     opts.Width,
		Verbosity: opts.Verbosity,
	}
}

// batchHasPriorTools reports whether the current batch already holds a
// tool call that a new step would join.
//
// A reply that separated the batches breaks the batch: the next call
// starts a new one. A thought does not break it — the reasoning
// between two calls belongs to the batch the calls are in.
func (d *Document) batchHasPriorTools(batch int, batchBreak bool) bool {
	if batchBreak || len(d.Runs) == 0 {
		return false
	}
	for i := len(d.Runs) - 1; i >= 0; i-- {
		if d.Runs[i].Step.Kind == toolsteps.KindThought {
			continue
		}
		return d.Runs[i].Step.Batch == batch
	}
	return false
}

// batchRuns returns the index of the batch's first run and the steps
// the batch holds, in paint order.
//
// The index is -1 when the batch holds no run.
func (d *Document) batchRuns(batch int) (int, []toolsteps.Step) {
	first := -1
	for i := range d.Runs {
		s := d.Runs[i].Step
		if s.Kind == toolsteps.KindThought || s.Batch != batch {
			continue
		}
		first = i
		break
	}
	if first < 0 {
		return -1, nil
	}
	steps := make([]toolsteps.Step, len(d.Runs))
	for i := range d.Runs {
		steps[i] = d.Runs[i].Step
	}
	return first, toolsteps.BatchSteps(steps, batch)
}

// AppendLiveStep paints a step into the transcript as a run of its own.
//
// The step's batch is the current batch; the batch is bumped when it
// holds no tool call yet or when a reply separated the batches. A
// reply that separated the batches is closed with a blank row.
//
// The step's own batch is assigned here: the caller reads it back
// from the step.
//
// It answers with the batch the step belongs to and whether the batch
// break was consumed.
func (d *Document) AppendLiveStep(step *toolsteps.Step, batch int, batchBreak bool, opts Options) (int, bool) {
	if !d.batchHasPriorTools(batch, batchBreak) {
		batch++
		if batchBreak {
			batchBreak = false
			d.Lines = BlankBefore(d.Lines)
		}
	}
	step.Batch = batch
	lines := toolsteps.RenderBlock([]toolsteps.Step{*step}, blockRenderOptions(opts))
	d.Runs = append(d.Runs, LiveStepRun{
		Start: len(d.Lines),
		Lines: len(lines),
		Step:  *step,
	})
	d.Lines = append(d.Lines, lines...)
	return batch, batchBreak
}

// AppendRunningStep paints the provisional row of a call that is in
// flight.
//
// The step joins the current batch when the batch already holds a tool
// call and no reply separated the batches: the batch's rows are
// repainted with the new call. Otherwise the step starts a new batch
// — see AppendLiveStep.
//
// The step's own batch is assigned here: the caller reads it back
// from the step.
//
// It answers with the batch the step belongs to and whether the batch
// break was consumed.
func (d *Document) AppendRunningStep(step *toolsteps.Step, batch int, batchBreak bool, opts Options) (int, bool) {
	if !d.batchHasPriorTools(batch, batchBreak) {
		return d.AppendLiveStep(step, batch, batchBreak, opts)
	}
	step.Batch = batch
	first, batchSteps := d.batchRuns(batch)
	batchSteps = append(batchSteps, *step)
	batchStart := d.Runs[first].Start
	batchEnd := len(d.Lines)
	lines := toolsteps.RenderBlock(batchSteps, blockRenderOptions(opts))
	d.Lines = ReplaceRange(d.Lines, batchStart, batchEnd, lines)
	d.Runs = append(d.Runs, LiveStepRun{
		Start: batchStart,
		Lines: len(lines),
		Step:  *step,
	})
	for i := first; i < len(d.Runs); i++ {
		if d.Runs[i].Step.Batch == batch {
			d.Runs[i].Lines = len(lines)
		}
	}
	return batch, batchBreak
}

// FinishLiveStep replaces the rows of the call that is in flight with
// the rows of the finished call.
//
// The finished step takes the running step's place in the batch, and
// the batch's rows are repainted with the block the batch now holds.
// The batch's runs take the block's height, and the indexes that
// follow the block move with it.
//
// It reports whether it replaced anything: a step with no running
// predecessor is not a replacement — the caller appends it instead.
func (d *Document) FinishLiveStep(step *toolsteps.Step, opts Options) bool {
	n := len(d.Runs)
	if n == 0 || !d.Runs[n-1].Step.Running {
		return false
	}
	step.Batch = d.Runs[n-1].Step.Batch
	d.Runs[n-1].Step = *step
	first, batchSteps := d.batchRuns(step.Batch)
	if first < 0 {
		return false
	}
	batchStart := d.Runs[first].Start
	batchEnd := batchStart + d.Runs[first].Lines
	if batchStart < 0 || batchEnd > len(d.Lines) {
		return false
	}
	lines := toolsteps.RenderBlock(batchSteps, blockRenderOptions(opts))
	delta := len(lines) - (batchEnd - batchStart)
	d.Lines = ReplaceRange(d.Lines, batchStart, batchEnd, lines)
	for i := first; i < len(d.Runs); i++ {
		if d.Runs[i].Step.Batch == step.Batch {
			d.Runs[i].Lines = len(lines)
		}
	}
	if delta != 0 {
		for j := range d.UserBlocks {
			if d.UserBlocks[j].Start >= batchEnd {
				d.UserBlocks[j].Start += delta
			}
		}
		if d.ReplyStart >= batchEnd {
			d.ReplyStart += delta
		}
	}
	return true
}
