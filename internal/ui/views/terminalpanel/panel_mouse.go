package terminalpanel

import (
	tea "charm.land/bubbletea/v2"
)

// MouseTrackingEnabled reports whether the child process inside the PTY has
// requested terminal mouse reporting (DEC modes 1000, 1002, or 1003).
func (p *Panel) MouseTrackingEnabled() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.mouseClicks || p.mouseCellMotion || p.mouseAllMotion
}

// MouseAllMotion reports whether the child process requested DEC mode 1003 (all motion).
func (p *Panel) MouseAllMotion() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.mouseAllMotion
}

func (p *Panel) bodyOriginLocked() (bodyTopRow, bodyLeftCol, bodyH, bodyW int) {
	bodyH, bodyW = p.bodyDimensionsLocked()
	bodyTopRow = 1
	if p.seams.OmniBar {
		bodyLeftCol = 0
		return bodyTopRow, bodyLeftCol, bodyH, bodyW
	}
	if len(p.tabsLocked()) > 1 {
		bodyTopRow = 2
	}
	bodyLeftCol = 2
	return bodyTopRow, bodyLeftCol, bodyH, bodyW
}

func clampCoord(v, max int) int {
	if v < 0 {
		return 0
	}
	if max > 0 && v >= max {
		return max - 1
	}
	return v
}

func (p *Panel) findHitLocked(x, y int, kinds ...hitActionKind) (hitRect, bool) {
	for i := len(p.hits) - 1; i >= 0; i-- {
		h := p.hits[i]
		if h.row != y || x < h.startX || x >= h.endX {
			continue
		}
		if len(kinds) == 0 {
			return h, true
		}
		for _, k := range kinds {
			if h.kind == k {
				return h, true
			}
		}
	}
	return hitRect{}, false
}

// HandleWheel processes mouse wheel events inside the terminal panel.
// Coordinates in ev must be panel-relative (0,0 = top-left corner of the panel).
func (p *Panel) HandleWheel(ev tea.Mouse) bool {
	if p == nil {
		return false
	}
	up := ev.Button == tea.MouseWheelUp
	down := ev.Button == tea.MouseWheelDown
	if !up && !down {
		return false
	}

	p.mu.Lock()

	// 1. Menu navigation when menu is open
	if p.menuOpen {
		if !p.menuRenaming {
			items := p.menuItemsLocked()
			if len(items) > 0 {
				if up {
					if p.menuCursor > 0 {
						p.menuCursor--
					} else {
						p.menuCursor = len(items) - 1
					}
				} else {
					p.menuCursor = (p.menuCursor + 1) % len(items)
				}
			}
		}
		p.mu.Unlock()
		return true
	}

	// 2. Forward wheel events to PTY when child process enabled mouse reporting
	if (p.mouseClicks || p.mouseCellMotion || p.mouseAllMotion) && !p.copyMode && !p.isScrollbackMode {
		bodyTop, bodyLeft, bodyH, bodyW := p.bodyOriginLocked()
		col := clampCoord(ev.X-bodyLeft, bodyW)
		row := clampCoord(ev.Y-bodyTop, bodyH)
		raw := EncodeMouse(ev, col, row, false, false, p.mouseSGR)
		writeFn := p.seams.WriteInput
		p.mu.Unlock()
		if len(raw) > 0 && writeFn != nil {
			_ = writeFn(raw)
		}
		return true
	}

	// 3. Alternate screen scroll mode (e.g. less/man/top without mouse reporting):
	// translate wheel into Up/Down arrow sequences.
	if p.term != nil && p.term.IsAlt && !p.copyMode && !p.isScrollbackMode {
		keyCode := tea.KeyDown
		if up {
			keyCode = tea.KeyUp
		}
		seq := EncodeKeyWithModes(tea.Key{Code: keyCode}, p.cursorKeysApp)
		writeFn := p.seams.WriteInput
		p.mu.Unlock()
		if len(seq) > 0 && writeFn != nil {
			var batch []byte
			for i := 0; i < 3; i++ {
				batch = append(batch, seq...)
			}
			_ = writeFn(batch)
		}
		return true
	}

	// 4. Scrollback / copy mode wheel scrolling
	const wheelStep = 3
	if p.copyMode {
		bodyH, _ := p.bodyDimensionsLocked()
		lines, _ := p.virtualBufferLocked()
		totalCount := len(lines)
		if totalCount > 0 {
			if up {
				p.copyRow -= wheelStep
				if p.copyRow < 0 {
					p.copyRow = 0
				}
			} else {
				p.copyRow += wheelStep
				if p.copyRow >= totalCount {
					p.copyRow = totalCount - 1
				}
			}
			p.ensureCopyCursorVisibleLocked(totalCount, bodyH)
		}
		p.mu.Unlock()
		return true
	}

	sbCount := p.scrollbackCount()
	if up {
		if sbCount > 0 {
			p.isScrollbackMode = true
			p.scrollOffset += wheelStep
			if p.scrollOffset > sbCount {
				p.scrollOffset = sbCount
			}
		}
	} else if p.isScrollbackMode {
		p.scrollOffset -= wheelStep
		if p.scrollOffset <= 0 {
			p.scrollOffset = 0
			p.isScrollbackMode = false
		}
	}

	p.mu.Unlock()
	return true
}

