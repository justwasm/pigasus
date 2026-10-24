package cliapp

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/tuilib"
)

var commandPanels = map[string]panelKind{
	"mcp": panelMCP,
	"tasks": panelTasks,
	"effort": panelEffort,
	"hooks": panelHooks,
	"permissions": panelPermissions,
	"agents": panelAgents,
	"credits": panelCredits,
	"codesearch": panelCodeSearch,
	"artifact": panelArtifact,
	"feedback": panelFeedback,
	"rewind": panelRewind,
	"context": panelContext,
	"model": panelModel,
	"config": panelConfig,
	"plugin": panelPlugin,
	"skills": panelSkills,
	"usage": panelUsage,
	"changelog": panelChangelog,
	"diff": panelDiff,
	"add-dir": panelAddDir,
	"rename": panelRename,
	"feature-diff": panelFeatDiff,
	// Cycle provider-model-editor: the configuration panel. It is *not*
	// in internal/tuilib.DefaultSlashCommands — the picker's entry count
	// is byte-pinned to the reference's 99 — so /provider is invocable
	// but unlisted.
	"provider": panelProvider,
	// Cycle multi-account-switch: the account switcher. The reference
	// has one credential and no switcher, so this is a clone-only
	// superset — invocable but unlisted, like /provider.
	"accounts": panelAccounts,
}

func panelForCommand(cmd string) (panelKind, bool) {
	kind, ok := commandPanels[cmd]
	return kind, ok
}

// currentEffort maps the clone's recovered /effort value onto the slider
// index. flags.effort is the parsed --effort value; anything that is not
// "low" or "medium" is treated as high, which is the reference default
// and the value the clone printed before this cycle.
func (m *runMode) currentEffort() int {
	switch flagStringValue(m.flags, "effort") {
	case "low":
		return effortLow
	case "medium":
		return effortMedium
	case "high":
		return effortHigh
	}
	// Cycle 292: with no explicit --effort the tier comes from the model
	// label, which is what the reference renders. A settings.json holding
	// "Gemini 3.8 Flash (Medium)" prints "· medium" in the status row; the
	// clone used to print "· high" there while the banner said Medium.
	return effortCursorForLabel(m.model)
}

// currentEffortName is the word form of currentEffort, used to feed the
// runtime layer with the slider's choice ("low", "medium", "high"). It
// picks up both --effort at startup and a /effort <level> / picker
// rewrite at runtime, since both surfaces mutate m.flags.
//
// Cycle effort-picker: the TUI's per-turn loop and the print path's PiG
// session both read the same flag value through this helper.
func (m *runMode) currentEffortName() string {
	return effortLevelName(m.currentEffort())
}

// panelRowCount is the cursor bound (0..count-1) for vertical-list
// panels. Effort is special: it is a horizontal slider (←/→ keys), not a
// row picker, but it still wants the cursor clamped against its 3
// levels. Cycle 193 adds the four cursor-bearing A-class panels (model,
// config, plugin, skills); the three viewer-only panels (usage,
// changelog, diff) scroll the body with ↑/↓ and never enter this table
// (a future cycle could implement a virtual page cursor).
//
// Cycle 222 made this a method on *runMode because panelRewind's count
// depends on m.session. The single legacy free-function caller in
// slash_cycle203_test.go passes a zero-valued runMode via the
// panelRowCountFor helper below.
func (m *runMode) tasksRowCount() int {
	if m.taskReg == nil {
		return 4
	}
	n := len(m.taskReg.List())
	if n == 0 {
		return 4
	}
	if n > tasksPanelLimit {
		n = tasksPanelLimit
	}
	return n + 3
}

func (m *runMode) configRowCount() int {
	if m.panel.configSubmenu {
		rows := configRowsFiltered(m.panel.configQuery)
		if m.panel.cursor >= 0 && m.panel.cursor < len(rows) {
			return len(rows[m.panel.cursor].Values)
		}
		return 0
	}
	return len(configRowsFiltered(m.panel.configQuery))
}

