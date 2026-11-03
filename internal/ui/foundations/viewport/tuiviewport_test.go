package viewport_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

func TestSelectionRange(t *testing.T) {
	sr, sc, er, ec, active := tuiviewport.SelectionRange(0, 0, 0, 0, false)
	if active {
		t.Errorf("expected inactive for zeros, got %d,%d to %d,%d", sr, sc, er, ec)
	}

	sr, sc, er, ec, active = tuiviewport.SelectionRange(1, 5, 1, 5, false)
	if active {
		t.Errorf("expected inactive for click without drag")
	}

	sr, sc, er, ec, active = tuiviewport.SelectionRange(5, 10, 2, 3, true)
	if !active || sr != 2 || sc != 3 || er != 5 || ec != 10 {
		t.Errorf("expected reverse drag normalized, got %d,%d to %d,%d active=%v", sr, sc, er, ec, active)
	}
}

func TestApplySelectionHighlight(t *testing.T) {
	row := "Hello, World!"
	highlighted := tuiviewport.ApplySelectionHighlight(row, 0, 0, 0, 4, 0)
	if !strings.Contains(highlighted, tuiviewport.SelHighlightOpen) {
		t.Errorf("expected highlight open escape, got %q", highlighted)
	}
	if !strings.Contains(highlighted, "Hello") {
		t.Errorf("expected Hello in highlight, got %q", highlighted)
	}

	untouched := tuiviewport.ApplySelectionHighlight(row, 1, 0, 2, 5, 0)
	if untouched != row {
		t.Errorf("expected untouched row outside selection range, got %q", untouched)
	}
}

func TestSelectionText(t *testing.T) {
	rows := []string{"Hello", "World"}
	text := tuiviewport.SelectionText(rows, 0, 0, 1, 4)
	if text != "Hello\nWorld" {
		t.Errorf("expected Hello\\nWorld, got %q", text)
	}
}

// linkRows builds two rows that are pieces of one hyperlink: a URL
// cut into two chunks, each chunk carrying the complete URL as its
// OSC 8 target, each row indented like a transcript row.
func linkRows(url string) []string {
	half := len(url) / 2
	chunk := func(s string) string {
		return "\x1b]8;;" + url + "\x1b\\\x1b[34;4m" + s + "\x1b[m\x1b]8;;\x1b\\"
	}
	return []string{"     " + chunk(url[:half]), "     " + chunk(url[half:])}
}

// TestSelectionText_JoinsLinkChunks pins the link rule: two rows that
// are pieces of the same hyperlink are one logical string, so the copy
// carries no newline between them and no indent in front of the
// continuation.
//
// A URL too long for the terminal is cut into chunks, one chunk per
// row (internal/urlchunk) — a copy that kept the newline and the
// indent would be a stack of lines, not a URL.
func TestSelectionText_JoinsLinkChunks(t *testing.T) {
	const url = "https://example.com/a?b=c&d=e"
	rows := linkRows(url)

	// The whole selection, starting at the first column: the first
	// row's indent is the layout's and stays, the continuation's is
	// dropped.
	got := tuiviewport.SelectionText(rows, 0, 0, 1, 20)
	if strings.Contains(got, "\n") {
		t.Errorf("the copy carries a newline: %q", got)
	}
	if want := "     " + url; got != want {
		t.Errorf("the copy = %q, want %q", got, want)
	}

	// A selection that starts at the link's first character copies the
	// URL and nothing else.
	got = tuiviewport.SelectionText(rows, 0, 5, 1, 20)
	if got != url {
		t.Errorf("the copy = %q, want %q", got, url)
	}
}

// TestSelectionText_KeepsTheNewlineBetweenDifferentLinks pins the
// other side of the rule: two rows that link to *different* URLs are
// two lines, and the newline stays.
func TestSelectionText_KeepsTheNewlineBetweenDifferentLinks(t *testing.T) {
	rows := []string{
		"     \x1b]8;;https://a.example/\x1b\\\x1b[34;4mhttps://a.example/\x1b[m\x1b]8;;\x1b\\",
		"     \x1b]8;;https://b.example/\x1b\\\x1b[34;4mhttps://b.example/\x1b[m\x1b]8;;\x1b\\",
	}
	got := tuiviewport.SelectionText(rows, 0, 0, 1, 20)
	if !strings.Contains(got, "\n") {
		t.Errorf("the copy lost the newline between two different links: %q", got)
	}
}

