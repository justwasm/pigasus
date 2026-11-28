// trajectory_summary_test.go — the Trajectory view's collapsed line.
//
// The bug these pin: the write path stored the transcript's collapsed
// display line ("Ran (go build ./...)") into render_info's title field,
// and every reader that re-attaches the verb printed "Ran Ran (go
// build ./...)". Both halves are tested because either alone leaves a
// broken path: the write half for new sessions, the read half for the
// rows already on disk.
package tui

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/views/debugpanel"
)

// TestLiveToolStepRoundTripsWithoutDoublingTheVerb walks the real path:
// a live tool step goes through recordTrajectoryCalls, is read back by
// the Trajectory view, and its collapsed line carries one verb.
func TestLiveToolStepRoundTripsWithoutDoublingTheVerb(t *testing.T) {
	m := mkPanel(panelregistry.Debug, "/debug", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.setSessionMessages([]backend.Message{
		{Role: "user", Content: "build it"},
		{Role: "assistant", Content: "done"},
	})

	// A live tool step, exactly as the turn records one.
	live := toolsteps.NewToolStep("run_command",
		[]byte(`{"CommandLine":"go build ./..."}`), "ok", false)
	if live.Summary == "" {
		t.Fatal("the live step has no summary; the test is not exercising the path")
	}
	m.recordTrajectoryCalls([]toolsteps.Step{live})

	ts := m.contextTrajectoryState()
	var tool *debugpanel.TrajStep
	for i := range ts.Steps {
		if ts.Steps[i].Kind == debugpanel.TrajTool {
			tool = &ts.Steps[i]
			break
		}
	}
	if tool == nil {
		t.Fatal("the trajectory has no tool step")
	}

	if n := strings.Count(tool.Summary, "Ran"); n != 1 {
		t.Errorf("summary %q repeats the verb %d times, want exactly 1", tool.Summary, n)
	}
	// The target is bracketed because the transcript brackets it: the
	// reference draws "Ran (go build ./...)", and the line is now built by the
	// package that owns that vocabulary rather than by joining the store's two
	// fields.
	if want := "Ran (go build ./...)"; tool.Summary != want {
		t.Errorf("summary = %q, want %q", tool.Summary, want)
	}
}

// TestStoredDisplayLineStillReadsOnce — the read half, for rows written
// before the fix.
//
// Old sessions carry "Ran (go build ./...)" in the title field. The
// reader must not print the verb twice for them either, which is what
// the guard in toolStepSummary is for.
func TestStoredDisplayLineStillReadsOnce(t *testing.T) {
	s := convstore.Step{
		Type:        convstore.StepToolCall,
		Status:      3,
		ToolName:    "run_command",
		ToolPrefix:  "Ran",
		ToolSummary: "Ran (go build ./...)", // the pre-fix shape on disk
		ToolArgs:    `{"CommandLine":"go build ./..."}`,
	}
	got := toolStepSummary(s)
	if n := strings.Count(got, "Ran"); n != 1 {
		t.Errorf("summary %q repeats the verb %d times, want exactly 1", got, n)
	}
}

// TestStoredNounPhraseReadsWithOneVerb — the shape the reference stores.
func TestStoredNounPhraseReadsWithOneVerb(t *testing.T) {
	s := convstore.Step{
		Type:        convstore.StepToolCall,
		Status:      3,
		ToolName:    "run_command",
		ToolPrefix:  "Ran",
		ToolSummary: "go build ./...",
	}
	// No arguments, so the line is rebuilt from the noun phrase and the
	// category's own shape. The transcript brackets a command's target, so
	// the result carries the parentheses the earlier version of this test did
	// not — the reference draws "Ran (go build ./...)", not "Ran go build
	// ./...".
	if got, want := toolStepSummary(s), "Ran (go build ./...)"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
	// With the arguments present the renderer answers directly, and the line
	// is the same one.
	s.ToolArgs = `{"CommandLine":"go build ./..."}`
	if got, want := toolStepSummary(s), "Ran (go build ./...)"; got != want {
		t.Errorf("with args: summary = %q, want %q", got, want)
	}
}
