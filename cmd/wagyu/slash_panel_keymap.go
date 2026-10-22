package main

import (
	tea "charm.land/bubbletea/v2"
)

// searchCursorBlock is the `Search:` input cursor REF draws as one
// reverse-video cell (`\x1b[7m\x1b[37m \x1b[0m`). Both /model and
// /config reuse it.
const searchCursorBlock = "\x1b[7m\x1b[37m \x1b[0m"

// searchUnderline is the 20-cell `─` row REF draws under the Search:
// input, in separator grey (256-colour 253).
const searchUnderline = "\x1b[38;2;238;232;213m────────────────────\x1b[39m"

func (m *runMode) handlePanelEarlyRoutes(key tea.Key) (bool, tea.Cmd, bool) {
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
	}
	if m.panel.kind == panelAddDir {
		h, c := m.handleAddDirKey(key)
		return h, c, true
	}
	if m.panel.kind == panelRename {
		h, c := m.handleRenamePanelKey(key)
		return h, c, true
	}
	if m.panel.kind.isViewer() {
		h, c := m.handleViewerPanelKey(key)
		return h, c, true
	}
	if m.panel.kind == panelFeedback && m.panel.stage > 0 {
		h, c := m.handleFeedbackTextKey(key)
		return h, c, true
	}
	if m.panel.kind == panelDiff {
		h, c := m.handleDiffPanelKey(key)
		return h, c, true
	}
	return false, nil, false
}

func (m *runMode) handleEffortKey(key tea.Key) (bool, tea.Cmd, bool) {
	if m.panel.kind != panelEffort {
		return false, nil, false
	}
	switch key.Code {
	case tea.KeyLeft:
		if m.panel.cursor > effortLow {
			m.panel.cursor--
		}
		return true, nil, true
	case tea.KeyRight:
		if m.panel.cursor < effortHigh {
			m.panel.cursor++
		}
		return true, nil, true
	case tea.KeyEnter:
		return true, m.applyEffortSelection(m.panel.cursor), true
	case tea.KeyUp, tea.KeyDown:
		return true, nil, true
	}
	return false, nil, false
}

func (m *runMode) handleModelKey(key tea.Key, n int) (bool, tea.Cmd, bool) {
	if m.panel.kind != panelModel {
		return false, nil, false
	}
	switch key.Code {
	case tea.KeyLeft:
		m.cycleModelEffort(-1)
		return true, nil, true
	case tea.KeyRight:
		m.cycleModelEffort(+1)
		return true, nil, true
	case tea.KeyEnter:
		m.selectModelEffort()
		return true, m.closePanel(), true
	case tea.KeyUp:
		if m.panel.cursor > 0 {
			m.panel.cursor--
			m.panel.effortCursor = effortCursorForLabel(m.panelCurrentModelLabel())
		}
		return true, nil, true
	case tea.KeyDown:
		if m.panel.cursor < n-1 {
			m.panel.cursor++
			m.panel.effortCursor = effortCursorForLabel(m.panelCurrentModelLabel())
		}
		return true, nil, true
	}
	return false, nil, false
}

func (m *runMode) handlePluginKey(key tea.Key) (bool, tea.Cmd, bool) {
	if m.panel.kind != panelPlugin {
		return false, nil, false
	}
	if m.pluginLocalInstallActive() {
		handled := m.handlePluginLocalInstallKey(key)
		return true, pluginCmdForFocus(handled, m), true
	}
	if m.panel.pluginSearch {
		handled := m.handlePluginSearchKey(key)
		return true, pluginCmdForFocus(handled, m), true
	}
	switch key.Code {
	case tea.KeyTab:
		if m.panel.pluginTab == panelInstalled {
			m.panel.pluginTab = panelDiscover
		} else {
			m.panel.pluginTab = panelInstalled
		}
		m.panel.cursor = 0
		return true, nil, true
	case tea.KeyUp:
		if m.panel.cursor > 0 {
			m.panel.cursor--
		}
		return true, nil, true
	case tea.KeyDown:
		if m.panel.cursor < m.pluginRowCount()-1 {
			m.panel.cursor++
		}
		return true, nil, true
	}
	if m.panel.pluginTab == panelInstalled {
		return m.handlePluginInstalledKey(key)
	}
	return m.handlePluginDiscoverKey(key)
}

