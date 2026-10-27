package cliapp

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/mdrender"
	"github.com/justwasm/pigasus/internal/seasonal"
	"github.com/justwasm/pigasus/internal/themepreview"
)

const ansiReset = "\x1b[0m"

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
	}
	return tea.ClearScreen
}

// configRowsFiltered returns the configuration rows matching the search query.
// When query is empty, all active rows are returned.
// Matching is case-insensitive substring search over the row name, description,
// and option display values.
func configRowsFiltered(query string) []configRow {
	all := configRows()
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return all
	}
	var filtered []configRow
	for _, r := range all {
		if strings.Contains(strings.ToLower(r.Name), q) ||
			strings.Contains(strings.ToLower(r.Description), q) {
			filtered = append(filtered, r)
			continue
		}
		for _, v := range r.Values {
			if strings.Contains(strings.ToLower(v.Display), q) {
				filtered = append(filtered, r)
				break
			}
		}
	}
	return filtered
}

// panelColorSchemePreviewLines renders the Color Scheme submenu with the
// right-side 22-line preview card.
func (m *runMode) panelColorSchemePreviewLines(row configRow, currentVal string) []string {
	displays := make([]string, len(row.Values))
	for i, v := range row.Values {
		displays[i] = v.Display
	}
	lines := themepreview.BuildPreviewLines(displays, m.panel.configSubmenuCursor, currentVal, cBlueBoldOpen, ansiReset)
	lines = append(lines, "", "", "", "", panelKeyboardHint("↑/↓", "Navigate", "enter", "Save", "esc", "Cancel"))
	return lines
}

// panelConfigSubmenuLines renders the inline option selection submenu when Enter
// is pressed on a setting in /settings or /config.
func (m *runMode) panelConfigSubmenuLines(rows []configRow) []string {
	if m.panel.cursor < 0 || m.panel.cursor >= len(rows) {
		m.panel.configSubmenu = false
		return m.panelConfigLines()
	}
	row := rows[m.panel.cursor]
	currentVal := m.configRowValue(row)

	if m.panel.configSubmenuCursor < 0 {
		m.panel.configSubmenuCursor = 0
	}
	if m.panel.configSubmenuCursor >= len(row.Values) {
		m.panel.configSubmenuCursor = len(row.Values) - 1
	}

	if row.Name == "Color Scheme" {
		return m.panelColorSchemePreviewLines(row, currentVal)
	}

	visible := configVisibleRowsFor(m.height)
	m.clampConfigPanelWindow(len(rows))

	searchContent := searchCursorBlock
	if m.panel.configQuery != "" {
		searchContent = m.panel.configQuery + searchCursorBlock
	}
	lines := []string{
		panelTitleOpen + "Settings" + panelTitleClose,
		"",
		"  Search: " + searchContent,
		"          " + searchUnderline,
		"",
	}
	lines = append(lines, m.configSubmenuRowLines(rows, currentVal, visible)...)

	desc := row.Description
	if isTelemetryRow(row) && IsBusinessAccount() {
		desc = telemetryBusinessDescription()
	}
	lines = append(lines, "")
	for _, wrapped := range wrapPanelText(desc, m.width) {
		lines = append(lines, "  "+panelGray238+wrapped+panelGray238C)
	}
	lines = append(lines, "", "", panelKeyboardHint("↑/↓", "Navigate", "enter", "Save", "esc", "Cancel"))
	return lines
}

// clampConfigPanelWindow keeps the config panel's scroll window aligned with
// the cursor so the highlighted row always stays visible.
func (m *runMode) clampConfigPanelWindow(rowCount int) {
	cursor := m.panel.cursor
	if cursor < 0 || cursor >= rowCount {
		cursor = 0
	}
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	visible := configVisibleRowsFor(m.height)
	if cursor < m.panel.window {
		m.panel.window = cursor
	}
	if cursor >= m.panel.window+visible {
		m.panel.window = cursor - visible + 1
	}
}

