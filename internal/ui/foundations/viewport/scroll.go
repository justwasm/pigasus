package viewport

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/palette"
)

// PinnedPromptJumpHint is retained as empty string for package compatibility.
// agy 1.4.10 renders no hint suffix on the pinned prompt header.
const PinnedPromptJumpHint = ""

// IsUserPromptRow reports whether row is a row that begins a user prompt.
func IsUserPromptRow(row string) bool {
	if strings.HasPrefix(row, palette.UserFgBoldOpen) {
		rest := row[len(palette.UserFgBoldOpen):]
		for len(rest) >= 2 && rest[0] == '\x1b' {
			end := strings.IndexByte(rest, 'm')
			if end < 0 {
				return false
			}
			rest = rest[end+1:]
		}
		return strings.HasPrefix(rest, "> ")
	}
	if strings.HasPrefix(row, palette.WarningBlueOpen) {
		rest := row[len(palette.WarningBlueOpen):]
		// Strip any leading ANSI that may precede the literal ">".
		for len(rest) >= 2 && rest[0] == '\x1b' {
			end := strings.IndexByte(rest, 'm')
			if end < 0 {
				return false
			}
			rest = rest[end+1:]
		}
		return strings.HasPrefix(rest, "> ")
	}
	for _, sc := range palette.Schemes {
		open := palette.SeqWith(sc.UserFg, "1")
		if open != "" && strings.HasPrefix(row, open) {
			rest := row[len(open):]
			for len(rest) >= 2 && rest[0] == '\x1b' {
				end := strings.IndexByte(rest, 'm')
				if end < 0 {
					return false
				}
				rest = rest[end+1:]
			}
			if strings.HasPrefix(rest, "> ") {
				return true
			}
		}
	}
	return false
}

// StickyUserPromptRow returns the index (in the full rows array) of the
// user-prompt row that should anchor the sticky header, or -1 if none.
// A prompt is only sticky if it has scrolled off above the top of the viewport (promptRow < scroll).
func StickyUserPromptRow(userPromptRows []int, autoScroll bool, scroll int) int {
	if len(userPromptRows) == 0 || autoScroll || scroll <= 0 {
		return -1
	}
	i := sort.SearchInts(userPromptRows, scroll+1) - 1
	if i < 0 {
		return -1
	}
	return userPromptRows[i]
}

// RenderPinnedPrompt wraps the producing user-prompt row in the pinned-prompt header style.
// agy 1.4.10: verbatim prompt row without any hint suffix, truncated to width if too wide.
func RenderPinnedPrompt(promptRow string, width int) string {
	if width > 0 && ansi.StringWidth(promptRow) > width {
		promptRow = ansi.Truncate(promptRow, width, "…")
	}
	return promptRow
}


// IsScrollKey reports whether k is one of the recovered transcript scroll bindings.
func IsScrollKey(k tea.Key) bool {
	switch k.Code {
	case tea.KeyPgUp, tea.KeyPgDown:
		return true
	case tea.KeyUp:
		return k.Mod.Contains(tea.ModShift)
	case tea.KeyDown:
		return k.Mod.Contains(tea.ModShift)
	case tea.KeyHome:
		return k.Mod.Contains(tea.ModCtrl)
	case tea.KeyEnd:
		return k.Mod.Contains(tea.ModCtrl)
	}
	return false
}

// IsScrollWheel reports whether m is a wheel event the alt-screen viewport owns.
func IsScrollWheel(m tea.MouseMsg) bool {
	switch m.Mouse().Button {
	case tea.MouseWheelUp, tea.MouseWheelDown:
		return true
	}
	return false
}

// IsScrollMsg reports whether msg can only move the viewport and never mutates the transcript.
func IsScrollMsg(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return IsScrollKey(msg.Key())
	case tea.MouseMsg:
		return IsScrollWheel(msg)
	}
	return false
}