func TestClipToScreenRows(t *testing.T) {
	rows := []string{"a\nb", "c", "d"}
	counts := tuiviewport.ScreenRowCounts(rows)
	if counts[0] != 2 || counts[1] != 1 || counts[2] != 1 {
		t.Fatalf("unexpected counts: %v", counts)
	}

	clipped := tuiviewport.ClipToScreenRows(rows, counts, 0, 3)
	if len(clipped) != 2 {
		t.Errorf("expected 2 entries (3 screen rows total), got %d entries", len(clipped))
	}
}

func TestClipToScreenRowsPrefix(t *testing.T) {
	rows := []string{"a", "b", "c", "d"}
	prefix := []int{0, 1, 2, 3, 4}
	clipped := tuiviewport.ClipToScreenRowsPrefix(rows, prefix, 0, 2)
	if len(clipped) != 2 {
		t.Errorf("expected 2 rows, got %v", clipped)
	}
}

func TestWrapSlotAndComposedRows(t *testing.T) {
	var slot tuiviewport.WrapSlot
	resp := []string{"line1", "line2"}
	if !slot.Rebuild(resp, 80) {
		t.Errorf("expected rebuild to return true on first run")
	}
	if slot.Rebuild(resp, 80) {
		t.Errorf("expected rebuild to return false on unchanged resp")
	}

	var comp tuiviewport.ComposedRows
	header := []string{"header1"}
	live := []string{"live1"}
	comp.Rebuild(1, 80, header, slot.Rows, live, slot.Counts)
	if !comp.Fresh(1, 80, header, live) {
		t.Errorf("expected composedRows to be fresh")
	}
	if len(comp.Rows) != 4 {
		t.Errorf("expected 4 composed rows, got %d", len(comp.Rows))
	}
}

func TestPromptAndScrollHelpers(t *testing.T) {
	promptRow := palette.WarningBlueOpen + "> Hello"
	if !tuiviewport.IsUserPromptRow(promptRow) {
		t.Errorf("expected promptRow to be recognized")
	}
	nonPrompt := "Just text"
	if tuiviewport.IsUserPromptRow(nonPrompt) {
		t.Errorf("expected nonPrompt to not be recognized")
	}

	userPrompts := []int{5, 15, 25}
	sticky := tuiviewport.StickyUserPromptRow(userPrompts, false, 18)
	if sticky != 15 {
		t.Errorf("expected sticky prompt 15, got %d", sticky)
	}

	pinned := tuiviewport.RenderPinnedPrompt(promptRow, 80)
	if strings.Contains(pinned, "jump to prompt") {
		t.Errorf("expected no jump hint in pinned prompt, got %q", pinned)
	}

	// Wide prompt: must be truncated within the requested width without hint suffix
	widePrompt := "> " + strings.Repeat("long question ", 15)
	pinnedWide := tuiviewport.RenderPinnedPrompt(widePrompt, 40)
	if strings.Contains(pinnedWide, "jump to prompt") {
		t.Errorf("expected no jump hint for wide prompt in %q", pinnedWide)
	}
	if ansi.StringWidth(pinnedWide) > 40 {
		t.Errorf("expected pinnedWide width <= 40, got %d", ansi.StringWidth(pinnedWide))
	}


	if !tuiviewport.IsScrollKey(tea.Key{Code: tea.KeyPgUp}) {
		t.Errorf("expected KeyPgUp to be scroll key")
	}
}

