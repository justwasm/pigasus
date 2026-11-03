// panel_bytes_test.go — the popup's rendered bytes, pinned.
//
// A panel's rendered bytes are pinned, not eyeballed: the column
// widths, the escape order and the row sequence are the reference's,
// and a refactor that moves one of them changes what the user sees.
//
// The colours below are the palette's "solarized light" values — the
// scheme the reference's captures were taken under. The test pushes
// that scheme first, so the bytes are the same whatever scheme the
// session wears.
package slashpicker

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/palette"
)

const (
	goldenBlue   = "\x1b[38;2;38;139;210m"
	goldenSubtle = "\x1b[38;2;88;110;117m"
	goldenDim    = "\x1b[38;2;101;123;131m"
	goldenBold   = "\x1b[1m"
	goldenOff    = "\x1b[0m"
	goldenReset  = "\x1b[39m"
)

// goldenHint is the keyboard hint row the popup ends with.
//
// 【一手】 agy 1.4.6, live: the row reads "↑/↓ Navigate · enter Select
// · tab Complete", with the key names in the bold brand blue and the
// words between them in the dim gray.
var goldenHint = "  " +
	goldenBold + goldenBlue + "↑/↓" + goldenOff +
	goldenDim + " Navigate · " +
	goldenBold + goldenBlue + "enter" + goldenOff +
	goldenDim + " Select · " +
	goldenBold + goldenBlue + "tab" + goldenOff +
	goldenDim + " Complete" + goldenReset + "\n"

// withScheme makes the palette's colours deterministic for the test
// and restores them afterwards.
func withScheme(t *testing.T) {
	t.Helper()
	restore := palette.PushScheme("solarized light")
	t.Cleanup(restore)
}

// TestRows_NoMatches pins the empty branch: a query that matches
// nothing keeps the popup up with one dim row that reads "   No
// matches", padded to the popup's width, then the blank separator and
// the hint row.
//
// 【一手】 agy 1.4.6, live.
func TestRows_NoMatches(t *testing.T) {
	withScheme(t)
	p := New(Seams{})
	p.Update([]rune("/xyz"))
	if !p.Empty() {
		t.Fatal("the popup should be empty for /xyz")
	}
	want := goldenSubtle + "   No matches" + strings.Repeat(" ", 220-13) + goldenReset + "\n" +
		"\n" +
		goldenHint
	if got := p.Rows(); got != want {
		t.Errorf("the empty popup's bytes changed:\n got %q\nwant %q", got, want)
	}
}

// TestRows_CommandRow pins one command row: the highlighted row is
// "> " plus the name in the brand blue, padded to the 51-column name
// field, and the description follows in the dim gray.
func TestRows_CommandRow(t *testing.T) {
	withScheme(t)
	p := New(Seams{})
	p.Update([]rune("/clear"))
	want := goldenBlue + "> /clear (new)" + strings.Repeat(" ", 51-len("> /clear (new)")) + goldenReset +
		goldenSubtle + "Clear conversation and start a new one" + goldenReset + "\n" +
		"\n" +
		goldenHint
	if got := p.Rows(); got != want {
		t.Errorf("the command row's bytes changed:\n got %q\nwant %q", got, want)
	}
}

// TestRows_ModelRows pins the "/model <query>" popup: the display
// label in the 30-column field, the catalogue id two columns further
// right in the dim gray, and the highlighted row's colour covering
// the label field only.
//
// 【一手】 agy 1.4.6, live tmux capture:
//
//	> Claude Sonnet 4.6 (Thinking)  claude-sonnet-4-6
//	  Claude Opus 4.6 (Thinking)    claude-opus-4-6-thinking
func TestRows_ModelRows(t *testing.T) {
	withScheme(t)
	p := New(Seams{})
	p.SetModelCatalogueSource(func() []ModelRow {
		return []ModelRow{
			{Label: "Claude Sonnet 4.6 (Thinking)", ID: "claude-sonnet-4-6"},
			{Label: "Claude Opus 4.6 (Thinking)", ID: "claude-opus-4-6-thinking"},
		}
	})
	p.Update([]rune("/model claude"))
	if !p.IsModelMode() {
		t.Fatal("the popup should hold the model rows for \"/model claude\"")
	}
	want := goldenBlue + "> Claude Sonnet 4.6 (Thinking)" + goldenReset +
		"  " + goldenSubtle + "claude-sonnet-4-6" + goldenReset + "\n" +
		"  Claude Opus 4.6 (Thinking)" + strings.Repeat(" ", 30-len("  Claude Opus 4.6 (Thinking)")) +
		"  " + goldenSubtle + "claude-opus-4-6-thinking" + goldenReset + "\n" +
		"\n" +
		goldenHint
	if got := p.Rows(); got != want {
		t.Errorf("the model popup's bytes changed:\n got %q\nwant %q", got, want)
	}
}
