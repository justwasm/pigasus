package tuilib

import (
	"strings"
	"unicode/utf8"

	"github.com/justwasm/pigasus/palette"
)

// The palette's colours follow the selected colour scheme, so these
// aliases are refreshed whenever the scheme changes.
var (
	HelpBlueOpen     = palette.BlueOpen
	HelpBlueClose    = palette.BlueClose
	HelpDimGrayOpen  = palette.DimGrayOpen
	HelpDimGrayClose = palette.DimGrayClose
	HelpDescOpen     = palette.SubtleOpen
)

const (
	HelpDescClose  = "\x1b[39m"
	HelpDescColumn = 51
)

func init() {
	palette.OnSchemeChange(refreshPaletteAliases)
}

// refreshPaletteAliases re-reads the palette's colours after the
// selected colour scheme changed, and rebuilds the footer's text with
// them.
func refreshPaletteAliases() {
	HelpBlueOpen = palette.BlueOpen
	HelpBlueClose = palette.BlueClose
	HelpDimGrayOpen = palette.DimGrayOpen
	HelpDimGrayClose = palette.DimGrayClose
	HelpDescOpen = palette.SubtleOpen
	HelpHeaderOpen = "\x1b[1m" + palette.SubtleOpen
	cBlueBoldOpen = palette.BlueBoldOpen
	cDimGrayOpen = palette.DimGrayOpen
	TabBarActiveOpen = "\x1b[1;4m" + palette.BlueOpen
	TabBarInactiveOpen = palette.SeparatorOpen
	TabBarInactiveClose = palette.SeparatorClose
	TabBarHintOpen = palette.SeparatorOpen
	TabBarHintClose = palette.SeparatorClose
	HelpKeyboardHint = buildHelpKeyboardHint()
}

// HelpListWindowFor returns the number of list rows the /help overlay
// renders at once for a terminal of the given height. Cycle 132 hard-
// coded 18 (correct only at 30 rows); cycle 182 recovered the real
// formula by scanning REF v1.4.0 at heights 13..60:
//
//	h :  13 14 15 16 17 18 20 22 24 26 28 30 34 40 50 60
//	n :   4  5  6  7  8  9 11 12 14 15 17 18 21 26 33 41
//
// Every sample satisfies n = min(3h/4 - 4, h - 9) with integer
// division (the `h-9` term dominates below h≈17). A height of 0 means
// the terminal size has not been reported yet — fall back to 18, the
// pre-182 behaviour the byte-exact snapshots assert.
func HelpListWindowFor(height int) int {
	if height <= 0 {
		return 18
	}
	n := height*3/4 - 4
	if alt := height - 9; alt < n {
		n = alt
	}
	if n < 1 {
		n = 1
	}
	return n
}

// HelpListVisible returns the number of rows actually drawn given the
// window start, catalog length and window size (the last window is
// short when the catalog doesn't fill it).
func HelpListVisible(total, window, size int) int {
	if total == 0 || window >= total {
		return 0
	}
	if size < 1 {
		size = 1
	}
	remain := total - window
	if remain > size {
		return size
	}
	return remain
}

