// rename_host.go — the host's side of the /rename panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/renamepanel: it owns the
// single-line input, the prompt, the placeholder, the rows and the
// keys. The host supplies the three things the panel cannot answer
// itself — the current conversation name, what to do with a submitted
// name and the way out of the panel — and nothing else.
//
// The panel is rebuilt every time it opens: the host's slashPanel
// struct is reset when a panel closes, so a stale seed or a stale
// cursor is never shown.
//
// # The two ways in
//
// The panel opens from three places, and all three build it the same
// way:
//
//   - the bare `/rename` command (the slash router opens it through
//     openSlashPanel);
//   - the F2 key;
//   - the /resume picker, which renames the conversation the user
//     picked.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/tuilib"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/renamepanel"
)

// newRenamePanel builds the /rename panel with the host's seams.
func (m *runMode) newRenamePanel() *renamepanel.Panel {
	return renamepanel.New(renamepanel.Seams{
		// The input opens with the current conversation name in it.
		Seed: func() string {
			return m.conversationName
		},
		// The commit is the host's: the conversation name, the
		// transcript block and the panel's close. The panel does not
		// own the transcript.
		Submit: func(name string) tea.Cmd {
			next, msg := tuilib.RenameFeedback(name, m.conversationName)
			if msg != "" {
				m.slashRespond("/rename", msg)
			}
			if next != "" {
				m.conversationName = next
			}
			return m.closePanelSilent()
		},
		// Esc leaves the panel.
		Close: m.closePanel,
	})
}

// renamePanel returns the /rename panel, building it if the host has
// not opened one yet.
func (m *runMode) renamePanel() *renamepanel.Panel {
	if m.panel.rn == nil {
		m.panel.rn = m.newRenamePanel()
	}
	return m.panel.rn
}

// openRenamePanel opens the /rename panel.
//
// The panel is built here and armed with the textinput's blink command,
// so the first keystroke edits the field rather than opening the
// prompt.
func (m *runMode) openRenamePanel() {
	m.panel.kind = panelregistry.Rename
	m.panel.cmd = "/rename"
	m.panel.rn = m.newRenamePanel()
	m.armRenameFocus()
}

// armRenameFocus sends the input's cursor-blink command to the running
// program.
//
// The textinput's Focus() returns the command that arms the blink;
// sending it through the bubbletea program is the same flow bubbles'
// own textarea uses. m.prog is nil in unit tests, so the blink arm is
// best-effort.
func (m *runMode) armRenameFocus() {
	if m.panel.rn == nil {
		return
	}
	cmd := m.panel.rn.Focus()
	if cmd != nil && m.prog != nil {
		m.prog.Send(cmd) //nolint:errcheck // best-effort blink arm
	}
}
