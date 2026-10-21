package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/mdrender"
	"github.com/justwasm/pigasus/internal/onboarding"
)

const ansiReset = "\x1b[0m"

// themeColorsFor returns the (promptColor, placeholderColor) for a given colorScheme.
func themeColorsFor(scheme string) (promptColor, placeholderColor string) {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "dark", "solarized dark", "colorblind-friendly dark":
		return "#268BD2", "#839496"
	case "tokyo night":
		return "#82aaff", "#828bb8"
	default:
		return "#268BD2", "#657B83"
	}
}

// applyThemeLive applies the selected color scheme live across the TUI:
// updating mdrender's markdown and chroma syntax highlighting styles,
// updating the textarea prompt/placeholder styles, and returning tea.ClearScreen.
func (m *runMode) applyThemeLive(scheme string) tea.Cmd {
	mdrender.SetColorScheme(scheme)
	promptColor, placeholderColor := themeColorsFor(scheme)
	if m != nil {
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
		if strings.Contains(strings.ToLower(r.name), q) ||
			strings.Contains(strings.ToLower(r.description), q) {
			filtered = append(filtered, r)
			continue
		}
		for _, v := range r.values {
			if strings.Contains(strings.ToLower(v.display), q) {
				filtered = append(filtered, r)
				break
			}
		}
	}
	return filtered
}

// colorSchemeFromString maps a color scheme label to its onboarding.ColorScheme enum.
func colorSchemeFromString(s string) onboarding.ColorScheme {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "terminal":
		return onboarding.SchemeTerminal
	case "light":
		return onboarding.SchemeLight
	case "solarized light":
		return onboarding.SchemeSolarizedLight
	case "colorblind-friendly light":
		return onboarding.SchemeColorblindLight
	case "dark":
		return onboarding.SchemeDark
	case "solarized dark":
		return onboarding.SchemeSolarizedDark
	case "colorblind-friendly dark":
		return onboarding.SchemeColorblindDark
	case "tokyo night":
		return onboarding.SchemeTokyoNight
	default:
		return onboarding.SchemeSolarizedLight
	}
}

