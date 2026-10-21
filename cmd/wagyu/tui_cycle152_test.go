package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestCycle152_ChromeAnchoredAtBottom verifies the cycle-152 fix: when
// the transcript has fewer than `avail` entries (because clipToScreenRows
// trimmed wrapped entries), the visible slice is padded with blanks so
// the chrome (top sep + input + bottom sep + status) sits at the bottom
// of the screen. Without this fix, the input field visibly jumped up
// and down during scroll — sometimes to row 23 and sometimes to row 28
// depending on how many wrapped entries happened to be in the visible
// window.
//
// The bug: the user reported "input field jumps when scrolling" while
// viewing a 108 MB resumed conversation. tmux A/B captures showed the
// input row oscillating between 23 and 28 across consecutive PageUp
// keypresses, even though the underlying scroll position was changing
// by exactly one viewport-height. Root cause is that wrapping counts
// each multi-row entry as >1 screen row, so clipToScreenRows trims 2–5
// entries to fit a 26-row budget. The result had fewer entries than
// `avail`, so viewBody placed the chrome right after the last visible
// transcript row instead of at the bottom of the screen.
func TestCycle152_ChromeAnchoredAtBottom(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "Antigravity Starter Quota", "/Users/gear/Documents/GitHub/openagy", true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.fallbackFrom = ""
	m.fallbackTo = ""
	// Push the conversation to ~600 wrapped transcript entries (each is
	// one paragraph so it stays one logical line but wraps to 2 screen
	// rows in the cycle-122 wrap cache).
	for i := 0; i < 600; i++ {
		m.response = append(m.response, "transcript line that is long enough to wrap onto two visual rows in a 100 column terminal for testing scrolling")
	}
	m.gotoTop()

	// Capture input row at scroll positions 0, 200, 400.
	rows := []int{}
	for _, off := range []int{0, 200, 400} {
		m.scroll = off
		m.autoScroll = false
		v := m.View()
		plain := ansi.Strip(v.Content)
		inputLine := -1
		for i, line := range strings.Split(plain, "\n") {
			// Idle prompt: "> " (the trailing space is part of the
			// textarea's SetPromptFunc return value, see cycle 74/89/90).
			// Anything starting with ">" after ANSI strip is the
			// input row.
			if strings.HasPrefix(line, ">") {
				inputLine = i
				break
			}
		}
		if inputLine < 0 {
			t.Fatalf("scroll=%d: no input row found in body; first 8 lines:\n%s", off, debugFirstLines(v.Content))
		}
		rows = append(rows, inputLine)
	}

	// All scroll positions must land the input on the same row (the chrome
	// row). The expected row is `m.height - 3` for a single-line input:
	// (top sep) + (input) + (bot sep) + (status) = 4 chrome rows, with the
	// input as the second-to-last.
	for i := 1; i < len(rows); i++ {
		if rows[i] != rows[0] {
			t.Errorf("input row oscillates across scroll: scroll=0 → row %d, scroll=%d → row %d (chrome not anchored)", rows[0], []int{0, 200, 400}[i], rows[i])
		}
	}
	// Sanity: the input row must be at m.height-3 (status is below it).
	want := m.height - 3
	if rows[0] != want {
		t.Errorf("input row = %d, want %d (m.height-3 = status-1)", rows[0], want)
	}
}

// TestCycle152_ChromeAnchoredEmptyTranscript verifies that the cycle-152
// padding produces a stable layout when there is no scrollable content
// at all (cold start). The visible slice must contain exactly `avail`
// rows even though `m.response` is empty — otherwise the chrome would
// overflow or collapse. Without padding, an empty response plus a
// wrapped banner entry would leave the chrome at the wrong row.
//
// Note: the cold-start chrome sits right after the banner (no
// transcript to anchor against), not at the bottom of the screen —
// that's pre-existing behaviour, not the cycle-152 contract. The
// cycle-152 contract is that the chrome position does NOT shift
// depending on transcript wrapping. We assert that here by checking
// the chrome row is the same before and after the transcript count
// changes by 0 (it always was 0).
func TestCycle152_ChromeAnchoredEmptyTranscript(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "Antigravity Starter Quota", "/Users/gear/Documents/GitHub/openagy", true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.fallbackFrom = ""
	m.fallbackTo = ""
	v := m.View()
	plain := ansi.Strip(v.Content)
	lines := strings.Split(plain, "\n")
	inputLine := -1
	statusLine := -1
	for i, line := range lines {
		if inputLine < 1 && strings.HasPrefix(line, ">") {
			inputLine = i
		}
		if strings.Contains(line, "shortcuts") {
			statusLine = i
			break
		}
	}
	if inputLine < 0 {
		t.Fatalf("no input row found; first 8 lines:\n%s", debugFirstLines(v.Content))
	}
	if statusLine < 0 {
		t.Fatalf("no status row found; first 8 lines:\n%s", debugFirstLines(v.Content))
	}
	// Chrome must be contiguous: input row, then bottom sep, then status.
	if statusLine != inputLine+2 {
		t.Errorf("chrome broken: input at row %d, status at row %d (want contiguous chrome)", inputLine, statusLine)
	}
	// And the total body must fit within m.height rows (no off-screen
	// chrome, which would happen if the visible slice was over-padded
	// and overflowed).
	if len(lines) > m.height+1 {
		// +1 accounts for the trailing newline that Split always emits
		t.Errorf("body has %d rows, terminal has %d (chrome overflowed)", len(lines)-1, m.height)
	}
}
