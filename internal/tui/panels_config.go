package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/tui/panels"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/views/configpanel"
	"github.com/justwasm/pigasus/internal/ui/views/themepreview"
	"github.com/justwasm/pigasus/palette"
	"github.com/justwasm/pigasus/render"
)

// Config
func (m *RootModel) RefreshSeasonalTheme() { m.refreshSeasonalTheme() }
func (m *RootModel) SlashRespond(cmd, msg string, extra ...string) {
	m.slashRespond(cmd, msg, extra...)
}
func (m *RootModel) SeasonalNow() time.Time { return seasonalNow() }
func (m *RootModel) ConfigOverrideFor(row configpanel.Row) (string, bool) {
	return m.configOverrideFor(row)
}
func (m *RootModel) AltScreenHint() string {
	caps := render.DetectAltScreenCapsFromEnv()
	return render.AltScreenHint(&caps)
}
func (m *RootModel) ApplyThemeLive(scheme string) tea.Cmd { return m.applyThemeLive(scheme) }
func (m *RootModel) ApplyAltScreenLive(stored any)        { m.applyAltScreenLive(stored) }
func (m *RootModel) ApplyVerbosityLive(verbosity string)  { m.applyVerbosityLive(verbosity) }
func (m *RootModel) newConfigPanel() *configpanel.Panel   { return panels.NewConfigPanel(m) }
func (m *RootModel) configPanel() *configpanel.Panel {
	if m.overlays.panel.cfg == nil {
		m.overlays.panel.cfg = m.newConfigPanel()
	}
	return m.overlays.panel.cfg
}

func (m *RootModel) applyThemeLive(scheme string) tea.Cmd {
	palette.SetScheme(scheme)
	mdrender.SetColorScheme(scheme)
	promptColor, placeholderColor := themepreview.ColorsFor(scheme)
	if m != nil {
		m.viewport.colorScheme = scheme
		st := m.prompt.input.Styles()
		promptStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(promptColor))
		placeholderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(placeholderColor))
		st.Focused.Prompt = promptStyle
		st.Focused.Placeholder = placeholderStyle
		st.Blurred.Prompt = promptStyle
		st.Blurred.Placeholder = placeholderStyle
		m.prompt.input.SetStyles(st)
		m.rerenderTranscriptForTheme()
	}
	return tea.ClearScreen
}

func (m *RootModel) applyAltScreenLive(stored any) {
	m.viewport.altScreen = altScreenModeValue(stored)
}

func (m *RootModel) applyVerbosityLive(verbosity string) {
	if m == nil {
		return
	}
	m.display.verbosity = verbosity
	m.viewport.expandedUserToggled = false
	m.rerenderBlocks()
	m.rerenderLiveSteps()
}

func (m *RootModel) configOverrideFor(row configpanel.Row) (string, bool) {
	if m == nil || m.session.flags == nil {
		return "", false
	}
	switch row.SettingsKey {
	case "enableTerminalSandbox":
		if cliflags.BoolValue(m.session.flags, "sandbox") {
			return "(overridden by --sandbox)", true
		}
	case "toolPermission":
		if cliflags.BoolValue(m.session.flags, "dangerously-skip-permissions") {
			return "(overridden by --dangerously-skip-permissions)", true
		}
	case "agentMode":
		if cliflags.StringValue(m.session.flags, "mode") != "" {
			return "(overridden by --mode)", true
		}
	case "acp":
		if cliflags.StringValue(m.session.flags, "acp") != "" {
			return "(overridden by --acp)", true
		}
	}
	return "", false
}
