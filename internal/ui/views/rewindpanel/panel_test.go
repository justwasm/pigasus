// panel_test.go — the /rewind picker's rows, its cursor and its keys.
//
// The frames are the reference's own: the empty state's literal
// `(current)` row (cycle 189) and the one-row-per-turn form (cycle 222).

package rewindpanel

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/session/rewind"
	"github.com/justwasm/pigasus/palette"
)

// sgrRe matches the SGR spans the panel's rows carry.
var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// stripSGR removes the SGR spans from a row.
func stripSGR(s string) string {
	return sgrRe.ReplaceAllString(s, "")
}

// plainLines strips the SGR spans from the panel's rows.
func plainLines(p *Panel) []string {
	lines := p.Lines()
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = stripSGR(l)
	}
	return out
}

// stepsFor builds a step list of n turns, newest first.
//
// The step is the session layer's (internal/app/session/rewind): the
// panel names a step, it does not decide what a step owns.
func stepsFor(n int) []rewind.Step {
	steps := make([]rewind.Step, n)
	for i := range steps {
		steps[i] = rewind.Step{
			UserIdx: i * 2,
			EndIdx:  i*2 + 2,
			Content: "turn-step",
		}
	}
	return steps
}

// newTestPanel builds a panel over a fixed step list.
func newTestPanel(steps []rewind.Step, height int) *Panel {
	return New(Seams{
		Steps:  func() []rewind.Step { return steps },
		Height: func() int { return height },
	})
}

// The empty state is the cycle-189 form: the literal `(current)` row.
func TestLinesEmptyState(t *testing.T) {
	p := newTestPanel(nil, 50)
	want := []string{
		"Rewind Conversation",
		"",
		"> (current)",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select  esc Cancel",
	}
	got := plainLines(p)
	if len(got) != len(want) {
		t.Fatalf("line count: got %d, want %d\n got: %q\nwant: %q", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got: %q\nwant: %q", i, got[i], want[i])
		}
	}
	if !strings.Contains(p.Lines()[2], palette.BlueOpen) {
		t.Errorf("selected row should be blue: %q", p.Lines()[2])
	}
	if p.RowCount() != 1 {
		t.Errorf("RowCount = %d, want 1 (the (current) row)", p.RowCount())
	}
}

// A three-turn session opens the picker with three rows; the cursor
// lands on the most-recent (index 0) by default; each row starts with
// `  ` except the cursor which starts with `> ` and carries the
// brand-blue span. Only the most-recent row carries the `(current)`
// annotation (dim gray 238); other rows are plain (no color).
func TestLinesNonEmpty(t *testing.T) {
	steps := []rewind.Step{{Content: "newest"}, {Content: "middle"}, {Content: "oldest"}}
	p := newTestPanel(steps, 50)
	plain := plainLines(p)
	// 7-row skeleton: title, blank, three turn rows, blank, keyboard.
	// No counter at height=50 / 3 turns (the counter only appears when
	// the turns overflow the visible window).
	if len(plain) != 7 {
		t.Fatalf("line count = %d, want 7\nplain:\n%s", len(plain),
			strings.Join(plain, "\n"))
	}
	want := []string{
		"Rewind Conversation",
		"",
		"> Turn 1  newest  (current)",
		"  Turn 2  middle",
		"  Turn 3  oldest",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select  esc Cancel",
	}
	for i, w := range want {
		if plain[i] != w {
			t.Errorf("row %d:\n got: %q\nwant: %q", i, plain[i], w)
		}
	}
	// The cursor row (row 2) must carry the brand-blue open code; rows
	// 3-4 must NOT.
	styled := p.Lines()
	if !strings.Contains(styled[2], palette.BlueOpen) {
		t.Errorf("cursor row missing blue: %q", stripSGR(styled[2]))
	}
	for _, idx := range []int{3, 4} {
		if strings.Contains(styled[idx], palette.BlueOpen) {
			t.Errorf("non-cursor row %d leaked blue: %q", idx, stripSGR(styled[idx]))
		}
	}
	if p.RowCount() != 3 {
		t.Errorf("RowCount = %d, want 3", p.RowCount())
	}
}

