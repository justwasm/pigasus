// slash_action_panels_hooks.go — /hooks and /agents panel rendering and keyboard interactions.

package cliapp

import (
	tea "charm.land/bubbletea/v2"
)

// hooksPanelLines dispatches the /hooks panel by stage.
func (m *runMode) hooksPanelLines() []string {
	switch m.panel.act.hookStage {
	case 1:
		return m.hooksMatchersLines()
	case 2:
		return m.hooksFormLines()
	}
	return m.panelHooksLines()
}

// hooksMatchersLines is stage 1: the matcher list for one hook type.
func (m *runMode) hooksMatchersLines() []string {
	a := m.panel.act
	idx := a.hookIdx
	if idx < 0 || idx >= len(hookRows) {
		idx = 0
	}
	name, desc := hookRows[idx][0], hookRows[idx][1]
	tool := hookIsTool(idx)
	addLabel := "+ Add new matcher..."
	if !tool {
		addLabel = "+ Add new hook..."
	}
	lines := []string{
		"\x1b[39m \x1b[1m" + name + " — Matchers\x1b[0m",
		" " + panelGray238 + "  " + desc + panelGray238C,
		"",
	}
	if m.panel.cursor == 0 {
		lines = append(lines, " "+cBlueOpen+"> "+addLabel+cBlueClose)
	} else {
		lines = append(lines, "   "+addLabel)
	}
	if tool {
		if m.panel.cursor == 1 {
			lines = append(lines, " "+cBlueOpen+"> + Match all (no filter)"+cBlueClose)
		} else {
			lines = append(lines, "   + Match all (no filter)")
		}
	}
	lines = append(lines, "   "+panelGray238+"No hooks configured"+panelGray238C,
		"",
		panelDotHint("   ", "↑/↓", "Navigate", "enter", "Select",
			"e", "Toggle", "backspace", "Delete", "esc", "Back"),
	)
	return lines
}

// hooksFormLines is stage 2: the add-matcher or add-hook form.
func (m *runMode) hooksFormLines() []string {
	a := m.panel.act
	idx := a.hookIdx
	if idx < 0 || idx >= len(hookRows) {
		idx = 0
	}
	name, desc := hookRows[idx][0], hookRows[idx][1]
	if hookIsTool(idx) {
		return hooksToolMatcherForm(name, desc, a.hookMatch)
	}
	return hooksGeneralForm(name, desc, a.hookName, a.hookField)
}

func hooksToolMatcherForm(name, desc, hookMatch string) []string {
	field := " " + cBlueOpen + ">  " + cBlueClose
	if hookMatch != "" {
		field = " " + cBlueOpen + "> " + cBlueClose + hookMatch
	}
	return []string{
		"\x1b[39m \x1b[1mAdd new matcher for " + name + "\x1b[0m",
		"",
		" " + panelGray238 + "  " + desc + panelGray238C,
		"",
		" \x1b[1m  Matcher:\x1b[0m",
		field,
		"",
		" " + panelGray238 + "  Examples:" + panelGray238C,
		" " + panelGray238 + "  • Write (single tool)" + panelGray238C,
		" " + panelGray238 + "  • Write|Edit (multiple tools)" + panelGray238C,
		" " + panelGray238 + "  • Web.* (regex pattern)" + panelGray238C,
		"",
		panelDotHint("   ", "enter", "Confirm", "esc", "Cancel"),
	}
}

func hooksGeneralForm(name, desc, hookName string, hookField int) []string {
	nameField := " " + cBlueOpen + "> " + "\x1b[7m\x1b[37m \x1b[0m"
	if hookName != "" {
		nameField = " " + cBlueOpen + "> " + cBlueClose + hookName
	}
	cmdField := "     " + panelGray238 + " " + panelGray238C
	if hookField == 1 {
		cmdField = "     \x1b[7m\x1b[37m \x1b[0m"
	}
	return []string{
		"\x1b[39m \x1b[1mAdd new hook\x1b[0m",
		"",
		" " + panelGray238 + "  Event: " + name + " — " + desc + panelGray238C,
		"",
		" \x1b[1m  Name:\x1b[0m",
		nameField,
		"",
		" " + panelGray238 + "  Command:" + panelGray238C,
		cmdField,
		"",
		" " + panelGray238 + "  Timeout (seconds):" + panelGray238C,
		"     " + panelGray238 + "default: 30" + panelGray238C,
		"",
		"   " + panelEffMedOpen + "⚠ Hooks run shell commands with your full permissions." + panelGray238C,
		"",
		panelDotHint("   ", "tab", "Switch field", "enter", "Confirm", "esc", "Cancel"),
	}
}

