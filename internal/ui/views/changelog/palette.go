package changelog

import (
	"strings"

	"github.com/justwasm/pigasus/internal/core/branding"
)

// The /changelog panel's palette. These are the byte sequences the
// reference emits in this panel.
const (
	TextOpen   = "\x1b[38;2;232;234;237m"
	TextClose  = "\x1b[39m"
	CodeOpen   = "\x1b[38;2;138;180;248m\x1b[48;2;48;48;48m"
	CodeClose  = "\x1b[38;2;232;234;237m\x1b[49m"
	HeaderOpen = "  \x1b[1m\x1b[38;2;0;153;255m"
	HeaderEnd  = "\x1b[0m"
)

var (
	TitleRow = "  \x1b[1m\x1b[38;5;228m\x1b[48;5;63m " + branding.Current().CLIName + " Changelog \x1b[0m"
	Tagline  = "  \x1b[38;2;232;234;237mThe terminal-first surface to interact with " + branding.Current().Name + " agents. Stay in your flow without context switching.\x1b[39m"
)

// PlainInline strips inline markdown backticks and bold markers.
func PlainInline(s string) string {
	s = strings.ReplaceAll(s, "`", "")
	return strings.ReplaceAll(s, "**", "")
}
