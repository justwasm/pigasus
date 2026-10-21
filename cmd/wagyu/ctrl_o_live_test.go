// ctrl_o_live_test.go — cycle 284: ctrl+o must work on the in-flight turn.
//
// Until turnDoneMsg lands, a running turn's tool steps are plain transcript
// rows; m.blocks only records *finished* turns. rerenderBlocks therefore did
// nothing while generating, so ctrl+o flipped m.expanded but the visible
// steps stayed collapsed until the turn completed. rerenderLiveSteps fixes
// that, and these tests drive it through Update() exactly as the key handler
// does.
package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func liveStepFixture(t *testing.T) (*runMode, toolStep) {
	t.Helper()
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.response = append(m.response, "> run it")
	m.liveStart = len(m.response)
	step := toolStep{
		kind:   stepKindTool,
		name:   "run_command",
		title:  "Bash (echo one)",
		detail: "Created At: x\nCompleted At: y\n\nThe command exited with code 0.\nOutput:\nhello-live\n",
	}
	return m, step
}

// TestCtrlOExpandsLiveStepDuringStreaming: a step painted while the turn is
// still running must expand in place on ctrl+o, before turnDoneMsg.
func TestCtrlOExpandsLiveStepDuringStreaming(t *testing.T) {
	m, step := liveStepFixture(t)

	m.Update(toolStepMsg{step: step})

	if strings.Contains(m.View().Content, "hello-live") {
		t.Fatalf("collapsed live step already shows its detail")
	}
	if !strings.Contains(m.View().Content, "ctrl+o to expand") {
		t.Fatalf("collapsed live step is missing the expand hint:\n%s", m.View().Content)
	}

	m.Update(ctrlKey('o'))

	view := m.View().Content
	if !strings.Contains(view, "hello-live") {
		t.Fatalf("ctrl+o did not expand the in-flight step:\n%s", view)
	}
	if !strings.Contains(view, "ctrl+o to collapse") {
		t.Fatalf("live step hint still says expand after ctrl+o:\n%s", view)
	}

	// Collapsing again must restore the single-row form, not leave a
	// half-expanded block behind.
	m.Update(ctrlKey('o'))
	if strings.Contains(m.View().Content, "hello-live") {
		t.Fatalf("second ctrl+o did not collapse the live step:\n%s", m.View().Content)
	}
	if got := len(m.response); got != 2 {
		t.Fatalf("response has %d rows after collapse, want 2 (prompt + step)", got)
	}
}

// TestCtrlOTogglesLiveStepsAroundAStreamedChunk pins the index arithmetic
// when reply text streamed between two live steps: expanding the earlier
// step must shift both the later step's recorded start and replyStart, so a
// second ctrl+o collapses exactly the rows it expanded.
func TestCtrlOTogglesLiveStepsAroundAStreamedChunk(t *testing.T) {
	m, first := liveStepFixture(t)
	second := toolStep{
		kind:   stepKindTool,
		name:   "run_command",
		title:  "Bash (echo two)",
		detail: "Created At: x\nOutput:\ntwo-live\n",
	}
	m.Update(toolStepMsg{step: first})
	m.Update(backendChunkMsg("between the steps"))
	m.Update(toolStepMsg{step: second})
	rowsBefore := len(m.response)

	m.Update(ctrlKey('o'))
	view := m.View().Content
	if !strings.Contains(view, "hello-live") || !strings.Contains(view, "two-live") {
		t.Fatalf("ctrl+o did not expand both live steps:\n%s", view)
	}

	m.Update(ctrlKey('o'))
	if got := len(m.response); got != rowsBefore {
		t.Fatalf("collapse left %d rows, want the original %d:\n%s", got, rowsBefore, strings.Join(m.response, "\n"))
	}
	view = m.View().Content
	if strings.Contains(view, "hello-live") || strings.Contains(view, "two-live") {
		t.Fatalf("details survived the collapse:\n%s", view)
	}
}

// TestTurnDoneKeepsStepsThatFollowAStreamedChunk: when the model streams
// text between two tool calls, replyStart points inside the live step rows.
// The turn-finalisation wipe used to start at replyStart and swallow every
// step after it, so a "2 commands" group header rendered only the first
// command. The whole live range must be rebuilt from msg.steps + reply.
func TestTurnDoneKeepsStepsThatFollowAStreamedChunk(t *testing.T) {
	m, first := liveStepFixture(t)
	second := toolStep{
		kind:   stepKindTool,
		name:   "run_command",
		title:  "Bash (sleep 2; echo two)",
		detail: "Created At: x\nCompleted At: y\n\nThe command exited with code 0.\nOutput:\ntwo\n",
	}

	m.Update(toolStepMsg{step: first})
	m.Update(backendChunkMsg("Running the second one now."))
	if m.replyStart < 0 {
		t.Fatalf("replyStart was not anchored by backendChunkMsg")
	}
	m.Update(toolStepMsg{step: second})
	m.Update(backendChunkMsg("DONE"))

	m.expanded = true
	m.Update(turnDoneMsg{reply: "DONE", steps: []toolStep{first, second}})

	joined := strings.Join(m.response, "\n")
	if !strings.Contains(joined, "echo one") {
		t.Fatalf("finalised block lost the first step:\n%s", joined)
	}
	if !strings.Contains(joined, "echo two") {
		t.Fatalf("finalised block lost the step that followed the streamed chunk:\n%s", joined)
	}
	if !strings.Contains(joined, "DONE") {
		t.Fatalf("finalised block lost the reply:\n%s", joined)
	}
	if i, j := strings.Index(joined, "sleep 2; echo two"), strings.Index(joined, "DONE"); j < i {
		t.Fatalf("reply rendered before the tool block (step at %d, reply at %d)", i, j)
	}
	if len(m.liveStepRuns) != 0 {
		t.Fatalf("liveStepRuns not cleared at turn end: %+v", m.liveStepRuns)
	}
}