func (m *runMode) panelRowCount(k panelKind) int {
	switch k {
	case panelTasks:
		return m.tasksRowCount()
	case panelEffort:
		return 3
	case panelHooks:
		return 5
	case panelPermissions:
		return 3
	case panelAgents:
		return 1 + len(m.panel.agents)
	case panelCredits:
		return 2
	case panelFeedback:
		return 6
	case panelRewind:
		if len(m.session) == 0 {
			return 1
		}
		return len(rewindSteps(m.session))
	case panelModel:
		// The row list is the reference's five static rows plus the
		// conditional 5.5 rows plus the models the user configured, so
		// the count is not a constant: a hard-coded 6 made every row
		// past the sixth unreachable (the cursor stopped on "MiniMax M3"
		// and could never reach "MiniMax M3.1 Flash"). It also follows
		// the search filter, so it shrinks with the query.
		return len(m.modelRows())
	case panelConfig:
		return m.configRowCount()
	case panelPlugin:
		return len(installedPluginRows())
	case panelSkills:
		return len(skillsPanelCatalog())
	case panelProvider:
		return len(m.panel.prov.rows)
	case panelAccounts:
		return len(m.panel.acct.rows)
	}
	return 0
}

// wraps reports whether ↑/↓ wrap at the panel's row boundaries. The
// reference's /feedback category picker wraps both directions
// (/tmp/agycmp/panels/ppfb_wrap.raw.txt); every other panel clamps.
func (p panelKind) wraps() bool { return p == panelFeedback }

// isViewer reports whether the panel is a scrollable *document* viewer
// rather than a pick-from-a-list picker. The two viewer panels run a
// window over their content: ↑/↓ move it by one line, pgup/pgdown by a
// page and ctrl+home / ctrl+end jump to the ends. /usage and /changelog
// announce exactly those keys in their footers
// (/tmp/agycmp/panels/ppref.usage.raw.txt, ppref.changelog.raw.txt).
func (p panelKind) isViewer() bool {
	return p == panelUsage || p == panelChangelog || p == panelFeatDiff
}

// trailingBlank reports whether the panel's legend row is followed by an
// empty row before the status row. /hooks and /context legends sit
// immediately above the status row; every other captured panel has a
// blank row between legend and status
// (/tmp/agycmp/panels/ppref.hooks.txt rows 9-10 vs ppref.mcp.txt 17-19;
// ppref.context.empty.raw.txt rows 29-30).
func (p panelKind) trailingBlank() bool {
	// Live 1.4.3 also puts the /usage legend directly above the status row
	// (captured 2026-10-20 at 120x40), so it joins the no-trailing-blank set.
	return p != panelHooks && p != panelContext && p != panelUsage && p != panelChangelog && p != panelDiff
}

// panelView renders the panel body. It returns "" when no panel is open,
// mirroring pickerView/resumeView so viewBody can call it unconditionally.
func (m *runMode) panelView() string {
	if m.panel.kind == panelNone {
		return ""
	}
	var b strings.Builder
	for _, l := range m.panelLines() {
		b.WriteString(l)
		b.WriteString("\n")
	}
	if m.panel.kind.trailingBlank() {
		b.WriteString("\n")
	}
	return b.String()
}

