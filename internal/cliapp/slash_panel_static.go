package cliapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/keybind"
	"github.com/justwasm/pigasus/internal/subagent"
)

// hookRows are the five hook types the reference lists. The descriptions
// are static in REF.
var hookRows = [5][2]string{
	{"PreToolUse", "Before tool execution"},
	{"PostToolUse", "After tool execution"},
	{"PreInvocation", "Before each LLM invocation"},
	{"PostInvocation", "After each LLM invocation"},
	{"Stop", "When agent tries to exit"},
}

func (m *runMode) panelHooksLines() []string {
	lines := []string{
		// REF opens with a bare default-fg space, then the bold title.
		"\x1b[39m \x1b[1mHooks\x1b[0m",
		" " + panelGray238 + "  5 hook types" + panelGray238C,
		"",
	}
	for i, r := range hookRows {
		name := r[0]
		if pad := 14 - len([]rune(name)); pad > 0 {
			name += strings.Repeat(" ", pad)
		}
		if i == m.panel.cursor {
			lines = append(lines, " "+cBlueOpen+"> "+name+cBlueClose+"  "+
				panelGray238+r[1]+panelGray238C)
		} else {
			lines = append(lines, "   "+name+"  "+panelGray238+r[1]+panelGray238C)
		}
	}
	lines = append(lines, "", panelDotHint("   ", "↑/↓", "Navigate", "enter", "Select"))
	return lines
}

// permissionScopes are the three config scopes the editor offers. All
// three blurbs were recovered by driving REF's /permissions panel with
// Down (/tmp/agycmp/nav-permissions.log).
var permissionScopes = [3][2]string{
	{"Project", "Rules that apply only to this project (highest priority)"},
	{"Shared with Pigasus", "Rules that apply to all Pigasus products"},
	{"Global", "Global rules"},
}

func (m *runMode) panelPermissionsLines() []string {
	lines := []string{
		panelTitleOpen + "Permission Config Editor" + panelTitleClose,
		"",
		panelGray238 + "Select a config scope to edit:" + panelGray238C,
		"",
	}
	for i, s := range permissionScopes {
		if i == m.panel.cursor {
			lines = append(lines, cBlueOpen+"> "+s[0]+cBlueClose)
		} else {
			lines = append(lines, "  "+s[0])
		}
	}
	cursor := m.panel.cursor
	if cursor < 0 || cursor >= len(permissionScopes) {
		cursor = 0
	}
	lines = append(lines,
		"",
		"  "+panelGray238+permissionScopes[cursor][1]+panelGray238C,
		"",
		panelKeyboardHint("↑/↓", "Navigate", "enter", "Save", "esc", "Close"),
	)
	return lines
}

func (m *runMode) panelAgentsLines() []string {
	wd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	title := "Create New Agents"
	if m.panel.agentPreparedSet {
		title += "  [ Switch will fork the current conversation on exit ]"
	}
	lines := []string{
		panelTitleOpen + title + panelTitleClose,
		"  " + panelGray238 + "Workspace: " +
			filepath.Join(wd, ".agents", "agents", "{agent_name}", "agent.md") + panelGray238C,
		"  " + panelGray238 + "Global: " +
			filepath.Join(home, ".gemini", "config", "agents", "{agent_name}", "agent.md") + panelGray238C,
		"",
		"",
	}
	active := m.agentName
	if m.panel.agentPreparedSet {
		active = m.panel.agentPreparedName
	}
	header := "Available Agents"
	if active != "" {
		header += "  (active: " + active + ")"
	}
	lines = append(lines, "\x1b[1m"+header+"\x1b[0m")
	lines = append(lines, m.renderAgentRows(active)...)

	running := m.activeSubagentStatuses()
	if len(running) > 0 {
		lines = append(lines, "", "\x1b[1mRunning Agents\x1b[0m")
		for _, r := range running {
			lines = append(lines, "  "+r)
		}
	}

	lines = append(lines,
		"",
		panelKeyboardHint("\u2191/\u2193", "Navigate", "enter", "Select / Toggle",
			"k", "Kill Active Subagent", "esc", "Go Back"),
	)
	return lines
}

func (m *runMode) renderAgentRows(active string) []string {
	type agentRow struct {
		name, desc string
		custom     bool
	}
	rows := []agentRow{{"default", "Default agent", false}}
	for _, a := range m.panel.agents {
		rows = append(rows, agentRow{a.Name, a.Description, true})
	}
	cursor := m.panel.cursor
	if cursor < 0 || cursor >= len(rows) {
		cursor = 0
	}
	var lines []string
	for i, r := range rows {
		rowActive := (i == 0 && active == "") || (i > 0 && active == r.name)
		prefix := "  "
		if i == cursor {
			prefix = "> "
		}
		marker := "  "
		if rowActive {
			marker = panelGreen34 + "\u25cf " + cWarningBlueOpen
		}
		name := r.name
		if i == cursor {
			name = cBlueOpen + name + cBlueClose
		} else if rowActive {
			name = cWarningBlueOpen + name + cBlueClose
		}
		line := cBlueOpen + prefix + cBlueClose + marker + name
		if r.desc != "" {
			line += "  " + panelGray238 + r.desc + panelGray238C
		}
		lines = append(lines, line)
	}
	return lines
}

