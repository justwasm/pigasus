// repaint_test.go — the transcript's repaint engine.
//
// The host's golden pins the bytes the whole host→transcript path
// produces; these tests pin the engine's own contract: what a repaint
// changes, what it leaves alone, and what it reports back to the host.

package transcript

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/palette"
)

// repaintStep is a tool step with a body, so the collapsed and the
// expanded renderings differ in height.
func repaintStep() toolsteps.Step {
	return toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "view_file",
		Title:   "Read(/tmp/report.txt)",
		Summary: "Read 3 lines",
		Detail:  "one\ntwo\nthree",
	}
}

func TestRerenderBlocks_ExpandsACollapsedBlockAndShiftsTheLaterIndexes(t *testing.T) {
	const width = 60
	step := repaintStep()
	collapsed := toolsteps.RenderBlock([]toolsteps.Step{step}, toolsteps.RenderOptions{
		Expanded:  false,
		Width:     width,
		Verbosity: toolsteps.VerbosityMedium,
	})
	lines := append([]string{"head"}, collapsed...)
	lines = append(lines, "tail")
	tail := len(lines) - 1

	d := &Document{
		Lines:  lines,
		Blocks: []Block{NewBlock(Block{Start: 1, Lines: len(collapsed), Steps: []toolsteps.Step{step}}, collapsed, width, toolsteps.VerbosityMedium)},
		// The assistant's reply sits on the last row, after the block.
		Assistants: []AssistantBlock{{Start: tail, Lines: 1}},
	}

	d.RerenderBlocks(Options{
		Width:     width,
		Height:    24,
		Expanded:  true,
		Verbosity: toolsteps.VerbosityMedium,
	})

	if !d.Blocks[0].Expanded {
		t.Fatalf("the block stayed collapsed")
	}
	if d.Blocks[0].Lines <= len(collapsed) {
		t.Fatalf("the expanded block is %d rows, want more than the collapsed %d", d.Blocks[0].Lines, len(collapsed))
	}
	if d.Lines[len(d.Lines)-1] != "tail" {
		t.Fatalf("the last row is %q, want the tail", d.Lines[len(d.Lines)-1])
	}
	if d.Assistants[0].Start != len(d.Lines)-1 {
		t.Fatalf("the assistant's start is %d, want %d", d.Assistants[0].Start, len(d.Lines)-1)
	}
}

func TestRerenderBlocks_LeavesABlockThatNeedsNoRepaintAlone(t *testing.T) {
	const width = 60
	step := repaintStep()
	collapsed := toolsteps.RenderBlock([]toolsteps.Step{step}, toolsteps.RenderOptions{
		Expanded:  false,
		Width:     width,
		Verbosity: toolsteps.VerbosityMedium,
	})
	lines := append([]string{"head"}, collapsed...)

	d := &Document{
		Lines:  lines,
		Blocks: []Block{NewBlock(Block{Start: 1, Lines: len(collapsed), Steps: []toolsteps.Step{step}}, collapsed, width, toolsteps.VerbosityMedium)},
	}
	before := strings.Join(d.Lines, "|")

	// The same expansion, width and verbosity: the block's cache answers
	// and the buffer is untouched.
	d.RerenderBlocks(Options{
		Width:     width,
		Height:    24,
		Expanded:  false,
		Verbosity: toolsteps.VerbosityMedium,
	})
	if got := strings.Join(d.Lines, "|"); got != before {
		t.Fatalf("rows = %q, want %q", got, before)
	}
}

func TestEnsureVisibleBlocksExpanded_ReportsWhetherItRepainted(t *testing.T) {
	const width = 60
	step := repaintStep()
	collapsed := toolsteps.RenderBlock([]toolsteps.Step{step}, toolsteps.RenderOptions{
		Expanded:  false,
		Width:     width,
		Verbosity: toolsteps.VerbosityMedium,
	})
	lines := append([]string{"head"}, collapsed...)

	// A block that needs no repaint: the window repaint is a no-op.
	d := &Document{
		Lines:  lines,
		Blocks: []Block{NewBlock(Block{Start: 1, Lines: len(collapsed), Steps: []toolsteps.Step{step}}, collapsed, width, toolsteps.VerbosityMedium)},
	}
	if d.EnsureVisibleBlocksExpanded(Options{Width: width, Height: 24}, 20, 0, false) {
		t.Fatalf("the window repaint reported a repaint it did not make")
	}

	// The same block, now expanded: it needs a repaint and it is inside
	// the window.
	d2 := &Document{
		Lines:  lines,
		Blocks: []Block{NewBlock(Block{Start: 1, Lines: len(collapsed), Steps: []toolsteps.Step{step}}, collapsed, width, toolsteps.VerbosityMedium)},
	}
	if !d2.EnsureVisibleBlocksExpanded(Options{Width: width, Height: 24, Expanded: true}, 20, 0, false) {
		t.Fatalf("the window repaint did not report the repaint it made")
	}
	if d2.Blocks[0].Lines <= len(collapsed) {
		t.Fatalf("the block is %d rows, want more than the collapsed %d", d2.Blocks[0].Lines, len(collapsed))
	}
}

func TestReflow_RewrapsTheAssistantReplyAtTheNewWidth(t *testing.T) {
	const (
		oldWidth = 80
		newWidth = 30
	)
	raw := "A reply that is long enough to wrap at the narrow width."
	styled := strings.Split(mdrender.RenderMarkdown(raw, oldWidth), "\n")
	d := &Document{
		Lines:      styled,
		Assistants: []AssistantBlock{{Start: 0, Lines: len(styled), Raw: raw}},
	}
	old := len(d.Lines)

	d.Reflow(Options{Width: newWidth, Height: 24})

	if len(d.Lines) <= old {
		t.Fatalf("the reply is %d rows after the reflow, want more than %d", len(d.Lines), old)
	}
	if d.Assistants[0].Lines != len(d.Lines) {
		t.Fatalf("the assistant's height is %d, want %d", d.Assistants[0].Lines, len(d.Lines))
	}
}

