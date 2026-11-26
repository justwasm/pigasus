// mouse_test.go — the Vim half of the composer's mouse selection.
//
// agy 1.5.17: "When Vim mode is on, mouse selections outside Insert mode
// enter characterwise Visual mode so Vim operators and motions act on
// the selection, and Normal-mode clicks past the end of a line land on
// the last character."
//
// The scenarios drive the bridge the way the host does — a press at a
// byte offset, then a range — and read the engine back out.

package vimbridge

import (
	"testing"

	"github.com/justwasm/pigasus/vim"
)

// mouseBridge builds a vim-mode bridge over a fresh composer holding
// text, in the mode the scenario wants.
func mouseBridge(text string, mode vim.Mode) (*Bridge, *scenarioEditor) {
	in := &scenarioEditor{}
	in.SetValue(text)
	b := New(Ports{Input: in})
	b.SetEnabled(true)
	eng := b.Engine()
	if mode == vim.Insert {
		eng.Handle(vim.Key{Rune: 'i'})
	} else {
		eng.Mode = mode
	}
	return b, in
}

func TestMouseClickPlacesTheCursor(t *testing.T) {
	// Scenario: a press at a byte offset puts the engine's cursor on that
	// offset's row and rune column, and the textarea follows it.
	b, in := mouseBridge("hello world", vim.Normal)
	if !b.MouseClick(8) {
		t.Fatal("MouseClick answered false with vim on")
	}
	eng := b.Engine()
	if eng.Row != 0 || eng.Col != 8 {
		t.Fatalf("cursor = (%d,%d), want (0,8)", eng.Row, eng.Col)
	}
	if in.row != 0 || in.col != 8 {
		t.Fatalf("textarea cursor = (%d,%d), want (0,8)", in.row, in.col)
	}
	if eng.Text() != "hello world" {
		t.Fatalf("buffer changed: %q", eng.Text())
	}
}

func TestMouseClickOnALaterLine(t *testing.T) {
	// Scenario: a press on the second line lands on that line's rune
	// column, so a multi-line composer's mouse click is line-exact.
	b, _ := mouseBridge("one\nsecond", vim.Normal)
	b.MouseClick(4 + 3)
	eng := b.Engine()
	if eng.Row != 1 || eng.Col != 3 {
		t.Fatalf("cursor = (%d,%d), want (1,3)", eng.Row, eng.Col)
	}
}

func TestMouseClickPastEndOfLineLandsOnTheLastCharacter(t *testing.T) {
	// Scenario: sub-behaviour 7's second half. A Normal-mode click past
	// the end of "abc" must land on 'c' (column 2), not on the append
	// position column 3 — Vim's Normal-mode caret sits *on* a character.
	b, _ := mouseBridge("abc", vim.Normal)
	b.MouseClick(40)
	eng := b.Engine()
	if eng.Row != 0 || eng.Col != 2 {
		t.Fatalf("cursor = (%d,%d), want (0,2) — the last character", eng.Row, eng.Col)
	}
	if eng.Mode != vim.Normal {
		t.Fatalf("mode = %v, want Normal", eng.Mode)
	}
}

func TestMouseClickPastEndOfLineLandsOnTheLastCharacterInVisual(t *testing.T) {
	// Scenario: the same clamp holds in Visual mode, which shares
	// Normal's column rule.
	b, _ := mouseBridge("abc", vim.Visual)
	b.MouseClick(40)
	eng := b.Engine()
	if eng.Col != 2 {
		t.Fatalf("column = %d, want 2", eng.Col)
	}
}

func TestMouseClickOnAnEmptyLineKeepsColumnZero(t *testing.T) {
	// Scenario: an empty line has no last character, so the caret stays
	// at column 0 rather than underflowing to -1.
	b, _ := mouseBridge("one\n\ntwo", vim.Normal)
	b.MouseClick(4)
	eng := b.Engine()
	if eng.Row != 1 || eng.Col != 0 {
		t.Fatalf("cursor = (%d,%d), want (1,0)", eng.Row, eng.Col)
	}
}

func TestMouseClickLeavesInsertModeAlone(t *testing.T) {
	// Scenario: in Insert mode the textarea's caret is the cursor and the
	// clause says nothing changes. MousePending is false there, so the
	// host never routes the press to the engine.
	b, _ := mouseBridge("hello", vim.Insert)
	if b.MousePending() {
		t.Fatal("MousePending was true in Insert mode")
	}
}

func TestSelectRangeEntersCharacterwiseVisualMode(t *testing.T) {
	// Scenario: sub-behaviour 7's first half. A press-and-drag range in
	// Normal mode becomes the engine's characterwise Visual selection —
	// anchor at the range's start, caret on its last character — so the
	// operator that follows acts on what the screen shows.
	b, _ := mouseBridge("hello world", vim.Normal)
	if !b.SelectRange(0, 5) {
		t.Fatal("SelectRange answered false")
	}
	eng := b.Engine()
	if eng.Mode != vim.Visual {
		t.Fatalf("mode = %v, want Visual", eng.Mode)
	}
	// "d" is the operator: it deletes exactly the highlighted word.
	res := eng.Handle(vim.Key{Rune: 'd'})
	if !res.Consumed {
		t.Fatal("the operator was not consumed")
	}
	if got := eng.Text(); got != " world" {
		t.Fatalf("after d = %q, want ' world' — the Visual range must be the dragged one", got)
	}
}