func (m *runMode) handleHooksKey(key tea.Key) (bool, tea.Cmd) {
	a := &m.panel.act
	switch a.hookStage {
	case 1:
		return m.handleHooksMatchersKey(key)
	case 2:
		return m.handleHooksFormKey(key)
	}
	switch key.Code {
	case tea.KeyUp:
		if m.panel.cursor > 0 {
			m.panel.cursor--
		}
		return true, nil
	case tea.KeyDown:
		if m.panel.cursor < len(hookRows)-1 {
			m.panel.cursor++
		}
		return true, nil
	case tea.KeyEnter:
		a.hookIdx = m.panel.cursor
		a.hookStage = 1
		m.panel.cursor = 0
		return true, nil
	case tea.KeyEsc:
		return true, m.closePanel()
	}
	if key.Mod == 0 && key.Text != "" {
		return true, nil
	}
	return false, nil
}

func hookSelectableRows(idx int) int {
	if hookIsTool(idx) {
		return 2
	}
	return 1
}

func (m *runMode) handleHooksMatchersKey(key tea.Key) (bool, tea.Cmd) {
	a := &m.panel.act
	n := hookSelectableRows(a.hookIdx)
	switch key.Code {
	case tea.KeyUp:
		if m.panel.cursor > 0 {
			m.panel.cursor--
		}
		return true, nil
	case tea.KeyDown:
		if m.panel.cursor < n-1 {
			m.panel.cursor++
		}
		return true, nil
	case tea.KeyEnter:
		if m.panel.cursor == 0 {
			a.hookStage = 2
			a.hookField = 0
			a.hookMatch = ""
			a.hookName = ""
		}
		return true, nil
	case tea.KeyEsc:
		a.hookStage = 0
		m.panel.cursor = a.hookIdx
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

func (m *runMode) handleHooksFormKey(key tea.Key) (bool, tea.Cmd) {
	a := &m.panel.act
	switch key.Code {
	case tea.KeyEsc:
		a.hookStage = 1
		m.panel.cursor = 0
		return true, nil
	case tea.KeyTab:
		if !hookIsTool(a.hookIdx) {
			a.hookField = (a.hookField + 1) % 3
		}
		return true, nil
	case tea.KeyBackspace:
		switch {
		case hookIsTool(a.hookIdx):
			if r := []rune(a.hookMatch); len(r) > 0 {
				a.hookMatch = string(r[:len(r)-1])
			}
		case a.hookField == 0:
			if r := []rune(a.hookName); len(r) > 0 {
				a.hookName = string(r[:len(r)-1])
			}
		}
		return true, nil
	}
	if key.Mod == 0 && key.Text != "" {
		switch {
		case hookIsTool(a.hookIdx):
			a.hookMatch += key.Text
		case a.hookField == 0:
			a.hookName += key.Text
		}
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

func (m *runMode) handleAgentsKey(key tea.Key) (bool, tea.Cmd) {
	rows := 1 + len(m.panel.agents)
	switch key.Code {
	case tea.KeyEsc:
		if m.panel.agentPreparedSet {
			m.applyAgentSwitch(m.panel.agentPreparedName, m.panel.agentPreparedBody)
			m.panel = slashPanel{}
			return true, nil
		}
		return true, m.closePanel()
	case tea.KeyUp:
		if m.panel.cursor > 0 {
			m.panel.cursor--
		}
		return true, nil
	case tea.KeyDown:
		if m.panel.cursor < rows-1 {
			m.panel.cursor++
		}
		return true, nil
	case tea.KeyEnter:
		return m.handleAgentsEnter(rows)
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

func (m *runMode) handleAgentsEnter(rows int) (bool, tea.Cmd) {
	cursor := m.panel.cursor
	if cursor < 0 || cursor >= rows {
		cursor = 0
	}
	name, body := "", ""
	if cursor > 0 {
		a := m.panel.agents[cursor-1]
		name, body = a.Name, a.Body
	}
	active := m.agentName
	if m.panel.agentPreparedSet {
		active = m.panel.agentPreparedName
	}
	if name == active {
		if name == "" {
			m.slashRespond("/agents", "Already using default agent.")
		} else {
			m.slashRespond("/agents", "Already selected agent: "+name)
		}
		return true, nil
	}
	m.panel.agentPreparedSet = true
	m.panel.agentPreparedName = name
	m.panel.agentPreparedBody = body
	verb := "create a new conversation"
	if len(m.session) > 0 {
		verb = "fork the current conversation"
	}
	if name == "" {
		m.slashRespond("/agents", "Prepared switch to default agent (will "+verb+" on exit).")
	} else {
		m.slashRespond("/agents", "Prepared selection: "+name+" (will "+verb+" on exit).")
	}
	return true, nil
}
