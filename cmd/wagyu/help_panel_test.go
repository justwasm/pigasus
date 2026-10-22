package main

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/tuilib"
)

// Test_HelpListWindowFor pins the cycle-182 height-adaptive /help list
// window. Cycle 132 hard-coded 18 rows, which is only right at the
// 30-row geometry the byte-exact snapshots use. The real v1.4.0
// formula, recovered by scanning REF at heights 13..60, is
//
//	n = min(3h/4 - 4, h - 9)   (integer division)
//
// The table below is the measured REF value at each height.
func Test_HelpListWindowFor(t *testing.T) {
	cases := []struct{ h, want int }{
		{13, 4}, {14, 5}, {15, 6}, {16, 7}, {17, 8}, {18, 9},
		{20, 11}, {22, 12}, {24, 14}, {26, 15}, {28, 17}, {30, 18},
		{32, 20}, {33, 20}, {34, 21}, {36, 23}, {38, 24}, {40, 26},
		{50, 33}, {60, 41},
	}
	for _, c := range cases {
		if got := helpListWindowFor(c.h); got != c.want {
			t.Errorf("helpListWindowFor(%d) = %d, want %d", c.h, got, c.want)
		}
	}
}

// Test_HelpListWindowForUnknownHeight verifies the pre-182 fallback:
// before the terminal reports its size (height 0) the window stays 18
// so the byte-exact snapshots and the unit tests are unchanged.
func Test_HelpListWindowForUnknownHeight(t *testing.T) {
	for _, h := range []int{0, -1} {
		if got := helpListWindowFor(h); got != 18 {
			t.Errorf("helpListWindowFor(%d) = %d, want 18", h, got)
		}
	}
	// A degenerate but positive height must still yield at least one
	// row — the overlay can never render an empty window.
	if got := helpListWindowFor(1); got < 1 {
		t.Errorf("helpListWindowFor(1) = %d, want >= 1", got)
	}
}

// Test_HelpListVisible covers the short last window: the window is
// clipped when the catalog does not fill it, and is 0 once the window
// start is past the end.
func Test_HelpListVisible(t *testing.T) {
	cases := []struct{ total, window, size, want int }{
		{99, 0, 18, 18},
		{99, 90, 18, 9}, // last window is short
		{99, 99, 18, 0}, // window start past the end
		{34, 0, 18, 18}, // shortcuts, first window
		{34, 30, 18, 4}, // shortcuts, last window
		{0, 0, 18, 0},   // no catalog
		{5, 0, 18, 5},   // catalog smaller than the window
		{99, 0, 0, 18},  // size < 1 is coerced to 1 -> clipped to 1
	}
	for _, c := range cases {
		if c.size == 0 {
			// helpListVisible coerces size<1 to 1, so the expected
			// value is min(total, 1).
			if got := helpListVisible(c.total, c.window, c.size); got != 1 {
				t.Errorf("helpListVisible(%d,%d,%d) = %d, want 1",
					c.total, c.window, c.size, got)
			}
			continue
		}
		if got := helpListVisible(c.total, c.window, c.size); got != c.want {
			t.Errorf("helpListVisible(%d,%d,%d) = %d, want %d",
				c.total, c.window, c.size, got, c.want)
		}
	}
}

// Test_HelpListNavigation pins the v1.4.0 window model that replaced
// the cycle-137 pagination: an 18-row window at h=30 with a cursor
// over the whole catalog, scrolling one row at a time once the cursor
// reaches an edge, wrapping at both ends.
func Test_HelpListNavigation(t *testing.T) {
	const total = 99
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.width, m.height = 220, 30
	size := m.helpListWindowSize()
	if size != 18 {
		t.Fatalf("window at h=30 = %d, want 18", size)
	}

	// Down 17 times keeps the window at the top and parks the cursor
	// on the last visible row.
	for i := 0; i < size-1; i++ {
		m.helpListDown(total)
	}
	if m.helpCursor != size-1 || m.helpWindow != 0 {
		t.Fatalf("after Down x%d: cursor=%d window=%d, want %d/0",
			size-1, m.helpCursor, m.helpWindow, size-1)
	}
	// One more Down scrolls the window by exactly one row.
	m.helpListDown(total)
	if m.helpCursor != size || m.helpWindow != 1 {
		t.Fatalf("after Down x%d: cursor=%d window=%d, want %d/1",
			size, m.helpCursor, m.helpWindow, size)
	}

	// Ctrl+End jumps to the last row and scrolls to the bottom.
	m.helpListEnd(total)
	if m.helpCursor != total-1 || m.helpWindow != total-size {
		t.Fatalf("Ctrl+End: cursor=%d window=%d, want %d/%d",
			m.helpCursor, m.helpWindow, total-1, total-size)
	}
	// Down at the last row wraps to the first row and the top window.
	m.helpListDown(total)
	if m.helpCursor != 0 || m.helpWindow != 0 {
		t.Fatalf("Down at last: cursor=%d window=%d, want 0/0",
			m.helpCursor, m.helpWindow)
	}
	// Up at the first row wraps to the last row and the bottom window.
	m.helpListUp(total)
	if m.helpCursor != total-1 || m.helpWindow != total-size {
		t.Fatalf("Up at first: cursor=%d window=%d, want %d/%d",
			m.helpCursor, m.helpWindow, total-1, total-size)
	}
	// Ctrl+Home resets both.
	m.helpListHome()
	if m.helpCursor != 0 || m.helpWindow != 0 {
		t.Fatalf("Ctrl+Home: cursor=%d window=%d, want 0/0",
			m.helpCursor, m.helpWindow)
	}
}