// renderColorSchemePreviewCard renders the 63-column wide, 22-line high
// preview card used by agy 1.4.3 when the Color Scheme submenu is open.
func renderColorSchemePreviewCard(scheme onboarding.ColorScheme) []string {
	p := onboarding.PaletteFor(scheme)

	fg := func(code int) string {
		return fmt.Sprintf("\x1b[38;5;%dm", code)
	}
	borderFg := "\x1b[90m"
	reset := "\x1b[m"

	inner := make([]string, 20)

	// Line 1: ` > you: add a greeting function`
	inner[0] = fmt.Sprintf(" %s> you:%s  add a greeting function", fg(p.Link)+"\x1b[1m", reset)
	// Line 2: blank
	inner[1] = ""
	// Line 3: `   Here's the change:`
	inner[2] = fmt.Sprintf(" %s  Here's the change:%s", fg(p.String)+"\x1b[1m", reset)
	// Line 4: blank
	inner[3] = ""
	// Line 5: `   3     import  "fmt"`
	inner[4] = fmt.Sprintf("   %s3 %s    %simport%s  %s\"fmt\"%s",
		borderFg, reset,
		fg(p.Keyword)+"\x1b[1m", reset,
		fg(p.String), reset)
	// Line 6: `   4 `
	inner[5] = fmt.Sprintf("   %s4 %s", borderFg, reset)
	// Line 7: `   5  -   func  main() {`
	inner[6] = fmt.Sprintf("   %s5 %s\x1b[48;5;131m %s-   func%s  %smain%s() {",
		borderFg, reset,
		fg(p.Err), reset,
		fg(p.FunctionName), reset)
	// Line 8: `   5  +   func  greet(name  string) {`
	inner[7] = fmt.Sprintf("   %s5 %s\x1b[48;5;65m %s+   func%s  %sgreet%s(name  %sstring%s) {",
		borderFg, reset,
		fg(p.String), reset,
		fg(p.FunctionName), reset,
		fg(p.Type), reset)
	// Line 9: `   6  +     fmt.Printf("Hello, %s!\n", name)`
	inner[8] = fmt.Sprintf("   %s6 %s\x1b[48;5;65m %s+ %s    fmt.%sPrintf%s(%s\"Hello, %%s!\\n\"%s, name)",
		borderFg, reset,
		fg(p.String), reset,
		fg(p.FunctionName), reset,
		fg(p.String), reset)
	// Line 10: `   7    }`
	inner[9] = fmt.Sprintf("   %s7 %s    }", borderFg, reset)
	// Line 11: blank
	inner[10] = ""
	// Line 12: `   ▾ Thought Process`
	inner[11] = fmt.Sprintf(" %s  ▾ Thought Process%s", fg(p.Dim_), reset)
	// Line 13: `     I need to add a greeting function. I'll use fmt.Printf.`
	inner[12] = fmt.Sprintf(" %s    I need to add a greeting function. I'll use fmt.Printf.%s", fg(p.Dim_), reset)
	// Line 14: `   ⚙ tool:  write_file main.go`
	inner[13] = fmt.Sprintf(" %s  ⚙ tool:  %swrite_file main.go", fg(p.Tool), reset)
	// Line 15: `   ◉ task:  Implementing greeting`
	inner[14] = fmt.Sprintf(" %s  ◉ task:  %sImplementing greeting", fg(p.Task), reset)
	// Line 16: `   ✗ error:  compilation failed`
	inner[15] = fmt.Sprintf(" %s  ✗ error:  %scompilation failed", fg(p.Err), reset)
	// Line 17: `   ⚠ warning:  deprecation warning`
	inner[16] = fmt.Sprintf(" %s  ⚠ warning:  %sdeprecation warning", fg(p.Warn), reset)
	// Line 18: `   → link: file:///path/to/main.go`
	inner[17] = fmt.Sprintf("   → link: %s%sfile:///path/to/main.go%s", fg(p.Link), "\x1b[4m", reset)
	// Line 19: `   ★ accent:  highlighted text`
	inner[18] = fmt.Sprintf(" %s  ★ accent:  %shighlighted text", fg(p.Accent)+"\x1b[1m", reset)
	// Line 20: `   · dim: press Enter to continue`
	inner[19] = fmt.Sprintf(" %s  · dim: press Enter to continue%s", fg(p.Dim_), reset)

	card := make([]string, 22)
	card[0] = borderFg + "╭" + strings.Repeat("─", 61) + "╮" + reset
	for i := 0; i < 20; i++ {
		plainText := stripANSI(inner[i])
		runeCount := len([]rune(plainText))
		pad := 61 - runeCount
		if pad < 0 {
			pad = 0
		}
		card[i+1] = borderFg + "│" + reset + inner[i] + strings.Repeat(" ", pad) + borderFg + "│" + reset
	}
	card[21] = borderFg + "╰" + strings.Repeat("─", 61) + "╯" + reset

	return card
}

