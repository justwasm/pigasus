package cliapp

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/internal/tuiviewer"
	"github.com/justwasm/pigasus/palette"
)

// searchCursorBlock is the `Search:` input cursor REF draws as one
// reverse-video cell (`\x1b[7m\x1b[37m \x1b[0m`). Both /model and
// /config reuse it.
const searchCursorBlock = "\x1b[7m\x1b[37m \x1b[0m"

// searchUnderline is the 20-cell `─` row REF draws under the Search:
// input, in the selected scheme's separator colour.
var searchUnderline = palette.SeparatorOpen + "────────────────────\x1b[39m"

func (m *runMode) handlePanelEarlyMiscRoutes(key tea.Key) (bool, tea.Cmd, bool) {
	switch {
	case m.panel.kind == panelAddDir:
		h, c := m.handleAddDirKey(key)
		return h, c, true
	case m.panel.kind == panelRename:
		h, c := m.handleRenamePanelKey(key)
		return h, c, true
	case m.panel.kind.isViewer():
		h, c := m.handleViewerPanelKey(key)
		return h, c, true
	case m.panel.kind == panelSkills && m.panel.detail:
		h, c := m.handleSkillsDetailKey(key)
		return h, c, true
	case m.panel.kind == panelFeedback && m.panel.stage > 0:
		h, c := m.handleFeedbackTextKey(key)
		return h, c, true
	case m.panel.kind == panelDiff:
		h, c := m.handleDiffPanelKey(key)
		return h, c, true
	default:
		return false, nil, false
	}
}

func (m *runMode) handlePanelEarlyRoutes(key tea.Key) (bool, tea.Cmd, bool) {
	// Cycle 315: the /tasks detail sub-views own every key while they are
	// open. They are routed before the /tasks list handling because the
	// panel kind is no longer panelTasks — the list handler must not see
	// the keys.
	if detailPanelKind(m.panel.kind) {
		h, c := m.handleTaskDetailKey(key)
		return h, c, true
	}
	if m.panel.kind == panelCodeSearch && len(m.panel.cs.hits) > 0 {
		if handled, _, ok := m.codesearchPanelKey(key); ok {
			return handled, nil, true
		}
	}
	if m.panel.kind == panelTasks {
		if handled, cmd := m.tasksPanelKey(key); handled {
			return true, cmd, true
		}
		h, c := m.listPanelOwningKey(key)
		return h, c, true
	}
	if listPanelKind(m.panel.kind) {
		h, c := m.listPanelOwningKey(key)
		return h, c, true
	}
	switch m.panel.kind {
	case panelPermissions:
		h, c := m.handlePermissionsKey(key)
		return h, c, true
	case panelHooks:
		h, c := m.handleHooksKey(key)
		return h, c, true
	case panelAgents:
		h, c := m.handleAgentsKey(key)
		return h, c, true
	case panelProvider:
		// Cycle provider-model-editor: the /provider panel owns every
		// key. Its form turns the arrow keys into field navigation, so
		// the generic list navigation must not see them.
		h, c := m.handleProviderPanelKey(key)
		return h, c, true
	case panelAccounts:
		// Cycle multi-account-switch: the /accounts panel owns every
		// key. Its own keys (a/d) would otherwise be read as text.
		h, c := m.handleAccountsPanelKey(key)
		return h, c, true
	}
	return m.handlePanelEarlyMiscRoutes(key)
}

func (m *runMode) handleNavAndEnterKey(key tea.Key, n int) (bool, tea.Cmd, bool) {
	if m.panel.kind == panelConfig {
		if handled, cmd := m.handleConfigKey(key, n); handled {
			return true, cmd, true
		}
	}
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavUp:
		if m.panel.kind == panelSkills {
			m.moveSkillsCursor(false)
			return true, nil, true
		}
		if m.panel.cursor > 0 {
			m.panel.cursor--
		} else if m.panel.kind.wraps() {
			m.panel.cursor = n - 1
		}
		return true, nil, true
	case tuilib.NavDown:
		if m.panel.kind == panelSkills {
			m.moveSkillsCursor(true)
			return true, nil, true
		}
		if m.panel.cursor < n-1 {
			m.panel.cursor++
		} else if m.panel.kind.wraps() {
			m.panel.cursor = 0
		}
		return true, nil, true
	case tuilib.NavPageUp:
		m.panelPageCursor(-10, n)
		return true, nil, true
	case tuilib.NavPageDown:
		m.panelPageCursor(+10, n)
		return true, nil, true
	case tuilib.NavTop:
		m.panel.cursor = 0
		return true, nil, true
	case tuilib.NavBottom:
		if n > 0 {
			m.panel.cursor = n - 1
		}
		return true, nil, true
	case tuilib.NavCancel:
		return true, m.closePanel(), true
	}
	return m.handleSpecialPanelEnterKey(key)
}

