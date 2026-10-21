// slash_cycle222_test.go — cycle 222: non-empty /rewind state.
//
// Cycle 189 shipped the empty-state form (literal `(current)` row only).
// Cycle 222 fills in the conversation-step picker on a multi-turn session:
// one row per user-anchored turn-step, newest first, the cursor row in
// brand blue, an optional `(current)` annotation on the most recent turn
// in dim gray 238, and (height-driven) windowing with a `(first-last of
// N turns)` counter when the conversation overflows the visible window.
//
// The tests below use synthetic session state — Google's OAuth wall
// blocks a real REF capture on this machine without a Google account,
// so the layout is verified against the cycle-189 cross-check (the
// empty-state code path is preserved verbatim) plus the cycle-211/213
// windowed-viewer conventions. See docs/cycles/cycle-222/deliverable-rewind-nonempty.md.

package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/backend"
)

// seedRewindSession writes a synthetic N-turn session into m. Each user
// message is followed by an "ack" assistant message so the trailing pair
// pattern matches a real chat.
func seedRewindSession(m *runMode, snippets []string) {
	for _, s := range snippets {
		m.session = append(m.session,
			backend.Message{Role: "user", Content: s},
			backend.Message{Role: "assistant", Content: "ack: " + s},
		)
	}
}

// TestPanelRewindEmptyStillCycle189 — the cycle-189 test (in
// slash_panels_test.go as TestPanelRewindEmptyLayout) is preserved
// unchanged. This companion pin re-runs the same assertion against the
// cycle-222 code path so any future regression surfaces locally.
func TestPanelRewindEmptyStillCycle189(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	assertLines(t, plainLines(m), []string{
		"Rewind Conversation",
		"",
		"> (current)",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select  esc Cancel",
	})
	if got := m.panelLines()[2]; !strings.Contains(got, cBlueOpen) {
		t.Errorf("selected row should be blue: %q", got)
	}
	if got := (&runMode{}).panelRowCount(panelRewind); got != 1 {
		t.Errorf("panelRowCount(rewind) on empty session = %d, want 1", got)
	}
}

// TestPanelRewindNonEmptyLayout — a 3-turn session opens the picker with
// 3 rows; cursor lands on the most-recent (index 0) by default; each row
// starts with `  ` except the cursor which starts with `> ` and carries
// the brand-blue span. Only the most-recent row carries the `(current)`
// annotation (dim gray 238); other rows are plain (no color).
func TestPanelRewindNonEmptyLayout(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	// 220×50 settles the visible-window budget at 50-5-1 = 44 rows,
	// which is plenty for the 3 turns below.
	m.width, m.height = 220, 50
	seedRewindSession(m, []string{
		"hello there",
		"/statusline",
		"/help me with slices",
	})
	plain := plainLines(m)
	// 7-row skeleton: title, blank, three turn rows, blank, keyboard.
	// No counter at height=50 / 3 turns (counter only appears when
	// total > visible).
	if len(plain) != 7 {
		t.Fatalf("line count = %d, want 7\nplain:\n%s", len(plain),
			strings.Join(plain, "\n"))
	}
	want := []string{
		"Rewind Conversation",
		"",
		"> Turn 1  /help me with slices  (current)",
		"  Turn 2  /statusline",
		"  Turn 3  hello there",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select  esc Cancel",
	}
	for i, w := range want {
		if plain[i] != w {
			t.Errorf("row %d:\n got: %q\nwant: %q", i, plain[i], w)
		}
	}
	// Cursor row (row 2) must carry the brand-blue open code; rows 3-4
	// must NOT.
	styled := m.panelLines()
	if !strings.Contains(styled[2], cBlueOpen) {
		t.Errorf("cursor row missing blue: %q", stripSGR(styled[2]))
	}
	for _, idx := range []int{3, 4} {
		if strings.Contains(styled[idx], cBlueOpen) {
			t.Errorf("non-cursor row %d leaked blue: %q", idx, stripSGR(styled[idx]))
		}
		if strings.Contains(styled[idx], panelGray238) {
			t.Errorf("non-cursor row %d leaked gray-238: %q", idx, stripSGR(styled[idx]))
		}
	}
	if got := m.panelRowCount(panelRewind); got != 3 {
		t.Errorf("panelRowCount(rewind) on 3-turn session = %d, want 3", got)
	}
}

