// tui_repaint_golden_test.go — the transcript's repaint bytes are pinned.
//
// Cycle 381's tranche 87 moved the transcript's repaint engine out of
// the host (internal/cliapp/tui_turn_blocks.go) into the ui tier
// (internal/ui/transcript/repaint.go). The host kept the decisions —
// when to repaint, which caches to drop — and the ui tier took the
// arithmetic.
//
// The golden below was produced by the *pre-tranche* tree (9a2857e7):
// the same fixture, the same sequence of repaints and the same dump,
// byte for byte. A change in the engine's bytes is a change in the
// reference's bytes, so the golden is the pin.
//
// The dump masks the thought's elapsed time ("Thought for 12ms"): the
// clock is not part of the transcript's bytes.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tas "charm.land/bubbles/v2/textarea"

	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// thoughtDuration matches the elapsed time the transcript prints in a
// finished thought's title.
var thoughtDuration = regexp.MustCompile(`Thought for [0-9.]+(ms|s|m|h)`)

// TestTranscriptRepaintGolden pins the transcript's repaint bytes.
func TestTranscriptRepaintGolden(t *testing.T) {
	// The golden must not depend on the terminal the test runs under.
	t.Setenv("COLUMNS", "80")

	origScheme, _ := palette.CurrentScheme()
	defer func() {
		palette.SetScheme(origScheme)
		mdrender.SetColorScheme(origScheme)
		mdrender.SetCurrentWidth(80)
	}()
	const scheme = "solarized light"
	palette.SetScheme(scheme)
	mdrender.SetColorScheme(scheme)

	const width = 72
	m := &runMode{
		viewport: uiState{
			width:  width,
			height: 24,
		},
		engine:   engineState{verbosity: "medium"},
		composer: composerState{input: tas.New()},
	}
	m.composer.input.Focus()
	mdrender.SetCurrentWidth(width)

	// 1. The turn separator and the user's prompt.
	m.engine.response = append(m.engine.response, transcript.TurnSeparator(width))

	promptRaw := "Please run the tests and tell me what broke."
	promptLines := transcript.RenderUserPrompt(promptRaw, width)
	promptStart := len(m.engine.response)
	m.engine.response = append(m.engine.response, promptLines...)
	m.engine.userBlocks = append(m.engine.userBlocks, transcript.UserBlock{
		Start: promptStart,
		Lines: len(promptLines),
		Raw:   promptRaw,
	})
	m.engine.response = append(m.engine.response, "")

	// 2. The assistant's markdown reply.
	replyRaw := "The **build** is green. Two checks failed:\n\n- `TestOne`\n- `TestTwo`"
	replyLines := strings.Split(mdrender.RenderMarkdown(replyRaw, width), "\n")
	replyStart := len(m.engine.response)
	m.engine.response = append(m.engine.response, replyLines...)
	m.engine.assistants = append(m.engine.assistants, transcript.AssistantBlock{
		Start: replyStart,
		Lines: len(replyLines),
		Raw:   replyRaw,
	})
	m.engine.response = append(m.engine.response, "")

	// 3. A collapsed tool-step block.
	stepA := toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "run_command",
		Title:   "Bash(go test ./internal/...)",
		Summary: "Ran (go test ./internal/...)",
	}
	linesA := toolsteps.RenderBlock([]toolsteps.Step{stepA}, toolsteps.RenderOptions{
		Expanded:  false,
		Width:     width,
		Verbosity: toolsteps.VerbosityMedium,
	})
	startA := len(m.engine.response)
	m.engine.response = append(m.engine.response, linesA...)
	m.engine.blocks = append(m.engine.blocks, transcript.NewBlock(transcript.Block{
		Start:    startA,
		Lines:    len(linesA),
		Steps:    []toolsteps.Step{stepA},
		Expanded: false,
	}, linesA, width, toolsteps.VerbosityMedium))

	// 4. An expanded tool-step block.
	stepB := toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "view_file",
		Title:   "Read(/tmp/report.txt)",
		Summary: "Read 3 lines",
		Detail:  "one\ntwo\nthree",
	}
	linesB := toolsteps.RenderBlock([]toolsteps.Step{stepB}, toolsteps.RenderOptions{
		Expanded:  true,
		Width:     width,
		Verbosity: toolsteps.VerbosityMedium,
	})
	startB := len(m.engine.response)
	m.engine.response = append(m.engine.response, linesB...)
	m.engine.blocks = append(m.engine.blocks, transcript.NewBlock(transcript.Block{
		Start:    startB,
		Lines:    len(linesB),
		Steps:    []toolsteps.Step{stepB},
		Expanded: true,
	}, linesB, width, toolsteps.VerbosityMedium))

	// 5. A prompt row and a slash echo row that no block owns: the
	// theme repaint reaches them.
	m.engine.response = append(m.engine.response, transcript.UserPromptRow("a plain prompt row"))
	m.engine.response = append(m.engine.response, tuilib.SlashEchoLines("/config", "Exited /config command")...)

	// 6. An in-flight step run.
	liveStep := toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "run_command",
		Title:   "Bash(git status)",
		Summary: "Ran (git status)",
		Running: true,
		Batch:   1,
	}
	liveLines := toolsteps.RenderBlock([]toolsteps.Step{liveStep}, toolsteps.RenderOptions{
		Expanded:  false,
		Width:     width,
		Verbosity: toolsteps.VerbosityMedium,
	})
	liveStart := len(m.engine.response)
	m.engine.response = append(m.engine.response, liveLines...)
	m.engine.liveStepRuns = append(m.engine.liveStepRuns, transcript.LiveStepRun{
		Start: liveStart,
		Lines: len(liveLines),
		Step:  liveStep,
	})
	m.engine.liveThoughtRunIdx = -1

	// 7. A streaming reply in flight.
	m.engine.streaming = true
	m.engine.liveReplyBuf = "A streaming reply that wraps across several rows at the current width."
	streamLines := strings.Split(mdrender.RenderMarkdown(m.engine.liveReplyBuf, width), "\n")
	m.engine.replyStart = len(m.engine.response)
	m.engine.liveTextLines = len(streamLines)
	m.engine.response = append(m.engine.response, streamLines...)

	var dump strings.Builder
	dump.WriteString(dumpTranscript(m, "initial"))

	// 8. The colour scheme changes.
	m.applyThemeLive("dark")
	dump.WriteString(dumpTranscript(m, "theme-dark"))

	// 9. The terminal narrows: the transcript reflows.
	m.reflowTranscript(50)
	dump.WriteString(dumpTranscript(m, "reflow-50"))

	// 10. The verbosity rises.
	m.applyVerbosityLive("high")
	dump.WriteString(dumpTranscript(m, "verbosity-high"))

	// 11. The user expands the trajectory.
	m.toggleTrajectoryExpanded()
	dump.WriteString(dumpTranscript(m, "expanded"))

	// 12. The turn thinks, and the thought is finished.
	m.engine.thinking = true
	m.engine.thinkingSince = time.Now()
	m.engine.liveThinking = "the model reasons about the failing check"
	m.rerenderLiveSteps()
	dump.WriteString(dumpTranscript(m, "live-thought"))
	m.finishLiveThinking()
	dump.WriteString(dumpTranscript(m, "finished-thought"))

	// 13. The terminal narrows without a full reflow: the blocks in the
	// visible window are repainted on the next frame.
	m.viewport.width = 60
	m.ensureVisibleBlocksExpanded(8)
	dump.WriteString(dumpTranscript(m, "visible-window"))

	// 14. The live-step batch path, on a turn of its own: a running
	// call starts the first batch, the finished call re-renders the
	// batch's rows, a second call joins the batch, a thought arrives
	// between the calls, a reply separates the batches, and the next
	// call starts a new batch.
	dump.WriteString(dumpLiveStepBatches(width))

	got := thoughtDuration.ReplaceAllString(dump.String(), "Thought for <dur>")

	golden := filepath.Join("testdata", "transcript_repaint.golden")
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden %s: %v", golden, err)
	}
	if got != string(want) {
		t.Errorf("transcript repaint bytes changed:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// dumpTranscript renders the host's transcript state: the rows and every
// index into them.
func dumpTranscript(m *runMode, stage string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== %s ===\n", stage)
	for i, row := range m.engine.response {
		fmt.Fprintf(&b, "row %d %q\n", i, row)
	}
	for i, blk := range m.engine.blocks {
		fmt.Fprintf(&b, "block %d start=%d lines=%d expanded=%t\n", i, blk.Start, blk.Lines, blk.Expanded)
	}
	for i, a := range m.engine.assistants {
		fmt.Fprintf(&b, "assistant %d start=%d lines=%d\n", i, a.Start, a.Lines)
	}
	for i, u := range m.engine.userBlocks {
		fmt.Fprintf(&b, "user %d start=%d lines=%d\n", i, u.Start, u.Lines)
	}
	for i, r := range m.engine.liveStepRuns {
		fmt.Fprintf(&b, "run %d start=%d lines=%d kind=%v batch=%d\n", i, r.Start, r.Lines, r.Step.Kind, r.Step.Batch)
	}
	fmt.Fprintf(&b, "liveStart=%d replyStart=%d liveThoughtRunIdx=%d liveTextLines=%d liveBatch=%d batchBreak=%t\n",
		m.engine.liveStart, m.engine.replyStart, m.engine.liveThoughtRunIdx, m.engine.liveTextLines, m.engine.liveBatch, m.engine.batchBreak)
	return b.String()
}

// dumpLiveStepBatches drives the live-step batch path and returns the
// dump of every stage.
//
// The fixture is a turn of its own: the calls the model makes while it
// runs, the thought between two of them and the reply that separates
// the batches. Every stage is dumped with its rows and with the
// indexes into them, so a change in the batch arithmetic is a change
// in the golden.
func dumpLiveStepBatches(width int) string {
	// The markdown renderer keeps the width of the last transcript.
	mdrender.SetCurrentWidth(width)

	live := &runMode{
		viewport: uiState{
			width:               width,
			height:              24,
			expanded:            true,
			expandedUserToggled: true,
		},
		engine:   engineState{verbosity: "medium"},
		composer: composerState{input: tas.New()},
	}
	live.composer.input.Focus()
	// The turn's own anchors: no reply is streaming and no thought is
	// live when the first call arrives.
	live.engine.replyStart = -1
	live.engine.liveReplyIdx = -1
	live.engine.liveThoughtRunIdx = -1

	// The turn's prompt.
	const prompt = "run the two checks"
	promptLines := transcript.RenderUserPrompt(prompt, width)
	live.engine.response = append(live.engine.response, promptLines...)
	live.engine.userBlocks = append(live.engine.userBlocks, transcript.UserBlock{
		Start: 0,
		Lines: len(promptLines),
		Raw:   prompt,
	})
	live.engine.response = append(live.engine.response, "")

	var dump strings.Builder

	// 14a. A running call starts the first batch.
	callA := toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "run_command",
		Title:   "Bash(echo one)",
		Summary: "Ran (echo one)",
		Running: true,
	}
	live.updateToolRunningMsg(toolsteps.ToolRunningMsg{Step: callA})
	dump.WriteString(dumpTranscript(live, "live-batch-start"))

	// 14b. The call finishes: the batch's rows are re-rendered, and the
	// block grows by the call's own detail.
	callADone := toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "run_command",
		Title:   "Bash(echo one)",
		Summary: "Ran (echo one)",
		Detail:  "one",
	}
	live.updateToolStepMsg(toolsteps.ToolStepMsg{Step: callADone})
	dump.WriteString(dumpTranscript(live, "live-batch-finish"))

	// 14c. A second call joins the batch: the batch's rows are
	// repainted with both calls.
	callB := toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "run_command",
		Title:   "Bash(echo two)",
		Summary: "Ran (echo two)",
		Running: true,
	}
	live.updateToolRunningMsg(toolsteps.ToolRunningMsg{Step: callB})
	dump.WriteString(dumpTranscript(live, "live-batch-join"))

	// 14d. The model thinks between the calls.
	live.engine.thinking = true
	live.engine.thinkingSince = time.Now()
	live.engine.liveThinking = "the model reasons between the calls"
	live.syncLiveThoughtStep()
	dump.WriteString(dumpTranscript(live, "live-batch-thought"))
	live.finishLiveThinking()
	dump.WriteString(dumpTranscript(live, "live-batch-thought-done"))

	// 14e. A third call joins the batch: the batch's rows reach the
	// end of the buffer, so the thought's own row goes with them.
	callC := toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "run_command",
		Title:   "Bash(echo three)",
		Summary: "Ran (echo three)",
		Running: true,
	}
	live.updateToolRunningMsg(toolsteps.ToolRunningMsg{Step: callC})
	dump.WriteString(dumpTranscript(live, "live-batch-thought-join"))

	// 14f. The third call finishes: the batch's rows are re-rendered
	// with the finished call.
	callCDone := toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "run_command",
		Title:   "Bash(echo three)",
		Summary: "Ran (echo three)",
		Detail:  "three",
	}
	live.updateToolStepMsg(toolsteps.ToolStepMsg{Step: callCDone})
	dump.WriteString(dumpTranscript(live, "live-batch-finish-two"))

	// 14g. The model answers: the reply separates the batches.
	live.updateBackendChunkMsg(backendChunkMsg("the model answers between the batches"))
	dump.WriteString(dumpTranscript(live, "live-batch-break"))

	// 14h. The next call starts a new batch.
	callD := toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "run_command",
		Title:   "Bash(echo four)",
		Summary: "Ran (echo four)",
		Running: true,
	}
	live.updateToolRunningMsg(toolsteps.ToolRunningMsg{Step: callD})
	dump.WriteString(dumpTranscript(live, "live-batch-new"))

	// 14i. The call finishes: the new batch's rows are re-rendered.
	callDDone := toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "run_command",
		Title:   "Bash(echo four)",
		Summary: "Ran (echo four)",
		Detail:  "four",
	}
	live.updateToolStepMsg(toolsteps.ToolStepMsg{Step: callDDone})
	dump.WriteString(dumpTranscript(live, "live-batch-finish-three"))

	return dump.String()
}
