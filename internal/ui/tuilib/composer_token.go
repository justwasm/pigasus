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

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
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

// StyleComposerCommandToken colours the command token of the composer.
// A value that is not a command (a plain prompt, or a prefix of one) is
// left alone.
func StyleComposerCommandToken(inputView, value string) string {
	token := ComposerCommandToken(value)
	if token == "" || !slashcommands.Known(token) {
		return inputView
	}
	i := strings.Index(inputView, token)
	if i < 0 {
		return inputView
	}
	return inputView[:i] + palette.BlueOpen + token + palette.BlueClose + inputView[i+len(token):]
}

// ComposerArgumentHint returns the argument hint the reference shows for
// empty or sub-arguments ("/model ", "/voice ", "/swarm", "/copy").
func ComposerArgumentHint(value string) (string, bool) {
	const swarmPrefix = "/swarm"
	if value == swarmPrefix {
		return " board", true
	}
	if strings.HasPrefix(value, "/swarm ") {
		rest := value[len("/swarm "):]
		if rest == "" {
			return "board", true
		}
		if strings.HasPrefix("board", rest) && rest != "board" {
			return "board"[len(rest):], true
		}
		return "", false
	}
	const copyPrefix = "/copy"
	if value == copyPrefix {
		return " btw", true
	}
	if strings.HasPrefix(value, "/copy ") {
		rest := value[len("/copy "):]
		if rest == "" {
			return "btw", true
		}
		if strings.HasPrefix("btw", rest) && rest != "btw" {
			return "btw"[len(rest):], true
		}
		return "", false
	}
	const modelPrefix = "/model "
	if strings.HasPrefix(value, modelPrefix) {
		if strings.TrimSpace(value[len(modelPrefix):]) == "" {
			return "[model]", true
		}
		return "", false
	}
	const voicePrefix = "/voice "
	if strings.HasPrefix(value, voicePrefix) {
		rest := strings.TrimSpace(value[len(voicePrefix):])
		if rest == "" {
			return "[live|dictate|stop|mute|speaker|duplex|volume|speed]", true
		}
		parts := strings.Fields(rest)
		if len(parts) == 1 && strings.HasSuffix(value, " ") {
			switch strings.ToLower(parts[0]) {
			case "speaker":
				return "[name]", true
			case "duplex":
				return "[on|off]", true
			case "volume":
				return "[0-100]", true
			case "speed":
				return "[0.5-3]", true
			}
		}
		return "", false
	}
	return "", false
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
