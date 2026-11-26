package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/tui/panels"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/diffpanel"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/ui/views/renamepanel"
)

// Rename
func (m *RootModel) ConversationName() string { return m.session.conversationName }
func (m *RootModel) SubmitRename(name string) tea.Cmd {
	next, msg := tuilib.RenameFeedback(name, m.session.conversationName)
	if msg != "" {
		m.slashRespond("/rename", msg)
	}
	if next != "" {
		m.session.conversationName = next
	}
	return m.closePanelSilent()
}
func (m *RootModel) newRenamePanel() *renamepanel.Panel { return panels.NewRenamePanel(m) }
func (m *RootModel) renamePanel() *renamepanel.Panel {
	if m.overlays.panel.rn == nil {
		m.overlays.panel.rn = m.newRenamePanel()
	}
	return m.overlays.panel.rn
}

func (m *RootModel) SessionTierFor(email string) (string, bool) {
	if m == nil || !m.session.tierFetched {
		return "", false
	}
	if !strings.EqualFold(m.session.tierAccount, email) {
		return "", false
	}
	return m.session.tier, true
}

func (m *RootModel) startAccountLogin() tea.Cmd {
	lm := loginui.New()
	lm.Width = m.viewport.width
	lm.Height = m.viewport.height
	m.auth.login = lm
	return lm.Init()
}

func (m *RootModel) ApplyActiveAccount(email string) {
	m.session.email = email
	// The account was switched inside the TUI, so the cached read of the
	// shared credential now names the previous account.
	m.forgetCredential()
	m.forgetSessionTier()
	if m.turn.backendFor != nil {
		if b := m.turn.backendFor(m.session.model); b != nil {
			m.turn.backend = b
		}
	}
}

func (m *RootModel) SignOutActiveAccount() {
	m.session.email = ""
}

func (m *RootModel) forgetSessionTier() {
	m.session.tier = ""
	m.session.tierAccount = ""
	m.session.tierFetched = false
}

// DiffTurnCount
func (m *RootModel) diffTurnCount() int {
	return m.DiffTurnCount()
}
func (m *RootModel) SessionCwd() string { return m.session.cwd }
func (m *RootModel) DiffTurnCount() int { return m.diffTurnCount() }
func (m *RootModel) diffPanel() *diffpanel.Panel {
	return m.diff.Panel(m)
}

func (m *RootModel) effortTiers() []string {
	return catalog.EffortTiersFor(m.effortModel())
}

func (m *RootModel) effortModel() string {
	if label := strings.TrimSpace(m.session.model); label != "" && catalog.ModelAvailable(label) {
		return label
	}
	if effective, _ := catalog.ResolveEffectiveModel(""); strings.TrimSpace(effective) != "" {
		return effective
	}
	return catalog.DefaultModel
}

func (m *RootModel) EffortTiers() []string {
	return m.effortTiers()
}

func (m *RootModel) CurrentEffortName() string {
	return m.currentEffortName()
}
func (m *RootModel) ApplyEffortSelection(level int) tea.Cmd { return m.applyEffortSelection(level) }
func (m *RootModel) newEffortPanel() *effortpanel.Panel     { return panels.NewEffortPanel(m) }
func (m *RootModel) effortPanel() *effortpanel.Panel {
	if m.overlays.panel.ef == nil {
		m.overlays.panel.ef = m.newEffortPanel()
	}
	return m.overlays.panel.ef
}
