package main

// Tests for the reference's bash mode (tui_bash_mode.go).
//
// The byte expectations are copied from the 1.4.4 pty captures in
// docs/cycles/bash-mode/captures.md, not reconstructed from the renderer.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
// failure with output (no status line), failure without output (the Go
// error text), and cancellation (no output, not failed).
func TestExecBashCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("from-cwd"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("success", func(t *testing.T) {
		out, failed, cancelled := execBashCommand(context.Background(), dir, "printf 'a\\nb\\n'")
		if out != "a\nb" || failed || cancelled {
			t.Fatalf("got (%q, %v, %v)", out, failed, cancelled)
		}
	})

	t.Run("cwd", func(t *testing.T) {
		out, failed, _ := execBashCommand(context.Background(), dir, "cat marker.txt")
		if out != "from-cwd" || failed {
			t.Fatalf("got (%q, %v)", out, failed)
		}
	})

	t.Run("failure with output has no status line", func(t *testing.T) {
		out, failed, _ := execBashCommand(context.Background(), dir, "echo out; echo err >&2; exit 3")
		if out != "out\nerr" || !failed {
			t.Fatalf("got (%q, %v)", out, failed)
		}
	})

	t.Run("failure without output is the Go error text", func(t *testing.T) {
		out, failed, _ := execBashCommand(context.Background(), dir, "exit 3")
		if out != "exit status 3" || !failed {
			t.Fatalf("got (%q, %v)", out, failed)
		}
	})

	t.Run("cancel leaves no output and is not a failure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		out, failed, cancelled := execBashCommand(ctx, dir, "sleep 5")
		if out != "" || failed || !cancelled {
			t.Fatalf("got (%q, %v, %v)", out, failed, cancelled)
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
