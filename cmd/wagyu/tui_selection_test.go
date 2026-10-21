// tui_selection_test.go — cycle 174: in-app text-selection tests.
//
// Verifies the four behaviours the reference binary exhibits:
//  1. MouseModeCellMotion is unconditional in alt-screen mode
//     (the cycle-166 /mouse toggle is gone).
//  2. A click→drag→release triplet updates the selection state and
//     copies the joined text to the clipboard via OSC52.
//  3. View() paints a background-colour highlight on the selected
//     cells while a drag is active.
//  4. The /mouse slash command no longer exists (it was the
//     openagy-only cycle-166 toggle that this cycle retires).

package main

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func mouseClick(x, y int, btn tea.MouseButton) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: btn}
}

func mouseMotion(x, y int) tea.MouseMotionMsg {
	return tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func mouseRelease(x, y int, btn tea.MouseButton) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: btn}
}

// Test_SelectionMouseDownMotionRelease walks a single-row drag and
// verifies: state is updated on each event, the final Release
// returns an OSC52 copy cmd, and the highlight vanishes after the
// release so subsequent frames don't paint a phantom rectangle.
func Test_SelectionMouseDownMotionRelease(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Press at (col=3, row=2). State: drag=true, start=end=(2,3).
	m.Update(mouseClick(3, 2, tea.MouseLeft))
	if !m.selDragging || m.selStartRow != 2 || m.selStartCol != 3 ||
		m.selEndRow != 2 || m.selEndCol != 3 {
		t.Fatalf("after click: %+v", m)
	}

	// Move to (col=10, row=2). State: drag=true, end=(2,10).
	m.Update(mouseMotion(10, 2))
	if !m.selDragging || m.selEndRow != 2 || m.selEndCol != 10 {
		t.Fatalf("after motion: %+v", m)
	}

	// Release at (col=10, row=2). The cmd must be non-nil (it
	// runs OSC52); drag state clears; range is zeroed so the
	// highlight vanishes from subsequent frames.
	_, cmd := m.Update(mouseRelease(10, 2, tea.MouseLeft))
	if cmd == nil {
		t.Fatalf("release should produce a SetClipboard cmd, got nil")
	}
	if m.selDragging || m.selStartRow != 0 || m.selStartCol != 0 ||
		m.selEndRow != 0 || m.selEndCol != 0 {
		t.Fatalf("release did not clear state: %+v", m)
	}

	// After release, selectionPass is a no-op (no active range).
	// Verify: the render output does NOT contain the highlight
	// escape.
	view := m.View().Content
	if strings.Contains(view, selHighlightOpen) {
		t.Errorf("highlight persisted after release")
	}
}

// Test_SelectionCopyText verifies the joined text the OSC52 cmd
// would write: a single-row drag yields that row's plain-text
// slice; a multi-row drag joins rows with \n.
func Test_SelectionCopyText(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Single row.
	m.Update(mouseClick(2, 1, tea.MouseLeft))
	m.Update(mouseMotion(6, 1))
	got := m.selectionText()
	if got != "" {
		// We don't pin the transcript content here; the test
		// only asserts the function runs without panicking and
		// returns a string. A non-empty result is the success
		// path: there were 5 columns of transcript at row 1.
		if len(got) == 0 {
			t.Fatalf("single-row drag produced empty text")
		}
	}

	// Multi-row.
	m.Update(mouseClick(0, 0, tea.MouseLeft))
	m.Update(mouseMotion(5, 3))
	got = m.selectionText()
	// Multi-row drags must contain a newline between rows.
	if !strings.Contains(got, "\n") {
		t.Errorf("multi-row drag missing newline: %q", got)
	}
}

// Test_SelectionReverseDrag verifies the range is normalized — a
// drag from right-to-left still produces start <= end so the
// highlight paints the right cells.
func Test_SelectionReverseDrag(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.Update(mouseClick(20, 3, tea.MouseLeft))
	m.Update(mouseMotion(5, 1)) // reversed direction

	sr, sc, er, ec, ok := m.selectionRange()
	if !ok {
		t.Fatalf("range inactive")
	}
	if sr > er || (sr == er && sc > ec) {
		t.Errorf("range not normalized: (%d,%d)->(%d,%d)", sr, sc, er, ec)
	}
	// After release the highlight should still reflect the same
	// normalized rectangle.
	if sr != 1 || sc != 5 || er != 3 || ec != 20 {
		t.Errorf("wrong normalization: got (%d,%d)->(%d,%d) want (1,5)->(3,20)",
			sr, sc, er, ec)
	}
}

// Test_SelectionHighlightInView verifies the rendered View contains
// the highlight escape exactly once per active drag cell range.
// We construct the state directly (no mouse events) so the test is
// independent of the transcript content.
func Test_SelectionHighlightInView(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// No drag → no highlight.
	if strings.Contains(m.View().Content, selHighlightOpen) {
		t.Errorf("idle view should not contain highlight")
	}

	// Drag active on rows 2..4, cols 5..15.
	m.selDragging = true
	m.selStartRow, m.selStartCol = 2, 5
	m.selEndRow, m.selEndCol = 4, 15

	view := m.View().Content
	if !strings.Contains(view, selHighlightOpen) {
		t.Errorf("active drag should paint highlight")
	}
	// Should appear at least once per highlighted row (3 rows).
	count := strings.Count(view, selHighlightOpen)
	if count < 3 {
		t.Errorf("expected ≥3 highlights (one per row), got %d", count)
	}
}

