// render.go — bash mode ("! mode") transcript and prompt rendering.
package bashmode

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/palette"
)

// The block's body colours come from the palette, so they follow the
// selected colour scheme: the muted foreground at the body's own
// intensity for a successful command, the remove foreground for a
// failed one.

// HeadRow renders the transcript head row for a bash-mode command.
func HeadRow(cmd string, failed, running bool) string {
	var bullet string
	switch {
	case running:
		bullet = palette.ToolBulletRunningOpen + "○" + "\x1b[m"
	case failed:
		bullet = palette.ToolBulletErrOpen + "●" + "\x1b[m"
	default:
		bullet = palette.ToolBulletOpen + "●" + "\x1b[m"
	}
	return bullet + " " + palette.WarningBlueOpen + "! " + cmd + "\x1b[m"
}

// BodyRows renders the "  ⎿  " body block.
func BodyRows(lines []string, failed bool) []string {
	bodyOpen, bodyCont := palette.WarningBodyOpen, palette.SubtleOpen
	if failed {
		bodyOpen, bodyCont = palette.ToolRemoveBodyOpen, palette.ToolRemoveOpen
	}
	rows := make([]string, 0, len(lines))
	for i, line := range lines {
		if i == 0 {
			rows = append(rows, palette.WarningBlueOpen+"  ⎿  "+bodyOpen+line+"\x1b[m")
			continue
		}
		rows = append(rows, bodyCont+line+"\x1b[m")
	}
	return rows
}

// OutputRows splits and formats output text as body rows.
func OutputRows(out string, failed bool) []string {
	return BodyRows(strings.Split(out, "\n"), failed)
}

// ContRows renders continuation rows only (no "  ⎿  " prefix).
func ContRows(lines []string, failed bool) []string {
	bodyCont := palette.SubtleOpen
	if failed {
		bodyCont = palette.ToolRemoveOpen
	}
	rows := make([]string, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, bodyCont+line+"\x1b[m")
	}
	return rows
}

// BodyCap returns how many trailing body rows a collapsed bash block shows.
func BodyCap(height int) int {
	if height <= 0 {
		height = 40
	}
	if n := height / 2; n > 0 {
		return n
	}
	return 1
}

// MarkerColour returns the body text color escape sequence for success/failure.
func MarkerColour(failed bool) string {
	if failed {
		return palette.ToolRemoveBodyOpen
	}
	return palette.WarningBodyOpen
}

// BlockLines renders a finished bash block plus its trailing blank row.
func BlockLines(cmd, body string, failed, running, expanded bool, height int) []string {
	out := []string{HeadRow(cmd, failed, running)}
	if body == "" {
		return append(out, "")
	}
	lines := strings.Split(body, "\n")
	cap := BodyCap(height)
	truncated := len(lines) > cap
	hint := ""
	switch {
	case truncated && expanded:
		hint = "(ctrl+o to collapse)"
		out = append(out, BodyRows(lines, failed)...)
	case truncated:
		hint = "(ctrl+o to expand)"
		elided := len(lines) - cap
		out = append(out, palette.WarningBlueOpen+"  ⎿  "+MarkerColour(failed)+
			fmt.Sprintf("<output +%d lines>", elided)+"\x1b[m")
		out = append(out, ContRows(lines[len(lines)-cap:], failed)...)
	default:
		out = append(out, BodyRows(lines, failed)...)
	}
	if hint != "" {
		out[len(out)-1] += " " + palette.SubtleOpen + hint + "\x1b[m"
	}
	return append(out, "")
}

// PromptGlyph transforms the composer prompt while bash mode is armed.
func PromptGlyph(inputView, blueOpen string) string {
	styled := blueOpen + "! \x1b[m"
	if !strings.Contains(inputView, styled) {
		return inputView
	}
	return strings.Replace(inputView, styled, palette.WarningBlueOpen+"!\x1b[m ", 1)
}

// TrimOutput strips trailing carriage returns and newlines.
func TrimOutput(s string) string {
	return strings.TrimRight(s, "\r\n")
}
