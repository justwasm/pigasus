// unsent.go — the reference's "Unsent Comments" screen.
//
// The screen is the reference's own: it appears when the user leaves the
// viewer with comments that were not sent yet, and it takes three keys
// — Y sends the comments to the agent, N exits without sending them and
// Esc cancels (official-docs/cli/commands/codesearch.md:94-100,
// official-docs/cli/commands/diff.md:130-136).
//
// The screen is shared by construction: the reference has one such
// screen, so the header, the answers and the rows live here and every
// viewer that carries unsent comments shows these bytes. The /codesearch
// viewer (internal/ui/views/codesearchpanel) is the first consumer.

package linecomments

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// Header is the reference's screen header.
//
// The doc names it: the **Unsent Comments** screen
// (official-docs/cli/commands/codesearch.md:98,
// official-docs/cli/commands/diff.md:133).
const Header = "Unsent Comments"

// Decision is what the Unsent Comments screen answers.
type Decision int

const (
	// DecisionNone means the key does not answer the screen.
	DecisionNone Decision = iota
	// DecisionSend sends the comments to the agent and exits.
	DecisionSend
	// DecisionDiscard exits without sending the comments.
	DecisionDiscard
	// DecisionCancel returns to the viewer.
	DecisionCancel
)

// Decide reads one key on the Unsent Comments screen.
//
// The two keys are the capital letters: Y sends the comments, N exits
// without sending them and Esc cancels. The small letters do **not**
// answer the screen — the reference's rule, and the clone keeps it: a
// lower-case y is not a yes.
func Decide(key tea.Key) Decision {
	switch {
	case isCapital(key, 'Y'):
		return DecisionSend
	case isCapital(key, 'N'):
		return DecisionDiscard
	case key.Code == tea.KeyEsc:
		return DecisionCancel
	}
	return DecisionNone
}

// isCapital reports whether the key is the capital letter.
//
// Bubbletea reports a shifted letter two ways: the plain terminal path
// delivers the capital rune itself, and the Kitty path delivers the
// small rune with the shift modifier and the capital text. Both are the
// capital letter; the small one is not.
func isCapital(key tea.Key, upper rune) bool {
	if key.Text != "" {
		return key.Text == string(upper)
	}
	if key.Mod.Contains(tea.ModShift) {
		return unicode.ToLower(key.Code) == unicode.ToLower(upper)
	}
	return key.Code == upper
}

// ScreenRows renders the Unsent Comments screen.
//
// The header is the reference's own. The rows are the comments in the
// reference's wire format — the very text the agent would receive — and
// the hint names the three keys that answer the screen.
//
// 【strings】 the two option labels are the reference's own words:
// "send + close" and "discard + close" (the 1.4.11 string table, at
// 0x35594ee and 0x359ead1). The third answer — Esc — is the doc's
// "Cancel and keep editing" (official-docs/cli/commands/codesearch.md:100).
func ScreenRows(file string, cs []Comment) []string {
	rows := []string{palette.BoldOpen + Header + palette.BoldClose, ""}
	for _, c := range cs {
		// The row is the wire line without its newline: the screen
		// shows exactly what would be sent.
		rows = append(rows, "  "+strings.TrimSuffix(Format(file, c), "\n"))
	}
	rows = append(rows, "")
	rows = append(rows, tuilib.PanelDotHint("  ",
		"Y", "send + close", "N", "discard + close", "esc", "cancel"))
	return rows
}
