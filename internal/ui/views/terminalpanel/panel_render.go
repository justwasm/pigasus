package terminalpanel

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiGreen  = "\x1b[38;2;133;153;0m"
	ansiYellow = "\x1b[38;2;181;137;0m"
	ansiRed    = "\x1b[38;2;220;50;47m"
	ansiBlue   = "\x1b[38;2;38;139;210m"
	ansiGray   = "\x1b[38;2;101;123;131m"
	ansiCyan   = "\x1b[38;2;42;161;152m"
)

// Render formats the virtual terminal panel into a styled TUI view.
func (p *Panel) Render() string {
	if p == nil {
		return ""
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	width := p.cols
	if p.seams.Width != nil && p.seams.Width() > 10 {
		w := p.seams.Width() - 4
		if w > 10 {
			width = w
		}
	}
	height := p.rows
	if p.seams.Height != nil && p.seams.Height() > 6 {
		h := p.seams.Height() - 4
		if h > 4 {
			height = h
		}
	}

	title := "Terminal"
	if p.seams.Title != nil && p.seams.Title() != "" {
		title = p.seams.Title()
	}

	// Status badge
	var statusBadge string
	switch {
	case strings.HasPrefix(p.status, "Running"):
		statusBadge = ansiGreen + "● " + p.status + ansiReset
	case strings.HasPrefix(p.status, "Exited"):
		statusBadge = ansiYellow + "○ " + p.status + ansiReset
	case strings.HasPrefix(p.status, "Error"):
		statusBadge = ansiRed + "✗ " + p.status + ansiReset
	default:
		statusBadge = ansiGray + p.status + ansiReset
	}

	var b strings.Builder

	// Top border with title and controls hint
	hint := "Esc: Detach · PgUp/PgDn: History"
	if p.isScrollbackMode {
		hint = fmt.Sprintf("Scrollback -%d (PgDn/End: Return)", p.scrollOffset)
	}

	headerLeft := fmt.Sprintf("╭─ %s%s%s · %s ", ansiBold, title, ansiReset, statusBadge)
	headerRight := fmt.Sprintf(" %s%s%s ─╮", ansiGray, hint, ansiReset)

	leftLen := ansi.StringWidth(headerLeft)
	rightLen := ansi.StringWidth(headerRight)
	midFill := width + 2 - leftLen - rightLen
	if midFill < 1 {
		midFill = 1
	}

	b.WriteString(headerLeft)
	b.WriteString(ansiGray + strings.Repeat("─", midFill) + ansiReset)
	b.WriteString(headerRight)
	b.WriteString("\n")

	// Collect visible lines
	lines := p.renderVisibleLinesLocked(height, width)
	for i := 0; i < height; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		lineWidth := ansi.StringWidth(line)
		pad := width - lineWidth
		if pad < 0 {
			pad = 0
		}

		b.WriteString(ansiGray + "│ " + ansiReset)
		b.WriteString(line)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(ansiGray + " │" + ansiReset)
		b.WriteString("\n")
	}

	// Bottom footer border
	footerLeft := fmt.Sprintf("╰─ %s%dx%d%s ", ansiDim, p.cols, p.rows, ansiReset)
	curX, curY := 0, 0
	if p.term != nil {
		curX, curY = p.term.Cursor.X, p.term.Cursor.Y
	}
	footerRight := fmt.Sprintf(" %scursor %d,%d%s ─╯", ansiDim, curX, curY, ansiReset)
	fLeftLen := ansi.StringWidth(footerLeft)
	fRightLen := ansi.StringWidth(footerRight)
	fMidFill := width + 2 - fLeftLen - fRightLen
	if fMidFill < 1 {
		fMidFill = 1
	}

	b.WriteString(footerLeft)
	b.WriteString(ansiGray + strings.Repeat("─", fMidFill) + ansiReset)
	b.WriteString(footerRight)

	return b.String()
}

// renderVisibleLinesLocked retrieves active lines from the terminal screen or scrollback buffer.
func (p *Panel) renderVisibleLinesLocked(height, width int) []string {
	if p.term == nil {
		return nil
	}

	// Extract active screen lines
	screenRows := p.term.Height
	screenLines := make([]string, 0, screenRows)
	for y := 0; y < screenRows; y++ {
		var buf strings.Builder
		_ = p.term.RenderLine(&buf, y)
		screenLines = append(screenLines, buf.String())
	}

	// Live bottom view (not scrolling back)
	if !p.isScrollbackMode || p.scrollOffset <= 0 {
		if len(screenLines) > height {
			return screenLines[len(screenLines)-height:]
		}
		return screenLines
	}

	// Browsing scrollback history:
	// Total virtual buffer is scrollback + screenLines
	totalLines := append(p.copyScrollback(), screenLines...)
	totalCount := len(totalLines)
	if totalCount <= height {
		return totalLines
	}

	// Window computation based on scrollOffset
	end := totalCount - p.scrollOffset
	if end > totalCount {
		end = totalCount
	}
	start := end - height
	if start < 0 {
		start = 0
		end = height
		if end > totalCount {
			end = totalCount
		}
	}

	return totalLines[start:end]
}
