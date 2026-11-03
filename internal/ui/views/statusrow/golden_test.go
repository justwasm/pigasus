// golden_test.go — the status row's bytes, pinned.
//
// The row is a view: it takes the facts and writes the line, and the
// bytes it writes are the session's own. This golden holds the exact
// bytes over the fixture set — the hint rules, the right segment, the
// padding arithmetic, the compaction label and the status cells.
//
// It replaces the out-of-tree two-tree dump of cycle 381's tranche 46
// (docs/cycles/cycle-381-cliapp-split/deliverable.md:817): that dump
// compared the host's rendering before and after the row's move, and it
// lived outside the tree, so nothing in the suite failed when the row
// changed. This one lives with the row.
//
// The fixture set covers the same ground as that dump — the idle row, a
// narrow terminal, an unknown width, a streaming turn, the bash mode, a
// queued prompt, a non-empty composer, the exit confirmation, the four
// modes, the --agent, the --agent with the queue mode, the queue mode
// alone, a running task, the two open panels, the /help overlay, the two
// picker states, the three compaction shapes, the vim badge, the vim
// badge with a buffer and the static non-tty row — and it adds the three
// status cells.
//
// One cell is masked: the running compaction's elapsed time, because it
// is read off the wall clock. The finished compaction's own elapsed
// time is a fixture and is not masked.
package statusrow

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// goldenFixture is one state the golden renders.
//
// maskElapsed marks the fixtures whose elapsed time is read off the wall
// clock; static marks the non-tty row, which is rendered by its own
// function.
type goldenFixture struct {
	name        string
	state       State
	static      bool
	maskElapsed bool
}

// with applies a mutation to a state and returns it.
func with(s State, mut func(*State)) State {
	mut(&s)
	return s
}

// goldenFixtures is the fixture set the golden is rendered from.
func goldenFixtures() []goldenFixture {
	// The running compaction's start is the clock: its elapsed time is
	// masked in the dump.
	running := time.Now().Add(-12 * time.Second)
	// The finished compaction is a fixture: its elapsed time is fixed.
	finished := time.Date(2026, 11, 2, 15, 4, 5, 0, time.UTC)

	return []goldenFixture{
		{name: "idle", state: base()},
		{name: "narrow-terminal", state: with(base(), func(s *State) { s.Width = 80 })},
		{name: "unknown-width", state: with(base(), func(s *State) { s.Width = 0 })},
		{name: "streaming", state: with(base(), func(s *State) { s.Streaming = true })},
		{name: "bash-mode", state: with(base(), func(s *State) { s.BashMode = true })},
		{name: "queued-prompt", state: with(base(), func(s *State) { s.Queued = 1 })},
		{name: "composer-not-empty", state: with(base(), func(s *State) { s.BufferEmpty = false })},
		{name: "exit-confirm", state: with(base(), func(s *State) { s.ExitConfirm = true })},
		{name: "mode-auto", state: with(base(), func(s *State) { s.Mode = "auto" })},
		{name: "mode-plan", state: with(base(), func(s *State) { s.Mode = "plan" })},
		{name: "mode-accept-edits", state: with(base(), func(s *State) { s.Mode = "accept-edits" })},
		{name: "mode-default", state: with(base(), func(s *State) { s.Mode = "" })},
		{name: "agent", state: with(base(), func(s *State) {
			s.AgentName = "reviewer"
			s.EffortSuffix = " · high"
		})},
		{name: "agent-queue-mode", state: with(base(), func(s *State) {
			s.AgentName = "reviewer"
			s.EffortSuffix = " · high"
			s.QueueMode = true
		})},
		{name: "queue-mode", state: with(base(), func(s *State) { s.QueueMode = true })},
		{name: "running-task", state: with(base(), func(s *State) { s.TaskSuffix = " · 2 tasks" })},
		{name: "panel-open", state: with(base(), func(s *State) { s.PanelOpen = true })},
		{name: "panel-keeps-hint", state: with(base(), func(s *State) {
			s.PanelOpen = true
			s.PanelKeepsHint = true
		})},
		{name: "help-overlay", state: with(base(), func(s *State) { s.HelpActive = true })},
		{name: "picker", state: with(base(), func(s *State) { s.PickerVisible = true })},
		{name: "picker-model-mode", state: with(base(), func(s *State) {
			s.PickerVisible = true
			s.PickerModelMode = true
		})},
		{name: "compaction-running", state: with(base(), func(s *State) {
			s.Compact = &Compact{Start: running}
		}), maskElapsed: true},
		{name: "compaction-tokens", state: with(base(), func(s *State) {
			s.Compact = &Compact{Start: running, TokensBefore: 184000, TokensAfter: 1200000}
		}), maskElapsed: true},
		{name: "compaction-done", state: with(base(), func(s *State) {
			s.Compact = &Compact{Start: finished, End: finished.Add(3 * time.Second), TokensBefore: 184000, TokensAfter: 1200}
		})},
		{name: "vim-badge", state: with(base(), func(s *State) { s.VimBadge = "-- INSERT --" })},
		{name: "vim-badge-buffer", state: with(base(), func(s *State) {
			s.VimBadge = "-- INSERT --"
			s.BufferEmpty = false
		})},
		{name: "agent-state", state: with(base(), func(s *State) { s.AgentState = "working" })},
		{name: "context-percent", state: with(base(), func(s *State) { s.ContextUsedPercent = 12.3 })},
		{name: "credits", state: with(base(), func(s *State) {
			s.Credits = 42
			s.CreditsKnown = true
		})},
		{name: "static", state: base(), static: true},
	}
}

// elapsedCell matches the compaction label's elapsed time.
var elapsedCell = regexp.MustCompile(`\(\d+s\)`)

// goldenDump renders every fixture.
//
// The rows are written with their escapes: the colours are part of the
// row's bytes.
func goldenDump() string {
	var b strings.Builder
	for _, f := range goldenFixtures() {
		var row string
		if f.static {
			row = Static(f.state.Model, f.state.CanonicalWidth)
		} else {
			row = Render(f.state)
		}
		if f.maskElapsed {
			row = elapsedCell.ReplaceAllString(row, "(elapsed)")
		}
		b.WriteString("=== " + f.name + " ===\n")
		b.WriteString(row)
		b.WriteString("\n")
	}
	return b.String()
}

// TestRender_Golden pins the row's exact bytes.
//
// The golden is the row's own: a change to the segments, the padding
// arithmetic, the compaction label or the status cells shows up here.
func TestRender_Golden(t *testing.T) {
	got := goldenDump()
	want, err := os.ReadFile(filepath.Join("testdata", "statusrow.golden"))
	if err != nil {
		t.Fatalf("read the golden: %v", err)
	}
	if !bytes.Equal([]byte(got), want) {
		t.Errorf("the status row's bytes changed:\n got %d bytes\nwant %d bytes\nfirst difference at %s",
			len(got), len(want), firstDifference(got, string(want)))
	}
}

// firstDifference names the first byte that differs between two
// renderings.
func firstDifference(got, want string) string {
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	for i := 0; i < n; i++ {
		if got[i] != want[i] {
			return "byte " + strconv.Itoa(i)
		}
	}
	return "byte " + strconv.Itoa(n)
}
