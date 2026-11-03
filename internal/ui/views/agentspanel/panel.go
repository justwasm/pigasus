// Package agentspanel holds the /agents panel: the markdown-agent
// list, the running-subagent section and the agent switch the panel
// prepares.
//
// # What it is
//
// The reference (`agy`) offers an agent picker: the workspace's
// markdown agents beside the built-in default, the subagents that are
// running right now, and the switch that installs the chosen agent.
//
// The reference keeps the panel open after Enter — it *prepares* the
// switch and applies it when the panel is left — and the switch forks
// the conversation at that point.
//
// The panel owns three keys of its own beyond the navigation: the kill
// ("k Kill Active Subagent" — the legend the reference's own screen
// carries), and the two answers to a pending authorization ("A" to
// allow, "D" to deny). The cursor reaches the subagent rows, so all
// three act on the subagent under it.
//
// # Where it lives
//
// The panel itself is here: the rows, the cursor, the keys, the
// prepared switch and the catalogue it lists. The host keeps only the
// seams — the facts the panel cannot answer itself.
//
// The panel is rebuilt every time it opens, so a stale copy of the
// cursor or of the prepared switch is never shown.
//
// # Its own vocabulary
//
// The panel lists the catalogue's own agent value — the one
// internal/features/agents owns. There is no second type here: the
// catalogue used to live in the engine host, so the panel had to carry
// its own copy of it, and the host mapped one into the other. The
// catalogue is a feature now, and the ui tier may name a feature.
package agentspanel

import (
	"sort"

	tea "charm.land/bubbletea/v2"

	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	lifecycle "github.com/justwasm/pigasus/internal/core/lifecycle"
	"github.com/justwasm/pigasus/internal/features/agents"
)

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves as
// if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// ActiveAgent returns the name of the agent the session runs right
	// now — the empty string for the built-in default.
	ActiveAgent func() string
	// Subagents returns the subagents that are running right now.
	//
	// The panel renders each one's name and status line, so the
	// subagent's own status wording is the app tier's (the manager
	// lives in internal/app/subagent/core).
	Subagents func() []*subagent.Subagent
	// HasSession reports whether the conversation has turns.
	//
	// The reference's prepared-switch message says the switch will
	// "fork the current conversation" when there is one and "create a
	// new conversation" when there is not.
	HasSession func() bool
	// Respond writes a line into the transcript.
	Respond func(msg string)
	// Switch installs the prepared agent switch and leaves the panel.
	//
	// The reference applies the switch when the panel is left, and the
	// switch forks the conversation — so the host owns both halves.
	Switch func(name, body string)
	// Close leaves the panel without switching.
	Close func() tea.Cmd
	// Kill terminates the highlighted subagent.
	//
	// The kill's policy belongs to the owner of the subagent — the
	// manager that tracks it — so the panel only names the subagent
	// and the host answers whether it was killed. A subagent that is
	// not running is left alone: the reference's key targets the
	// active (running) subagent processes only
	// (official-docs/cli/commands/agents.md:115).
	Kill func(sa *subagent.Subagent) bool
	// PendingAuthorization returns the protected operation the named
	// subagent waits on, if any.
	//
	// The payload is the reference's own recovered queue entry
	// (internal/core/lifecycle.PendingApproval): the id the answer is
	// matched by and what the subagent wants to do.
	PendingAuthorization func(subagentID string) (lifecycle.PendingApproval, bool)
	// Approve answers a pending authorization with "yes".
	Approve func(approvalID string) bool
	// Deny answers a pending authorization with "no".
	Deny func(approvalID string) bool
}

// DetailMode reports whether the panel is in the subagent detail view.
func (p *Panel) DetailMode() bool {
	if p == nil {
		return false
	}
	return p.detailMode
}

// DetailSubagent returns the subagent currently inspected in the detail view.
func (p *Panel) DetailSubagent() *subagent.Subagent {
	if p == nil {
		return nil
	}
	return p.detailSubagent
}

// Panel is the /agents panel.
//
// The panel owns the whole surface: the agent list, the running-agent
// section, the cursor, the keys and the prepared switch. Nothing of it
// is left in the host but the seams.
type Panel struct {
	seams Seams

	// agents is the catalogue of markdown agents the panel lists.
	//
	// The catalogue is handed over when the panel opens: the host
	// discovers the markdown agents (that is internal/features/agents's
	// fact) and hands the panel the values it found.
	agents []agents.Agent

	// cursor is the selected row: 0 is the built-in default, 1..n are
	// the custom agents.
	cursor int

	// prepared says the panel holds a prepared switch.
	prepared bool

	// preparedName is the prepared switch's agent name — the empty
	// string for the built-in default.
	preparedName string

	// preparedBody is the prepared switch's agent body.
	preparedBody string

	// detailMode indicates whether the full-screen Subagent Detail View is open.
	detailMode bool

	// detailSubagent is the subagent inspected in detailMode.
	detailSubagent *subagent.Subagent
}

