// composer_token.go — the composer's command token and argument hint.
//
// The reference colours the command token in the composer brand blue when
// the token names a command exactly (or is one of that command's aliases)
// and leaves it plain when it is only a prefix. 【一手】 agy 1.4.6, live
// tmux captures:
//
//	> /model claude   →  "> " blue, "/model" blue, " claude" plain
//	> /codesear       →  "> " blue, "/codesear" plain
//	> /quit           →  "> " blue, "/quit" blue
//
// It also shows an argument hint for an empty "/model " argument:
// "> /model [model]". The hint belongs to the command, not to the popup:
// it stays after Esc closes the popup.
package tuilib

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/palette"
)

// ComposerPrompt is the prompt glyph the composer draws before the typed
// text. The hint's column is measured from it.
const ComposerPrompt = "> "

// ComposerCommandToken returns the command token of a composer value —
// its first word, when that word starts with "/".
func ComposerCommandToken(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "/") {
		return ""
	}
	if i := strings.IndexAny(value, " \t"); i >= 0 {
		return value[:i]
	}
	return value
}

// KnownSlashCommand reports whether token names a command the picker
// offers — exactly, or as one of that command's aliases ("/quit" is an
// alias of "/exit").
func KnownSlashCommand(token string) bool {
	for _, c := range DefaultSlashCommands {
		if c.Name == token {
			return true
		}
		for _, alias := range c.Aliases {
			if alias == token || "/"+alias == token {
				return true
			}
		}
	}
	return false
}

// StyleComposerCommandToken colours the command token of the composer.
// A value that is not a command (a plain prompt, or a prefix of one) is
// left alone.
func StyleComposerCommandToken(inputView, value string) string {
	token := ComposerCommandToken(value)
	if token == "" || !KnownSlashCommand(token) {
		return inputView
	}
	i := strings.Index(inputView, token)
	if i < 0 {
		return inputView
	}
	return inputView[:i] + palette.BlueOpen + token + palette.BlueClose + inputView[i+len(token):]
}

// ComposerArgumentHint returns the argument hint the reference shows for
// an empty argument. Only "/model" carries one: "/agent " shows nothing
// (【一手】 agy 1.4.6, live).
func ComposerArgumentHint(value string) (string, bool) {
	const prefix = "/model "
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	if strings.TrimSpace(value[len(prefix):]) != "" {
		return "", false
	}
	return "[model]", true
}

// AppendComposerHint inserts the hint into the composer view right after
// the typed text — the reference draws it there, not at the end of the
// padded line.
//
// The column is measured in visible cells: the prompt plus the text.
// 【一手】 agy 1.4.6, live: the reference renders "> /model [model]" — the
// hint follows the text it belongs to.
func AppendComposerHint(inputView, hint, value string) string {
	col := ansi.StringWidth(ComposerPrompt) + ansi.StringWidth(FirstLine(value))
	i := VisibleColumnOffset(inputView, col)
	return inputView[:i] + hint + inputView[i:]
}

// VisibleColumnOffset returns the byte offset in s where the given
// visible column starts. ANSI escape sequences are skipped and do not
// count towards the column; a column past the end yields len(s).
func VisibleColumnOffset(s string, col int) int {
	if col <= 0 {
		return 0
	}
	cur := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i = ansiSequenceEnd(s, i)
			continue
		}
		if cur == col {
			return i
		}
		cur++
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return len(s)
}

// ansiSequenceEnd returns the byte index just past the ANSI escape
// sequence that starts at i (s[i] == 0x1b).
func ansiSequenceEnd(s string, i int) int {
	j := i + 1
	if j < len(s) && s[j] == '[' {
		j++
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		if j < len(s) {
			j++
		}
		return j
	}
	if j < len(s) {
		return j + 1
	}
	return j
}

// FirstLine returns the first line of s — the text up to the first newline.
func FirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
