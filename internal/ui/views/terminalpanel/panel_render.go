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
	if p.seams.OmniBar {
		width = p.cols
		if p.seams.Width != nil && p.seams.Width() >= 10 {
			width = p.seams.Width()
		}
		height = p.rows
		if p.seams.Height != nil && p.seams.Height() >= 5 {
			height = p.seams.Height() - 1
		}
		if height < 4 {
			height = 4
		}
		return height, width
	}
	width = p.cols
	if p.seams.Width != nil && p.seams.Width() > 10 {
		w := p.seams.Width() - 4
		if w > 10 {
			width = w
		}
	}
	height = p.rows
	if p.seams.Height != nil {
		if p.seams.Standalone {
			if p.seams.Height() > 4 {
				h := p.seams.Height() - 2
				if h >= 4 {
					height = h
				}
			}
		} else if p.seams.Height() > 6 {
			h := p.seams.Height() - 4
			if h > 4 {
				height = h
			}
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

	p.hits = p.hits[:0]
	p.menuBox = menuBoxRect{}

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
	case p.isScrollbackMode:
		statusBadge = ansiCyan + fmt.Sprintf("◆ Scrollback -%d", p.scrollOffset) + ansiReset
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

	if p.seams.OmniBar {
		b.WriteString(p.renderOmniBarLocked(tabs, width, title, cwd, statusBadge))
		b.WriteString("\n")

		lines := p.renderVisibleLinesLocked(height, width)
		for len(lines) < height {
			lines = append(lines, "")
		}
		if p.menuOpen {
			lines = p.overlayMenuLocked(lines, 1, width)
		}
		for i := 0; i < height; i++ {
			line := ""
			if i < len(lines) {
				line = lines[i]
			}
			line = ansi.Truncate(line, width, "")
			pad := width - ansi.StringWidth(line)
			if pad < 0 {
				pad = 0
			}
			b.WriteString(line)
			if pad > 0 {
				b.WriteString(strings.Repeat(" ", pad))
			}
			if i < height-1 {
				b.WriteString("\n")
			}
		}
		return b.String()
	}

	// Top border with title, clickable buttons, and controls hint
	hint := "Esc: Detach · Alt+C: Copy · Alt+M: Menu"
	if p.seams.PassThroughEsc {
		if p.seams.Quit != nil {
			hint = "Alt+C: Copy · Alt+M: Menu · Alt+Q: Quit"
		} else {
			hint = "Alt+C: Copy · Alt+M: Menu"
		}
	}
	if p.copyMode {
		hint = "hjkl: Move · v/V: Select · y/Enter: Yank · Esc/q: Exit"
	} else if p.isScrollbackMode {
		hint = fmt.Sprintf("Scrollback -%d (v: Copy · PgDn/End: Return)", p.scrollOffset)
	}

	newBtnText := "[+ New]"
	menuBtnText := "[≡ Menu]"
	newBtn := ansiCyan + newBtnText + ansiReset
	menuBtn := ansiCyan + menuBtnText + ansiReset
	if p.menuOpen {
		menuBtn = ansiBold + "\x1b[7m" + menuBtnText + ansiReset
	}

	headerLeft := fmt.Sprintf("╭─ %s%s%s · %s ", ansiBold, title, ansiReset, statusBadge)
	totalBoxWidth := width + 4
	leftLen := ansi.StringWidth(headerLeft)

	hasNewBtn := true
	hasMenuBtn := true
	headerRight := fmt.Sprintf(" %s %s · %s%s%s ─╮", newBtn, menuBtn, ansiGray, hint, ansiReset)
	rightLen := ansi.StringWidth(headerRight)

	if leftLen+rightLen+1 > totalBoxWidth {
		// Drop hint first so clickable buttons stay intact
		headerRight = fmt.Sprintf(" %s %s ─╮", newBtn, menuBtn)
		rightLen = ansi.StringWidth(headerRight)
	}
	if leftLen+rightLen+1 > totalBoxWidth {
		hasNewBtn = false
		headerRight = fmt.Sprintf(" %s ─╮", menuBtn)
		rightLen = ansi.StringWidth(headerRight)
	}
	if leftLen+rightLen+1 > totalBoxWidth {
		maxLeft := totalBoxWidth - rightLen - 1
		if maxLeft > 6 {
			headerLeft = ansi.Truncate(headerLeft, maxLeft, "… ")
			leftLen = ansi.StringWidth(headerLeft)
		} else {
			hasMenuBtn = false
			headerRight = " ─╮"
			rightLen = ansi.StringWidth(headerRight)
		}
	}
	midFill := totalBoxWidth - leftLen - rightLen
	if midFill < 1 {
		midFill = 1
	}

	if hasNewBtn {
		newStart := leftLen + midFill + 1
		newEnd := newStart + ansi.StringWidth(newBtnText)
		p.hits = append(p.hits, hitRect{row: 0, startX: newStart, endX: newEnd, kind: hitActionNewTab})
		if hasMenuBtn {
			menuStart := newEnd + 1
			menuEnd := menuStart + ansi.StringWidth(menuBtnText)
			p.hits = append(p.hits, hitRect{row: 0, startX: menuStart, endX: menuEnd, kind: hitActionOpenMenu})
		}
	} else if hasMenuBtn {
		menuStart := leftLen + midFill + 1
		menuEnd := menuStart + ansi.StringWidth(menuBtnText)
		p.hits = append(p.hits, hitRect{row: 0, startX: menuStart, endX: menuEnd, kind: hitActionOpenMenu})
	}

	b.WriteString(headerLeft)
	b.WriteString(ansiGray + strings.Repeat("─", midFill) + ansiReset)
	b.WriteString(headerRight)
	b.WriteString("\n")

	// Optional multi-session tab bar
	bodyStartRow := 1
	if len(tabs) > 1 {
		bodyStartRow = 2
		tabLine := p.renderTabBarLocked(tabs, width)
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

	// Collect visible lines and overlay interactive menu if open
	lines := p.renderVisibleLinesLocked(height, width)
	for len(lines) < height {
		lines = append(lines, "")
	}
	if p.menuOpen {
		lines = p.overlayMenuLocked(lines, bodyStartRow, width)
	}
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
	if fLeftLen+fRightLen+1 > totalBoxWidth {
		maxLeft := totalBoxWidth - fRightLen - 2
		if maxLeft > 10 {
			footerLeft = ansi.Truncate(footerLeft, maxLeft, "… ")
			fLeftLen = ansi.StringWidth(footerLeft)
		}
	}
	fMidFill := totalBoxWidth - fLeftLen - fRightLen
	if fMidFill < 1 {
		fMidFill = 1
	}

	b.WriteString(footerLeft)
	b.WriteString(ansiGray + strings.Repeat("─", fMidFill) + ansiReset)
	b.WriteString(footerRight)

	return b.String()
}

func (p *Panel) renderTabBarLocked(tabs []TabInfo, width int) string {
	var parts []string
	curX := 2 // left border "│ " occupies columns 0..1
	for i, tab := range tabs {
		label := tab.Title
		if label == "" {
			label = tab.ID
		}
		if label == "" {
			label = fmt.Sprintf("term-%d", i+1)
		}
		var seg string
		var labelPart string
		var closePart string
		var suffixPart string
		if tab.Active {
			labelPart = fmt.Sprintf(" %d:%s ", i+1, label)
			closePart = "×"
			suffixPart = " "
			seg = ansiBold + "\x1b[7m" + labelPart + closePart + suffixPart + ansiReset
		} else {
			color := ansiGray
			if tab.Exited {
				color = ansiDim
			}
			labelPart = fmt.Sprintf("[%d:%s ", i+1, label)
			closePart = "×"
			suffixPart = "]"
			seg = color + labelPart + closePart + suffixPart + ansiReset
		}
		segW := ansi.StringWidth(seg)
		labelW := ansi.StringWidth(labelPart)
		closeW := ansi.StringWidth(closePart)
		if curX+labelW <= 2+width {
			p.hits = append(p.hits, hitRect{
				row:    1,
				startX: curX,
				endX:   curX + labelW,
				kind:   hitActionSwitchTab,
				id:     tab.ID,
			})
		}
		if curX+labelW+closeW <= 2+width {
			p.hits = append(p.hits, hitRect{
				row:    1,
				startX: curX + labelW,
				endX:   curX + labelW + closeW,
				kind:   hitActionCloseTab,
				id:     tab.ID,
			})
		}
		parts = append(parts, seg)
		curX += segW + 1 // +1 for space separator in strings.Join
	}

	plusBtn := ansiCyan + "[+]" + ansiReset
	plusW := ansi.StringWidth(plusBtn)
	if curX+plusW <= 2+width {
		p.hits = append(p.hits, hitRect{
			row:    1,
			startX: curX,
			endX:   curX + plusW,
			kind:   hitActionNewTab,
		})
		parts = append(parts, plusBtn)
	}

	hint := ansiDim + "Alt+N:New Alt+←/→:Switch Alt+W:Close" + ansiReset
	joined := strings.Join(parts, " ")
	if ansi.StringWidth(joined)+2+ansi.StringWidth(hint) <= width {
		gap := width - ansi.StringWidth(joined) - ansi.StringWidth(hint)
		return joined + strings.Repeat(" ", gap) + hint
	}
	return joined
}

func shortenOmniCwd(cwd string, maxLen int) string {
	if cwd == "" || maxLen <= 0 {
		return ""
	}
	if ansi.StringWidth(cwd) <= maxLen {
		return cwd
	}
	parts := strings.Split(strings.TrimRight(cwd, "/"), "/")
	if len(parts) > 0 {
		base := parts[len(parts)-1]
		if base != "" && ansi.StringWidth(base) <= maxLen {
			return "…/" + base
		}
	}
	return ansi.Truncate(cwd, maxLen, "…")
}

func (p *Panel) renderOmniBarLocked(tabs []TabInfo, width int, title, cwd, statusBadge string) string {
	if len(tabs) == 0 {
		tabs = []TabInfo{{
			ID:     "",
			Title:  title,
			Active: true,
			Exited: p.exitCode != nil,
		}}
	}

	// Compact status badge for the omni bar
	compactStatus := ansiGreen + "●" + ansiReset
	switch {
	case p.copyMode:
		modeTag := "COPY"
		if p.copyVisual == copyVisualChar {
			modeTag = "VISUAL"
		} else if p.copyVisual == copyVisualLine {
			modeTag = "V-LINE"
		}
		compactStatus = ansiCyan + fmt.Sprintf("◆ %s %d,%d", modeTag, p.copyCol, p.copyRow) + ansiReset
	case p.isScrollbackMode:
		compactStatus = ansiCyan + fmt.Sprintf("◆ -%d", p.scrollOffset) + ansiReset
	case strings.HasPrefix(p.status, "Running"):
		compactStatus = ansiGreen + "●" + ansiReset
	case strings.HasPrefix(p.status, "Exited"):
		compactStatus = ansiYellow + "○ " + p.status + ansiReset
	case strings.HasPrefix(p.status, "Error"):
		compactStatus = ansiRed + "✗ " + p.status + ansiReset
	default:
		compactStatus = ansiGray + p.status + ansiReset
	}

	newBtnText := "[+]"
	menuBtnText := "[≡ Menu]"
	if width < 44 {
		menuBtnText = "[≡]"
	}
	newBtn := ansiCyan + newBtnText + ansiReset
	menuBtn := ansiCyan + menuBtnText + ansiReset
	if p.menuOpen {
		menuBtn = ansiBold + "\x1b[7m" + menuBtnText + ansiReset
	}

	// Build right-side candidates from richest to leanest so tabs get priority
	// while keeping status + menu accessible.
	type rightCandidate struct {
		text       string
		hasNew     bool
		hasMenu    bool
		newOffset  int
		menuOffset int
	}

	buildRight := func(meta string, includeNew, includeMenu bool) rightCandidate {
		var segs []string
		if meta != "" {
			segs = append(segs, meta)
		}
		segs = append(segs, compactStatus)
		newOff := -1
		menuOff := -1
		prefixLen := 0
		for _, s := range segs {
			prefixLen += ansi.StringWidth(s) + 1
		}
		if includeNew {
			newOff = prefixLen
			segs = append(segs, newBtn)
			prefixLen += ansi.StringWidth(newBtnText) + 1
		}
		if includeMenu {
			menuOff = prefixLen
			segs = append(segs, menuBtn)
		}
		return rightCandidate{
			text:       strings.Join(segs, " "),
			hasNew:     includeNew,
			hasMenu:    includeMenu,
			newOffset:  newOff,
			menuOffset: menuOff,
		}
	}

	var modeHint string
	if p.copyMode {
		modeHint = ansiGray + "hjkl:Move v:Sel y:Yank Esc:Exit" + ansiReset
	} else if p.isScrollbackMode {
		modeHint = ansiGray + "v:Copy End:Live" + ansiReset
	}

	sizeMeta := ansiDim + fmt.Sprintf("%dx%d", p.cols, p.rows) + ansiReset
	cwdMeta := ""
	if cwd != "" {
		cwdMeta = ansiDim + shortenOmniCwd(cwd, 24) + ansiReset
	}

	var candidates []rightCandidate
	if modeHint != "" {
		candidates = append(candidates, buildRight(modeHint, true, true))
	}
	if cwdMeta != "" {
		candidates = append(candidates, buildRight(cwdMeta+" · "+sizeMeta, true, true))
		candidates = append(candidates, buildRight(cwdMeta, true, true))
	}
	candidates = append(candidates, buildRight(sizeMeta, true, true))
	candidates = append(candidates, buildRight("", true, true))
	candidates = append(candidates, buildRight("", false, true))
	candidates = append(candidates, buildRight("", false, false))

	// Pre-build tab segments
	type tabSeg struct {
		tab        TabInfo
		rendered   string
		labelWidth int
		closeWidth int
		totalWidth int
	}
	activeIdx := 0
	allSegs := make([]tabSeg, len(tabs))
	for i, tab := range tabs {
		if tab.Active {
			activeIdx = i
		}
		label := tab.Title
		if label == "" {
			label = tab.ID
		}
		if label == "" {
			label = fmt.Sprintf("term-%d", i+1)
		}
		maxLabel := 20
		if len(tabs) == 1 {
			maxLabel = 32
		}
		if ansi.StringWidth(label) > maxLabel {
			label = ansi.Truncate(label, maxLabel, "…")
		}
		var labelPart, closePart, suffixPart, rendered string
		if tab.Active {
			labelPart = fmt.Sprintf(" %d:%s ", i+1, label)
			closePart = "×"
			suffixPart = " "
			rendered = ansiBold + "\x1b[7m" + labelPart + closePart + suffixPart + ansiReset
		} else {
			color := ansiGray
			if tab.Exited {
				color = ansiDim
			}
			labelPart = fmt.Sprintf(" %d:%s ", i+1, label)
			closePart = "×"
			suffixPart = " "
			rendered = color + labelPart + closePart + suffixPart + ansiReset
		}
		allSegs[i] = tabSeg{
			tab:        tab,
			rendered:   rendered,
			labelWidth: ansi.StringWidth(labelPart),
			closeWidth: ansi.StringWidth(closePart),
			totalWidth: ansi.StringWidth(rendered),
		}
	}

	// Pick the richest right candidate that still leaves room for at least the active tab.
	minTabNeed := allSegs[activeIdx].totalWidth
	if minTabNeed > width/2 && width >= 20 {
		minTabNeed = width / 2
	}
	chosenRight := candidates[len(candidates)-1]
	for _, cand := range candidates {
		rw := ansi.StringWidth(cand.text)
		if rw+1+minTabNeed <= width {
			chosenRight = cand
			break
		}
	}
	rightW := ansi.StringWidth(chosenRight.text)
	availLeft := width - rightW - 1
	if availLeft < 4 {
		availLeft = width - rightW
	}
	if availLeft < 0 {
		availLeft = 0
	}

	// Select a contiguous window of tabs around activeIdx that fits in availLeft.
	startIdx, endIdx := activeIdx, activeIdx+1
	usedW := allSegs[activeIdx].totalWidth
	for {
		expanded := false
		if endIdx < len(allSegs) {
			need := allSegs[endIdx].totalWidth + 1
			hiddenAfter := len(allSegs) - (endIdx + 1) + startIdx
			overflowReserve := 0
			if hiddenAfter > 0 {
				overflowReserve = len(fmt.Sprintf(" +%d", hiddenAfter))
			}
			if usedW+need+overflowReserve <= availLeft {
				usedW += need
				endIdx++
				expanded = true
			}
		}
		if startIdx > 0 {
			need := allSegs[startIdx-1].totalWidth + 1
			hiddenAfter := (len(allSegs) - endIdx) + (startIdx - 1)
			overflowReserve := 0
			if hiddenAfter > 0 {
				overflowReserve = len(fmt.Sprintf(" +%d", hiddenAfter))
			}
			if usedW+need+overflowReserve <= availLeft {
				usedW += need
				startIdx--
				expanded = true
			}
		}
		if !expanded {
			break
		}
	}

	var leftParts []string
	curX := 0
	for i := startIdx; i < endIdx; i++ {
		seg := allSegs[i]
		rendered := seg.rendered
		if curX+seg.totalWidth > availLeft && availLeft > 0 {
			rendered = ansi.Truncate(rendered, availLeft-curX, "…")
		}
		segW := ansi.StringWidth(rendered)
		if seg.tab.ID != "" && curX+seg.labelWidth <= availLeft {
			p.hits = append(p.hits, hitRect{
				row:    0,
				startX: curX,
				endX:   curX + seg.labelWidth,
				kind:   hitActionSwitchTab,
				id:     seg.tab.ID,
			})
		}
		if (seg.tab.ID != "" || p.seams.Close != nil) && curX+seg.labelWidth+seg.closeWidth <= availLeft {
			p.hits = append(p.hits, hitRect{
				row:    0,
				startX: curX + seg.labelWidth,
				endX:   curX + seg.labelWidth + seg.closeWidth,
				kind:   hitActionCloseTab,
				id:     seg.tab.ID,
			})
		}
		leftParts = append(leftParts, rendered)
		curX += segW + 1
	}

	hiddenCount := len(allSegs) - (endIdx - startIdx)
	if hiddenCount > 0 {
		moreText := fmt.Sprintf("+%d", hiddenCount)
		moreW := ansi.StringWidth(moreText)
		if curX+moreW <= availLeft {
			p.hits = append(p.hits, hitRect{
				row:    0,
				startX: curX,
				endX:   curX + moreW,
				kind:   hitActionOpenMenu,
			})
			leftParts = append(leftParts, ansiCyan+moreText+ansiReset)
		}
	}

	leftStr := strings.Join(leftParts, " ")
	leftW := ansi.StringWidth(leftStr)
	if leftW > availLeft {
		leftStr = ansi.Truncate(leftStr, availLeft, "")
		leftW = ansi.StringWidth(leftStr)
	}

	gap := width - leftW - rightW
	if gap < 0 {
		gap = 0
	}
	rightStart := leftW + gap
	if chosenRight.hasNew && chosenRight.newOffset >= 0 {
		ns := rightStart + chosenRight.newOffset
		p.hits = append(p.hits, hitRect{
			row:    0,
			startX: ns,
			endX:   ns + ansi.StringWidth(newBtnText),
			kind:   hitActionNewTab,
		})
	}
	if chosenRight.hasMenu && chosenRight.menuOffset >= 0 {
		ms := rightStart + chosenRight.menuOffset
		p.hits = append(p.hits, hitRect{
			row:    0,
			startX: ms,
			endX:   ms + ansi.StringWidth(menuBtnText),
			kind:   hitActionOpenMenu,
		})
	}

	bar := leftStr + strings.Repeat(" ", gap) + chosenRight.text
	if bw := ansi.StringWidth(bar); bw > width {
		bar = ansi.Truncate(bar, width, "")
	} else if bw < width {
		bar += strings.Repeat(" ", width-bw)
	}
	return bar
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

	if p.term.CursorVisible && p.exitCode == nil {
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