func TestSyncLiveThoughtStep_PaintsAndThenUpdatesTheThoughtRow(t *testing.T) {
	d := &Document{Lines: []string{"head"}}
	opts := Options{
		Width:    60,
		Height:   24,
		Expanded: true,
		Thought:  LiveThought{Active: true, Text: "the model reasons"},
	}
	if !d.SyncLiveThoughtStep(opts) {
		t.Fatalf("the live thought was not painted")
	}
	if len(d.Runs) != 1 {
		t.Fatalf("the buffer holds %d runs, want 1", len(d.Runs))
	}
	if d.Runs[0].Step.Kind != toolsteps.KindThought {
		t.Fatalf("the run is a %v, want a thought", d.Runs[0].Step.Kind)
	}
	rows := len(d.Lines)

	// A second sync updates the same row instead of adding one.
	opts.Thought.Text = "the model reasons some more"
	if !d.SyncLiveThoughtStep(opts) {
		t.Fatalf("the live thought was not updated")
	}
	if len(d.Runs) != 1 {
		t.Fatalf("the buffer holds %d runs, want 1", len(d.Runs))
	}
	if len(d.Lines) != rows {
		t.Fatalf("the buffer holds %d rows, want %d", len(d.Lines), rows)
	}
}

func TestSyncLiveThoughtStep_DropsTheThoughtRowWhenTheTranscriptIsCollapsed(t *testing.T) {
	d := &Document{Lines: []string{"head"}}
	expanded := Options{
		Width:    60,
		Height:   24,
		Expanded: true,
		Thought:  LiveThought{Active: true, Text: "the model reasons"},
	}
	if !d.SyncLiveThoughtStep(expanded) {
		t.Fatalf("the live thought was not painted")
	}
	rows := len(d.Lines)
	thoughtRows := d.Runs[0].Lines

	collapsed := expanded
	collapsed.Expanded = false
	if !d.SyncLiveThoughtStep(collapsed) {
		t.Fatalf("the live thought was not dropped")
	}
	if len(d.Runs) != 0 {
		t.Fatalf("the buffer holds %d runs, want 0", len(d.Runs))
	}
	if len(d.Lines) != rows-thoughtRows {
		t.Fatalf("the buffer holds %d rows, want %d", len(d.Lines), rows-thoughtRows)
	}
}

func TestFinishLiveThinking_ClosesTheThoughtRow(t *testing.T) {
	d := &Document{Lines: []string{"head"}}
	opts := Options{
		Width:    60,
		Height:   24,
		Expanded: true,
		Thought:  LiveThought{Active: true, Text: "the model reasons"},
	}
	if !d.SyncLiveThoughtStep(opts) {
		t.Fatalf("the live thought was not painted")
	}

	// The thought is finished from the thought that was running: the
	// options carry the running thought.
	done := opts
	if !d.FinishLiveThinking(done) {
		t.Fatalf("the thought was not finished")
	}
	if len(d.Runs) != 1 {
		t.Fatalf("the buffer holds %d runs, want 1", len(d.Runs))
	}
	if d.Runs[0].Step.Running {
		t.Fatalf("the finished thought is still running")
	}

	// A thought with no text closes the run without painting one.
	empty := &Document{Lines: []string{"head"}, LiveThoughtRunIdx: -1}
	blank := done
	blank.Thought.Text = ""
	if empty.FinishLiveThinking(blank) {
		t.Fatalf("an empty thought was painted")
	}
}

func TestRerenderLiveSteps_ReportsThatTheLiveRowsNeedARewrap(t *testing.T) {
	d := &Document{Lines: []string{"head"}}
	if d.RerenderLiveSteps(Options{Width: 60, Height: 24}) {
		t.Fatalf("the live repaint reported a repaint it did not make")
	}

	step := toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "run_command",
		Title:   "Bash(git status)",
		Summary: "Ran (git status)",
		Running: true,
		Batch:   1,
	}
	rows := toolsteps.RenderBlock([]toolsteps.Step{step}, toolsteps.RenderOptions{
		Expanded:  false,
		Width:     60,
		Verbosity: toolsteps.VerbosityMedium,
	})
	d.Lines = append(d.Lines, rows...)
	d.Runs = append(d.Runs, LiveStepRun{Start: 1, Lines: len(rows), Step: step})

	if !d.RerenderLiveSteps(Options{Width: 60, Height: 24, Verbosity: toolsteps.VerbosityMedium}) {
		t.Fatalf("the live repaint did not report the repaint it made")
	}
}

func TestRerenderForTheme_KeepsTheRowsTheHostDoesNotOwn(t *testing.T) {
	prompt := UserPromptRow("a plain prompt row")
	echo := palette.WarningBlueOpen + "  \u23bf  " + palette.WarningBlueClose +
		palette.SubtleOpen + "Exited /config command" + "\x1b[39m"
	d := &Document{
		Lines: []string{prompt, echo},
	}
	d.RerenderForTheme(Options{Width: 60, Height: 24})
	if len(d.Lines) != 2 {
		t.Fatalf("the buffer holds %d rows, want 2", len(d.Lines))
	}
	if !strings.Contains(d.Lines[0], "a plain prompt row") {
		t.Fatalf("the prompt row is %q, want the prompt", d.Lines[0])
	}
	if !strings.Contains(d.Lines[1], "\u23bf") || !strings.Contains(d.Lines[1], "Exited /config command") {
		t.Fatalf("the echo row is %q, want the glyph and the message", d.Lines[1])
	}
}
