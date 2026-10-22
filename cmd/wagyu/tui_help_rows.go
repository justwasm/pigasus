package main

import (
	"strings"
	"unicode/utf8"

	"github.com/justwasm/pigasus/internal/tuilib"
)

const (
	helpBlueOpen     = cBlueOpen
	helpBlueClose    = cBlueClose
	helpDimGrayOpen  = cDimGrayOpen
	helpDimGrayClose = cDimGrayClose
	helpDescColumn   = 51
)

func formatShortcutDesc(b *strings.Builder, desc string, highlighted bool) {
	if desc == "" {
		return
	}
	if highlighted {
		b.WriteString(cHelpDescOpen)
		b.WriteString(desc)
		b.WriteString(cHelpDescClose)
	} else {
		b.WriteString(helpDimGrayOpen)
		b.WriteString(desc)
		b.WriteString(helpDimGrayClose)
	}
}

func buildShortcutRow(row string, highlighted bool) string {
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
		b.WriteString(helpBlueOpen + "> " + key + helpBlueClose)
	} else {
		b.WriteString("  " + helpBlueOpen + key + helpBlueClose)
	}
	b.WriteString(strings.Repeat(" ", pad))
	formatShortcutDesc(&b, desc, highlighted)
	return b.String()
}

// helpShortcuts is the keyboard binding catalog displayed in the /help SHORTCUTS tab.
var helpShortcuts = []string{
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
	"  down                            Move down",
	"  enter                           Send message or confirm",
	"  f2                              Rename",
	"  f4                              Delete",
	"  f5                              Start voice dictation",
	"  left                            Move left / Prev tab",
	"  pgdown                          Page down",
	"  pgup                            Page up",
	"  right                           Move right / Next tab",
	"  shift+down                      Half page down",
	"  shift+tab                       Cycle mode",
	"  shift+up                        Half page up",
	"  shift/alt+click                 Select and copy text",
	"  tab                             Tab",
	"  up                              Move up",
}

func truncateCommandDesc(desc string, width int) string {
	if width <= 0 {
		return desc
	}
	cap := width - helpDescColumn - 1
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

func buildCommandRow(c tuilib.SlashCommand, highlighted bool, width int) string {
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
	if leftRunes < helpDescColumn {
		pad = strings.Repeat(" ", helpDescColumn-leftRunes)
	}
	desc := truncateCommandDesc(c.Description, width)

	var b strings.Builder
	if highlighted {
		b.WriteString(helpBlueOpen + nameCol.String() + helpBlueClose)
	} else {
		b.WriteString(nameCol.String())
	}
	b.WriteString(pad + cHelpDescOpen + desc + cHelpDescClose)
	return b.String()
}