// configSubmenuRowLines renders the visible rows of the config submenu, marking
// the current value and the cursor position.
func (m *runMode) configSubmenuRowLines(rows []configRow, currentVal string, visible int) []string {
	lines := make([]string, 0, visible)
	for i := m.panel.window; i < m.panel.window+visible && i < len(rows); i++ {
		r := rows[i]
		if i == m.panel.cursor {
			lines = append(lines, "  "+r.Name)
			for valIdx, val := range r.Values {
				isCurrent := (val.Display == currentVal)
				suffix := ""
				if isCurrent {
					suffix = " (current)"
				}
				if valIdx == m.panel.configSubmenuCursor {
					lines = append(lines, cBlueBoldOpen+"  > "+val.Display+suffix+ansiReset)
				} else {
					lines = append(lines, "    "+val.Display+suffix)
				}
			}
		} else {
			value := m.configRowValue(r)
			pad := configNamePad(r.Name, 29)
			lines = append(lines, "  "+r.Name+strings.Repeat(" ", pad)+value)
		}
	}
	return lines
}

// applyConfigValue sets the chosen configuration value, persisting to
// settings.json unless the row is marked memoryOnly.
func (m *runMode) applyConfigValue(row configRow, val cfgVal) {
	if isTelemetryRow(row) && IsBusinessAccount() {
		m.slashRespond("/config", telemetryDisabledResponse)
		return
	}
	if row.Name == "Color Scheme" {
		m.applyThemeLive(val.Display)
	}
	if row.SettingsKey == "altScreenMode" {
		m.applyAltScreenLive(val.Stored)
	}
	if row.MemoryOnly {
		if m.panel.configMemory == nil {
			m.panel.configMemory = map[string]string{}
		}
		m.panel.configMemory[row.SettingsKey] = val.Display
		return
	}
	if val.Stored == nil {
		_ = auth.DeleteSettingsField(row.SettingsKey)
		m.applySeasonalOptIn(row.SettingsKey)
		return
	}
	_ = auth.SetSettingsField(row.SettingsKey, val.Stored)
	m.applySeasonalOptIn(row.SettingsKey)
}

