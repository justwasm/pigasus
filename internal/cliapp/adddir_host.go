// adddir_host.go — the host's side of the /add-dir panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/adddirpanel: it owns the path
// buffer, the trusted-workspace list, the rows and the keys. The host
// supplies the three things the panel cannot answer itself — where
// the trusted list comes from, what to do with a submitted path and
// the way out of the panel — and nothing else.
//
// The panel is rebuilt every time it opens: the host's slashPanel
// struct is reset when a panel closes, so a stale buffer is never
// shown.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/ui/tuilib"

	tea "charm.land/bubbletea/v2"

	trust "github.com/justwasm/pigasus/internal/core/policy/trust"
	"github.com/justwasm/pigasus/internal/ui/views/adddirpanel"
)

// newAddDirPanel builds the /add-dir panel with the host's seams.
func (m *runMode) newAddDirPanel() *adddirpanel.Panel {
	return adddirpanel.New(adddirpanel.Seams{
		// The trusted list is the trust store's own reader — the same
		// one the on-startup trust gate uses.
		Trusted: trust.LoadTrustedWorkspaces,
		// The commit is the host's: the trust store, the transcript
		// block and the panel's close. The panel does not own the
		// transcript.
		Submit: func(path string) tea.Cmd {
			msg := tuilib.AddDirFeedback(path)
			m.slashRespond("/add-dir", msg)
			return m.closePanelSilent()
		},
		// Esc leaves the panel.
		Close: m.closePanel,
	})
}

// addDirPanel returns the /add-dir panel, building it if the host has
// not opened one yet.
func (m *runMode) addDirPanel() *adddirpanel.Panel {
	if m.panel.ad == nil {
		m.panel.ad = m.newAddDirPanel()
	}
	return m.panel.ad
}