// HandleMouse processes mouse click, motion, and release events inside the terminal panel.
// Coordinates in ev must be panel-relative (0,0 = top-left corner of the panel).
// Returns (true, cmd) if the panel consumed the event (chrome click, menu, or PTY mouse passthrough),
// or (false, nil) if the host should handle it (e.g. drag-to-select text when mouse tracking is off).
func (p *Panel) HandleMouse(ev tea.Mouse, isRelease, isMotion bool) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}

	p.mu.Lock()

	// 1. Handle open menu interactions first
	if p.menuOpen {
		if isMotion {
			if hit, ok := p.findHitLocked(ev.X, ev.Y, hitActionMenuItem); ok && !p.menuRenaming {
				p.menuCursor = hit.index
			}
			p.mu.Unlock()
			return true, nil
		}
		if !isRelease {
			if hit, ok := p.findHitLocked(ev.X, ev.Y, hitActionMenuItem); ok && !p.menuRenaming {
				items := p.menuItemsLocked()
				if hit.index >= 0 && hit.index < len(items) {
					p.menuCursor = hit.index
					runFn := p.executeMenuItemLocked(items[hit.index])
					p.mu.Unlock()
					if runFn != nil {
						return true, runFn()
					}
					return true, nil
				}
			}
			if p.menuBox.valid && ev.X >= p.menuBox.left && ev.X < p.menuBox.right && ev.Y >= p.menuBox.top && ev.Y < p.menuBox.bottom {
				p.mu.Unlock()
				return true, nil
			}
			// Click outside menu closes it
			p.menuOpen = false
			p.menuRenaming = false
			p.menuRenameBuf = ""
			if _, ok := p.findHitLocked(ev.X, ev.Y, hitActionOpenMenu); ok {
				p.mu.Unlock()
				return true, nil
			}
		} else {
			p.mu.Unlock()
			return true, nil
		}
	}

	// 2. Handle chrome clicks (header buttons & tab bar)
	if !isRelease && !isMotion && ev.Button == tea.MouseLeft {
		if hit, ok := p.findHitLocked(ev.X, ev.Y, hitActionNewTab, hitActionOpenMenu, hitActionSwitchTab, hitActionCloseTab); ok {
			switch hit.kind {
			case hitActionNewTab:
				newFn := p.seams.NewTab
				p.mu.Unlock()
				if newFn != nil {
					return true, newFn()
				}
				return true, nil
			case hitActionOpenMenu:
				p.menuOpen = !p.menuOpen
				p.menuRenaming = false
				p.menuRenameBuf = ""
				p.menuCursor = 0
				p.mu.Unlock()
				return true, nil
			case hitActionSwitchTab:
				switchFn := p.seams.SwitchTab
				tabID := hit.id
				p.mu.Unlock()
				if switchFn != nil && tabID != "" {
					return true, switchFn(tabID)
				}
				return true, nil
			case hitActionCloseTab:
				closeTabFn := p.seams.CloseTab
				closeFn := p.seams.Close
				tabID := hit.id
				p.mu.Unlock()
				if closeTabFn != nil && tabID != "" {
					return true, closeTabFn(tabID)
				}
				if closeFn != nil {
					return true, closeFn()
				}
				return true, nil
			}
		}
	}

	// 3. Forward body mouse events to PTY when child process enabled mouse reporting
	mouseEnabled := p.mouseClicks || p.mouseCellMotion || p.mouseAllMotion
	if !mouseEnabled || p.copyMode || p.isScrollbackMode {
		p.mu.Unlock()
		return false, nil
	}

	bodyTop, bodyLeft, bodyH, bodyW := p.bodyOriginLocked()
	inBody := ev.X >= bodyLeft && ev.X < bodyLeft+bodyW && ev.Y >= bodyTop && ev.Y < bodyTop+bodyH
	if !inBody && p.mousePressedBtn == tea.MouseNone {
		p.mu.Unlock()
		return false, nil
	}

	if isMotion {
		dragging := p.mousePressedBtn != tea.MouseNone || ev.Button != tea.MouseNone
		if !p.mouseAllMotion && !(p.mouseCellMotion && dragging) {
			p.mu.Unlock()
			return true, nil
		}
	}

	evCopy := ev
	if !isRelease && !isMotion {
		p.mousePressedBtn = ev.Button
	} else if isMotion {
		if evCopy.Button == tea.MouseNone && p.mousePressedBtn != tea.MouseNone {
			evCopy.Button = p.mousePressedBtn
		}
	} else if isRelease {
		if evCopy.Button == tea.MouseNone && p.mousePressedBtn != tea.MouseNone {
			evCopy.Button = p.mousePressedBtn
		}
		p.mousePressedBtn = tea.MouseNone
	}

	col := clampCoord(ev.X-bodyLeft, bodyW)
	row := clampCoord(ev.Y-bodyTop, bodyH)
	raw := EncodeMouse(evCopy, col, row, isRelease, isMotion, p.mouseSGR)
	writeFn := p.seams.WriteInput
	p.mu.Unlock()

	if len(raw) > 0 && writeFn != nil {
		_ = writeFn(raw)
	}
	return true, nil
}