// The windowed forms: a 50-turn session at h=10 shows four rows and the
// dim-gray "(1-4 of 50 turns)" counter. The visible budget is
// h - 5 chrome - 1 status = 4.
func TestLinesWindowed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		height  int
		rows    int
		last    string
		counter string
	}{
		{"h=10", 10, 4, "  Turn 4", "(1–4 of 50 turns)"},
		{"h=20", 20, 14, "  Turn 14", "(1–14 of 50 turns)"},
		{"h=40", 40, 34, "  Turn 34", "(1–34 of 50 turns)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPanel(stepsFor(50), tc.height)
			plain := plainLines(p)
			// title + blank + rows + blank + counter + keyboard
			want := 2 + tc.rows + 3
			if len(plain) != want {
				t.Fatalf("line count = %d, want %d\nplain:\n%s", len(plain), want,
					strings.Join(plain, "\n"))
			}
			if !strings.HasPrefix(plain[2], "> Turn 1") {
				t.Errorf("first visible row should be the cursor's Turn 1, got %q", plain[2])
			}
			if !strings.HasPrefix(plain[1+tc.rows], tc.last) {
				t.Errorf("last visible row = %q, want the prefix %q", plain[1+tc.rows], tc.last)
			}
			if !strings.Contains(plain[3+tc.rows], tc.counter) {
				t.Errorf("counter row missing/wrong: %q", plain[3+tc.rows])
			}
			if !strings.Contains(plain[len(plain)-1], "Keyboard:") {
				t.Errorf("keyboard hint missing: %q", plain[len(plain)-1])
			}
		})
	}
}

// The cursor moves over the turns and clamps at both ends.
func TestHandleKeyMovesCursor(t *testing.T) {
	p := newTestPanel(stepsFor(3), 50)
	for i := 0; i < 2; i++ {
		if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyDown}); !handled {
			t.Fatalf("Down %d not handled", i)
		}
	}
	if p.Cursor() != 2 {
		t.Errorf("after Down x2: cursor = %d, want 2", p.Cursor())
	}
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("Down past the end not handled")
	}
	if p.Cursor() != 2 {
		t.Errorf("Down past end: cursor = %d, want 2 (clamp)", p.Cursor())
	}
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyUp}); !handled {
		t.Fatal("Up not handled")
	}
	if p.Cursor() != 1 {
		t.Errorf("after Up: cursor = %d, want 1", p.Cursor())
	}
	styled := p.Lines()
	if !strings.Contains(styled[3], palette.BlueOpen) {
		t.Errorf("new cursor row 3 missing blue: %q", stripSGR(styled[3]))
	}
}

// The window follows the cursor: a cursor that moves past the visible
// slice scrolls the slice, so the row the cursor is on is the row the
// user sees.
func TestWindowFollowsCursor(t *testing.T) {
	p := newTestPanel(stepsFor(50), 10)
	// Four rows are visible at h=10.
	for i := 0; i < 6; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyDown})
	}
	if p.Cursor() != 6 {
		t.Fatalf("cursor = %d, want 6", p.Cursor())
	}
	plain := plainLines(p)
	// The window scrolls so the cursor's row is the last visible one.
	if !strings.HasPrefix(plain[2+3], "> Turn 7") {
		t.Errorf("the cursor's row should be visible, got %q", plain[2+3])
	}
	if !strings.Contains(plain[3+4], "(4–7 of 50 turns)") {
		t.Errorf("counter should follow the window, got %q", plain[3+4])
	}
	// Back up past the top: the window follows the cursor again.
	for i := 0; i < 6; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyUp})
	}
	plain = plainLines(p)
	if !strings.HasPrefix(plain[2], "> Turn 1") {
		t.Errorf("the first turn should be visible again, got %q", plain[2])
	}
}

// Enter picks the step the cursor is on; Esc leaves.
func TestHandleKeyPicksAndCloses(t *testing.T) {
	var picked int = -1
	closed := 0
	p := New(Seams{
		Steps:  func() []rewind.Step { return stepsFor(3) },
		Height: func() int { return 50 },
		Pick: func(index int) int {
			picked = index
			return 2
		},
		Close: func() tea.Cmd {
			closed++
			return nil
		},
	})
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if picked != 1 {
		t.Errorf("picked = %d, want 1", picked)
	}
	if closed != 1 {
		t.Errorf("closed = %d, want 1", closed)
	}

	p2 := New(Seams{
		Steps:  func() []rewind.Step { return stepsFor(3) },
		Height: func() int { return 50 },
		Close: func() tea.Cmd {
			closed++
			return nil
		},
	})
	if handled, _ := p2.HandleKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("Esc not handled")
	}
	if closed != 2 {
		t.Errorf("closed = %d, want 2", closed)
	}
}

// A key the panel does not own is reported as unhandled, so the host's
// global handlers (Ctrl+C) still see it.
func TestHandleKeyFallsThrough(t *testing.T) {
	p := newTestPanel(stepsFor(3), 50)
	if handled, _ := p.HandleKey(tea.Key{Code: 'c', Mod: tea.ModCtrl}); handled {
		t.Error("Ctrl+C must not be handled by the panel")
	}
}