// panelColorSchemePreviewLines renders the Color Scheme submenu with the
// right-side 22-line preview card.
func (m *runMode) panelColorSchemePreviewLines(row configRow, currentVal string) []string {
	schemeCount := len(row.values)
	if schemeCount > 8 {
		schemeCount = 8
	}

	hoveredScheme := onboarding.ColorScheme(m.panel.configSubmenuCursor)
	if m.panel.configSubmenuCursor < 0 || m.panel.configSubmenuCursor >= schemeCount {
		hoveredScheme = onboarding.SchemeSolarizedLight
	}
	cardLines := renderColorSchemePreviewCard(hoveredScheme)

	leftLines := make([]string, 22)
	leftLines[0] = "  Color Scheme" + strings.Repeat(" ", 33-len("  Color Scheme"))
	for i := 0; i < schemeCount; i++ {
		v := row.values[i]
		isCurrent := (v.display == currentVal)
		suffix := ""
		if isCurrent {
			suffix = " (current)"
		}
		var optText string
		plainOpt := ""
		if i == m.panel.configSubmenuCursor {
			optText = cBlueBoldOpen + "  > " + v.display + suffix + ansiReset
			plainOpt = "  > " + v.display + suffix
		} else {
			optText = "    " + v.display + suffix
			plainOpt = "    " + v.display + suffix
		}
		pad := 33 - len([]rune(plainOpt))
		if pad < 0 {
			pad = 0
		}
		leftLines[i+1] = optText + strings.Repeat(" ", pad)
	}
	for i := schemeCount + 1; i < 22; i++ {
		leftLines[i] = strings.Repeat(" ", 33)
	}

	lines := make([]string, 0, 26)
	for i := 0; i < 22; i++ {
		lines = append(lines, leftLines[i]+cardLines[i])
	}
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
	if m.panel.configSubmenuCursor >= len(row.values) {
		m.panel.configSubmenuCursor = len(row.values) - 1
	}

	if row.name == "Color Scheme" {
		return m.panelColorSchemePreviewLines(row, currentVal)
	}

	visible := configVisibleRowsFor(m.height)
	cursor := m.panel.cursor
	if cursor < 0 || cursor >= len(rows) {
		cursor = 0
	}
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	if cursor < m.panel.window {
		m.panel.window = cursor
	}
	if cursor >= m.panel.window+visible {
		m.panel.window = cursor - visible + 1
	}
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

	for i := m.panel.window; i < m.panel.window+visible && i < len(rows); i++ {
		r := rows[i]
		if i == m.panel.cursor {
			lines = append(lines, "  "+r.name)
			for valIdx, val := range r.values {
				isCurrent := (val.display == currentVal)
				suffix := ""
				if isCurrent {
					suffix = " (current)"
				}
				if valIdx == m.panel.configSubmenuCursor {
					lines = append(lines, cBlueBoldOpen+"  > "+val.display+suffix+ansiReset)
				} else {
					lines = append(lines, "    "+val.display+suffix)
				}
			}
		} else {
			value := m.configRowValue(r)
			pad := configNamePad(r.name, 29)
			lines = append(lines, "  "+r.name+strings.Repeat(" ", pad)+value)
		}
	}

	desc := row.description
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

// applyConfigValue sets the chosen configuration value, persisting to
// settings.json unless the row is marked memoryOnly.
func (m *runMode) applyConfigValue(row configRow, val cfgVal) {
	if isTelemetryRow(row) && IsBusinessAccount() {
		m.slashRespond("/config", telemetryDisabledResponse)
		return
	}
	if row.name == "Color Scheme" {
		m.applyThemeLive(val.display)
	}
	if row.memoryOnly {
		if m.panel.configMemory == nil {
			m.panel.configMemory = map[string]string{}
		}
		m.panel.configMemory[row.settingsKey] = val.display
		return
	}
	if val.stored == nil {
		_ = auth.DeleteSettingsField(row.settingsKey)
		return
	}
	_ = auth.SetSettingsField(row.settingsKey, val.stored)
}

// cycleConfigValueFiltered cycles through the allowed values for the current row
// in a potentially filtered rows slice.
func (m *runMode) cycleConfigValueFiltered(rows []configRow, dir int) tea.Cmd {
	idx := m.panel.cursor
	if idx < 0 || idx >= len(rows) {
		return nil
	}
	row := rows[idx]
	if len(row.values) < 2 {
		return nil
	}
	if isTelemetryRow(row) && IsBusinessAccount() {
		m.slashRespond("/config", telemetryDisabledResponse)
		return nil
	}
	current := m.configRowValue(row)
	next := row.values[0]
	for i, v := range row.values {
		if v.display == current {
			next = row.values[(i+dir+len(row.values))%len(row.values)]
			break
		}
	}
	m.applyConfigValue(row, next)
	if row.name == "Color Scheme" {
		return tea.ClearScreen
	}
	return nil
}

// handleConfigKey handles keyboard events when the config/settings panel is open.
func (m *runMode) handleConfigKey(key tea.Key, n int) (bool, tea.Cmd) {
	if m.panel.configSubmenu {
		rows := configRowsFiltered(m.panel.configQuery)
		if m.panel.cursor < 0 || m.panel.cursor >= len(rows) {
			m.panel.configSubmenu = false
			return true, nil
		}
		row := rows[m.panel.cursor]
		valCount := len(row.values)
		var cmd tea.Cmd
		switch key.Code {
		case tea.KeyUp:
			if m.panel.configSubmenuCursor > 0 {
				m.panel.configSubmenuCursor--
			} else {
				m.panel.configSubmenuCursor = valCount - 1
			}
			if row.name == "Color Scheme" && m.panel.configSubmenuCursor >= 0 && m.panel.configSubmenuCursor < valCount {
				cmd = m.applyThemeLive(row.values[m.panel.configSubmenuCursor].display)
			}
			return true, cmd
		case tea.KeyDown:
			if m.panel.configSubmenuCursor < valCount-1 {
				m.panel.configSubmenuCursor++
			} else {
				m.panel.configSubmenuCursor = 0
			}
			if row.name == "Color Scheme" && m.panel.configSubmenuCursor >= 0 && m.panel.configSubmenuCursor < valCount {
				cmd = m.applyThemeLive(row.values[m.panel.configSubmenuCursor].display)
			}
			return true, cmd
		case tea.KeyEnter:
			if m.panel.configSubmenuCursor >= 0 && m.panel.configSubmenuCursor < valCount {
				m.applyConfigValue(row, row.values[m.panel.configSubmenuCursor])
			}
			m.panel.configSubmenu = false
			if row.name == "Color Scheme" {
				cmd = tea.ClearScreen
			}
			return true, cmd
		case tea.KeyEsc:
			if row.name == "Color Scheme" && m.panel.configSubmenuOrigVal != "" {
				cmd = m.applyThemeLive(m.panel.configSubmenuOrigVal)
			}
			m.panel.configSubmenu = false
			return true, cmd
		}
		return true, nil
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
		if len(rows) > 0 && m.panel.cursor >= 0 && m.panel.cursor < len(rows) {
			row := rows[m.panel.cursor]
			m.panel.configSubmenu = true
			currentVal := m.configRowValue(row)
			m.panel.configSubmenuOrigVal = currentVal
			m.panel.configSubmenuCursor = 0
			for i, v := range row.values {
				if v.display == currentVal {
					m.panel.configSubmenuCursor = i
					break
				}
			}
		}
		return true, nil
	case tea.KeyBackspace:
		if m.panel.configQuery != "" {
			r := []rune(m.panel.configQuery)
			m.panel.configQuery = string(r[:len(r)-1])
			m.panel.cursor = 0
			m.panel.window = 0
		}
		return true, nil
	case tea.KeyEsc:
		if m.panel.configQuery != "" {
			m.panel.configQuery = ""
			m.panel.cursor = 0
			m.panel.window = 0
			return true, nil
		}
		return true, m.closePanel()
	case tea.KeySpace:
		m.panel.configQuery += " "
		m.panel.cursor = 0
		m.panel.window = 0
		return true, nil
	}

	if key.Mod == 0 && key.Text != "" && key.Text != "\t" && key.Text != "\n" && key.Text != "\r" {
		m.panel.configQuery += key.Text
		m.panel.cursor = 0
		m.panel.window = 0
		return true, nil
	}

	return false, nil
}