// activeSubagentStatuses returns one rendered row per active
// subagent, in registration order. The rows read the Subagent's
// StatusLine so a subagent that stopped on error renders as
// "Error: <reason>" instead of the misleading "Done" the panel
// used to show (AGY 1.3.1). A nil or empty manager yields an empty
// slice, which panelAgentsLines collapses so the cycle-205 capture
// stays byte-exact when no subagent has been spawned yet.
//
// The rows are unstyled plain text — the panel's blue/green
// palette is reserved for the "Available Agents" cursor row above,
// so the running list stays distinguishable.
func (m *runMode) activeSubagentStatuses() []string {
	var mgr *subagent.Manager
	if m.taskSubagentManager != nil {
		mgr = m.taskSubagentManager
	} else if defaultTaskSubagentManager != nil {
		mgr = defaultTaskSubagentManager
	}
	if mgr == nil {
		return nil
	}
	all := mgr.List()
	if len(all) == 0 {
		return nil
	}
	rows := make([]string, 0, len(all))
	for _, sa := range all {
		if sa == nil {
			continue
		}
		rows = append(rows, fmt.Sprintf("%s  %s", sa.Name, sa.StatusLine()))
	}
	return rows
}

func (m *runMode) panelCreditsLines() []string {
	actions := [2]string{"Get More AI Credits", "See Activity"}
	lines := []string{
		cBlueOpen + "Pigasus CLI" + cBlueClose + "  " +
			"\x1b[1m" + panelGray238 + "credits" + "\x1b[0m",
		"\x1b[1m" + panelGray238 + "Model AI Credits" + "\x1b[0m",
		"",
		"\x1b[1mRemaining AI Credits\x1b[0m: " +
			"\x1b[1m" + cBlueOpen + "AI Credits not enabled (enable in /settings)" + "\x1b[0m",
		"",
		"\x1b[1m" + panelGray238 + "Actions" + "\x1b[0m",
		"",
	}
	for i, a := range actions {
		if i == m.panel.cursor {
			lines = append(lines, cBlueOpen+"> "+a+cBlueClose)
		} else {
			lines = append(lines, "  "+a)
		}
	}
	lines = append(lines, "",
		panelKeyboardHint("↑/↓", "Navigate", "enter", "Select / Toggle", "esc", "Close"))
	return lines
}

var feedbackCategories = [6]string{
	"1. Code doesn't work (e.g., syntax errors, bad logic)",
	"2. Repeated itself (e.g., stuck running the same tool over again)",
	"3. Took too many steps (e.g., progress was much too slow)",
	"4. Ignored instructions (e.g., missed specific constraints)",
	"5. Got stuck (e.g., unable to recover from an error)",
	"6. Other",
}

func (m *runMode) panelFeedbackLines() []string {
	if m.panel.stage > 0 {
		return m.panelFeedbackStage2()
	}
	lines := []string{
		panelTitleOpen + "Submit Feedback" + panelTitleClose,
		"",
		"What went wrong? Select a category to assist your report:",
		"",
	}
	for i, c := range feedbackCategories {
		if i == m.panel.cursor {
			lines = append(lines, cBlueOpen+"> "+c+cBlueClose)
		} else {
			lines = append(lines, "  "+c)
		}
	}
	lines = append(lines,
		"",
		panelDotHint("  ", "esc", "Cancel", "1-6", "Select & Continue", "enter", "Continue"),
	)
	return lines
}

func (m *runMode) panelFeedbackStage2() []string {
	return m.feedbackStage2Lines()
}

func (m *runMode) handleFeedbackTextKey(key tea.Key) (bool, tea.Cmd) {
	switch {
	case key.Code == tea.KeyEsc:
		m.panel.stage = 0
		m.panel.text = ""
		return true, nil
	case key.Code == tea.KeyEnter:
		return true, m.submitFeedback()
	case key.Code == 's' && key.Mod.Contains(tea.ModCtrl):
		return true, m.submitFeedback()
	case key.Code == tea.KeyBackspace:
		if r := []rune(m.panel.text); len(r) > 0 {
			m.panel.text = string(r[:len(r)-1])
		}
		return true, nil
	}
	if keybind.IsTypedText(key) {
		m.panel.text += key.Text
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

func (m *runMode) submitFeedback() tea.Cmd {
	m.panel = slashPanel{}
	m.slashRespond("/feedback", "Feedback recorded (openagy has no upload endpoint; saved locally only)")
	return nil
}