func (m *runMode) handlePluginInstalledKey(key tea.Key) (bool, tea.Cmd, bool) {
	switch {
	case key.Code == tea.KeySpace:
		m.togglePluginEnabled()
		return true, nil, true
	case key.Code == tea.KeyEnter:
		m.togglePluginExpanded()
		return true, nil, true
	case key.Code == '/':
		m.panel.pluginEnabledOnly = !m.panel.pluginEnabledOnly
		m.panel.cursor = 0
		return true, nil, true
	case key.Code == 's' && key.Mod.Contains(tea.ModCtrl):
		notice, _ := m.uninstallHighlightedPlugin()
		if notice != "" {
			m.panel.pluginLocalError = notice
		}
		return true, nil, true
	}
	return false, nil, false
}

func (m *runMode) handlePluginDiscoverKey(key tea.Key) (bool, tea.Cmd, bool) {
	switch {
	case key.Code == tea.KeyLeft:
		m.panel.cursor = 0
		return true, nil, true
	case key.Code == tea.KeyRight:
		if n := m.pluginRowCount(); n > 0 {
			m.panel.cursor = n - 1
		}
		return true, nil, true
	case key.Code == tea.KeySpace:
		m.enterPluginSearch()
		return true, pluginBlink(), true
	case key.Code == tea.KeyEnter:
		if !m.panel.pluginSearch && m.panel.cursor == 0 {
			m.openPluginLocalInstall()
			return true, pluginBlink(), true
		}
		m.togglePluginDiscoverExpanded()
		return true, nil, true
	case key.Code == 's' && key.Mod.Contains(tea.ModCtrl):
		if !m.panel.pluginSearch && m.panel.cursor == 0 {
			m.openPluginLocalInstall()
			return true, pluginBlink(), true
		}
	}
	if isTypedText(key) {
		m.enterPluginSearch()
		m.panel.pluginQuery = key.Text
		return true, pluginBlink(), true
	}
	return false, nil, false
}

func (m *runMode) handleNavAndEnterKey(key tea.Key, n int) (bool, tea.Cmd, bool) {
	if m.panel.kind == panelConfig {
		if handled, cmd := m.handleConfigKey(key, n); handled {
			return true, cmd, true
		}
	}
	switch key.Code {
	case tea.KeyUp:
		if m.panel.cursor > 0 {
			m.panel.cursor--
		} else if m.panel.kind.wraps() {
			m.panel.cursor = n - 1
		}
		return true, nil, true
	case tea.KeyDown:
		if m.panel.cursor < n-1 {
			m.panel.cursor++
		} else if m.panel.kind.wraps() {
			m.panel.cursor = 0
		}
		return true, nil, true
	case tea.KeyPgUp:
		m.panelPageCursor(-10, n)
		return true, nil, true
	case tea.KeyPgDown:
		m.panelPageCursor(+10, n)
		return true, nil, true
	case tea.KeyHome:
		m.panel.cursor = 0
		return true, nil, true
	case tea.KeyEnd:
		if n > 0 {
			m.panel.cursor = n - 1
		}
		return true, nil, true
	}
	return m.handleSpecialPanelEnterKey(key)
}

func (m *runMode) handleSpecialPanelEnterKey(key tea.Key) (bool, tea.Cmd, bool) {
	if m.panel.kind == panelSkills {
		if m.panel.detail {
			if handled, cmd := m.handleSkillsDetailKey(key); handled {
				return true, cmd, true
			}
			return false, nil, true
		}
		if key.Code == tea.KeyEnter {
			m.openSkillDetail()
			return true, nil, true
		}
		if key.Code == tea.KeyUp || key.Code == tea.KeyDown {
			m.moveSkillsCursor(key.Code == tea.KeyDown)
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
	max := len(spec.lines) - spec.visible
	if max < 0 {
		max = 0
	}
	return max
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
	spec, ok := m.viewerSpecFor()
	if !ok {
		return false, nil
	}
	page := spec.visible - 1
	if page < 1 {
		page = 1
	}
	switch {
	case key.Code == tea.KeyUp:
		m.viewerScrollBy(spec, -1)
		return true, nil
	case key.Code == tea.KeyDown:
		m.viewerScrollBy(spec, 1)
		return true, nil
	case key.Code == tea.KeyPgUp:
		m.viewerScrollBy(spec, -page)
		return true, nil
	case key.Code == tea.KeyPgDown:
		m.viewerScrollBy(spec, page)
		return true, nil
	case key.Code == tea.KeyHome && key.Mod.Contains(tea.ModCtrl):
		m.panel.window = 0
		return true, nil
	case key.Code == tea.KeyEnd && key.Mod.Contains(tea.ModCtrl):
		m.panel.window = viewerMaxOffset(spec)
		return true, nil
	case key.Code == tea.KeyEsc:
		return true, m.closePanel()
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}