// Test_HelpListNavigationShortCatalog covers the SHORTCUTS tab (34
// rows), where the catalog never scrolls: the window stays at 0 and
// only the cursor moves.
func Test_HelpListNavigationShortCatalog(t *testing.T) {
	const total = 34
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.width, m.height = 220, 30
	for i := 0; i < total-1; i++ {
		m.helpListDown(total)
	}
	if m.helpCursor != total-1 {
		t.Fatalf("cursor = %d, want %d", m.helpCursor, total-1)
	}
	// A 34-row catalog in an 18-row window does scroll once past the
	// edge — but only to total-size.
	wantWindow := total - m.helpListWindowSize()
	if m.helpWindow != wantWindow {
		t.Fatalf("window = %d, want %d", m.helpWindow, wantWindow)
	}
	m.helpListDown(total) // wrap
	if m.helpCursor != 0 || m.helpWindow != 0 {
		t.Fatalf("wrap: cursor=%d window=%d, want 0/0", m.helpCursor, m.helpWindow)
	}
}

// Test_HelpListLenMatchesCatalogs guards the row counts the /help
// status indicator prints: REF v1.4.0 reports "of 99 items" on the
// COMMANDS tab and "of 34 items" on SHORTCUTS.
func Test_HelpListLenMatchesCatalogs(t *testing.T) {
	if got := helpListLen(1); got != len(tuilib.DefaultSlashCommands) {
		t.Errorf("helpListLen(1) = %d, want %d", got, len(tuilib.DefaultSlashCommands))
	}
	if got := helpListLen(2); got != len(helpShortcuts) {
		t.Errorf("helpListLen(2) = %d, want %d", got, len(helpShortcuts))
	}
	if got := helpListLen(0); got != 0 {
		t.Errorf("helpListLen(0) = %d, want 0 (GENERAL has no list)", got)
	}
	if len(helpShortcuts) != 34 {
		t.Errorf("helpShortcuts has %d rows, want 34 (v1.4.0)", len(helpShortcuts))
	}
}

// Test_BuildCommandRowTruncation pins the cycle-182 width-relative
// description truncation. REF keeps `width - 52` description cells at
// the measured description column (51, 0-based) and replaces the
// overflow with `<cap-3>...`.
func Test_BuildCommandRowTruncation(t *testing.T) {
	long := strings.Repeat("x", 200)
	c := tuilib.SlashCommand{Name: "/demo", Description: long}

	// Untruncated at 220 columns: cap = 168 -> 165 runes + "...".
	row := buildCommandRow(c, false, 220)
	desc := row[strings.Index(row, cHelpDescOpen)+len(cHelpDescOpen):]
	desc = strings.TrimSuffix(desc, cHelpDescClose)
	if got := len([]rune(desc)); got != 168 {
		t.Errorf("width=220 desc len = %d, want 168", got)
	}
	if !strings.HasSuffix(desc, "...") {
		t.Errorf("width=220 desc should end in ellipsis, got %q", desc[len(desc)-6:])
	}

	// Narrow terminal: cap = width - 52. At w=60 the cap is 8.
	row = buildCommandRow(c, false, 60)
	desc = row[strings.Index(row, cHelpDescOpen)+len(cHelpDescOpen):]
	desc = strings.TrimSuffix(desc, cHelpDescClose)
	if got := len([]rune(desc)); got != 8 {
		t.Errorf("width=60 desc len = %d, want 8", got)
	}

	// width <= 0 disables truncation entirely (the pre-182 behaviour
	// the byte-exact snapshots rely on).
	row = buildCommandRow(c, false, 0)
	desc = row[strings.Index(row, cHelpDescOpen)+len(cHelpDescOpen):]
	desc = strings.TrimSuffix(desc, cHelpDescClose)
	if got := len([]rune(desc)); got != 200 {
		t.Errorf("width=0 desc len = %d, want 200 (no truncation)", got)
	}

	// A short description is never touched, at any width.
	short := tuilib.SlashCommand{Name: "/demo", Description: "short"}
	row = buildCommandRow(short, false, 60)
	if !strings.Contains(row, cHelpDescOpen+"short"+cHelpDescClose) {
		t.Errorf("short description was mangled: %q", row)
	}
}

