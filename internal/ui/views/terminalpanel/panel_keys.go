package terminalpanel

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func isCopyModeToggleKey(key tea.Key) bool {
	if (key.Code == 'c' || key.Code == 'C') && key.Mod.Contains(tea.ModAlt) && !key.Mod.Contains(tea.ModCtrl) {
		return true
	}
	if (key.Code == tea.KeySpace || key.Code == ' ' || key.Code == '@') && key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) {
		return true
	}
	return false
}

// HandleKey processes keyboard input when the terminal panel is focused.
// It implements AGENTS.md §10 modal isolation:
//   - Esc detaches from the panel without killing the PTY process or leaking to global turn cancellation.
//   - Ctrl+C is passed through to the PTY process as byte 0x03 (SIGINT).
//   - PgUp/PgDn navigate the scrollback buffer when history is available.
//   - Alt+C / Ctrl+Space (or 'v' in scrollback mode) toggles keyboard Copy Mode.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}

	p.mu.Lock()

	// -1. Interactive menu active or toggled (Alt+M / F10)
	if p.menuOpen {
		handled, cmd, runFn := p.handleMenuKeyLocked(key)
		p.mu.Unlock()
		if runFn != nil {
			if rcmd := runFn(); rcmd != nil {
				return handled, rcmd
			}
		}
		return handled, cmd
	}
	if isMenuToggleKey(key) {
		p.menuOpen = true
		p.menuRenaming = false
		p.menuRenameBuf = ""
		p.menuCursor = 0
		p.mu.Unlock()
		return true, nil
	}

	// 0. Copy mode toggle (Alt+C or Ctrl+Space)
	if isCopyModeToggleKey(key) {
		if p.copyMode {
			p.exitCopyModeLocked()
		} else {
			p.enterCopyModeLocked(copyVisualNone)
		}
		p.mu.Unlock()
		return true, nil
	}

	// 1. Copy mode active: handle vi-style navigation, visual selection, yank, exit
	if p.copyMode {
		copyFn, yanked, hasYank := p.handleCopyModeKeyLocked(key)
		p.mu.Unlock()
		if hasYank && copyFn != nil {
			_ = copyFn(yanked)
		}
		return true, nil
	}

	// 2. Esc key handling (Modal isolation or passthrough):
	if key.Code == tea.KeyEsc {
		if p.isScrollbackMode {
			// Exit scrollback mode back to live output view
			p.isScrollbackMode = false
			p.scrollOffset = 0
			p.mu.Unlock()
			return true, nil
		}
		if !p.seams.PassThroughEsc {
			detachFn := p.seams.Detach
			closeFn := p.seams.Close
			p.mu.Unlock()
			// Detach from panel modal; leaves PTY running
			if detachFn != nil {
				return true, detachFn()
			}
			if closeFn != nil {
				return true, closeFn()
			}
			return true, nil
		}
	}

	// 3. Scrollback navigation triggers:
	// Unmodified PgUp activates scrollback mode if history exists
	sbCount := p.scrollbackCount()
	if key.Code == tea.KeyPgUp && key.Mod == 0 && sbCount > 0 {
		p.isScrollbackMode = true
		step := p.rows / 2
		if step < 1 {
			step = 1
		}
		p.scrollOffset += step
		if p.scrollOffset > sbCount {
			p.scrollOffset = sbCount
		}
		p.mu.Unlock()
		return true, nil
	}

	// If currently browsing scrollback history:
	if p.isScrollbackMode {
		switch key.Code {
		case tea.KeyUp:
			if p.scrollOffset < sbCount {
				p.scrollOffset++
			}
			p.mu.Unlock()
			return true, nil
		case tea.KeyDown:
			p.scrollOffset--
			if p.scrollOffset <= 0 {
				p.scrollOffset = 0
				p.isScrollbackMode = false
			}
			p.mu.Unlock()
			return true, nil
		case tea.KeyPgDown:
			step := p.rows / 2
			if step < 1 {
				step = 1
			}
			p.scrollOffset -= step
			if p.scrollOffset <= 0 {
				p.scrollOffset = 0
				p.isScrollbackMode = false
			}
			p.mu.Unlock()
			return true, nil
		case tea.KeyHome:
			p.scrollOffset = sbCount
			p.mu.Unlock()
			return true, nil
		case tea.KeyEnd:
			p.scrollOffset = 0
			p.isScrollbackMode = false
			p.mu.Unlock()
			return true, nil
		}
		if (key.Code == 'v' || key.Text == "v") && !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) {
			p.enterCopyModeLocked(copyVisualChar)
			p.mu.Unlock()
			return true, nil
		}

		// Any other character input drops out of scrollback mode and sends key to PTY
		p.scrollOffset = 0
		p.isScrollbackMode = false
	}

	// 4. Encode key and forward directly to the backend PTY
	writeFn := p.seams.WriteInput
	encodeOpts := KeyEncodeOptions{
		CursorKeysApp:   p.cursorKeysApp,
		ModifyOtherKeys: p.modifyOtherKeys,
	}
	p.mu.Unlock()

	raw := EncodeKeyWithOptions(key, encodeOpts)
	if len(raw) > 0 && writeFn != nil {
		_ = writeFn(raw)
	}

	// Per AGENTS.md §10, all keystrokes in focused modal are claimed
	return true, nil
}