// panelLines returns the panel's fully styled rows, one per terminal row.
func (m *runMode) panelLinesCore() []string {
	switch m.panel.kind {
	case panelMCP:
		return []string{
			panelTitleOpen + "MCP Servers" + panelTitleClose,
			"",
			panelGray238 + "  No MCP servers configured." + panelGray238C,
			"",
			panelKeyboardHint("↑/↓", "Navigate", "enter", "Actions", "esc", "Exit"),
		}
	case panelTasks:
		return m.panelTasksLines()
	case panelEffort:
		return m.panelEffortLines()
	case panelHooks:
		return m.hooksPanelLines()
	case panelPermissions:
		return m.permissionsPanelLines()
	case panelAgents:
		return m.panelAgentsLines()
	case panelCredits:
		return m.panelCreditsLines()
	case panelCodeSearch:
		return m.codesearchPanelLines()
	case panelArtifact:
		return []string{
			panelTitleOpen + "Artifacts" + panelTitleClose,
			"  " + cDimGrayOpen + "No artifacts" + cDimGrayClose,
			"",
			panelKeyboardHint("↑/↓", "Navigate", "p", "preview", "enter", "open",
				"ctrl+g", "open in editor", "esc", "Dismiss"),
		}
	case panelFeedback:
		return m.panelFeedbackLines()
	case panelRewind:
		return m.panelRewindLines()
	case panelContext:
		return m.panelContextLines()
	case panelProvider:
		return m.panelProviderLines()
	case panelAccounts:
		return m.panelAccountsLines()
	}
	return nil
}

func (m *runMode) panelLines() []string {
	if lines := m.panelLinesCore(); lines != nil {
		return lines
	}
	switch m.panel.kind {
	case panelModel:
		return m.panelModelLines()
	case panelConfig:
		return m.panelConfigLines()
	case panelPlugin:
		return m.panelPluginLines()
	case panelSkills:
		return m.panelSkillsLines()
	case panelUsage:
		return m.panelUsageLines()
	case panelChangelog:
		return m.panelChangelogLines()
	case panelDiff:
		return m.panelDiffLines()
	case panelAddDir:
		return m.panelAddDirLines()
	case panelRename:
		return m.panelRenameLines()
	case panelFeatDiff:
		return m.featDiffPanelLines()
	}
	return nil
}

// panelKeyboardHint builds the `Keyboard: <key> <label>  <key> <label>` legend row.
func panelKeyboardHint(pairs ...string) string {
	return tuilib.PanelKeyboardHint(pairs...)
}

// panelDotHint builds the `   <key> <label> · <key> <label>` legend row.
func panelDotHint(lead string, pairs ...string) string {
	return tuilib.PanelDotHint(lead, pairs...)
}





// closePanel clears the panel and, when the reference does so, appends
// the "> /cmd" + "⎿  Exited /cmd command" block to the transcript.
func (m *runMode) closePanel() tea.Cmd {
	cmd := m.panel.cmd
	notice := m.panel.kind.exitNotice()
	m.panel = slashPanel{}
	// Cycle 216: /config can change "Editor Mode › Vim" and its Insert
	// First child, so the vim gate is refreshed when the panel closes.
	m.reloadVimSettings()
	if notice {
		m.response = append(m.response, slashEchoLines(cmd, "Exited "+cmd+" command")...)
	}
	// The input was already cleared when the panel opened (see the
	// openSlashPanel call site in Update) and every key was swallowed
	// while it was up, so there is nothing left to reset here.
	return nil
}

// closePanelSilent clears the panel without writing the
// `Exited /cmd command` echo. Cycle 223: /add-dir's commit path
// (Enter → addDirFeedback → slashRespond → close) already prints its
// own transcript block via slashRespond; emitting the exit notice on
// top would yield two stacked `> /add-dir` headers and is not what
// the brief wants. Esc still goes through closePanel so the standard
// exit notice does fire when the user backs out without committing.
//
// reloadVimSettings is called by both paths — /add-dir doesn't toggle
// vim gates, but every close should refresh so a follow-up panel
// across a /config change sees the fresh state.
func (m *runMode) closePanelSilent() tea.Cmd {
	m.panel = slashPanel{}
	m.reloadVimSettings()
	return nil
}


// slashEchoLines renders the reference's transcript block for one slash command.
func slashEchoLines(cmd, msg string) []string {
	return tuilib.SlashEchoLines(cmd, msg)
}

