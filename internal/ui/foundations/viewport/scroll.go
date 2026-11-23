package viewport

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/palette"
)

// FormatScrollPositionIndicator formats the scroll position indicator (AGY 1.5.12 item 3).
// Example: "↓ 25 lines · 2 turns", "↓ 1 line · 1 turn", "↓ 10 lines".
func FormatScrollPositionIndicator(linesBelow, turnsBelow int) string {
	if linesBelow <= 0 {
		return ""
	}
	lineStr := fmt.Sprintf("%d lines", linesBelow)
	if linesBelow == 1 {
		lineStr = "1 line"
	}
	if turnsBelow <= 0 {
		return fmt.Sprintf("↓ %s", lineStr)
	}
	turnStr := fmt.Sprintf("%d turns", turnsBelow)
	if turnsBelow == 1 {
		turnStr = "1 turn"
	}
	return fmt.Sprintf("↓ %s · %s", lineStr, turnStr)
}

// PinnedPromptJumpHint is retained as empty string for package compatibility.
// agy 1.4.10 renders no hint suffix on the pinned prompt header.
const PinnedPromptJumpHint = ""

var schemeUserPromptOpens = func() []string {
	seen := make(map[string]struct{}, len(palette.Schemes))
	var opens []string
	for _, sc := range palette.Schemes {
		open := palette.SeqWith(sc.UserFg, "1")
		if open == "" {
			continue
		}
		if _, ok := seen[open]; ok {
			continue
		}
		seen[open] = struct{}{}
		opens = append(opens, open)
	}
	return opens
}()

func hasPromptMarkerAfterOpen(row, open string) bool {
	if open == "" || !strings.HasPrefix(row, open) {
		return false
	}
	rest := row[len(open):]
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

// IsUserPromptRow reports whether row is a row that begins a user prompt.
func IsUserPromptRow(row string) bool {
	if !strings.HasPrefix(row, "\x1b[") {
		return false
	}
	if hasPromptMarkerAfterOpen(row, palette.UserFgBoldOpen) {
		return true
	}
	if hasPromptMarkerAfterOpen(row, palette.WarningBlueOpen) {
		return true
	}
	for _, open := range schemeUserPromptOpens {
		if hasPromptMarkerAfterOpen(row, open) {
			return true
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
