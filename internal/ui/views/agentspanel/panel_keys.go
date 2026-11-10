// panel_keys.go — the /agents panel's keys.
//
// The panel owns every key it receives: the ↑/↓ navigation, the Enter
// that prepares a switch, the Esc that applies it, and the three
// subagent keys — the kill and the two answers to a pending
// authorization. A key the panel does not own is answered with
// handled=false, so the host's global handlers (Ctrl+C) still see it.

package agentspanel

import (
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// HandleKey routes one key to the panel.
//
// The returned bool says whether the panel owned the key; the returned
// command is the host's (the panel itself has none).
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	switch key.Code {
	case tea.KeyEsc:
		if p.detailMode {
			p.detailMode = false
			p.detailSubagent = nil
			p.window = 0
			return true, nil
		}
		// The reference applies a prepared switch when the panel is
		// left — the switch forks the conversation at that point.
		if p.prepared {
			name, body := p.preparedName, p.preparedBody
			p.applySwitch()
			return true, func() tea.Msg {
				return MsgSwitchAgent{Name: name, Body: body}
			}
		}
		if p.seams.Close != nil {
			return true, p.seams.Close()
		}
		return true, func() tea.Msg { return MsgClose{} }
	case tea.KeyUp:
		if p.detailMode {
			p.scroll(-1)
			return true, nil
		}
		if p.cursor > 0 {
			p.cursor--
		}
		return true, nil
	case tea.KeyDown:
		if p.detailMode {
			p.scroll(1)
			return true, nil
		}
		if p.cursor < p.RowCount()-1 {
			p.cursor++
		}
		return true, nil
	case tea.KeyPgUp:
		if p.detailMode {
			p.scroll(-10)
			return true, nil
		}
		return true, nil
	case tea.KeyPgDown:
		if p.detailMode {
			p.scroll(10)
			return true, nil
		}
		return true, nil
	case tea.KeyEnter:
		p.prepare()
		return true, nil
	}
	// The three subagent keys.
	//
	// The reference's own legend advertises the kill in lower case
	// ("k Kill Active Subagent" — the live frame cycle 205 captured),
	// while its documentation writes all three keys in upper case
	// ("K", "A" and "D", official-docs/cli/commands/agents.md:99-115).
	// Both spellings are accepted, so the key works whichever the user
	// presses; the reference's own legend bytes are not changed.
	if letterKey(key, 'k') {
		sa := p.highlightedSubagent()
		p.killHighlighted()
		if sa != nil {
			return true, func() tea.Msg { return MsgKillSubagent{Subagent: sa} }
		}
		return true, nil
	}
	if letterKey(key, 'a') {
		sa := p.highlightedSubagent()
		appr, ok := p.pendingAuthorization(sa)
		p.answerHighlighted(true)
		if ok {
			return true, func() tea.Msg {
				return MsgAuthorizeSubagent{ApprovalID: appr.ID, Approved: true}
			}
		}
		return true, nil
	}
	if letterKey(key, 'd') {
		sa := p.highlightedSubagent()
		appr, ok := p.pendingAuthorization(sa)
		p.answerHighlighted(false)
		if ok {
			return true, func() tea.Msg {
				return MsgAuthorizeSubagent{ApprovalID: appr.ID, Approved: false}
			}
		}
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

// letterKey reports whether the key is the given letter, with or
// without the shift modifier.
//
// A terminal that reports the shift reports the *lower* case code and
// sets the shift modifier (an uppercase "K" arrives as code 'k' with
// the shift modifier), while a terminal that reports the shifted code
// reports 'K' itself — so both spellings are accepted.
//
// Ctrl and Alt are different keys: a Ctrl+K is not the kill.
func letterKey(key tea.Key, lower rune) bool {
	if key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModAlt) {
		return false
	}
	return key.Code == lower || key.Code == unicode.ToUpper(lower)
}

// killHighlighted kills the subagent under the cursor.
//
// The panel only *names* the subagent: the kill's policy belongs to
// the owner of the subagent — the manager that tracks it and holds its
// state — so the host answers whether the kill happened. A cursor on
// an agent row names no subagent, and the reference's own key does
// nothing when there is no active subagent (cycle 205's live frame).
func (p *Panel) killHighlighted() {
	sa := p.highlightedSubagent()
	if sa == nil || p.seams.Kill == nil {
		return
	}
	p.seams.Kill(sa)
}

// answerHighlighted answers the pending authorization of the subagent
// under the cursor.
//
// The reference's inline approval offers the two answers — "Yes, allow
// tool call" and "No, deny tool call" (【strings】) — and the panel
// answers with the key the user pressed. A subagent that waits for
// nothing has no answer to give, so the key does nothing.
func (p *Panel) answerHighlighted(approve bool) {
	sa := p.highlightedSubagent()
	if sa == nil {
		return
	}
	appr, ok := p.pendingAuthorization(sa)
	if !ok {
		return
	}
	if approve {
		if p.seams.Approve == nil {
			return
		}
		p.seams.Approve(appr.ID)
		return
	}
	if p.seams.Deny == nil {
		return
	}
	p.seams.Deny(appr.ID)
}

// prepare handles the Enter on an agent row or subagent row.
//
// The reference keeps the panel open and *prepares* the switch: the
// title gains the fork notice and the transcript carries the prepared
// message. The switch itself is applied when the panel is left.
//
// The Enter on a *subagent* row opens the reference's Subagent Detail
// View (official-docs/cli/commands/agents.md:85).
func (p *Panel) prepare() {
	if p.cursor >= p.agentRowCount() {
		// Enter on subagent row opens detail view
		sa := p.highlightedSubagent()
		if sa != nil {
			p.detailMode = true
			p.detailSubagent = sa
			p.window = 0
		}
		return
	}
	rows := p.agentRowCount()
	cursor := p.cursor
	if cursor < 0 || cursor >= rows {
		cursor = 0
	}
	name, body := "", ""
	if cursor > 0 {
		a := p.agents[cursor-1]
		name, body = a.Name, a.Body
	}
	if name == p.activeAgent() {
		if name == "" {
			p.respond("Already using default agent.")
		} else {
			p.respond("Already selected agent: " + name)
		}
		return
	}
	p.prepared = true
	p.preparedName = name
	p.preparedBody = body
	verb := "create a new conversation"
	if p.hasSession() {
		verb = "fork the current conversation"
	}
	if name == "" {
		p.respond("Prepared switch to default agent (will " + verb + " on exit).")
	} else {
		p.respond("Prepared selection: " + name + " (will " + verb + " on exit).")
	}
}
