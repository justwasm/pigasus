package viewport_test

import (
	"testing"

	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
)

// TestIsPromptTurnSeparator pins the rule: a separator is a row whose
// visible text is made of nothing but the box-drawing rule character.
func TestIsPromptTurnSeparator(t *testing.T) {
	cases := []struct {
		name string
		row  string
		want bool
	}{
		{"empty", "", false},
		{"spaces", "   ", false},
		{"tab", "\t", false},
		{"one rule char", "─", true},
		{"rule", "────────────", true},
		{"padded rule", "  ──────  ", true},
		{"coloured rule", "\x1b[38;5;250m────────────\x1b[m", true},
		{"bold rule", "\x1b[1m────\x1b[0m", true},
		{"box border", "┌────────┐", false},
		{"box side", "│", false},
		{"ascii dashes", "---", false},
		{"rule inside text", "hello ─ world", false},
		{"rule with text", "─x─", false},
		{"plain text", "text", false},
		{"coloured text", "\x1b[31mred\x1b[m", false},
	}
	for _, tc := range cases {
		if got := tuiviewport.IsPromptTurnSeparator(tc.row); got != tc.want {
			t.Errorf("%s: IsPromptTurnSeparator(%q) = %v, want %v", tc.name, tc.row, got, tc.want)
		}
	}
}

// promptTurnRows is a transcript with two prompt turns.
var promptTurnRows = []string{
	"",               // 0
	"banner line",    // 1
	"",               // 2
	"────────────",   // 3 separator
	"> prompt one",   // 4
	"answer line",    // 5
	"",               // 6
	"────────────",   // 7 separator
	"────────────",   // 8 separator (stacked)
	"",               // 9
	"> prompt two",   // 10
	"more text",      // 11
	"still text",     // 12
	"",               // 13
	"────────────",   // 14 separator
	"tail",           // 15
	"tail two",       // 16
	"",               // 17
	"> prompt three", // 18
	"end",            // 19
}

// TestPromptTurnStart pins the turn's first row: the blank line that
// precedes the turn's separator rule.
func TestPromptTurnStart(t *testing.T) {
	sets := [][]string{
		{},
		{"> only prompt"},
		{"", "banner", "", "────────", "> p"},
		promptTurnRows,
	}
	for si, rows := range sets {
		for _, idx := range []int{-1, 0, 3, 5, 99} {
			got := tuiviewport.PromptTurnStart(rows, idx)
			want := 0
			switch {
			case idx <= 0 || idx >= len(rows):
				want = 0
			case si == 3 && idx == 3:
				// rows[2] is blank, so the turn starts one row earlier.
				want = 2
			case si == 3 && idx == 5:
				want = 5
			case si == 2 && idx == 3:
				want = 2
			}
			if got != want {
				t.Errorf("set=%d idx=%d: PromptTurnStart = %d, want %d", si, idx, got, want)
			}
		}
	}

	// The blank line before the turn's separator is part of the turn.
	if got := tuiviewport.PromptTurnStart(promptTurnRows, 14); got != 13 {
		t.Errorf("PromptTurnStart(rows, 14) = %d, want 13", got)
	}

	// A stack of separator rules travels with the turn: the walk goes
	// over every rule and stops at the blank line above the stack.
	stacked := []string{"banner", "", "────────", "────────", "> prompt"}
	if got := tuiviewport.PromptTurnStart(stacked, 4); got != 1 {
		t.Errorf("PromptTurnStart(stacked, 4) = %d, want 1", got)
	}
}

// TestPromptTurnVisible pins the visibility rule: a row at or below the
// scroll offset is visible, and a row above it is visible when the
// prompt turn it belongs to brackets the offset.
func TestPromptTurnVisible(t *testing.T) {
	// Below the offset: visible.
	if !tuiviewport.PromptTurnVisible(promptTurnRows, 5, 5, []int{4, 10, 18}) {
		t.Errorf("row 5 at scroll 5 must be visible")
	}
	// Above the offset and outside every turn: not visible.
	if tuiviewport.PromptTurnVisible(promptTurnRows, 0, 9, []int{4, 10, 18}) {
		t.Errorf("row 0 at scroll 9 must not be visible")
	}
	// Above the offset but inside the turn that brackets it: visible.
	if !tuiviewport.PromptTurnVisible(promptTurnRows, 3, 4, []int{4, 10, 18}) {
		t.Errorf("row 3 at scroll 4 must be visible: the offset is inside the turn")
	}
	// The turn's own blank line travels with it.
	if !tuiviewport.PromptTurnVisible(promptTurnRows, 7, 9, []int{4, 10, 18}) {
		t.Errorf("row 7 at scroll 9 must be visible: it is the turn's separator")
	}
	// The offset has left the first turn: its separator is gone with it.
	if tuiviewport.PromptTurnVisible(promptTurnRows, 3, 9, []int{4, 10, 18}) {
		t.Errorf("row 3 at scroll 9 must not be visible: the offset left the turn")
	}
	// Without any prompt row there is no turn to travel.
	if tuiviewport.PromptTurnVisible(promptTurnRows, 3, 9, nil) {
		t.Errorf("row 3 at scroll 9 must not be visible without prompt rows")
	}
	// A scroll below the row makes it visible whatever the prompts are.
	if !tuiviewport.PromptTurnVisible(promptTurnRows, 19, 3, nil) {
		t.Errorf("row 19 at scroll 3 must be visible")
	}
}
