// credits_host.go — the host's side of the /credits panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/creditspanel: it owns the two
// actions, the cursor, the frame and the keys. The host supplies the
// one seam — the way out of the panel.
//
// # The actions
//
// The reference's two actions are inert in the clone: the clone has no
// credits endpoint to buy credits from or to read the activity from.
// The panel therefore swallows Enter and the host has nothing to do.

package cliapp

import (
	"os/exec"
	"runtime"

	"github.com/justwasm/pigasus/internal/ui/views/creditspanel"
)

// openBrowser opens url with the default system browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// newCreditsPanel builds the /credits panel with the host's seams.
func (m *runMode) newCreditsPanel() *creditspanel.Panel {
	return creditspanel.New(creditspanel.Seams{
		// Esc leaves the panel.
		Close: m.closePanel,
		OpenURL: func(url string) error {
			return openBrowser(url)
		},
	})
}

// creditsPanel returns the open /credits panel.
//
// The panel is built when the /credits panel opens — the host's
// slashPanel struct is reset whenever a panel closes — so the accessor
// is here so that a caller which finds the panel missing still gets one
// that answers.
func (m *runMode) creditsPanel() *creditspanel.Panel {
	if m.panel.cr == nil {
		m.panel.cr = m.newCreditsPanel()
	}
	return m.panel.cr
}
