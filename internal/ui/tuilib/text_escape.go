package tuilib

import "github.com/charmbracelet/x/ansi"

// SkipEscape returns the byte index just past the escape sequence starting at s[i].
//
// The scanner is charmbracelet/x/ansi's decoder — the library the
// reference's own binary embeds (its seeds carry x/ansi's
// DecodeSequence). It reads the same forms the hand-rolled scanner did:
// a CSI sequence, an OSC payload (which ends at the BEL or at the ST),
// a DCS / SOS / PM / APC payload (which ends at the ST), and a plain
// ESC.
//
// The state is the parser's ground state — the initial state of a
// sequence — and the parser itself is left nil: the caller only needs
// the length of the sequence, not its parameters.
func SkipEscape(s string, i int) int {
	if i >= len(s) || s[i] != 0x1b {
		return i
	}
	_, _, n, _ := ansi.DecodeSequence(s[i:], 0, nil)
	if n <= 0 {
		return i + 1
	}
	return i + n
}
