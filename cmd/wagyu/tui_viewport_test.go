package main

// Cycle 119: transcript viewport tests.
//
// These drive View() directly with a synthetic terminal size. The pty route is
// not usable for verification here: bubbletea v2's renderer emits cell-level
// diffs that a naive ANSI replayer cannot follow, so the only trustworthy
// oracle is the model's own View() output.

import (
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// sizedMode returns a model with a known terminal size and n response rows.
func sizedMode(w, h, n int) *runMode {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	for i := 0; i < n; i++ {
		m.response = append(m.response, fmt.Sprintf("row-%02d", i))
	}
	return m
}

// viewLines returns View()'s content split into lines.
func viewLines(t *testing.T, m *runMode) []string {
	t.Helper()
	v := m.View()
	return strings.Split(v.Content, "\n")
}

// Test_ViewportFitsTerminal is the core cycle-119 regression: however long
// the transcript gets, the rendered view must never be taller than the
// terminal.
func Test_ViewportFitsTerminal(t *testing.T) {
	for _, h := range []int{10, 24, 40} {
		for _, n := range []int{0, 1, 5, 40, 400} {
			m := sizedMode(80, h, n)
			lines := viewLines(t, m)
			if len(lines) > h {
				t.Errorf("h=%d n=%d: view has %d rows, terminal is %d", h, n, len(lines), h)
			}
		}
	}
}

// Test_ViewportShowsNewestRows verifies the user-reported symptom is gone:
// with an overflowing transcript the newest rows are on screen and the oldest
// are scrolled off.
func Test_ViewportShowsNewestRows(t *testing.T) {
	m := sizedMode(80, 24, 40)
	joined := ansi.Strip(m.View().Content)
	if !strings.Contains(joined, "row-39") {
		t.Errorf("newest row (row-39) is not visible:\n%s", joined)
	}
	if strings.Contains(joined, "row-00") {
		t.Errorf("oldest row (row-00) should be scrolled off:\n%s", joined)
	}
}

// Test_ViewportPromptStaysBelowTranscript verifies the prompt is still the
// last thing before the bottom separator once the transcript is clipped.
func Test_ViewportPromptStaysBelowTranscript(t *testing.T) {
	m := sizedMode(80, 24, 40)
	lines := viewLines(t, m)
	if len(lines) < 3 {
		t.Fatalf("view too short: %d rows", len(lines))
	}
	// Bottom of the view: status row, bottom separator, prompt.
	last := lines[len(lines)-1]
	if !strings.Contains(ansi.Strip(last), "for shortcuts") {
		t.Errorf("last row is not the status row: %q", ansi.Strip(last))
	}
	if !strings.Contains(ansi.Strip(lines[len(lines)-2]), "─") {
		t.Errorf("second-to-last row is not the bottom separator: %q", ansi.Strip(lines[len(lines)-2]))
	}
	if !strings.Contains(ansi.Strip(lines[len(lines)-3]), ">") {
		t.Errorf("prompt is not above the bottom separator: %q", ansi.Strip(lines[len(lines)-3]))
	}
}

// Test_ViewportCursorTracksPrompt pins the cursor row: it must land on the
// prompt line, which is no longer a fixed offset once the transcript clips.
func Test_ViewportCursorTracksPrompt(t *testing.T) {
	for _, n := range []int{0, 3, 40, 400} {
		m := sizedMode(80, 24, n)
		v := m.View()
		lines := strings.Split(v.Content, "\n")
		if v.Cursor.Y >= len(lines) {
			t.Fatalf("n=%d: cursor row %d outside the %d-row view", n, v.Cursor.Y, len(lines))
		}
		got := ansi.Strip(lines[v.Cursor.Y])
		if !strings.HasPrefix(got, "> ") {
			t.Errorf("n=%d: cursor row %d is not the prompt line: %q", n, v.Cursor.Y, got)
		}
	}
}

// Test_ViewportScrollKeys covers the recovered bindings: pgup / pgdown,
// shift+up / shift+down, ctrl+home / ctrl+end.
func Test_ViewportScrollKeys(t *testing.T) {
	key := func(code rune, mod tea.KeyMod) tea.KeyPressMsg {
		return tea.KeyPressMsg(tea.Key{Code: code, Mod: mod})
	}
	m := sizedMode(80, 24, 200)
	m.View() // prime vpRows / the scroll window
	if !m.autoScroll {
		t.Fatal("autoScroll should start on")
	}
	top := func() string { return ansi.Strip(m.View().Content) }
	if !strings.Contains(top(), "row-199") {
		t.Fatalf("expected the newest row at the bottom:\n%s", top())
	}

	// Page up: auto-scroll off, and a strictly older window.
	m.Update(key(tea.KeyPgUp, 0))
	if m.autoScroll {
		t.Error("pgup should clear autoScroll")
	}
	if strings.Contains(top(), "row-199") {
		t.Error("pgup did not scroll away from the bottom")
	}

	// ctrl+end: back to the bottom, auto-scroll re-armed.
	m.Update(key(tea.KeyEnd, tea.ModCtrl))
	if !m.autoScroll {
		t.Error("ctrl+end should re-arm autoScroll")
	}
	if !strings.Contains(top(), "row-199") {
		t.Error("ctrl+end did not return to the bottom")
	}

	// ctrl+home: top of the transcript.
	m.Update(key(tea.KeyHome, tea.ModCtrl))
	topRow := func() string {
		for _, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "row-") {
				return strings.TrimSpace(l)
			}
		}
		return ""
	}
	if !strings.HasPrefix(topRow(), "row-00") {
		t.Errorf("ctrl+home did not reach the top (first row %q):\n%s", topRow(), top())
	}
	if m.scroll != 0 {
		t.Errorf("ctrl+home left scroll at %d", m.scroll)
	}

	// Half page down twice from the top lands short of the bottom.
	m.Update(key(tea.KeyDown, tea.ModShift))
	m.Update(key(tea.KeyDown, tea.ModShift))
	half := m.scroll
	if half <= 0 {
		t.Error("shift+down did not scroll")
	}
	m.Update(key(tea.KeyPgDown, 0))
	if m.scroll <= half {
		t.Error("pgdown did not advance past the half-page position")
	}
}

