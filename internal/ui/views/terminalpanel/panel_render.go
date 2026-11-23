package terminalpanel

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
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

func (p *Panel) tabsLocked() []TabInfo {
	if p.seams.Tabs == nil {
		return nil
	}
	return p.seams.Tabs()
}

func (p *Panel) bodyDimensionsLocked() (height, width int) {
	width = p.cols
	if p.seams.Width != nil && p.seams.Width() > 10 {
		w := p.seams.Width() - 4
		if w > 10 {
			width = w
		}
	}
	height = p.rows
	if p.seams.Height != nil && p.seams.Height() > 6 {
		h := p.seams.Height() - 4
		if h > 4 {
			height = h
		}
	}
	if len(p.tabsLocked()) > 1 && height > 4 {
		height--
	}
	return height, width
}

// Render formats the virtual terminal panel into a styled TUI view.
func (p *Panel) Render() string {
	if p == nil {
		return ""
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	tabs := p.tabsLocked()
	height, width := p.bodyDimensionsLocked()

	title := p.title
	if title == "" && p.seams.Title != nil {
		title = p.seams.Title()
	}
	if title == "" {
		title = "Terminal"
	}

	cwd := p.cwd
	if cwd == "" && p.seams.Cwd != nil {
		cwd = p.seams.Cwd()
	}

	// Status badge
	var statusBadge string
	switch {
	case p.copyMode:
		statusBadge = ansiCyan + "◆ COPY MODE" + ansiReset
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
	hint := "Esc: Detach · Alt+C: Copy · PgUp: History"
	if p.copyMode {
		hint = "hjkl: Move · v/V: Select · y/Enter: Yank · Esc/q: Exit"
	} else if p.isScrollbackMode {
		hint = fmt.Sprintf("Scrollback -%d (v: Copy · PgDn/End: Return)", p.scrollOffset)
	}

	headerLeft := fmt.Sprintf("╭─ %s%s%s · %s ", ansiBold, title, ansiReset, statusBadge)
	headerRight := fmt.Sprintf(" %s%s%s ─╮", ansiGray, hint, ansiReset)

	leftLen := ansi.StringWidth(headerLeft)
	rightLen := ansi.StringWidth(headerRight)
	if leftLen+rightLen+1 > width+2 {
		maxRight := width + 2 - leftLen - 2
		if maxRight > 8 {
			headerRight = ansi.Truncate(headerRight, maxRight, "… ─╮")
			rightLen = ansi.StringWidth(headerRight)
		} else {
			headerRight = " ─╮"
			rightLen = ansi.StringWidth(headerRight)
		}
	}
	midFill := width + 2 - leftLen - rightLen
	if midFill < 1 {
		midFill = 1
	}

	b.WriteString(headerLeft)
	b.WriteString(ansiGray + strings.Repeat("─", midFill) + ansiReset)
	b.WriteString(headerRight)
	b.WriteString("\n")

	// Optional multi-session tab bar
	if len(tabs) > 1 {
		tabLine := renderTabBar(tabs, width)
		tabLine = ansi.Truncate(tabLine, width, "")
		pad := width - ansi.StringWidth(tabLine)
		if pad < 0 {
			pad = 0
		}
		b.WriteString(ansiGray + "│ " + ansiReset)
		b.WriteString(tabLine)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(ansiGray + " │" + ansiReset)
		b.WriteString("\n")
	}

	// Collect visible lines
	lines := p.renderVisibleLinesLocked(height, width)
	for i := 0; i < height; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		line = ansi.Truncate(line, width, "")
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
	footerLeftText := fmt.Sprintf("%dx%d", p.cols, p.rows)
	if cwd != "" {
		footerLeftText += " · " + cwd
	}
	footerLeft := fmt.Sprintf("╰─ %s%s%s ", ansiDim, footerLeftText, ansiReset)
	curX, curY := 0, 0
	if p.term != nil {
		curX, curY = p.term.Cursor.X, p.term.Cursor.Y
	}
	var footerRight string
	if p.copyMode {
		modeTag := "COPY"
		if p.copyVisual == copyVisualChar {
			modeTag = "VISUAL"
		} else if p.copyVisual == copyVisualLine {
			modeTag = "V-LINE"
		}
		footerRight = fmt.Sprintf(" %s%s %d,%d%s ─╯", ansiCyan, modeTag, p.copyCol, p.copyRow, ansiReset)
	} else {
		footerRight = fmt.Sprintf(" %scursor %d,%d%s ─╯", ansiDim, curX, curY, ansiReset)
	}
	fLeftLen := ansi.StringWidth(footerLeft)
	fRightLen := ansi.StringWidth(footerRight)
	if fLeftLen+fRightLen+1 > width+2 {
		maxLeft := width + 2 - fRightLen - 2
		if maxLeft > 10 {
			footerLeft = ansi.Truncate(footerLeft, maxLeft, "… ")
			fLeftLen = ansi.StringWidth(footerLeft)
		}
	}
	fMidFill := width + 2 - fLeftLen - fRightLen
	if fMidFill < 1 {
		fMidFill = 1
	}

	b.WriteString(footerLeft)
	b.WriteString(ansiGray + strings.Repeat("─", fMidFill) + ansiReset)
	b.WriteString(footerRight)

	return b.String()
}

func renderTabBar(tabs []TabInfo, width int) string {
	var parts []string
	for i, tab := range tabs {
		label := tab.Title
		if label == "" {
			label = tab.ID
		}
		if label == "" {
			label = fmt.Sprintf("term-%d", i+1)
		}
		if tab.Active {
			parts = append(parts, fmt.Sprintf("%s\x1b[7m %d:%s %s", ansiBold, i+1, label, ansiReset))
		} else if tab.Exited {
			parts = append(parts, fmt.Sprintf("%s[%d:%s]%s", ansiDim, i+1, label, ansiReset))
		} else {
			parts = append(parts, fmt.Sprintf("%s[%d:%s]%s", ansiGray, i+1, label, ansiReset))
		}
	}
	hint := ansiDim + "Alt+N:New Alt+←/→:Switch Alt+W:Close" + ansiReset
	joined := strings.Join(parts, " ")
	if ansi.StringWidth(joined)+2+ansi.StringWidth(hint) <= width {
		gap := width - ansi.StringWidth(joined) - ansi.StringWidth(hint)
		return joined + strings.Repeat(" ", gap) + hint
	}
	return joined
}

func (p *Panel) screenLinesWithoutCursorLocked() []string {
	if p.term == nil {
		return nil
	}
	wasVisible := p.term.CursorVisible
	p.term.CursorVisible = false
	defer func() {
		p.term.CursorVisible = wasVisible
	}()

	screenRows := p.term.Height
	screenLines := make([]string, 0, screenRows)
	for y := 0; y < screenRows; y++ {
		var buf strings.Builder
		_ = p.term.RenderLine(&buf, y)
		screenLines = append(screenLines, buf.String())
	}
	return screenLines
}

func (p *Panel) virtualBufferLocked() (lines []string, sbLen int) {
	sb := p.copyScrollback()
	screenLines := p.screenLinesWithoutCursorLocked()
	lines = make([]string, 0, len(sb)+len(screenLines))
	lines = append(lines, sb...)
	lines = append(lines, screenLines...)
	return lines, len(sb)
}

func (p *Panel) visibleWindowLocked(totalCount, height int) (start, end int) {
	if totalCount <= height {
		return 0, totalCount
	}
	if !p.isScrollbackMode || p.scrollOffset <= 0 {
		return totalCount - height, totalCount
	}
	end = totalCount - p.scrollOffset
	if end > totalCount {
		end = totalCount
	}
	if end < 0 {
		end = 0
	}
	start = end - height
	if start < 0 {
		start = 0
		end = height
		if end > totalCount {
			end = totalCount
		}
	}
	return start, end
}

// renderVisibleLinesLocked retrieves active lines from the terminal screen or scrollback buffer,
// applying either the live cursor cell or copy-mode cursor/selection highlight.
func (p *Panel) renderVisibleLinesLocked(height, width int) []string {
	if p.term == nil {
		return nil
	}

	// Copy mode: render from virtual buffer with copy cursor & selection highlight
	if p.copyMode {
		totalLines, _ := p.virtualBufferLocked()
		start, end := p.visibleWindowLocked(len(totalLines), height)
		visible := make([]string, 0, end-start)
		sr, sc, er, ec, lineMode, hasSel := p.copySelectionRangeLocked()
		for vRow := start; vRow < end; vRow++ {
			line := ansi.Truncate(totalLines[vRow], width, "")
			if hasSel && vRow >= sr && vRow <= er {
				if lineMode {
					to := ansi.StringWidth(line)
					if to < 1 {
						to = 1
					}
					line = highlightCellRange(line, 0, to)
				} else {
					from := 0
					to := ansi.StringWidth(line)
					if vRow == sr {
						from = sc
					}
					if vRow == er {
						to = ec + 1
					}
					if to <= from {
						to = from + 1
					}
					line = highlightCellRange(line, from, to)
				}
			} else if vRow == p.copyRow {
				col := p.copyCol
				if col < 0 {
					col = 0
				}
				if col >= width {
					col = width - 1
				}
				line = highlightCellRange(line, col, col+1)
			}
			visible = append(visible, line)
		}
		return visible
	}

	// Scrollback browsing (non-copy mode)
	if p.isScrollbackMode && p.scrollOffset > 0 {
		totalLines, _ := p.virtualBufferLocked()
		start, end := p.visibleWindowLocked(len(totalLines), height)
		visible := make([]string, 0, end-start)
		for vRow := start; vRow < end; vRow++ {
			visible = append(visible, ansi.Truncate(totalLines[vRow], width, ""))
		}
		return visible
	}

	// Live bottom view: render screen lines and overlay visible cursor cell
	screenLines := p.screenLinesWithoutCursorLocked()
	start := 0
	if len(screenLines) > height {
		start = len(screenLines) - height
	}
	visible := make([]string, 0, len(screenLines)-start)
	for i := start; i < len(screenLines); i++ {
		visible = append(visible, ansi.Truncate(screenLines[i], width, ""))
	}

	if p.term.CursorVisible {
		curY := p.term.Cursor.Y - start
		curX := p.term.Cursor.X
		if curX < 0 {
			curX = 0
		}
		if curX >= width {
			curX = width - 1
		}
		if curY >= 0 && curY < len(visible) && curX >= 0 {
			visible[curY] = highlightCellRange(visible[curY], curX, curX+1)
		}
	}
	return visible
}

func highlightCellRange(line string, fromCell, toCell int) string {
	if fromCell < 0 {
		fromCell = 0
	}
	if toCell <= fromCell {
		toCell = fromCell + 1
	}
	w := ansi.StringWidth(line)
	if w < toCell {
		line += strings.Repeat(" ", toCell-w)
	}
	return tuiviewport.ApplySelectionHighlightToRow(line, fromCell, toCell)
}
