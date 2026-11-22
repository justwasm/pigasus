package terminalpanel

import (
	tea "charm.land/bubbletea/v2"
)

// HandleKey processes keyboard input when the terminal panel is focused.
// It implements AGENTS.md §10 modal isolation:
//   - Esc detaches from the panel without killing the PTY process or leaking to global turn cancellation.
//   - Ctrl+C is passed through to the PTY process as byte 0x03 (SIGINT).
//   - PgUp/PgDn navigate the scrollback buffer when history is available.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// 1. Esc key handling (Modal isolation):
	if key.Code == tea.KeyEsc {
		if p.isScrollbackMode {
			// Exit scrollback mode back to live output view
			p.isScrollbackMode = false
			p.scrollOffset = 0
			return true, nil
		}
		// Detach from panel modal; leaves PTY running
		if p.seams.Detach != nil {
			return true, p.seams.Detach()
		}
		if p.seams.Close != nil {
			return true, p.seams.Close()
		}
		return true, nil
	}

	// 2. Scrollback navigation triggers:
	// PgUp activates scrollback mode if history exists
	sbCount := p.scrollbackCount()
	if key.Code == tea.KeyPgUp && sbCount > 0 {
		p.isScrollbackMode = true
		step := p.rows / 2
		if step < 1 {
			step = 1
		}
		p.scrollOffset += step
		if p.scrollOffset > sbCount {
			p.scrollOffset = sbCount
		}
		return true, nil
	}

	// If currently browsing scrollback history:
	if p.isScrollbackMode {
		switch key.Code {
		case tea.KeyUp:
			if p.scrollOffset < sbCount {
				p.scrollOffset++
			}
			return true, nil
		case tea.KeyDown:
			p.scrollOffset--
			if p.scrollOffset <= 0 {
				p.scrollOffset = 0
				p.isScrollbackMode = false
			}
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
			return true, nil
		case tea.KeyHome:
			p.scrollOffset = sbCount
			return true, nil
		case tea.KeyEnd:
			p.scrollOffset = 0
			p.isScrollbackMode = false
			return true, nil
		}

		// Any character input drops out of scrollback mode and sends key to PTY
		p.scrollOffset = 0
		p.isScrollbackMode = false
	}

	// 3. Encode key and forward directly to the backend PTY
	raw := EncodeKey(key)
	if len(raw) > 0 && p.seams.WriteInput != nil {
		_ = p.seams.WriteInput(raw)
	}

	// Per AGENTS.md §10, all keystrokes in focused modal are claimed
	return true, nil
}
