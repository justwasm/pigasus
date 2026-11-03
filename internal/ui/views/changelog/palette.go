package changelog

import "strings"

// The /changelog panel's palette. These are the byte sequences the
// reference emits in this panel.
const (
	TextOpen   = "\x1b[38;5;235m"
	TextClose  = "\x1b[39m"
	CodeOpen   = "\x1b[38;5;69m\x1b[48;5;254m"
	CodeClose  = "\x1b[38;5;235m\x1b[49m"
	TitleRow   = "  \x1b[1m\x1b[38;5;228m\x1b[48;5;63m Pigasus CLI Changelog \x1b[0m"
	HeaderOpen = "  \x1b[1m\x1b[38;5;27m"
	HeaderEnd  = "\x1b[0m"
	Tagline    = "  \x1b[38;5;235mThe terminal-first surface to interact with Pigasus agents. Stay in your flow without context switching.\x1b[39m"
)

// PlainInline strips inline markdown backticks and bold markers.
func PlainInline(s string) string {
	s = strings.ReplaceAll(s, "`", "")
	return strings.ReplaceAll(s, "**", "")
}