// TestPanelRewindCursorMoves — ↑/↓ move the cursor on a non-empty
// session. The first row keeps the brand-blue prefix only when the
// cursor is on it. REF bounds the cursor with panelRowCount and clamps
// — the test follows the same convention.
func TestPanelRewindCursorMoves(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	m.width, m.height = 220, 50
	seedRewindSession(m, []string{"a", "b", "c"})
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Error("Down not handled")
	}
	if m.panel.cursor != 1 {
		t.Errorf("after Down: cursor = %d, want 1", m.panel.cursor)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Error("Down not handled (2)")
	}
	if m.panel.cursor != 2 {
		t.Errorf("after Down x2: cursor = %d, want 2", m.panel.cursor)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Error("Down not handled (3)")
	}
	if m.panel.cursor != 2 {
		t.Errorf("Down past end: cursor = %d, want 2 (clamp)", m.panel.cursor)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyUp}); !handled {
		t.Error("Up not handled")
	}
	if m.panel.cursor != 1 {
		t.Errorf("after Up: cursor = %d, want 1", m.panel.cursor)
	}
	styled := m.panelLines()
	if !strings.Contains(styled[3], cBlueOpen) {
		t.Errorf("new cursor row 3 missing blue: %q", stripSGR(styled[3]))
	}
}

// TestPanelRewindWindowed_50TurnsHeight10 — a 50-turn session at
// h=10 shows a windowed slice with the dim-gray `(first-last of N
// turns)` counter. Visible budget = h - 5 chrome - 1 status = 4.
// Total output lines = 1 + 1 + 4 + 1 + 1 + 1 = 9.
func TestPanelRewindWindowed_50TurnsHeight10(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	m.width, m.height = 80, 10
	snips := make([]string, 50)
	for i := range snips {
		snips[i] = "turn-step"
	}
	seedRewindSession(m, snips)
	plain := plainLines(m)
	if len(plain) != 9 {
		t.Fatalf("line count at h=10 = %d, want 9\nplain:\n%s",
			len(plain), strings.Join(plain, "\n"))
	}
	if plain[0] != "Rewind Conversation" {
		t.Errorf("row 0 = %q, want title", plain[0])
	}
	if !strings.HasPrefix(plain[2], "> Turn 1") {
		t.Errorf("first visible row should be cursor's Turn 1, got %q", plain[2])
	}
	// 4 visible rows: rows 2,3,4,5 = Turn 1..4.
	if !strings.HasPrefix(plain[5], "  Turn 4") {
		t.Errorf("last visible row should be Turn 4, got %q", plain[5])
	}
	if !strings.Contains(plain[7], "(1\u20134 of 50 turns)") {
		t.Errorf("counter row missing/wrong: %q", plain[7])
	}
	if !strings.Contains(plain[8], "Keyboard:") {
		t.Errorf("keyboard hint missing: %q", plain[8])
	}
}

// TestPanelRewindWindowed_50TurnsHeight20 — at h=20 the visible budget
// is 14 rows; 50 turns still overflows. Total = 1 + 1 + 14 + 1 + 1 + 1 = 19.
func TestPanelRewindWindowed_50TurnsHeight20(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	m.width, m.height = 80, 20
	snips := make([]string, 50)
	for i := range snips {
		snips[i] = "step"
	}
	seedRewindSession(m, snips)
	plain := plainLines(m)
	if len(plain) != 19 {
		t.Fatalf("line count at h=20 = %d, want 19\nplain:\n%s",
			len(plain), strings.Join(plain, "\n"))
	}
	if !strings.HasPrefix(plain[2], "> Turn 1") {
		t.Errorf("first visible: %q", plain[2])
	}
	if !strings.HasPrefix(plain[15], "  Turn 14") {
		t.Errorf("last visible row 15: %q", plain[15])
	}
	if !strings.Contains(plain[17], "(1\u201314 of 50 turns)") {
		t.Errorf("counter: %q", plain[17])
	}
}