// New builds the /agents panel.
//
// The catalogue is the markdown agents the panel lists: the host
// discovers them when the panel opens and hands them over here.
func New(seams Seams, catalogue []agents.Agent) *Panel {
	return &Panel{seams: seams, agents: catalogue}
}

// ---- the panel's state, read from outside --------------------------------

// Cursor is the selected row's index: 0 is the built-in default.
func (p *Panel) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// SetCursor puts the cursor on the row at the given index.
//
// The index is clamped into the row list, so a stale index cannot
// strand the cursor on a row that is not on screen.
func (p *Panel) SetCursor(idx int) {
	if p == nil {
		return
	}
	n := p.RowCount()
	if idx < 0 {
		idx = 0
	}
	if n > 0 && idx >= n {
		idx = n - 1
	}
	p.cursor = idx
}

// Prepared reports whether the panel holds a prepared switch.
//
// The reference keeps the panel open after Enter and applies the switch
// when the panel is left, so the prepared switch is the panel's own
// state.
func (p *Panel) Prepared() bool {
	if p == nil {
		return false
	}
	return p.prepared
}

// PreparedName is the prepared switch's agent name — the empty string
// for the built-in default.
func (p *Panel) PreparedName() string {
	if p == nil {
		return ""
	}
	return p.preparedName
}

// RowCount is the number of selectable rows: the built-in default plus
// the catalogue's agents plus the subagent rows.
//
// The subagent rows are part of the row list because the reference's
// cursor moves over them: its keybinding table says "Move the cursor
// between headers, subagents, and available agents"
// (official-docs/cli/commands/agents.md:112), and the kill and the
// inline approvals act on the subagent row under the cursor.
func (p *Panel) RowCount() int {
	if p == nil {
		return 1
	}
	return p.agentRowCount() + len(p.subagents())
}

// agentRowCount is the number of agent rows: the built-in default plus
// the catalogue's agents.
//
// The agent rows come first in the panel's frame, so their indices are
// the cursor's first ones.
func (p *Panel) agentRowCount() int {
	if p == nil {
		return 1
	}
	return 1 + len(p.agents)
}

// ---- the panel's seams ----------------------------------------------------

// activeAgent is the name of the agent the panel shows as active: the
// prepared one when the panel holds a prepared switch, the session's
// own otherwise.
func (p *Panel) activeAgent() string {
	if p.prepared {
		return p.preparedName
	}
	if p.seams.ActiveAgent == nil {
		return ""
	}
	return p.seams.ActiveAgent()
}

// runningSubagents is the subagents that are running right now.
func (p *Panel) runningSubagents() []*subagent.Subagent {
	if p == nil || p.seams.Subagents == nil {
		return nil
	}
	return p.seams.Subagents()
}

// subagents is the panel's subagent rows, in a stable order.
//
// The manager's own list comes out of a map, so its order is arbitrary
// from one frame to the next — a cursor over an arbitrary order would
// name a different subagent every time the panel redraws. The rows are
// therefore sorted by id, and an id carries the subagent's creation
// time ("sub-<unix-nano>-<counter>"), so the order is the order the
// subagents were started in.
func (p *Panel) subagents() []*subagent.Subagent {
	subs := p.runningSubagents()
	out := make([]*subagent.Subagent, 0, len(subs))
	for _, sa := range subs {
		if sa == nil {
			continue
		}
		out = append(out, sa)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// highlightedSubagent is the subagent under the cursor, if any.
//
// The panel's rows are the agent rows first and the subagent rows
// after them, so a cursor past the agent rows names a subagent.
func (p *Panel) highlightedSubagent() *subagent.Subagent {
	if p == nil {
		return nil
	}
	subs := p.subagents()
	idx := p.cursor - p.agentRowCount()
	if idx < 0 || idx >= len(subs) {
		return nil
	}
	return subs[idx]
}

// pendingAuthorization is the protected operation the given subagent
// waits on, if any.
func (p *Panel) pendingAuthorization(sa *subagent.Subagent) (lifecycle.PendingApproval, bool) {
	if p == nil || sa == nil || p.seams.PendingAuthorization == nil {
		return lifecycle.PendingApproval{}, false
	}
	return p.seams.PendingAuthorization(sa.ID)
}

// hasSession reports whether the conversation has turns.
func (p *Panel) hasSession() bool {
	if p == nil || p.seams.HasSession == nil {
		return false
	}
	return p.seams.HasSession()
}

func (p *Panel) respond(msg string) {
	if p == nil || p.seams.Respond == nil {
		return
	}
	p.seams.Respond(msg)
}

// applySwitch installs the prepared switch and leaves the panel.
func (p *Panel) applySwitch() {
	if p == nil || p.seams.Switch == nil {
		return
	}
	p.seams.Switch(p.preparedName, p.preparedBody)
}

func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}
