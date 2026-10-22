package main

// Tests for the reference's bash mode (tui_bash_mode.go).
//
// The byte expectations are copied from the 1.4.4 pty captures in
// docs/cycles/bash-mode/captures.md, not reconstructed from the renderer.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func bashModeRunMode(t *testing.T) *runMode {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (Medium)", "", "/tmp", false)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m
}

// TestBashHeadRow pins the three bullet palettes and the bold-blue "! cmd".
func TestBashHeadRow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failed  bool
		running bool
		want    string
	}{
		{
			name: "success",
			want: "\x1b[38;2;133;153;0m\u25cf\x1b[m \x1b[38;2;38;139;210;1m! echo hello\x1b[m",
		},
		{
			name:   "failure",
			failed: true,
			want:   "\x1b[38;2;220;50;47m\u25cf\x1b[m \x1b[38;2;38;139;210;1m! echo hello\x1b[m",
		},
		{
			name:    "running",
			running: true,
			want:    "\x1b[38;2;181;137;0m\u25cb\x1b[m \x1b[38;2;38;139;210;1m! echo hello\x1b[m",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bashHeadRow("echo hello", tc.failed, tc.running); got != tc.want {
				t.Fatalf("bashHeadRow =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// TestBashOutputRows pins the "  -|  " body: blue bold prefix, ";22" first
// row, continuation rows starting at column 0, red on failure.
func TestBashOutputRows(t *testing.T) {
	cases := []struct {
		name   string
		out    string
		failed bool
		want   []string
	}{
		{
			name: "success single line",
			out:  "hello",
			want: []string{
				"\x1b[38;2;38;139;210;1m  \u23bf  \x1b[38;2;88;110;117;22mhello\x1b[m",
			},
		},
		{
			name: "success multi line",
			out:  "x\ny",
			want: []string{
				"\x1b[38;2;38;139;210;1m  \u23bf  \x1b[38;2;88;110;117;22mx\x1b[m",
				"\x1b[38;2;88;110;117my\x1b[m",
			},
		},
		{
			name:   "failure keeps the output and adds no status line",
			out:    "out\nerr",
			failed: true,
			want: []string{
				"\x1b[38;2;38;139;210;1m  \u23bf  \x1b[38;2;220;50;47;22mout\x1b[m",
				"\x1b[38;2;220;50;47merr\x1b[m",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bashOutputRows(tc.out, tc.failed)
			if len(got) != len(tc.want) {
				t.Fatalf("rows = %d, want %d: %#v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("row %d =\n%q\nwant\n%q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestExecBashCommand covers the four measured result shapes: success,
// failure with output (no status line), failure without output (the Go error
// text), and cancellation (no output, not failed).
func TestExecBashCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("from-cwd"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context, command string) bashDoneMsg {
		t.Helper()
		return execBashCommand(ctx, dir, command, &bashOutput{}, 1)
	}

	t.Run("success", func(t *testing.T) {
		msg := run(context.Background(), "printf 'a\\nb\\n'")
		if msg.output != "a\nb" || msg.failed || msg.cancelled {
			t.Fatalf("got (%q, %v, %v)", msg.output, msg.failed, msg.cancelled)
		}
	})

	t.Run("cwd", func(t *testing.T) {
		msg := run(context.Background(), "cat marker.txt")
		if msg.output != "from-cwd" || msg.failed {
			t.Fatalf("got (%q, %v)", msg.output, msg.failed)
		}
	})

	t.Run("failure with output has no status line", func(t *testing.T) {
		msg := run(context.Background(), "echo out; echo err >&2; exit 3")
		if msg.output != "out\nerr" || !msg.failed {
			t.Fatalf("got (%q, %v)", msg.output, msg.failed)
		}
	})

	t.Run("failure without output is the Go error text", func(t *testing.T) {
		msg := run(context.Background(), "exit 3")
		if msg.output != "exit status 3" || !msg.failed {
			t.Fatalf("got (%q, %v)", msg.output, msg.failed)
		}
	})

	t.Run("cancel leaves no output and is not a failure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		msg := run(ctx, "sleep 5")
		if msg.output != "" || msg.failed || !msg.cancelled {
			t.Fatalf("got (%q, %v, %v)", msg.output, msg.failed, msg.cancelled)
		}
	})

	// The reference stops the whole tree, so a cancelled command must not
	// keep the turn alive for as long as an orphaned grandchild runs. Before
	// the process-group kill this blocked for the full 30 s: `zsh -c` died,
	// `sh -c`/`sleep` inherited the stdout pipe, and exec.Cmd.Wait waited on
	// the copy goroutine until EOF.
	t.Run("cancel kills grandchildren instead of waiting", func(t *testing.T) {
		out := &bashOutput{}
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			for out.String() == "" {
				time.Sleep(5 * time.Millisecond)
			}
			cancel()
		}()
		started := time.Now()
		msg := execBashCommand(ctx, dir, "sh -c 'echo first; sleep 30'", out, 7)
		if elapsed := time.Since(started); elapsed > 5*time.Second {
			t.Fatalf("cancel took %v; grandchildren kept the pipe open", elapsed)
		}
		if !msg.cancelled {
			t.Fatalf("cancelled = false, want true")
		}
		if got := trimBashOutput(out.String()); got != "first" {
			t.Fatalf("partial output = %q, want %q (a preemption snapshots this)", got, "first")
		}
	})
}

// TestBashModeKeyArming: "!" on an empty composer arms the mode and is
// consumed; on a non-empty composer it is a literal rune.
func TestBashModeKeyArming(t *testing.T) {
	m := bashModeRunMode(t)
	m.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
	if !m.bashMode {
		t.Fatal("! on empty composer did not arm bash mode")
	}
	if m.input.Value() != "" {
		t.Fatalf("buffer = %q, want empty (the ! is the mode marker)", m.input.Value())
	}

	// Esc leaves the mode.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.bashMode {
		t.Fatal("Esc did not leave bash mode")
	}

	// A space then "!" is ordinary text: the mode only arms on an empty
	// buffer (【一手】 ` !echo lead` went to the model).
	m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	m.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
	if m.bashMode {
		t.Fatal("! after a space armed bash mode")
	}
	if got := m.input.Value(); got != " !" {
		t.Fatalf("buffer = %q, want %q", got, " !")
	}
}

// TestBashModeSubmitRunsLocally: Enter in bash mode never touches the
// model. The transcript gets the running head row and the mode clears.
func TestBashModeSubmitRunsLocally(t *testing.T) {
	m := bashModeRunMode(t)
	m.bashMode = true
	m.input.SetValue("echo hello")

	cmd := m.submitPrompt()
	if cmd == nil {
		t.Fatal("submitPrompt returned nil, want the exec cmd")
	}
	if !m.streaming {
		t.Fatal("streaming not set while the command runs")
	}
	if m.bashMode {
		t.Fatal("bash mode survived submit")
	}
	if len(m.session) != 0 {
		t.Fatalf("shell command reached the model session: %#v", m.session)
	}
	head := m.response[len(m.response)-1]
	if want := bashHeadRow("echo hello", false, true); head != want {
		t.Fatalf("head row =\n%q\nwant\n%q", head, want)
	}
	if got := m.history[len(m.history)-1]; got != "!echo hello" {
		t.Fatalf("history entry = %q, want %q", got, "!echo hello")
	}

	// Completion repaints the bullet and appends the body + blank row.
	m.finishBashCommand(bashDoneMsg{cmd: "echo hello", output: "hello"})
	if m.streaming {
		t.Fatal("streaming survived completion")
	}
	if m.bashRowIdx != -1 {
		t.Fatalf("bashRowIdx = %d, want -1", m.bashRowIdx)
	}
	if want := bashHeadRow("echo hello", false, false); m.response[len(m.response)-3] != want {
		t.Fatalf("final head row = %q", m.response[len(m.response)-3])
	}
	if m.response[len(m.response)-1] != "" {
		t.Fatal("no trailing blank row after the block")
	}
}

// TestBashModeEmptyCommandJustLeaves: `!` + Enter prints nothing.
func TestBashModeEmptyCommandJustLeaves(t *testing.T) {
	m := bashModeRunMode(t)
	m.bashMode = true
	if cmd := m.submitPrompt(); cmd != nil {
		t.Fatal("empty bash command returned a cmd")
	}
	if m.bashMode {
		t.Fatal("bash mode survived the empty submit")
	}
	if len(m.response) != 0 {
		t.Fatalf("empty bash command wrote a transcript row: %#v", m.response)
	}
}

// TestBashModeCancel: Esc runs the cancel func and completion leaves the
// head row without a body (【一手】 `!sleep 5` + Esc).
func TestBashModeCancel(t *testing.T) {
	m := bashModeRunMode(t)
	cancelled := false
	m.bashCancel = func() { cancelled = true }
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !cancelled {
		t.Fatal("Esc did not cancel the running command")
	}
	if m.bashCancel != nil {
		t.Fatal("bashCancel still installed")
	}
}

// TestBashHistoryRecall re-arms the mode when the entry was a command.
func TestBashHistoryRecall(t *testing.T) {
	m := bashModeRunMode(t)
	m.history = []string{"hello", "!echo hi"}
	m.input.Reset()

	m.historyUp()
	if !m.bashMode || m.input.Value() != "echo hi" {
		t.Fatalf("recall = (%v, %q), want (true, %q)", m.bashMode, m.input.Value(), "echo hi")
	}
	m.historyUp()
	if m.bashMode || m.input.Value() != "hello" {
		t.Fatalf("recall = (%v, %q), want (false, %q)", m.bashMode, m.input.Value(), "hello")
	}
}

// TestBashPreemptsRunningCommand: a shell command submitted while another is
// running interrupts it — the old block keeps the output it had produced and
// the new command starts at once, instead of queueing behind an Esc.
//
// 【一手】 agy 1.4.5 `!sh -c 'echo first; sleep 30'` then `!echo queued2`:
// the first block gains `  ⎿  first` while keeping its hollow `○`, and the
// second block completes, all before any key press.
func TestBashPreemptsRunningCommand(t *testing.T) {
	m := bashModeRunMode(t)
	m.streaming = true
	m.bashGen = 4
	m.bashRowIdx = len(m.response)
	m.response = append(m.response, bashHeadRow("top", false, true))
	headIdx := m.bashRowIdx
	out := &bashOutput{}
	if _, err := out.Write([]byte("first\nsecond\n")); err != nil {
		t.Fatal(err)
	}
	m.bashRun = out
	m.bashCmd = "top"
	cancelled := false
	m.bashCancel = func() { cancelled = true }

	m.bashMode = true
	m.input.SetValue("echo next")
	cmd := m.submitPrompt()
	if cmd == nil {
		t.Fatal("preemption returned no cmd for the new command")
	}
	if !cancelled {
		t.Fatal("the running command was not cancelled")
	}
	if len(m.bashQueue) != 0 {
		t.Fatalf("preemption queued instead of running: %#v", m.bashQueue)
	}
	if !m.streaming {
		t.Fatal("streaming cleared during preemption")
	}
	if got := m.response[headIdx]; got != bashHeadRow("top", false, true) {
		t.Fatalf("interrupted head was repainted:\n%q", got)
	}
	wantBody := bashOutputRows("first\nsecond", false)
	for i, want := range wantBody {
		if got := m.response[headIdx+1+i]; got != want {
			t.Fatalf("body row %d =\n%q\nwant\n%q", i, got, want)
		}
	}
	if got := m.response[len(m.response)-1]; got != bashHeadRow("echo next", false, true) {
		t.Fatalf("new running head =\n%q", got)
	}
	if m.bashGen != 5 {
		t.Fatalf("bashGen = %d, want 5", m.bashGen)
	}

	// The preempted command's late completion carries the stale generation
	// and must not touch the live block.
	before := len(m.response)
	m.updateBashDoneMsg(bashDoneMsg{gen: 4, cmd: "top", output: "late"})
	if len(m.response) != before || !m.streaming {
		t.Fatal("stale completion mutated the live block")
	}
}

// TestBashCtrlCCancelsCommand: Ctrl+C stops a running shell command and still
// arms the two-step exit confirmation on the same press (【一手】 agy 1.4.5).
func TestBashCtrlCCancelsCommand(t *testing.T) {
	m := bashModeRunMode(t)
	cancelled := false
	m.bashCancel = func() { cancelled = true }
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !cancelled {
		t.Fatal("Ctrl+C did not cancel the running command")
	}
	if !m.exitConfirm {
		t.Fatal("Ctrl+C did not arm the exit confirmation")
	}
	if m.quitting {
		t.Fatal("the first Ctrl+C quit")
	}
}

// TestBashCancelDropsPartialOutput: Esc / Ctrl+C finalise the block with the
// head only, even when the command had already printed something.
func TestBashCancelDropsPartialOutput(t *testing.T) {
	m := bashModeRunMode(t)
	m.bashRowIdx = 0
	m.response = []string{bashHeadRow("x", false, true)}
	m.finishBashCommand(bashDoneMsg{gen: 1, cmd: "x", output: "first", cancelled: true})
	if len(m.response) != 2 {
		t.Fatalf("cancelled block rows = %#v, want head + blank", m.response)
	}
	if want := bashHeadRow("x", false, false); m.response[0] != want {
		t.Fatalf("head = %q, want %q", m.response[0], want)
	}
	if m.response[1] != "" {
		t.Fatalf("expected the trailing blank row, got %q", m.response[1])
	}
}

// TestBashBlockTruncates pins the collapsed long-output form: the elided
// count, the last `height/2` rows, and the expand hint on the final row.
//
// 【一手】 agy 1.4.5 at 120×40: `!seq 1 21` →
//
//	  ⎿  <output +1 lines>
//	2 … 21 (ctrl+o to expand)
func TestBashBlockTruncates(t *testing.T) {
	lines := make([]string, 21)
	for i := range lines {
		lines[i] = fmt.Sprintf("%d", i+1)
	}
	rows := bashBlockLines(&bashBlock{cmd: "seq 1 21", body: strings.Join(lines, "\n")}, false, 40)

	// head + marker + 20 body rows + blank
	if len(rows) != 23 {
		t.Fatalf("rows = %d, want 23: %#v", len(rows), rows)
	}
	if want := bashHeadRow("seq 1 21", false, false); rows[0] != want {
		t.Fatalf("head = %q, want %q", rows[0], want)
	}
	wantMarker := cWarningBlueOpen + "  \u23bf  " + bashBodyOKOpen + "<output +1 lines>\x1b[m"
	if rows[1] != wantMarker {
		t.Fatalf("marker = %q, want %q", rows[1], wantMarker)
	}
	if want := cSubtleOpen + "2\x1b[m"; rows[2] != want {
		t.Fatalf("first kept row = %q, want %q (continuation style, no \u23bf)", rows[2], want)
	}
	last := rows[len(rows)-2]
	if !strings.Contains(last, "21\x1b[m ") || !strings.Contains(last, "(ctrl+o to expand)") {
		t.Fatalf("last row = %q, want 21 + expand hint", last)
	}
	if rows[len(rows)-1] != "" {
		t.Fatal("missing trailing blank row")
	}

	// 20 rows never truncate and never show a hint (【一手】 `!seq 1 20`).
	rows = bashBlockLines(&bashBlock{cmd: "seq 1 20", body: strings.Join(lines[:20], "\n")}, false, 40)
	if len(rows) != 22 {
		t.Fatalf("20-line rows = %d, want 22", len(rows))
	}
	for _, r := range rows {
		if strings.Contains(r, "ctrl+o") || strings.Contains(r, "<output") {
			t.Fatalf("short body grew a marker/hint: %q", r)
		}
	}

	// The cap is half the terminal height (measured 20/25/30 at 40/50/60):
	// 40 lines at height 60 show 30 and elide 10.
	var forty []string
	for i := 1; i <= 40; i++ {
		forty = append(forty, fmt.Sprintf("%d", i))
	}
	rows = bashBlockLines(&bashBlock{cmd: "seq 1 40", body: strings.Join(forty, "\n")}, false, 60)
	if !strings.Contains(rows[1], "<output +10 lines>") {
		t.Fatalf("60-row terminal marker = %q, want <output +10 lines>", rows[1])
	}
	if len(rows) != 33 { // head + marker + 30 + blank
		t.Fatalf("60-row rows = %d, want 33", len(rows))
	}
}

// TestBashBlockExpanded: ctrl+o drops the marker, shows the full body and
// flips the hint to "(ctrl+o to collapse)".
func TestBashBlockExpanded(t *testing.T) {
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, fmt.Sprintf("%d", i))
	}
	b := &bashBlock{cmd: "seq 1 200", body: strings.Join(lines, "\n")}

	collapsed := bashBlockLines(b, false, 40)
	expanded := bashBlockLines(b, true, 40)
	if len(expanded) <= len(collapsed) {
		t.Fatalf("expanded rows = %d, collapsed = %d", len(expanded), len(collapsed))
	}
	for _, r := range expanded {
		if strings.Contains(r, "<output +") {
			t.Fatalf("expanded block kept the marker: %q", r)
		}
	}
	last := expanded[len(expanded)-2]
	if !strings.Contains(last, "200\x1b[m ") || !strings.Contains(last, "(ctrl+o to collapse)") {
		t.Fatalf("expanded last row = %q", last)
	}
}

// TestBashCtrlOTogglesBlock: the ctrl+o key re-renders the recorded block in
// place, so the transcript row count follows the hint.
func TestBashCtrlOTogglesBlock(t *testing.T) {
	m := bashModeRunMode(t)
	m.bashRowIdx = len(m.response)
	m.response = append(m.response, bashHeadRow("seq 1 21", false, true))
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, fmt.Sprintf("%d", i))
	}
	m.finishBashCommand(bashDoneMsg{gen: 1, cmd: "seq 1 200", output: strings.Join(lines, "\n")})
	collapsed := len(m.response)

	m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	expanded := len(m.response)
	if expanded != collapsed+179 {
		t.Fatalf("expanded rows = %d, want %d (179 more than the marker + 20 kept rows)", expanded, collapsed+179)
	}
	if strings.Contains(strings.Join(m.response, "\n"), "<output +") {
		t.Fatal("ctrl+o kept the truncation marker")
	}
	if !strings.Contains(m.response[len(m.response)-2], "(ctrl+o to collapse)") {
		t.Fatalf("missing collapse hint: %q", m.response[len(m.response)-2])
	}

	m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if len(m.response) != collapsed {
		t.Fatalf("collapse rows = %d, want %d", len(m.response), collapsed)
	}
}

// TestBashQueueWhileStreaming: a command submitted mid-turn is queued and
// rendered as "▸ <cmd>" without the (alt+m …) prompt suffix.
func TestBashQueueWhileStreaming(t *testing.T) {
	m := bashModeRunMode(t)
	m.streaming = true
	m.bashMode = true
	m.input.SetValue("echo queued")
	if cmd := m.submitPrompt(); cmd != nil {
		t.Fatal("a queued bash command returned a cmd")
	}
	if len(m.bashQueue) != 1 || m.bashQueue[0] != "echo queued" {
		t.Fatalf("bashQueue = %#v", m.bashQueue)
	}
	if m.streaming != true {
		t.Fatal("queueing cleared the streaming flag")
	}
	if view := m.View().Content; !strings.Contains(view, "\u25b8 echo queued") {
		t.Fatalf("queue row missing from view:\n%s", view)
	}
}