func (p *Panel) enterCopyModeLocked(visual copyVisualKind) {
	bodyH, width := p.bodyDimensionsLocked()
	lines, sbLen := p.virtualBufferLocked()
	totalCount := len(lines)
	if totalCount == 0 {
		return
	}
	p.copyMode = true
	p.isScrollbackMode = true

	winStart, winEnd := p.visibleWindowLocked(totalCount, bodyH)
	curRow := sbLen
	curCol := 0
	if p.term != nil {
		curRow = sbLen + p.term.Cursor.Y
		curCol = p.term.Cursor.X
	}
	if curRow < winStart {
		curRow = winStart
	}
	if curRow >= winEnd {
		curRow = winEnd - 1
	}
	if curRow < 0 {
		curRow = 0
	}
	if curCol < 0 {
		curCol = 0
	}
	if curCol >= width {
		curCol = width - 1
	}
	p.copyRow = curRow
	p.copyCol = curCol
	p.copyVisual = visual
	p.copyAnchorRow = curRow
	p.copyAnchorCol = curCol
}

func (p *Panel) exitCopyModeLocked() {
	p.copyMode = false
	p.copyVisual = copyVisualNone
	p.isScrollbackMode = false
	p.scrollOffset = 0
}

func (p *Panel) handleCopyModeKeyLocked(key tea.Key) (func(string) error, string, bool) {
	bodyH, width := p.bodyDimensionsLocked()
	lines, _ := p.virtualBufferLocked()
	totalCount := len(lines)
	if totalCount == 0 {
		p.exitCopyModeLocked()
		return nil, "", false
	}

	// Exit copy mode on Esc or q
	if key.Code == tea.KeyEsc || ((key.Code == 'q' || key.Text == "q") && !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt)) {
		p.exitCopyModeLocked()
		return nil, "", false
	}

	// Yank on y or Enter
	if key.Code == tea.KeyEnter || ((key.Code == 'y' || key.Text == "y") && !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt)) {
		text := p.extractCopySelectionLocked(lines)
		copyFn := p.seams.CopyText
		p.exitCopyModeLocked()
		return copyFn, text, true
	}

	// Visual mode toggles: v (char) / V (line)
	if !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) {
		if key.Text == "V" || key.Code == 'V' || (key.Code == 'v' && key.Mod.Contains(tea.ModShift)) {
			if p.copyVisual == copyVisualLine {
				p.copyVisual = copyVisualNone
			} else {
				p.copyVisual = copyVisualLine
				p.copyAnchorRow = p.copyRow
				p.copyAnchorCol = p.copyCol
			}
			return nil, "", false
		}
		if key.Text == "v" || key.Code == 'v' {
			if p.copyVisual == copyVisualChar {
				p.copyVisual = copyVisualNone
			} else {
				p.copyVisual = copyVisualChar
				p.copyAnchorRow = p.copyRow
				p.copyAnchorCol = p.copyCol
			}
			return nil, "", false
		}
	}

	step := bodyH / 2
	if step < 1 {
		step = 1
	}

	switch {
	case key.Code == tea.KeyLeft || (!key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) && (key.Code == 'h' || key.Text == "h")):
		if p.copyCol > 0 {
			p.copyCol--
		}
	case key.Code == tea.KeyRight || (!key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) && (key.Code == 'l' || key.Text == "l")):
		if p.copyCol < width-1 {
			p.copyCol++
		}
	case key.Code == tea.KeyUp || (!key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) && (key.Code == 'k' || key.Text == "k")):
		if p.copyRow > 0 {
			p.copyRow--
		}
	case key.Code == tea.KeyDown || (!key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) && (key.Code == 'j' || key.Text == "j")):
		if p.copyRow < totalCount-1 {
			p.copyRow++
		}
	case key.Code == tea.KeyHome || (!key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) && (key.Code == '0' || key.Text == "0")):
		p.copyCol = 0
	case key.Code == tea.KeyEnd || (!key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) && (key.Code == '$' || key.Text == "$")):
		plain := strings.TrimRight(ansi.Strip(lines[p.copyRow]), " ")
		w := ansi.StringWidth(plain)
		if w > 0 {
			p.copyCol = w - 1
		} else {
			p.copyCol = 0
		}
		if p.copyCol >= width {
			p.copyCol = width - 1
		}
	case !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) && (key.Text == "G" || key.Code == 'G' || (key.Code == 'g' && key.Mod.Contains(tea.ModShift))):
		p.copyRow = totalCount - 1
	case !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) && (key.Code == 'g' || key.Text == "g"):
		p.copyRow = 0
	case key.Code == tea.KeyPgUp || (key.Code == 'u' && key.Mod.Contains(tea.ModCtrl)):
		p.copyRow -= step
		if p.copyRow < 0 {
			p.copyRow = 0
		}
	case key.Code == tea.KeyPgDown || (key.Code == 'd' && key.Mod.Contains(tea.ModCtrl)):
		p.copyRow += step
		if p.copyRow >= totalCount {
			p.copyRow = totalCount - 1
		}
	}

	p.ensureCopyCursorVisibleLocked(totalCount, bodyH)
	return nil, "", false
}