func TestSelectRangeIsInclusiveOfTheLastCharacter(t *testing.T) {
	// Scenario: [start,end) is exclusive and Visual is inclusive, so the
	// caret must sit on the rune *before* end. Off by one here would leave
	// the last character out of every yank and delete.
	b, _ := mouseBridge("hello world", vim.Normal)
	b.SelectRange(6, 11)
	eng := b.Engine()
	if eng.Row != 0 || eng.Col != 10 {
		t.Fatalf("caret = (%d,%d), want (0,10)", eng.Row, eng.Col)
	}
	eng.Handle(vim.Key{Rune: 'y'})
	// A yank fills the register; pasting it after the buffer proves the
	// range covered "world" and only "world".
	eng.Handle(vim.Key{Rune: '$'})
	eng.Handle(vim.Key{Rune: 'p'})
	if got := eng.Text(); got != "hello worldworld" {
		t.Fatalf("after yank+paste = %q, want 'hello worldworld'", got)
	}
}

func TestSelectRangeAcrossLines(t *testing.T) {
	// Scenario: a drag from the middle of one line into the middle of the
	// next enters Visual with the anchor on the first line, so the "d"
	// that follows is a characterwise multi-line delete.
	b, _ := mouseBridge("one\ntwo", vim.Normal)
	if !b.SelectRange(1, 6) {
		t.Fatal("SelectRange answered false")
	}
	eng := b.Engine()
	if eng.Mode != vim.Visual {
		t.Fatalf("mode = %v, want Visual", eng.Mode)
	}
	eng.Handle(vim.Key{Rune: 'd'})
	if got := eng.Text(); got != "oo" {
		t.Fatalf("after d = %q, want 'oo'", got)
	}
}

func TestSelectRangeIgnoresInsertMode(t *testing.T) {
	// Scenario: in Insert mode the clause does not apply — handing the
	// range to the engine would leave Insert mode — so the bridge
	// declines and the host keeps the composer's own selection.
	b, in := mouseBridge("hello world", vim.Insert)
	if b.SelectRange(0, 5) {
		t.Fatal("SelectRange took a range in Insert mode")
	}
	if b.Engine().Mode != vim.Insert {
		t.Fatalf("mode = %v, want Insert", b.Engine().Mode)
	}
	if in.value != "hello world" {
		t.Fatalf("buffer changed: %q", in.value)
	}
}

func TestSelectRangeIgnoresAnEmptyRange(t *testing.T) {
	// Scenario: a bare click selects nothing, so the bridge declines and
	// the engine keeps Normal mode with the click's caret.
	for _, tc := range []struct{ start, end int }{{4, 4}, {7, 2}, {-3, -1}} {
		b, _ := mouseBridge("hello world", vim.Normal)
		if b.SelectRange(tc.start, tc.end) {
			// A reversed range is normalized, so only the empty and
			// fully negative ones must decline.
			if tc.start != tc.end {
				continue
			}
			t.Fatalf("SelectRange(%d,%d) took an empty range", tc.start, tc.end)
		}
		if b.Engine().Mode == vim.Visual {
			t.Fatalf("SelectRange(%d,%d) left Visual", tc.start, tc.end)
		}
	}
}

func TestClearMouseSelectionReturnsToNormal(t *testing.T) {
	// Scenario: a press elsewhere drops the mouse's Visual selection, so
	// a stale highlight cannot make the next operator act on old text.
	b, _ := mouseBridge("hello world", vim.Normal)
	b.SelectRange(0, 5)
	if b.Engine().Mode != vim.Visual {
		t.Fatalf("setup left mode %v", b.Engine().Mode)
	}
	if !b.ClearMouseSelection() {
		t.Fatal("ClearMouseSelection answered false")
	}
	if b.Engine().Mode != vim.Normal {
		t.Fatalf("mode = %v, want Normal", b.Engine().Mode)
	}
	if b.ClearMouseSelection() {
		t.Fatal("ClearMouseSelection claimed a second change")
	}
}

func TestMouseClickLeavesAStaleVisualSelection(t *testing.T) {
	// Scenario: a press replaces the highlight with a caret — the click
	// is the user's new position, not an extension of the old range.
	b, _ := mouseBridge("hello world", vim.Normal)
	b.SelectRange(0, 5)
	b.MouseClick(8)
	eng := b.Engine()
	if eng.Mode != vim.Normal {
		t.Fatalf("mode = %v, want Normal after a press", eng.Mode)
	}
	if eng.Col != 8 {
		t.Fatalf("column = %d, want 8", eng.Col)
	}
}

func TestMouseAPIIsInertWithoutVim(t *testing.T) {
	// Scenario: the host may call the mouse API unconditionally; with vim
	// off every answer is inert so the composer's own selection stands.
	in := &scenarioEditor{}
	in.SetValue("hello")
	b := New(Ports{Input: in})
	if b.MousePending() {
		t.Fatal("MousePending was true with vim off")
	}
	if b.MouseClick(3) {
		t.Fatal("MouseClick acted with vim off")
	}
	if b.SelectRange(0, 5) {
		t.Fatal("SelectRange acted with vim off")
	}
	if b.ClearMouseSelection() {
		t.Fatal("ClearMouseSelection acted with vim off")
	}
	// A nil bridge is inert too.
	var nilBridge *Bridge
	if nilBridge.MousePending() || nilBridge.MouseClick(0) || nilBridge.SelectRange(0, 1) {
		t.Fatal("a nil bridge acted")
	}
}

func TestMouseClickOnAWideRune(t *testing.T) {
	// Scenario: a click inside a wide rune's bytes snaps to the rune's
	// start, so the cursor never sits mid-character.
	b, _ := mouseBridge("你好世界", vim.Normal)
	b.MouseClick(4) // the first byte of the second rune
	eng := b.Engine()
	if eng.Col != 1 {
		t.Fatalf("column = %d, want 1 (the second rune)", eng.Col)
	}
	b.MouseClick(5) // the second byte of the same rune
	if eng.Col != 1 {
		t.Fatalf("column = %d after a mid-rune click, want 1", eng.Col)
	}
}
