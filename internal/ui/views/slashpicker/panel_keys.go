// panel_keys.go — the popup's keys.
//
// The popup owns the navigation arrows and Tab. Every other key is
// left to the host: the composer still has to receive the letters the
// user types, and the host's global handlers (the /help overlay, the
// panels) still have to see their own keys.
//
// The popup is not modal in the way the /help overlay is: it is a
// completion popup over a live composer, so the keys it does not own
// reach the composer.

package slashpicker

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/slashcomplete"
)

// UpdateKey handles a key while the popup is up in Elm single-direction
// message passing style. It reports whether the key was handled and returns an optional tea.Cmd.
func (p *Panel) UpdateKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	switch key.Code {
	case tea.KeyUp:
		if !p.Visible() {
			return false, nil
		}
		p.MoveUp()
		return true, nil
	case tea.KeyDown:
		if !p.Visible() {
			return false, nil
		}
		p.MoveDown()
		return true, nil
	case tea.KeyTab:
		if key.Mod.Contains(tea.ModShift) {
			return false, nil
		}
		if p.Selected() == nil {
			return false, nil
		}
		// Before committing one highlighted row, try to advance the
		// composer to the longest prefix every match shares. This is
		// the shell-style first-Tab behaviour: push the user as far as
		// the candidates agree, leaving the popup open so the second
		// Tab still commits the highlighted row. It only applies to
		// the plain command list; the model/voice/terminal/resume
		// sub-modes complete a row's argument, where a shared prefix
		// would be meaningless.
		if handled, cmd, advanced := p.completeSharedPrefix(); advanced {
			return handled, cmd
		}
		return p.CommitSelectionCmd()
	}
	return false, nil
}

// completeSharedPrefix advances the composer to the longest common
// prefix of the currently matching rows.
//
// advanced is false when the caller must fall back to committing the
// highlighted row: no popup, a sub-mode, a single match, or a shared
// prefix that adds nothing to what the user already typed. In that
// last case returning false is what preserves the long-standing
// behaviour where Tab on an ambiguous buffer picks the highlighted
// row.
func (p *Panel) completeSharedPrefix() (handled bool, cmd tea.Cmd, advanced bool) {
	if p == nil || !p.Visible() || len(p.all) < 2 {
		return false, nil, false
	}
	if p.modelMode || p.voiceMode || p.terminalMode || p.resumeMode {
		return false, nil, false
	}
	if p.commandSource != nil {
		// A host-supplied source owns its own rows; the shared-prefix
		// rule is only defined over the real command catalogue.
		return false, nil, false
	}
	buf := p.buffer()
	res := slashcomplete.Complete(buf, p.all)
	if res.Kind != slashcomplete.Multiple || res.Text == "" {
		return false, nil, false
	}
	if res.Text == buf {
		// Nothing new to add; let the caller commit the highlight.
		return false, nil, false
	}
	p.setBuffer(res.Text)
	// Re-filter against the longer prefix so the popup shows the
	// narrowed candidate set and the highlight stays on a valid row.
	p.Update([]rune(res.Text))
	return true, CompletedCmd(res.Text, false, ""), true
}

// HandleKey handles a key while the popup is up.
//
// It reports whether the key was handled. A hidden popup handles none.
func (p *Panel) HandleKey(key tea.Key) bool {
	handled, _ := p.UpdateKey(key)
	return handled
}

// Cancel closes the popup. It reports whether the popup was open.
//
// The host's Esc handler asks the popup first: an open popup is the
// modal that owns the key.
func (p *Panel) Cancel() bool {
	handled, _ := p.CancelCmd()
	return handled
}

// CancelCmd closes the popup and returns a tea.Cmd emitting DismissedMsg.
func (p *Panel) CancelCmd() (bool, tea.Cmd) {
	if p == nil || !p.Visible() {
		return false, nil
	}
	p.Reset()
	return true, DismissCmd()
}

// CommitSelectionCmd completes the highlighted row into the composer and returns
// a tea.Cmd emitting CompletedMsg.
func (p *Panel) CommitSelectionCmd() (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	sel := p.Selected()
	if sel == nil {
		return false, nil
	}
	text := sel.Name
	if p.modelMode {
		target := sel.Description
		if target == "" {
			target = sel.Name
		}
		text = "/model " + target
		p.setBuffer(text)
		p.Reset()
		return true, CompletedCmd(text, true, target)
	}
	if p.voiceMode {
		if p.voiceSubcmd == "" {
			switch sel.Name {
			case "speaker", "duplex", "volume", "speed":
				text = "/voice " + sel.Name + " "
				p.setBuffer(text)
				p.Update([]rune(text))
				return true, CompletedCmd(text, false, sel.Name)
			default:
				text = "/voice " + sel.Name
				p.setBuffer(text)
				p.Reset()
				return true, CompletedCmd(text, true, sel.Name)
			}
		} else {
			text = "/voice " + p.voiceSubcmd + " " + sel.Name
			if p.seams.Buffer != nil && strings.HasPrefix(p.seams.Buffer(), "/speaker ") {
				text = "/speaker " + sel.Name
			}
			p.setBuffer(text)
			p.Reset()
			return true, CompletedCmd(text, true, sel.Name)
		}
	}
	if p.terminalMode {
		if p.terminalSubcmd == "" {
			switch sel.Name {
			case "switch", "attach", "close", "kill", "rename", "run":
				text = "/terminal " + sel.Name + " "
				p.setBuffer(text)
				p.Update([]rune(text))
				return true, CompletedCmd(text, false, sel.Name)
			default:
				text = "/terminal " + sel.Name
				p.setBuffer(text)
				p.Reset()
				return true, CompletedCmd(text, true, sel.Name)
			}
		}
		text = "/terminal " + p.terminalSubcmd + " " + sel.Name
		p.setBuffer(text)
		p.Reset()
		return true, CompletedCmd(text, true, sel.Name)
	}
	if p.resumeMode {
		bufStr := ""
		if p.seams.Buffer != nil {
			bufStr = p.seams.Buffer()
		}
		prefix := "/resume "
		if strings.HasPrefix(bufStr, "/sessions ") {
			prefix = "/sessions "
		}
		text = prefix + sel.Name
		p.setBuffer(text)
		p.Reset()
		return true, CompletedCmd(text, true, sel.Name)
	}
	text += " "
	p.setBuffer(text)
	p.Update([]rune(text))
	return true, CompletedCmd(text, false, sel.Name)
}

// CommitSelectionToBuffer completes the highlighted row into the
// composer.
//
// In the command mode the composer takes the command's name; in the
// model mode it keeps the command and gains the display label
// (【一手】 agy 1.4.6, live: "/model oss" + Tab → "/model GPT-OSS
// 120B (Medium)", and the popup closes).
func (p *Panel) CommitSelectionToBuffer() bool {
	handled, _ := p.CommitSelectionCmd()
	return handled
}