// Test_BuildCommandRowCursorStyling verifies the COMMANDS tab colours:
// the cursor row wraps the whole name column in brand blue, a normal
// row leaves it plain, and every row's description uses the darker
// description gray.
func Test_BuildCommandRowCursorStyling(t *testing.T) {
	c := tuilib.SlashCommand{Name: "/demo", Description: "does a thing"}

	cursor := buildCommandRow(c, true, 220)
	if !strings.HasPrefix(cursor, helpBlueOpen+"> /demo") {
		t.Errorf("cursor row should open with blue \"> /demo\", got %q", cursor[:40])
	}
	if !strings.Contains(cursor, helpBlueClose) {
		t.Error("cursor row should close the blue wrap")
	}
	if !strings.Contains(cursor, cHelpDescOpen+"does a thing"+cHelpDescClose) {
		t.Errorf("description missing description-gray wrap: %q", cursor)
	}

	normal := buildCommandRow(c, false, 220)
	if strings.HasPrefix(normal, helpBlueOpen) {
		t.Error("normal row should not open with blue")
	}
	if !strings.HasPrefix(normal, "  /demo") {
		t.Errorf("normal row should start with two-space marker, got %q", normal[:20])
	}
}

// Test_RenderHelpGeneralTierSuffix verifies cycle 182: the GENERAL
// panel's email row carries the plan tier in parentheses, exactly like
// the banner. The pre-182 comment claimed REF omitted it; the v1.4.0
// raw capture shows 'navigaid@gmail.com (Antigravity Starter Quota)'.
func Test_RenderHelpGeneralTierSuffix(t *testing.T) {
	var b strings.Builder
	renderHelpGeneral(&b, "Gemini 3.8 Flash (High)", "Antigravity Starter Quota",
		"~/GitHub/openagy", "navigaid@gmail.com", "uuid")
	out := b.String()
	if !strings.Contains(out, helpDimGrayOpen+"navigaid@gmail.com (Antigravity Starter Quota)"+helpDimGrayClose) {
		t.Errorf("email row missing tier suffix: %q", out)
	}

	// An empty tier must not leave a dangling " ()".
	b.Reset()
	renderHelpGeneral(&b, "Gemini 3.8 Flash", "", "~/x", "u@t", "uuid")
	out = b.String()
	if strings.Contains(out, "u@t (") {
		t.Errorf("empty tier should not render parentheses: %q", out)
	}
	if !strings.Contains(out, helpDimGrayOpen+"u@t"+helpDimGrayClose) {
		t.Errorf("bare email row missing: %q", out)
	}
}

// Test_StatusRowRightLabelColouredInHelpMode verifies cycle 182: the
// right-aligned model label keeps its dim-gray wrap while the /help
// overlay owns the screen. Pre-182 it reused leftOpen/leftClose, which
// the helpMode blanking branch had cleared to "", so the label was
// emitted uncoloured while REF keeps `38;2;101;123;131` (tmux) / rgb(95,99,104).
func Test_StatusRowRightLabelColouredInHelpMode(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "Antigravity Starter Quota", "/", true)
	m.width = 220
	m.helpMode = true
	if m.statusModel != "Gemini 3.8 Flash" {
		t.Fatalf("statusModel = %q, want %q", m.statusModel, "Gemini 3.8 Flash")
	}

	row := m.statusRow()
	if !strings.Contains(row, cDimGrayOpen+"Gemini 3.8 Flash"+statusRightPrefix+cDimGrayClose) {
		t.Errorf("helpMode status row lost the right-label colour: %q", row)
	}
	if strings.Contains(row, "? for shortcuts") {
		t.Errorf("helpMode status row should not carry the left hint: %q", row)
	}

	// The idle row must keep the same wrap (regression guard: the fix
	// must not change the byte-exact idle capture).
	m.helpMode = false
	idle := m.statusRow()
	if !strings.Contains(idle, cDimGrayOpen+"? for shortcuts"+cDimGrayClose) {
		t.Errorf("idle left label lost its wrap: %q", idle)
	}
	if !strings.Contains(idle, cDimGrayOpen+"Gemini 3.8 Flash"+statusRightPrefix+cDimGrayClose) {
		t.Errorf("idle right label lost its wrap: %q", idle)
	}
}