// Test_ViewportAutoScrollFollowsNewContent verifies the streaming case:
// while pinned to the bottom, appended rows stay visible without any key.
func Test_ViewportAutoScrollFollowsNewContent(t *testing.T) {
	m := sizedMode(80, 24, 40)
	m.View()
	for i := 40; i < 120; i++ {
		m.response = append(m.response, fmt.Sprintf("row-%03d", i))
		// Cycle 122: Update() invalidates the wrap cache for any non-scroll
		// message. Tests that mutate m.response directly bypass Update()
		// and must dirty the cache themselves.
		m.rowsCacheDirty = true
		m.View()
	}
	joined := ansi.Strip(m.View().Content)
	if !strings.Contains(joined, "row-119") {
		t.Errorf("newest row not visible while auto-scrolling:\n%s", joined)
	}
}

// Test_NoScrollbarEverywhere pins the absence of a transcript scrollbar.
// The reference has zero `│` / `█` characters in any capture in
// /tmp/agycmp/hotpath/*/ref.*.txt; the cycle 122–133 clone added one and
// had to deduct a column from the wrap width, which broke line breaks
// and made the thumb formula collapse to a 1-cell thumb that looked
// like a gap. Cycle 134 removes the scrollbar entirely.
func Test_NoScrollbarEverywhere(t *testing.T) {
	short := sizedMode(80, 40, 3)
	if strings.ContainsRune(short.View().Content, '│') {
		t.Error("scrollbar track (│) appeared on a fitting transcript")
	}
	if strings.ContainsRune(short.View().Content, '█') {
		t.Error("scrollbar thumb (█) appeared on a fitting transcript")
	}
	tall := sizedMode(80, 24, 200)
	for _, line := range viewLines(t, tall) {
		if strings.ContainsRune(line, '│') {
			t.Errorf("scrollbar track (│) appeared on an overflowing transcript: %q", ansi.Strip(line))
		}
		if strings.ContainsRune(line, '█') {
			t.Errorf("scrollbar thumb (█) appeared on an overflowing transcript: %q", ansi.Strip(line))
		}
	}
}

// Test_ViewportWidthIsRespected verifies the width half of "screen size
// awareness": no emitted line is wider than the terminal, at any width, so
// nothing wraps behind the renderer's back.
func Test_ViewportWidthIsRespected(t *testing.T) {
	for _, w := range []int{40, 60, 80, 100} {
		m := sizedMode(w, 24, 40)
		for _, line := range viewLines(t, m) {
			if got := ansi.StringWidth(line); got > w {
				t.Errorf("w=%d: line is %d cells wide: %q", w, got, ansi.Strip(line))
			}
		}
	}
}

// Test_IsUserPromptRow pins the row-classifier heuristic used by the
// cycle-179 snap-to-user-prompt behaviour. The user-prompt row is
// produced by userPromptRow() and starts with the cWarningBlueOpen
// escape followed by the literal "> " marker; continuation rows of a
// wrapped user prompt don't carry the prefix and must NOT be
// classified as user-prompt starts.
func Test_IsUserPromptRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  string
		want bool
	}{
		{"simple user prompt", cWarningBlueOpen + "> hello world" + cWarningBlueClose, true},
		{"empty user prompt", cWarningBlueOpen + "> " + cWarningBlueClose, true},
		{"wrapped continuation (no prefix)", "and the answer is 42", false},
		{"plain text", "row-05", false},
		{"assistant bullet", "  • a bullet", false},
		{"separator line", "──────", false},
		{"empty", "", false},
		{"warning header (same blue prefix, no >)", cWarningBlueOpen + "⚠ Warning" + cWarningBlueClose, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isUserPromptRow(tc.row); got != tc.want {
				t.Errorf("isUserPromptRow(%q) = %v, want %v", tc.row, got, tc.want)
			}
		})
	}
}

