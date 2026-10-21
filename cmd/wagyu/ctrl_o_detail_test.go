// ctrl_o_detail_test.go — cycle 278: expanding a tool group must show the
// command output, not a blank row.
//
// Cycle 277 made `run_command` results CRLF-terminated so the text handed
// to the model is byte-identical to the reference. That string also lands
// in toolStep.detail, and stepDetailLines splits it on "\n" — leaving a
// bare "\r" at the end of every row. ultraviolet treats a bare CR as
// "move cursor to column 0", and the viewport pads each row *after* that
// CR, so the pad repainted the line from the left edge and the expanded
// output rendered blank. The detail is only drawn after ctrl+o, which is
// why the user saw it as "output not displayed after ctrl+o".
package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/backend"
	uv "github.com/charmbracelet/ultraviolet"
)

// TestCtrlOExpandedRunCommandOutputIsNotBlank is the regression guard:
// build a one-step turn whose detail carries the CRLF shape cycle 277
// produces, expand it with ctrl+o, and assert both that no bare CR
// reaches the screen string and that the output text survives an actual
// ultraviolet draw (the padding-after-CR bug is invisible to substring
// checks on the pre-draw rows).
func TestCtrlOExpandedRunCommandOutputIsNotBlank(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})

	m.response = append(m.response, "> ls")
	m.liveStart = len(m.response)
	m.session = append(m.session, backend.Message{Role: "user", Content: "ls"})

	_, _ = m.Update(turnDoneMsg{
		reply: "done",
		steps: []toolStep{{
			kind:  stepKindTool,
			name:  "run_command",
			title: "Bash (ls)",
			detail: "Created At: x\r\nCompleted At: y\r\n\r\n" +
				"The command exited with code 0.\r\nOutput:\r\nhello\r\nworld\r\n",
		}},
	})

	// Collapsed: the detail is not drawn, so the bug is invisible.
	if got := m.View().Content; strings.Contains(got, "hello") {
		t.Fatalf("collapsed view should not show the detail:\n%s", got)
	}

	_, _ = m.Update(ctrlKey('o')) // expand

	content := m.View().Content
	if i := strings.IndexByte(content, '\r'); i >= 0 {
		t.Fatalf("View() emits a bare CR at byte %d; the viewport pads after it, "+
			"so ultraviolet repaints the row from column 0:\n%q", i, content)
	}

	buf := uv.NewScreenBuffer(90, 30)
	uv.NewStyledString(content).Draw(&buf, buf.Bounds())
	screen := buf.String()

	if !strings.Contains(screen, "hello") {
		t.Fatalf("expanded output line 'hello' was blanked after ctrl+o:\n%s", screen)
	}
	if !strings.Contains(screen, "world") {
		t.Fatalf("expanded output line 'world' was blanked after ctrl+o:\n%s", screen)
	}
	if !strings.Contains(screen, "Command output:") {
		t.Fatalf("expanded header 'Command output:' missing after ctrl+o:\n%s", screen)
	}
}

// TestNormalizeDetailNewlines covers the helper directly: CRLF collapses
// to LF, a bare CR is dropped, and a string without CR is returned as the
// same value (no allocation churn on the common path).
func TestNormalizeDetailNewlines(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain", "a\nb", "a\nb"},
		{"crlf", "a\r\nb\r\n", "a\nb\n"},
		{"bare-cr", "progress 10%\rprogress 20%\n", "progress 10%progress 20%\n"},
		{"mixed", "a\r\r\nb\nc\rd", "a\nb\ncd"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeDetailNewlines(tc.in); got != tc.want {
				t.Fatalf("normalizeDetailNewlines(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
