package tuiviewport_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/palette"
	"github.com/justwasm/pigasus/internal/tuiviewport"
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
	if !strings.Contains(pinned, tuiviewport.PinnedPromptJumpHint) {
		t.Errorf("expected pinned prompt jump hint in %q", pinned)
	}

	if !tuiviewport.IsScrollKey(tea.Key{Code: tea.KeyPgUp}) {
		t.Errorf("expected KeyPgUp to be scroll key")
	}
}