// applySeasonalOptIn re-reads the seasonal theme after the /config row
// switched it.
//
// 【一手】 agy 1.4.7: the theme follows the setting, so the themed
// banner, the themed prompt-box rules and the moon-phase spinner follow
// the row as it is switched.
func (m *runMode) applySeasonalOptIn(key string) {
	if key != seasonal.OptInKey {
		return
	}
	m.refreshSeasonalTheme()
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

// cycleConfigValueFiltered cycles through the allowed values for the current row
// in a potentially filtered rows slice.
func (m *runMode) cycleConfigValueFiltered(rows []configRow, dir int) tea.Cmd {
	idx := m.panel.cursor
	if idx < 0 || idx >= len(rows) {
		return nil
	}
	row := rows[idx]
	if len(row.Values) < 2 {
		return nil
	}
	if isTelemetryRow(row) && IsBusinessAccount() {
		m.slashRespond("/config", telemetryDisabledResponse)
		return nil
	}
	current := m.configRowValue(row)
	next := row.Values[0]
	for i, v := range row.Values {
		if v.Display == current {
			next = row.Values[(i+dir+len(row.Values))%len(row.Values)]
			break
		}
	}
	m.applyConfigValue(row, next)
	if row.Name == "Color Scheme" {
		return tea.ClearScreen
	}
	return nil
}

// handleConfigKey handles keyboard events when the config/settings panel is open.
func (m *runMode) handleConfigKey(key tea.Key, n int) (bool, tea.Cmd) {
	if m.panel.configSubmenu {
		return m.handleConfigSubmenuKey(key)
	}

	rows := configRowsFiltered(m.panel.configQuery)

	switch key.Code {
	case tea.KeyLeft:
		cmd := m.cycleConfigValueFiltered(rows, -1)
		return true, cmd
	case tea.KeyRight:
		cmd := m.cycleConfigValueFiltered(rows, +1)
		return true, cmd
	case tea.KeyUp:
		if m.panel.cursor > 0 {
			m.panel.cursor--
		}
		return true, nil
	case tea.KeyDown:
		if m.panel.cursor < len(rows)-1 {
			m.panel.cursor++
		}
		return true, nil
	case tea.KeyEnter:
		m.openConfigSubmenu(rows)
		return true, nil
	case tea.KeyBackspace:
		m.backspaceConfigQuery()
		return true, nil
	case tea.KeyEsc:
		if m.panel.configQuery != "" {
			m.clearConfigQuery()
			return true, nil
		}
		return true, m.closePanel()
	case tea.KeySpace:
		m.appendConfigQuery(" ")
		return true, nil
	}

	if key.Mod == 0 && key.Text != "" && key.Text != "\t" && key.Text != "\n" && key.Text != "\r" {
		m.appendConfigQuery(key.Text)
		return true, nil
	}

	return false, nil
}

// backspaceConfigQuery removes the last rune from the config panel's search
// query and resets the scroll position.
func (m *runMode) backspaceConfigQuery() {
	if m.panel.configQuery == "" {
		return
	}
	r := []rune(m.panel.configQuery)
	m.panel.configQuery = string(r[:len(r)-1])
	m.panel.cursor = 0
	m.panel.window = 0
}

// clearConfigQuery empties the config panel's search query and resets the
// scroll position.
func (m *runMode) clearConfigQuery() {
	m.panel.configQuery = ""
	m.panel.cursor = 0
	m.panel.window = 0
}

// appendConfigQuery appends text to the config panel's search query and
// resets the scroll position.
func (m *runMode) appendConfigQuery(text string) {
	m.panel.configQuery += text
	m.panel.cursor = 0
	m.panel.window = 0
}

// handleConfigSubmenuKey handles keyboard events while the inline option
// selection submenu is open on top of the config/settings panel.
func (m *runMode) handleConfigSubmenuKey(key tea.Key) (bool, tea.Cmd) {
	rows := configRowsFiltered(m.panel.configQuery)
	if m.panel.cursor < 0 || m.panel.cursor >= len(rows) {
		m.panel.configSubmenu = false
		return true, nil
	}
	row := rows[m.panel.cursor]
	valCount := len(row.Values)
	var cmd tea.Cmd
	switch key.Code {
	case tea.KeyUp:
		if m.panel.configSubmenuCursor > 0 {
			m.panel.configSubmenuCursor--
		} else {
			m.panel.configSubmenuCursor = valCount - 1
		}
		cmd = m.previewThemeForRow(row, valCount)
		return true, cmd
	case tea.KeyDown:
		if m.panel.configSubmenuCursor < valCount-1 {
			m.panel.configSubmenuCursor++
		} else {
			m.panel.configSubmenuCursor = 0
		}
		cmd = m.previewThemeForRow(row, valCount)
		return true, cmd
	case tea.KeyEnter:
		if m.panel.configSubmenuCursor >= 0 && m.panel.configSubmenuCursor < valCount {
			m.applyConfigValue(row, row.Values[m.panel.configSubmenuCursor])
		}
		m.panel.configSubmenu = false
		if row.Name == "Color Scheme" {
			cmd = tea.ClearScreen
		}
		return true, cmd
	case tea.KeyEsc:
		if row.Name == "Color Scheme" && m.panel.configSubmenuOrigVal != "" {
			cmd = m.applyThemeLive(m.panel.configSubmenuOrigVal)
		}
		m.panel.configSubmenu = false
		return true, cmd
	}
	return true, nil
}

// previewThemeForRow applies the live theme preview when the highlighted row
// is the Color Scheme setting; otherwise it returns a nil command.
func (m *runMode) previewThemeForRow(row configRow, valCount int) tea.Cmd {
	if row.Name != "Color Scheme" {
		return nil
	}
	if m.panel.configSubmenuCursor < 0 || m.panel.configSubmenuCursor >= valCount {
		return nil
	}
	return m.applyThemeLive(row.Values[m.panel.configSubmenuCursor].Display)
}

// openConfigSubmenu opens the inline option selection submenu for the row
// under the cursor, seeding the cursor with the row's current value.
func (m *runMode) openConfigSubmenu(rows []configRow) {
	if len(rows) == 0 || m.panel.cursor < 0 || m.panel.cursor >= len(rows) {
		return
	}
	row := rows[m.panel.cursor]
	m.panel.configSubmenu = true
	currentVal := m.configRowValue(row)
	m.panel.configSubmenuOrigVal = currentVal
	m.panel.configSubmenuCursor = 0
	for i, v := range row.Values {
		if v.Display == currentVal {
			m.panel.configSubmenuCursor = i
			break
		}
	}
}