func TestFormatVisibleRows_AltScreenAlignment(t *testing.T) {
	visible := []string{"row1", "row2"}
	// When altScreen is false, no sticky header must be prepended even if stickyBarRow or pinnedPromptText are set
	outInline := tuiviewport.FormatVisibleRows(visible, 40, "> pinned", "> sticky", false)
	if len(outInline) != 2 {
		t.Errorf("inline mode: expected 2 rows, got %d", len(outInline))
	}
	if strings.TrimSpace(outInline[0]) != "row1" {
		t.Errorf("inline mode: expected row1 first, got %q", outInline[0])
	}

	// When altScreen is true and pinnedPromptText is non-empty, prepends 2 rows (pinned prompt + rule)
	outAlt := tuiviewport.FormatVisibleRows(visible, 40, "> pinned", "", true)
	if len(outAlt) != 4 {
		t.Fatalf("altscreen mode: expected 4 rows (2 sticky + 2 visible), got %d", len(outAlt))
	}
	if !strings.HasPrefix(outAlt[0], "> pinned") {
		t.Errorf("altscreen mode: expected pinned prompt at row 0, got %q", outAlt[0])
	}
	if !strings.Contains(outAlt[1], "─") {
		t.Errorf("altscreen mode: expected separator rule at row 1, got %q", outAlt[1])
	}
}