// TestPanelRewindWindowed_50TurnsHeight40 — h=40 visible budget is 34.
// 50 turns still overflows. Total = 1 + 1 + 34 + 1 + 1 + 1 = 39.
func TestPanelRewindWindowed_50TurnsHeight40(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	m.width, m.height = 80, 40
	snips := make([]string, 50)
	for i := range snips {
		snips[i] = "step"
	}
	seedRewindSession(m, snips)
	plain := plainLines(m)
	if len(plain) != 39 {
		t.Fatalf("line count at h=40 = %d, want 39\nplain:\n%s",
			len(plain), strings.Join(plain, "\n"))
	}
	if !strings.Contains(plain[37], "(1\u201334 of 50 turns)") {
		t.Errorf("counter at h=40: %q", plain[37])
	}
}

// TestPanelRewindEnterTruncates — selecting a non-current step and
// pressing Enter truncates m.session and emits a slashRespond echo
// block via the cycle-195 `> /rewind / ⎿` format. The panel then
// closes silently (same exit-notice=false set as /mcp /feedback).
func TestPanelRewindEnterTruncates(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	m.width, m.height = 220, 50
	seedRewindSession(m, []string{"first", "second", "third"})
	// Move down twice to land on Turn 3 (the oldest = "first" turn).
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Error("Down not handled")
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Error("Down x2 not handled")
	}
	if m.panel.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", m.panel.cursor)
	}
	// Session has 6 messages before the test runs Enter.
	if len(m.session) != 6 {
		t.Fatalf("session before Enter: %d, want 6", len(m.session))
	}
	// Press Enter — the picker closes (closePanel returns a nil Cmd;
	// the panel-clearing mutation is the deliverable), the echo fires,
	// session truncates.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatalf("Enter not handled")
	}
	// 6 - 2 = 4 messages dropped — the picked Turn 3 (oldest) is the
	// "first" user/assistant pair; everything after that survives.
	if len(m.session) != 2 {
		t.Errorf("after Enter session = %d messages, want 2", len(m.session))
	}
	if m.session[0].Role != "user" || m.session[0].Content != "first" {
		t.Errorf("truncated head user = %+v, want first", m.session[0])
	}
	// The slashRespond echo must be present in m.response.
	resp := strings.Join(m.response, "\n")
	if !strings.Contains(resp, "> /rewind") {
		t.Errorf("echo block missing slashRespond header; m.response=%q",
			strings.Join(m.response, "\n"))
	}
	if !strings.Contains(resp, "Rewound to step") {
		t.Errorf("echo body missing Rewound to step; got %q", resp)
	}
	if !strings.Contains(resp, "4 message(s) removed") {
		t.Errorf("echo should report 4 removed, got %q", resp)
	}
	// Panel must close on Enter.
	if m.panel.kind != panelNone {
		t.Errorf("panel did not close: kind = %v", m.panel.kind)
	}
}

// TestPanelRewindEnterOnCurrent — pressing Enter while on the cursor's
// default (most-recent) position (the `(current)` row) is a no-op:
// no messages are removed, no echo is emitted. This matches REF, which
// silently dismisses the picker when the user picks the current step.
func TestPanelRewindEnterOnCurrent(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	m.width, m.height = 220, 50
	seedRewindSession(m, []string{"a", "b", "c"})
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Error("Enter not handled")
	}
	if len(m.session) != 6 {
		t.Errorf("session = %d messages after Enter-on-current, want 6",
			len(m.session))
	}
	if len(m.response) != 0 {
		t.Errorf("echo should be empty on Enter-on-current; got %q",
			strings.Join(m.response, "\n"))
	}
	// Panel must close on Enter.
	if m.panel.kind != panelNone {
		t.Errorf("panel did not close: kind = %v", m.panel.kind)
	}
}

// TestRewindExitNoticeCycle222 — panelRewind remains in the silent-close
// set (cycle 189 added it; cycle 222 keeps it).
func TestRewindExitNoticeCycle222(t *testing.T) {
	if panelRewind.exitNotice() {
		t.Error("panelRewind.exitNotice() = true, want false (silent close)")
	}
}