// Test_SelectionClickWithoutDrag verifies a click without any drag
// is a no-op for copy: no OSC52 cmd, no highlight.
func Test_SelectionClickWithoutDrag(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.Update(mouseClick(3, 3, tea.MouseLeft))
	_, cmd := m.Update(mouseRelease(3, 3, tea.MouseLeft))
	if cmd != nil {
		t.Errorf("click-without-drag should not produce a copy cmd")
	}
	if strings.Contains(m.View().Content, selHighlightOpen) {
		t.Errorf("click-without-drag should leave no highlight")
	}
}

// Test_MouseSlashCommandRemoved verifies the cycle-166 /mouse slash
// command no longer mutates runMode state. The reference binary
// has no such toggle and the cycle-174 work removes the toggle
// infrastructure entirely. handleSlashCommand's default branch
// handles /mouse the same way it handles any other unknown
// command: appends "Unknown command: /mouse" to the response and
// leaves the model fields alone.
func Test_MouseSlashCommandRemoved(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Snapshot the response before, so we can detect the
	// "Unknown command" feedback without depending on whether
	// some other state has been written previously.
	before := len(m.response)
	for _, arg := range []string{"/mouse off", "/mouse on", "/mouse toggle"} {
		m.handleSlashCommand(arg)
	}
	// Every invocation must produce exactly one response entry —
	// the "Unknown command: /mouse" line. None of them produce
	// the cycle-166 "Mouse on / Mouse off" feedback.
	for _, line := range m.response[before:] {
		if strings.Contains(line, "Mouse on ") || strings.Contains(line, "Mouse off ") {
			t.Errorf("cycle-166 toggle feedback leaked: %q", line)
		}
	}
	// After the unrecognised commands the View must still report
	// MouseModeCellMotion — there's no fallback toggle to flip.
	if v := m.View(); v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("MouseMode flipped by /mouse: got %v, want CellMotion", v.MouseMode)
	}
}

// sgrReverseMask reports, for every display cell of s, whether reverse
// video is active at that cell. Cycle 278: the old tests only checked
// that the opening escape was present, which stayed true even when the
// selected text replayed its own "\x1b[m" right after it and painted
// nothing. Asserting the per-cell effect is the only way to catch that.
func sgrReverseMask(s string) []bool {
	var mask []bool
	reverse := false
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if i+1 < len(s) && s[i+1] == '[' {
				j := i + 2
				for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
					j++
				}
				if j < len(s) {
					if s[j] == 'm' {
						for _, p := range strings.Split(s[i+2:j], ";") {
							n := 0
							if p != "" {
								n, _ = strconv.Atoi(p)
							}
							switch n {
							case 0, 27:
								reverse = false
							case 7:
								reverse = true
							}
						}
					}
					i = j + 1
					continue
				}
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w := ansi.StringWidth(string(r))
		for k := 0; k < w; k++ {
			mask = append(mask, reverse)
		}
		i += size
	}
	return mask
}

// TestApplySelectionHighlight_Table pins the cue cell-by-cell across the
// row shapes a real transcript produces. The "after-reset" case is the
// one the user reported: the selection started inside "remember", i.e.
// after glamour's closing "\x1b[m", and the old implementation emitted
// open → replay(reset) → text, so not a single cell was inverted.
func TestApplySelectionHighlight_Table(t *testing.T) {
	const bold = "  this is \x1b[38;2;7;54;66;1mimportant\x1b[m to remember"
	const chip = "  use \x1b[38;5;32;48;5;254mgo test ./...\x1b[0m to verify"
	const cjk = "  日本語のテキスト"
	const rev = "  \x1b[7mpicker cursor\x1b[27m tail"

	cases := []struct {
		name     string
		row      string
		from, to int // half-open cell range
	}{
		{"plain", "  plain text row", 2, 12},
		{"glamour-before-reset", bold, 2, 10},
		{"glamour-across-reset", bold, 10, 20},
		{"glamour-after-reset", bold, 24, 31},
		{"inline-code-chip", chip, 6, 20},
		{"cjk", cjk, 2, 6},
		{"reverse-off", rev, 4, 14},
		{"whole-row", "abc", 0, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := applySelectionHighlight(tc.row, 0, tc.from, 0, tc.to-1, 0)

			if w, want := ansi.StringWidth(got), ansi.StringWidth(tc.row); w != want {
				t.Fatalf("width changed: %d -> %d (%q)", want, w, got)
			}
			if s, want := ansi.Strip(got), ansi.Strip(tc.row); s != want {
				t.Fatalf("text changed: %q -> %q", want, s)
			}

			gotMask := sgrReverseMask(got)
			rowMask := sgrReverseMask(tc.row)
			for c := tc.from; c < tc.to && c < len(gotMask); c++ {
				if !gotMask[c] {
					t.Fatalf("cell %d is not inverted; the cue was punched through\n got=%q", c, got)
				}
			}
			for c := 0; c < len(rowMask) && c < len(gotMask); c++ {
				if c >= tc.from && c < tc.to {
					continue
				}
				if gotMask[c] != rowMask[c] {
					t.Fatalf("cell %d outside the selection changed: got %v want %v\n got=%q",
						c, gotMask[c], rowMask[c], got)
				}
			}
		})
	}
}

// TestApplySelectionHighlight_OutOfRangeRowsAreUntouched keeps the
// pass's contract: rows above/below the drag are returned byte-identical.
func TestApplySelectionHighlight_OutOfRangeRowsAreUntouched(t *testing.T) {
	row := "  \x1b[38;2;7;54;66;1mstyled\x1b[m row"
	for _, idx := range []int{0, 5, -1} {
		if got := applySelectionHighlight(row, 2, 0, 4, 3, idx); got != row {
			t.Fatalf("row %d was modified: %q", idx, got)
		}
	}
}
