package storybook

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Layout holds the computed geometry of the dual-pane Storybook TUI.
type Layout struct {
	TotalWidth   int
	TotalHeight  int
	SidebarWidth int
	CanvasWidth  int
	BodyHeight   int // Content rows between top header and bottom status bar
}

// ComputeLayout calculates dual-pane dimensions based on terminal width and height.
func ComputeLayout(width, height int) Layout {
	if width < 40 {
		width = 40
	}
	if height < 12 {
		height = 12
	}

	// Sidebar takes ~25-30% of screen, clamped between 24 and 38 columns.
	sidebarW := width * 28 / 100
	if sidebarW < 24 {
		sidebarW = 24
	}
	if sidebarW > 38 {
		sidebarW = 38
	}
	if sidebarW > width-20 {
		sidebarW = width - 20
	}

	// 1 column for the vertical divider.
	canvasW := width - sidebarW - 1
	if canvasW < 10 {
		canvasW = 10
	}

	// Top header bar (1 row) + bottom status bar (1 row) = 2 fixed rows.
	bodyH := height - 2
	if bodyH < 4 {
		bodyH = 4
	}

	return Layout{
		TotalWidth:   width,
		TotalHeight:  height,
		SidebarWidth: sidebarW,
		CanvasWidth:  canvasW,
		BodyHeight:   bodyH,
	}
}

// JoinColumns renders two multi-line column strings side-by-side with a vertical divider.
func JoinColumns(left string, leftWidth int, right string, rightWidth int, height int, dividerChar string) string {
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")

	var b strings.Builder
	for row := 0; row < height; row++ {
		var lStr, rStr string
		if row < len(leftLines) {
			lStr = leftLines[row]
		}
		if row < len(rightLines) {
			rStr = rightLines[row]
		}

		// Pad or truncate left column to exact leftWidth
		lPadded := padOrTruncate(lStr, leftWidth)
		// Truncate right column to exact rightWidth
		rPadded := padOrTruncate(rStr, rightWidth)

		b.WriteString(lPadded)
		b.WriteString(dividerChar)
		b.WriteString(rPadded)
		if row < height-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func padOrTruncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := ansi.StringWidth(s)
	if sw > w {
		return ansi.Truncate(s, w, "")
	}
	if sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}
