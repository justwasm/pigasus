package viewport_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
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