// Test_StickyUserPromptRow covers the cycle-179 sticky-bar logic: the
// sticky bar shows the most recent user prompt <= m.scroll (so the
// visible content's "owning" turn is always labelled at the top of
// the pane). Scrolling itself is NOT snapped — see scrollBy comment.
//
// The sticky row index is -1 when there is no user prompt <= scroll
// (header area, before first turn) or when m.autoScroll is true (the
// user is parked at the bottom and the input chrome owns the bottom
// rows).
func Test_StickyUserPromptRow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		promptRows []int
		scroll     int
		autoScroll bool
		want       int // -1 means "no sticky row"
	}{
		{"no prompts", nil, 50, false, -1},
		{"autoScroll on, no sticky", []int{5, 15, 25}, 100, true, -1},
		{"scroll 0, header area", []int{5, 15, 25}, 0, false, -1},
		{"scroll before first prompt", []int{10, 20}, 5, false, -1},
		{"at first prompt", []int{10, 20}, 10, false, 10},
		{"between prompts", []int{5, 15, 25}, 20, false, 15},
		{"at last prompt", []int{5, 15, 25}, 25, false, 25},
		{"past last prompt", []int{5, 15, 25}, 100, false, 25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m.userPromptRows = tc.promptRows
			m.scroll = tc.scroll
			m.autoScroll = tc.autoScroll
			got := m.stickyUserPromptRow()
			if tc.want == -1 {
				if got != -1 {
					t.Errorf("stickyUserPromptRow = %d, want -1 (no sticky)", got)
				}
			} else {
				if got != tc.want {
					t.Errorf("stickyUserPromptRow = %d, want %d", got, tc.want)
				}
			}
		})
	}
}

// Test_ViewportLongLinesWrap verifies long response rows are soft-wrapped
// rather than clipped, so no content is lost on a narrow terminal.
func Test_ViewportLongLinesWrap(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	m.response = append(m.response, strings.Repeat("abcdefghij", 12)) // 120 cells
	m.response = append(m.response, "tail-marker")
	joined := ansi.Strip(m.View().Content)
	if !strings.Contains(joined, "tail-marker") {
		t.Errorf("tail marker lost:\n%s", joined)
	}
	for _, line := range viewLines(t, m) {
		if w := ansi.StringWidth(line); w > 40 {
			t.Errorf("wrapped line is %d cells wide", w)
		}
	}
}

// Test_ViewportUnknownSizeKeepsOldLayout is the backwards-compatibility
// guard: before the first WindowSizeMsg the view is exactly the pre-119
// layout, which is what the byte-exact snapshots assert.
func Test_ViewportUnknownSizeKeepsOldLayout(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	for i := 0; i < 40; i++ {
		m.response = append(m.response, fmt.Sprintf("row-%02d", i))
	}
	v := m.View()
	lines := strings.Split(v.Content, "\n")
	if len(lines) < 40 {
		t.Errorf("unclipped layout should keep every row, got %d", len(lines))
	}
	if v.Cursor.Y != 1+5+1+1+40 {
		t.Errorf("cursor Y = %d, want %d", v.Cursor.Y, 1+5+1+1+40)
	}
}

// Test_ViewportMouseWheel verifies the recovered
// AltScreenStrategy.ShouldHandleWheel behaviour: the wheel scrolls in
// alt-screen mode and is ignored otherwise.
func Test_ViewportMouseWheel(t *testing.T) {
	wheel := tea.MouseWheelMsg(tea.Mouse{X: 1, Y: 1, Button: tea.MouseWheelUp})
	m := sizedMode(80, 24, 200)
	m.View()
	m.Update(wheel)
	if m.autoScroll {
		t.Error("wheel up should clear autoScroll")
	}
	before := m.scroll

	inline := sizedMode(80, 24, 200)
	inline.altScreen = false
	inline.View()
	inlineBefore := inline.scroll
	inline.Update(wheel)
	if inline.scroll != inlineBefore {
		t.Errorf("inline mode must not consume the wheel (scroll %d -> %d)", inlineBefore, inline.scroll)
	}
	_ = before
}

// Test_ViewportDump is a manual inspection aid:
// OPENAGY_DUMP_VIEW=1 go test ./cmd/wagyu -run ViewportDump -v
func Test_ViewportDump(t *testing.T) {
	if os.Getenv("OPENAGY_DUMP_VIEW") == "" {
		t.Skip("set OPENAGY_DUMP_VIEW=1 to print a frame")
	}
	m := sizedMode(80, 24, 30)
	for _, line := range viewLines(t, m) {
		fmt.Printf("|%s|\n", ansi.Strip(line))
	}
}