func TestCapAtHeldLines(t *testing.T) {
	// If no lines held, splitPoint is untouched
	if got := tuiviewport.CapAtHeldLines(10, 20, 0); got != 10 {
		t.Errorf("expected 10, got %d", got)
	}

	// 5 lines held at the end of 20 lines -> max split is 15.
	// A splitPoint of 12 (< 15) should remain 12.
	if got := tuiviewport.CapAtHeldLines(12, 20, 5); got != 12 {
		t.Errorf("expected 12, got %d", got)
	}

	// A splitPoint of 18 (> 15) should be capped at 15 so the 5 held lines stay in view.
	if got := tuiviewport.CapAtHeldLines(18, 20, 5); got != 15 {
		t.Errorf("expected 15, got %d", got)
	}

	// If heldLines >= totalLines, splitPoint is capped at 0.
	if got := tuiviewport.CapAtHeldLines(5, 10, 12); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

func TestIncompleteTableLines(t *testing.T) {
	// No table lines
	rows1 := []string{"hello", "world"}
	if got := tuiviewport.IncompleteTableLines(rows1); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}

	// Trailing table lines without blank line
	rows2 := []string{
		"Some intro text",
		"",
		"| Col 1 | Col 2 |",
		"|-------|-------|",
		"| Val 1 | Val 2 |",
	}
	if got := tuiviewport.IncompleteTableLines(rows2); got != 3 {
		t.Errorf("expected 3, got %d", got)
	}

	// Table followed by blank line (closed table) -> 0 held lines
	rows3 := []string{
		"| Col 1 | Col 2 |",
		"|-------|-------|",
		"| Val 1 | Val 2 |",
		"",
	}
	if got := tuiviewport.IncompleteTableLines(rows3); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

func TestComputeVisibleWindow_EdgeCases(t *testing.T) {
	rows := make([]string, 2021)
	for i := range rows {
		rows[i] = "row"
	}
	prefix := make([]int, len(rows)+1)
	for i := range rows {
		prefix[i+1] = prefix[i] + 1
	}

	// 1. Exact condition of the panic: availScroll = -1 (due to pinned prompt subtracting 2 from avail=1)
	vis, scroll, auto := tuiviewport.ComputeVisibleWindow(rows, prefix, 2022, -1, true)
	if vis != nil || scroll != 0 {
		t.Errorf("expected nil visible and 0 scroll for negative avail, got %v, %d", vis, scroll)
	}

	// 2. availScroll = 0
	vis, scroll, _ = tuiviewport.ComputeVisibleWindow(rows, prefix, 100, 0, false)
	if vis != nil || scroll != 0 {
		t.Errorf("expected nil visible for availScroll=0")
	}

	// 3. len(rows) < availScroll (rows=5, availScroll=20)
	shortRows := []string{"a", "b", "c"}
	shortPrefix := []int{0, 1, 2, 3}
	vis, scroll, auto = tuiviewport.ComputeVisibleWindow(shortRows, shortPrefix, 10, 5, false)
	if len(vis) != 5 {
		t.Errorf("expected 5 visible rows padded, got %d", len(vis))
	}
	if scroll != 0 {
		t.Errorf("expected scroll=0, got %d", scroll)
	}
	if !auto {
		t.Errorf("expected autoScroll=true when visible window covers all rows")
	}

	// 4. Empty rows
	vis, scroll, _ = tuiviewport.ComputeVisibleWindow(nil, nil, 0, 10, true)
	if vis != nil || scroll != 0 {
		t.Errorf("expected nil for empty rows")
	}
}

func TestWindow_InvariantsAndSafeSlice(t *testing.T) {
	// Negative offset and negative count
	w1 := tuiviewport.NewWindow(100, -10, -5)
	if w1.Start() != 0 || w1.End() != 0 || !w1.IsEmpty() || w1.Len() != 0 {
		t.Errorf("expected empty window for negative count, got start=%d end=%d", w1.Start(), w1.End())
	}

	// Count exceeding total
	w2 := tuiviewport.NewWindow(10, 5, 20)
	if w2.Start() != 5 || w2.End() != 10 || w2.Len() != 5 {
		t.Errorf("expected start=5, end=10, got start=%d end=%d", w2.Start(), w2.End())
	}

	// Offset exceeding total
	w3 := tuiviewport.NewWindow(10, 20, 5)
	if w3.Start() != 10 || w3.End() != 10 || !w3.IsEmpty() {
		t.Errorf("expected empty window clamped to total, got start=%d end=%d", w3.Start(), w3.End())
	}

	// Zero total
	w4 := tuiviewport.NewWindow(0, 5, 5)
	if w4.Start() != 0 || w4.End() != 0 {
		t.Errorf("expected 0, 0 for zero total")
	}

	// Safe generic slice extraction
	items := []int{10, 20, 30, 40, 50}
	win := tuiviewport.NewWindow(len(items), 1, 3)
	sliced := tuiviewport.Slice(items, win)
	if len(sliced) != 3 || sliced[0] != 20 || sliced[2] != 40 {
		t.Errorf("unexpected sliced result: %v", sliced)
	}

	// Window exceeding actual slice
	winExceed := tuiviewport.NewWindow(100, 10, 20)
	slicedShort := tuiviewport.Slice(items, winExceed)
	if slicedShort != nil {
		t.Errorf("expected nil when window start exceeds slice len, got %v", slicedShort)
	}
}

func TestScrollPosition_LayoutAndScrollBy(t *testing.T) {
	sp := tuiviewport.ScrollPosition{Offset: 0, AutoScroll: true}

	// Total fits in avail
	w := sp.Layout(10, 20)
	if w.Start() != 0 || w.End() != 10 || !sp.AutoScroll {
		t.Errorf("expected [0, 10] with autoscroll, got start=%d end=%d auto=%v", w.Start(), w.End(), sp.AutoScroll)
	}

	// Content overflows, autoScroll snaps to end
	sp.AutoScroll = true
	w = sp.Layout(100, 20)
	if sp.Offset != 80 || w.Start() != 80 || w.End() != 100 {
		t.Errorf("expected offset=80, got %d, window=[%d, %d]", sp.Offset, w.Start(), w.End())
	}

	// Scroll up disables autoScroll
	sp.ScrollBy(-10, 100, 20)
	if sp.Offset != 70 || sp.AutoScroll {
		t.Errorf("expected offset=70 and autoScroll=false, got offset=%d auto=%v", sp.Offset, sp.AutoScroll)
	}

	// Scroll past max re-enables autoScroll
	sp.ScrollBy(50, 100, 20)
	if sp.Offset != 80 || !sp.AutoScroll {
		t.Errorf("expected offset=80 and autoScroll=true, got offset=%d auto=%v", sp.Offset, sp.AutoScroll)
	}
}

func TestScrollWindow(t *testing.T) {
	// Normal scroll within bounds
	w1 := tuiviewport.ScrollWindow(100, 10, 20)
	if w1.Start() != 10 || w1.End() != 30 || w1.Len() != 20 {
		t.Errorf("expected [10, 30], got [%d, %d]", w1.Start(), w1.End())
	}

	// Overscroll clamped to total - visible
	w2 := tuiviewport.ScrollWindow(100, 95, 20)
	if w2.Start() != 80 || w2.End() != 100 || w2.Len() != 20 {
		t.Errorf("expected [80, 100], got [%d, %d]", w2.Start(), w2.End())
	}

	// Underscroll clamped to 0
	w3 := tuiviewport.ScrollWindow(100, -10, 20)
	if w3.Start() != 0 || w3.End() != 20 {
		t.Errorf("expected [0, 20], got [%d, %d]", w3.Start(), w3.End())
	}

	// Total smaller than visible
	w4 := tuiviewport.ScrollWindow(5, 10, 20)
	if w4.Start() != 0 || w4.End() != 5 || w4.Len() != 5 {
		t.Errorf("expected [0, 5], got [%d, %d]", w4.Start(), w4.End())
	}

	// Zero or negative visible
	w5 := tuiviewport.ScrollWindow(50, 10, 0)
	if !w5.IsEmpty() {
		t.Errorf("expected empty window for visible=0")
	}
	w6 := tuiviewport.ScrollWindow(50, 10, -5)
	if !w6.IsEmpty() {
		t.Errorf("expected empty window for visible=-5")
	}
}

func TestListNav(t *testing.T) {
	var nav tuiviewport.ListNav

	// Empty list
	w := nav.Sync(0, 10)
	if nav.Cursor != 0 || nav.Window != 0 || !w.IsEmpty() {
		t.Fatalf("empty list should yield 0, 0, got cursor=%d window=%d", nav.Cursor, nav.Window)
	}

	// Move down in 20 items, visible 5
	nav.MoveDown(1, 20, 5)
	if nav.Cursor != 1 || nav.Window != 0 {
		t.Errorf("cursor=%d window=%d, want 1, 0", nav.Cursor, nav.Window)
	}

	// Move down past window bottom (cursor reaches 5, window should become 1)
	for i := 0; i < 4; i++ {
		nav.MoveDown(1, 20, 5)
	}
	if nav.Cursor != 5 || nav.Window != 1 {
		t.Errorf("cursor=%d window=%d, want 5, 1", nav.Cursor, nav.Window)
	}

	// Page down by 5
	nav.PageDown(20, 5)
	if nav.Cursor != 10 || nav.Window != 6 {
		t.Errorf("cursor=%d window=%d, want 10, 6", nav.Cursor, nav.Window)
	}

	// End jumps to last item and aligns window to bottom
	nav.End(20, 5)
	if nav.Cursor != 19 || nav.Window != 15 {
		t.Errorf("cursor=%d window=%d, want 19, 15", nav.Cursor, nav.Window)
	}
	if !nav.IsSelected(19) {
		t.Errorf("expected 19 to be selected")
	}

	// Home jumps to top
	nav.Home(20, 5)
	if nav.Cursor != 0 || nav.Window != 0 {
		t.Errorf("cursor=%d window=%d, want 0, 0", nav.Cursor, nav.Window)
	}

	// Move up at top does not go below 0
	nav.MoveUp(1, 20, 5)
	if nav.Cursor != 0 || nav.Window != 0 {
		t.Errorf("cursor=%d window=%d, want 0, 0", nav.Cursor, nav.Window)
	}

	// List shrinks (e.g. search filter: 20 -> 3 items)
	nav.SetCursor(18, 20, 5)
	nav.Sync(3, 5)
	if nav.Cursor != 2 || nav.Window != 0 {
		t.Errorf("shrunk list: cursor=%d window=%d, want 2, 0", nav.Cursor, nav.Window)
	}

	// HandleNav dispatch
	if !nav.HandleNav(tuilib.NavDown, 10, 5) || nav.Cursor != 3 {
		t.Errorf("HandleNav NavDown failed: cursor=%d", nav.Cursor)
	}
	if !nav.HandleNav(tuilib.NavTop, 10, 5) || nav.Cursor != 0 {
		t.Errorf("HandleNav NavTop failed: cursor=%d", nav.Cursor)
	}
	if nav.HandleNav(tuilib.NavSelect, 10, 5) {
		t.Errorf("HandleNav NavSelect should not be handled by list nav")
	}
}


