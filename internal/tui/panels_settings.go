package tui

import (
	"flag"
	"os"
	"os/exec"
	"runtime"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/cli"
	plugins "github.com/justwasm/pigasus/internal/features/plugins/core"
	"github.com/justwasm/pigasus/internal/tui/panels"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/adddirpanel"
	"github.com/justwasm/pigasus/internal/ui/views/creditspanel"
	"github.com/justwasm/pigasus/internal/ui/views/feedbackpanel"
	"github.com/justwasm/pigasus/internal/ui/views/hookspanel"
	"github.com/justwasm/pigasus/internal/ui/views/permissionspanel"
	"github.com/justwasm/pigasus/internal/ui/views/pluginsui"
	"github.com/justwasm/pigasus/internal/ui/views/providerpanel"
	"github.com/justwasm/pigasus/internal/ui/views/skillsui"
)

// Permissions
func (m *RootModel) newPermissionsPanel() *permissionspanel.Panel {
	return panels.NewPermissionsPanel(m)
}
func (m *RootModel) permissionsPanel() *permissionspanel.Panel {
	if m.overlays.panel.perm == nil {
		m.overlays.panel.perm = m.newPermissionsPanel()
	}
	return m.overlays.panel.perm
}

// Hooks
func (m *RootModel) newHooksPanel() *hookspanel.Panel { return panels.NewHooksPanel(m) }
func (m *RootModel) hooksPanel() *hookspanel.Panel {
	if m.overlays.panel.hk == nil {
		m.overlays.panel.hk = m.newHooksPanel()
	}
	return m.overlays.panel.hk
}

// Plugins
var pluginWatcherPtr = &plugins.WatcherPtr

func (m *RootModel) newPluginPanel() *pluginsui.Panel { return panels.NewPluginPanel(m) }
func (m *RootModel) pluginPanel() *pluginsui.Panel {
	if m.overlays.panel.plg == nil {
		m.overlays.panel.plg = m.newPluginPanel()
	}
	return m.overlays.panel.plg
}

// openBrowser opens url with the default system browser.
func openBrowser(url string) error {
	if flag.Lookup("test.v") != nil {
		return nil
	}
	if v := os.Getenv("OPENAGY_NO_BROWSER"); v != "" {
		switch v {
		case "1", "true", "TRUE", "True", "yes", "YES":
			return nil
		}
	}
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

// Credits
func (m *RootModel) OpenURL(url string) error             { return openBrowser(url) }
func (m *RootModel) newCreditsPanel() *creditspanel.Panel { return panels.NewCreditsPanel(m) }
func (m *RootModel) creditsPanel() *creditspanel.Panel {
	if m.overlays.panel.cr == nil {
		m.overlays.panel.cr = m.newCreditsPanel()
	}
	return m.overlays.panel.cr
}

// AddDir
func (m *RootModel) ClosePanelSilent() tea.Cmd { return m.closePanelSilent() }
func (m *RootModel) SubmitAddDir(path string) tea.Cmd {
	msg := tuilib.AddDirFeedback(path)
	m.slashRespond("/add-dir", msg)
	return m.closePanelSilent()
}
func (m *RootModel) newAddDirPanel() *adddirpanel.Panel { return panels.NewAddDirPanel(m) }
func (m *RootModel) addDirPanel() *adddirpanel.Panel {
	if m.overlays.panel.ad == nil {
		m.overlays.panel.ad = m.newAddDirPanel()
	}
	return m.overlays.panel.ad
}

// Feedback
func (m *RootModel) SubmitFeedback() {
	m.submitFeedback()
}

func (m *RootModel) submitFeedback() {
	m.overlays.panel = slashPanel{}
	m.slashRespond("/feedback", "Feedback recorded (openagy has no upload endpoint; saved locally only)")
}
func (m *RootModel) newFeedbackPanel() *feedbackpanel.Panel { return panels.NewFeedbackPanel(m) }
func (m *RootModel) feedbackPanel() *feedbackpanel.Panel {
	if m.overlays.panel.fb == nil {
		m.overlays.panel.fb = m.newFeedbackPanel()
	}
	return m.overlays.panel.fb
}

// Skills
func (m *RootModel) SkillsCatalog() []skillsui.SkillInfo { return slashSkillCatalog() }
func (m *RootModel) TerminalOSC8() bool                  { return m.viewport.hyperlinkCaps.OSC8 }
func (m *RootModel) newSkillsPanel() *skillsui.Panel     { return panels.NewSkillsPanel(m) }
func (m *RootModel) skillsPanel() *skillsui.Panel {
	if m.overlays.panel.sk == nil {
		m.overlays.panel.sk = m.newSkillsPanel()
	}
	return m.overlays.panel.sk
}

// Provider
func (m *RootModel) OpenBrowser(url string) error           { return cli.OpenBrowser(url) }
func (m *RootModel) ClipboardRead() string                  { return clipboardReadText() }
func (m *RootModel) ClipboardWrite(text string) error       { return clipboardWrite(text) }
func (m *RootModel) CurrentModelLabel() string              { return m.panelCurrentModelLabel() }
func (m *RootModel) newProviderPanel() *providerpanel.Panel { return panels.NewProviderPanel(m) }
func (m *RootModel) providerPanel() *providerpanel.Panel {
	if m.overlays.panel.prov == nil {
		m.overlays.panel.prov = m.newProviderPanel()
		m.overlays.panel.prov.Reload()
	}
	return m.overlays.panel.prov
}
