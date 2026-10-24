package cliapp

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/keybind"
)

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
	if keybind.IsTypedText(key) {
		m.enterPluginSearch()
		m.panel.pluginQuery = key.Text
		return true, pluginBlink(), true
	}
	return false, nil, false
}