// HelpShortcuts is the keyboard binding catalog displayed in the /help SHORTCUTS tab.
//
// v1.4.7 dropped the basic navigation keys (up, down, left, right, tab,
// pgup, pgdown) from this list so the more useful shortcuts are easier to
// find; the reference's own /help shows 27 rows and the clone matches it
// row for row.
var HelpShortcuts = []string{
	"> /                               Open slash commands",
	"  \\ + enter                       Insert newline fallback",
	"  alt+enter, ctrl+j, shift+enter  Insert newline",
	"  alt+j                           Manage subagent",
	"  alt+m                           Send with opposite Queued Messages setting",
	"  alt+v, ctrl+v                   Paste image/video",
	"  ctrl+_, ctrl+shift+-            Undo",
	"  ctrl+c, esc                     Go back / dismiss",
	"  ctrl+d                          Exit",
	"  ctrl+end                        Go to bottom",
	"  ctrl+g                          Open prompt in $EDITOR",
	"  ctrl+home                       Go to top",
	"  ctrl+k                          Approve subagent fast",
	"  ctrl+l                          Clear CLI screen",
	"  ctrl+o                          Toggle trajectory view",
	"  ctrl+r                          Review artifact",
	"  ctrl+shift+z                    Redo",
	"  ctrl+y                          Yank (paste from kill ring)",
	"  ctrl+z                          Suspend CLI",
	"  enter                           Send message or confirm",
	"  f2                              Rename",
	"  f4                              Delete",
	"  f5                              Start voice dictation",
	"  shift+down                      Half page down",
	"  shift+tab                       Cycle mode",
	"  shift+up                        Half page up",
	"  shift/alt+click                 Select and copy text",
}

// FormatShortcutDesc renders the description column with either highlighted or dim formatting.
func FormatShortcutDesc(b *strings.Builder, desc string, highlighted bool) {
	if desc == "" {
		return
	}
	if highlighted {
		b.WriteString(HelpDescOpen)
		b.WriteString(desc)
		b.WriteString(HelpDescClose)
	} else {
		b.WriteString(HelpDimGrayOpen)
		b.WriteString(desc)
		b.WriteString(HelpDimGrayClose)
	}
}

// BuildShortcutRow renders one row of the /help SHORTCUTS tab.
func BuildShortcutRow(row string, highlighted bool) string {
	if len(row) < 2 {
		return row
	}
	const keyFieldWidth = 32
	runes := []rune(row)
	keyEnd := 2 + keyFieldWidth
	if keyEnd > len(runes) {
		keyEnd = len(runes)
	}
	key := strings.TrimRight(string(runes[2:keyEnd]), " ")
	desc := ""
	if keyEnd < len(runes) {
		desc = strings.TrimLeft(string(runes[keyEnd:]), " ")
	}
	pad := keyFieldWidth - utf8.RuneCountInString(key)
	if pad < 0 {
		pad = 0
	}
	var b strings.Builder
	if highlighted {
		b.WriteString(HelpBlueOpen + "> " + key + HelpBlueClose)
	} else {
		b.WriteString("  " + HelpBlueOpen + key + HelpBlueClose)
	}
	b.WriteString(strings.Repeat(" ", pad))
	FormatShortcutDesc(&b, desc, highlighted)
	return b.String()
}

// TruncateCommandDesc clips description if it exceeds available display columns.
func TruncateCommandDesc(desc string, width int) string {
	if width <= 0 {
		return desc
	}
	cap := width - HelpDescColumn - 1
	if cap < 0 {
		cap = 0
	}
	if utf8.RuneCountInString(desc) > cap {
		if cap <= 3 {
			return ""
		}
		return string([]rune(desc)[:cap-3]) + "..."
	}
	return desc
}

// BuildCommandRow formats a single command row for the /help COMMANDS tab.
func BuildCommandRow(c SlashCommand, highlighted bool, width int) string {
	var nameCol strings.Builder
	if highlighted {
		nameCol.WriteString("> ")
	} else {
		nameCol.WriteString("  ")
	}
	nameCol.WriteString(c.Name)
	if len(c.Aliases) > 0 {
		nameCol.WriteString(" (" + strings.Join(c.Aliases, ", ") + ")")
	}
	leftRunes := utf8.RuneCountInString(nameCol.String())
	pad := ""
	if leftRunes < HelpDescColumn {
		pad = strings.Repeat(" ", HelpDescColumn-leftRunes)
	}
	desc := TruncateCommandDesc(c.Description, width)

	var b strings.Builder
	if highlighted {
		b.WriteString(HelpBlueOpen + nameCol.String() + HelpBlueClose)
	} else {
		b.WriteString(nameCol.String())
	}
	b.WriteString(pad + HelpDescOpen + desc + HelpDescClose)
	return b.String()
}
