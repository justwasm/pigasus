// provider_host.go — the host's side of the /provider panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/providerpanel: it owns the rows,
// the edit form, the login dialog, the keys and the writes. The host
// supplies the four things the panel cannot answer itself — the browser,
// the clipboard, the session's current model, and the way out of the
// panel — and nothing else.
//
// The panel is rebuilt every time it opens: the host's slashPanel struct
// is reset when a panel closes, and the rows are re-read from the
// configuration file on every open, so a stale copy is never shown.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/ui/views/providerpanel"
)

// newProviderPanel builds the /provider panel with the host's seams.
func (m *runMode) newProviderPanel() *providerpanel.Panel {
	return providerpanel.New(providerpanel.Seams{
		// The browser is the machine's, not the panel's: the panel
		// cannot import the CLI host, so it asks for it.
		OpenBrowser:    cli.OpenBrowser,
		ClipboardRead:  clipboardReadText,
		ClipboardWrite: clipboardWrite,
		// The panel marks the running model with "(current)".
		CurrentModel: m.panelCurrentModelLabel,
		// Esc closes the panel.
		Close: m.closePanel,
	})
}

// providerPanel returns the open /provider panel.
//
// The panel is built when the /provider panel opens — the host's
// slashPanel struct is reset whenever a panel closes — and the rows are
// re-read at that point, so the panel always shows what is on disk. The
// accessor is here so that a caller which finds the panel missing still
// gets one that answers.
func (m *runMode) providerPanel() *providerpanel.Panel {
	if m.panel.prov == nil {
		m.panel.prov = m.newProviderPanel()
		m.panel.prov.Reload()
	}
	return m.panel.prov
}
