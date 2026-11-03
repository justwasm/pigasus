// config_host.go — the host's side of the /config panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/configpanel: it owns the rows,
// the search box, the inline option submenu, the keys and the writes.
// The host supplies the six things the panel cannot answer itself —
// the live theme, the alternate screen, the seasonal theme, the way out
// of the panel, the transcript block the locked telemetry row answers
// with, and the calendar that decides whether the seasonal row is drawn
// — and nothing else.
//
// The panel is rebuilt every time it opens: the host's slashPanel struct
// is reset when a panel closes, so a stale copy of the query, the cursor
// or the memory-only values is never shown.

package cliapp

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	cliflags "github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/views/configpanel"
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
	"github.com/justwasm/pigasus/internal/ui/views/themepreview"
	"github.com/justwasm/pigasus/palette"
)

// newConfigPanel builds the /config panel with the host's seams.
func (m *runMode) newConfigPanel() *configpanel.Panel {
	return configpanel.New(configpanel.Seams{
		// The live theme is the host's: the panel previews a scheme
		// while the user walks the Color Scheme row's values, and the
		// preview has to repaint the prompt, the transcript and the
		// markdown renderer — none of which the panel can reach.
		ApplyTheme: m.applyThemeLive,
		// The alternate screen is the session's: the Rendering Mode
		// row switches the session between the alternate buffer and the
		// inline view, and the renderer reads the flag off the view.
		ApplyAltScreen: m.applyAltScreenLive,
		// Verbosity changes immediately repaint the transcript.
		ApplyVerbosity: m.applyVerbosityLive,
		// The seasonal theme follows the seasonal row.
		RefreshSeasonal: m.refreshSeasonalTheme,
		// Esc leaves the panel.
		Close: m.closePanel,
		// The locked telemetry row answers with a transcript block.
		Respond: func(cmd, msg string) { m.slashRespond(cmd, msg) },
		// The calendar is the host's: the seasonal row is drawn only
		// while the theme is in season, and the host is what pins the
		// clock in its tests.
		SeasonalEvent: func() *seasonal.Event { return seasonal.Current(seasonalNow()) },
		// OverrideFor names any CLI flags that override the setting for
		// this session (e.g. --sandbox, --dangerously-skip-permissions, --mode).
		OverrideFor: m.configOverrideFor,
	})
}

// configPanel returns the open /config panel.
//
// The panel is built when the /config panel opens — the host's slashPanel
// struct is reset whenever a panel closes — so the accessor is here so
// that a caller which finds the panel missing still gets one that
// answers.
func (m *runMode) configPanel() *configpanel.Panel {
	if m.panel.cfg == nil {
		m.panel.cfg = m.newConfigPanel()
	}
	return m.panel.cfg
}

// applyThemeLive applies the selected color scheme live across the TUI:
// recording the scheme the session wears, updating mdrender's markdown and
// chroma syntax highlighting styles, updating the textarea prompt/placeholder
// styles, and returning tea.ClearScreen.
//
// The reference repaints the whole interface when the scheme changes, not
// only the preview card: every colour the interface paints is resolved
// from the scheme's slots. The TUI pushes the recorded scheme around the
// work that paints a frame (Update and View), so the frame wears it.
func (m *runMode) applyThemeLive(scheme string) tea.Cmd {
	palette.SetScheme(scheme)
	mdrender.SetColorScheme(scheme)
	promptColor, placeholderColor := themepreview.ColorsFor(scheme)
	if m != nil {
		m.colorScheme = scheme
		st := m.input.Styles()
		promptStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(promptColor))
		placeholderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(placeholderColor))
		st.Focused.Prompt = promptStyle
		st.Focused.Placeholder = placeholderStyle
		st.Blurred.Prompt = promptStyle
		st.Blurred.Placeholder = placeholderStyle
		m.input.SetStyles(st)
		m.rerenderTranscriptForTheme()
	}
	return tea.ClearScreen
}

// applyAltScreenLive switches the session between the terminal's
// alternate screen buffer and the inline view.
//
// The reference does it in the same keystroke: agy 1.4.7 leaves the
// alternate buffer (?1049l) right after the /config Rendering Mode row
// is switched to "native terminal (inline)" (measured 2026-10-25).
// bubbletea reads the flag off the view, so flipping it is enough — the
// renderer enters or leaves the alternate buffer on the next frame.
func (m *runMode) applyAltScreenLive(stored any) {
	m.altScreen = altScreenModeValue(stored)
}

// applyVerbosityLive sets the active verbosity and repaints the transcript.
func (m *runMode) applyVerbosityLive(verbosity string) {
	if m == nil {
		return
	}
	m.verbosity = verbosity
	m.expandedUserToggled = false
	m.rerenderBlocks()
	m.rerenderLiveSteps()
}

// configOverrideFor reports launch flag overrides for settings rows
// (e.g. "on (overridden by --sandbox)").
func (m *runMode) configOverrideFor(row configpanel.Row) (string, bool) {
	if m == nil || m.flags == nil {
		return "", false
	}
	switch row.SettingsKey {
	case "enableTerminalSandbox":
		if cliflags.BoolValue(m.flags, "sandbox") {
			return "(overridden by --sandbox)", true
		}
	case "toolPermission":
		if cliflags.BoolValue(m.flags, "dangerously-skip-permissions") {
			return "(overridden by --dangerously-skip-permissions)", true
		}
	case "agentMode":
		if cliflags.WasSet(m.flags, "mode") && cliflags.StringValue(m.flags, "mode") != "" {
			return "(overridden by --mode)", true
		}
	case "altScreenMode":
		if cliflags.BoolValue(m.flags, "no-alt-screen") {
			return "(overridden by --no-alt-screen)", true
		}
	}
	return "", false
}