func (p *Panel) ensureCopyCursorVisibleLocked(totalCount, bodyH int) {
	maxScroll := totalCount - bodyH
	if maxScroll < 0 {
		maxScroll = 0
	}
	winStart, winEnd := p.visibleWindowLocked(totalCount, bodyH)
	if p.copyRow < winStart {
		winStart = p.copyRow
		winEnd = winStart + bodyH
		if winEnd > totalCount {
			winEnd = totalCount
		}
		p.scrollOffset = totalCount - winEnd
	} else if p.copyRow >= winEnd {
		winEnd = p.copyRow + 1
		p.scrollOffset = totalCount - winEnd
	}
	if p.scrollOffset < 0 {
		p.scrollOffset = 0
	}
	if p.scrollOffset > maxScroll {
		p.scrollOffset = maxScroll
	}
}

func (p *Panel) copySelectionRangeLocked() (sr, sc, er, ec int, lineMode bool, active bool) {
	if !p.copyMode || p.copyVisual == copyVisualNone {
		return 0, 0, 0, 0, false, false
	}
	sr, sc = p.copyAnchorRow, p.copyAnchorCol
	er, ec = p.copyRow, p.copyCol
	if sr > er || (sr == er && sc > ec) {
		sr, sc, er, ec = er, ec, sr, sc
	}
	return sr, sc, er, ec, p.copyVisual == copyVisualLine, true
}

func (p *Panel) extractCopySelectionLocked(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	sr, sc, er, ec, lineMode, active := p.copySelectionRangeLocked()
	if !active {
		if p.copyRow >= 0 && p.copyRow < len(lines) {
			return strings.TrimRight(ansi.Strip(lines[p.copyRow]), " ")
		}
		return ""
	}
	if sr < 0 {
		sr = 0
	}
	if er >= len(lines) {
		er = len(lines) - 1
	}
	if lineMode {
		out := make([]string, 0, er-sr+1)
		for r := sr; r <= er; r++ {
			out = append(out, strings.TrimRight(ansi.Strip(lines[r]), " "))
		}
		return strings.Join(out, "\n")
	}
	var out []string
	for r := sr; r <= er; r++ {
		plain := ansi.Strip(lines[r])
		rw := ansi.StringWidth(plain)
		from, to := 0, rw
		if r == sr {
			from = sc
		}
		if r == er {
			to = ec + 1
		}
		if from < 0 {
			from = 0
		}
		if from > rw {
			from = rw
		}
		if to > rw {
			to = rw
		}
		if from > to {
			from = to
		}
		seg := ansi.Cut(plain, from, to)
		if r < er {
			seg = strings.TrimRight(seg, " ")
		}
		out = append(out, seg)
	}
	return strings.Join(out, "\n")
}
