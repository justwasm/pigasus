// diff_host.go — the host's side of the /diff panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/diffpanel: it owns the three
// tabs, the cursor, the visible window, the inline diff subview, its
// row cursor, the per-hunk fold state, the keys and the rows. The host
// supplies the four things the panel cannot answer itself — the
// workspace, the terminal's size, the conversation's turn count and
// the way out of the panel — and nothing else.
//
// The panel is rebuilt every time it opens: the host's slashPanel
// struct is reset when a panel closes, so a stale cursor, a stale tab
// or a stale fold state is never shown.
//
// The git rules — the working-copy change list, the diff body, the tab
// row, the row renderers and the hunk primitives — live in
// internal/ui/views/diff; the panel drives them, it does not
// reimplement them.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/ui/views/diffpanel"
)

// newDiffPanel builds the /diff panel with the host's seams.
func (m *runMode) newDiffPanel() *diffpanel.Panel {
	return diffpanel.New(diffpanel.Seams{
		// The working copy is read from the session's directory.
		Cwd: func() string {
			return m.cwd
		},
		// The terminal's height sizes the visible window and its width
		// lays the inline diff's rows out.
		Size: func() (int, int) {
			return m.width, m.height
		},
		// The Per Turn tab renders one row per user turn, so the count
		// is the conversation's.
		Turns: m.diffTurnCount,
		// VimEnabled reports whether vim modal editing is active in the CLI.
		VimEnabled: m.vimEnabled,
		// Esc leaves the panel.
		Close: m.closePanel,
	})
}

// diffPanel returns the /diff panel, building it if the host has not
// opened one yet.
func (m *runMode) diffPanel() *diffpanel.Panel {
	if m.panel.df == nil {
		m.panel.df = m.newDiffPanel()
	}
	return m.panel.df
}

// diffTurnCount returns the number of user-role turns in the session.
//
// Cycle 226 follows the cycle-194 convention (the last user message
// walks `Role == "user"`), so the count is the count of user messages.
func (m *runMode) diffTurnCount() int {
	n := 0
	for _, msg := range m.session {
		if msg.Role == "user" {
			n++
		}
	}
	return n
}