func (m *runMode) handleSpecialPanelEnterKey(key tea.Key) (bool, tea.Cmd, bool) {
	if m.panel.kind == panelSkills {
		if key.Code == tea.KeyEnter {
			m.openSkillDetail()
			return true, nil, true
		}
	}
	if m.panel.kind == panelFeedback {
		if key.Code == tea.KeyEnter {
			m.panel.stage = 1
			return true, nil, true
		}
		if key.Mod == 0 && len(key.Text) == 1 && key.Text[0] >= '1' && key.Text[0] <= '6' {
			m.panel.cursor = int(key.Text[0] - '1')
			m.panel.stage = 1
			return true, nil, true
		}
	}
	if m.panel.kind == panelRewind && key.Code == tea.KeyEnter {
		m.panelRewindPickStep()
		return true, m.closePanel(), true
	}
	return false, nil, false
}

func (m *runMode) handlePanelKey(key tea.Key) (bool, tea.Cmd) {
	if m.panel.kind == panelNone {
		return false, nil
	}
	if handled, cmd, ok := m.handlePanelEarlyRoutes(key); ok {
		return handled, cmd
	}
	n := m.panelRowCount(m.panel.kind)
	if handled, cmd, ok := m.handleEffortKey(key); ok {
		return handled, cmd
	}
	if handled, cmd, ok := m.handleModelKey(key, n); ok {
		return handled, cmd
	}
	if handled, cmd, ok := m.handlePluginKey(key); ok {
		return handled, cmd
	}
	if handled, cmd, ok := m.handleNavAndEnterKey(key, n); ok {
		return handled, cmd
	}
	switch key.Code {
	case tea.KeyEsc:
		return true, m.closePanel()
	case tea.KeyEnter, tea.KeyTab, tea.KeyBackspace:
		return true, nil
	}
	if key.Mod == 0 && key.Text != "" {
		return true, nil
	}
	return false, nil
}

func (m *runMode) panelPageCursor(n, count int) {
	if count <= 0 {
		return
	}
	c := m.panel.cursor + n
	if c < 0 {
		c = 0
	} else if c >= count {
		c = count - 1
	}
	m.panel.cursor = c
}

func viewerMaxOffset(spec viewerSpec) int {
	return tuiviewer.MaxOffset(toViewerSpec(spec))
}

func (m *runMode) viewerScrollBy(spec viewerSpec, delta int) {
	m.panel.window += delta
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	if max := viewerMaxOffset(spec); m.panel.window > max {
		m.panel.window = max
	}
}

func (m *runMode) handleViewerPanelKey(key tea.Key) (bool, tea.Cmd) {
	return m.handleViewerPanelKeyVP(key)
}

// listPanelKind reports whether k is one of the four key-owning list
// panels: /tasks, /artifact, /codesearch, /context.
func listPanelKind(k panelKind) bool {
	switch k {
	case panelTasks, panelArtifact, panelCodeSearch, panelContext:
		return true
	}
	return false
}

// detailPanelKind reports whether k is one of the two /tasks detail
// sub-views (cycle 315): the task detail and the subagent detail.
//
// They are not list panels — they have no rows to pick from — but they
// own every key while they are open, so the global key handlers must
// not see them.
func detailPanelKind(k panelKind) bool {
	return k == panelTaskDetail || k == panelSubagentDetail
}

// canonicalPanelCommand resolves a slash-command alias to the name the
// panel table is keyed by.
func canonicalPanelCommand(cmd string) string {
	if _, ok := panelForCommand(cmd); ok {
		return cmd
	}
	for _, c := range tuilib.DefaultSlashCommands {
		for _, a := range c.Aliases {
			if a == cmd {
				return strings.TrimPrefix(c.Name, "/")
			}
		}
	}
	return cmd
}

// listPanelOwningKey is the key handler for the four list panels.
func (m *runMode) listPanelOwningKey(key tea.Key) (bool, tea.Cmd) {
	if key.Code == tea.KeyEsc {
		return true, m.closePanel()
	}
	if key.Code == 'c' && key.Mod.Contains(tea.ModCtrl) {
		return false, nil
	}
	return true, nil
}
