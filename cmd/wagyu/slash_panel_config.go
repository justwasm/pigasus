package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/justwasm/pigasus/internal/auth"
)

// cfgVal is one stopping point of a setting's ←/→ cycle, as measured
// against the reference with /tmp/agycmp/probe_ref_walk2.py and
// probe_ref_vocab.py. `stored` is the JSON value the reference writes;
// nil means the key is *absent* from settings.json.
//
// The absence convention is a load-bearing finding: the reference
// represents every setting at its default by removing the key. Cycling
// Agent Mode back to "default" deletes `agentMode`; Copy on Select's
// "on" is absent while "off" is JSON false. Writing the literal default
// (e.g. `agentMode: "default"`) makes 1.4.0 reject the whole file with
//
//	⚠ Settings Error
//	  invalid settings: agentMode: unrecognized value "default"
//
// and silently run with defaults — so the clone must delete, not write.
type cfgVal struct {
	display string
	stored  any
}

// configRow is one rendered line of the /config picker. The table below is
// the reference's complete 21-row catalogue, recovered verbatim from
// /tmp/agycmp/probe_ref_scroll.py (all 20 top-level rows at height 50):
//
//	Agent Mode                   default            agentMode
//	Animation Speed              medium             runningLightSpeed
//	Artifact Review              always proceeds    artifactReviewPolicy
//	Color Scheme                 light              colorScheme
//	Conversation Picker Grouping none               pickerGrouping
//	Copy on Select               on                 copyOnSelect
//	Editor                       auto ($EDITOR)     editor
//	Editor Mode                  default            editorMode
//	Editor Mode › Insert First   off                vimInsertFirst   ← nested, only when Editor Mode = vim
//	Enable Telemetry             on                 (not persisted)
//	Non-Workspace Access         off                allowNonWorkspaceAccess
//	Notifications                off                notifications
//	Queued Messages              Queue              queuedMessages
//	Rendering Mode               no flickering …    altScreenMode
//	Sandbox Mode                 off                enableTerminalSandbox
//	Show Active Tasks            on                 hideActiveItems   ← inverted
//	Show Feedback Survey         on                 showFeedbackSurvey
//	Show Tips                    on                 showTips
//	Tool Permission              always-proceed     toolPermission
//	Use AI Credits               off                useG1Credits
//	Verbosity                    medium             verbosity
//
// Every value list and every persisted form was read off the reference by
// cycling ←/→ and diffing settings.json (probe_ref_walk2.py); the row→key
// mapping below is that probe's diff table, not an inference.
type configRow struct {
	name        string
	settingsKey string
	values      []cfgVal
	description string
	// showWhenKey/showWhenVal gate a child row on another setting's raw
	// value ("Editor Mode › Insert First" is drawn only while
	// editorMode == "vim").
	showWhenKey string
	showWhenVal string
	// memoryOnly: the reference toggles the value but writes nothing to
	// settings.json (Enable Telemetry). The clone keeps it in panel
	// state so the row still responds.
	memoryOnly bool
}

// cv builds a cfgVal whose stored form is the given string, or nil
// (absent) when s is "".
func cfgv(display, stored string) cfgVal {
	if stored == "" {
		return cfgVal{display: display, stored: nil}
	}
	return cfgVal{display: display, stored: stored}
}

// cbv builds a boolean cfgVal. The default reads as absent (nil);
// the non-default persists as a JSON bool.
func cfgb(display string, stored any) cfgVal {
	return cfgVal{display: display, stored: stored}
}

// configSettingsRows is the static catalogue. Rows whose showWhen gate is
// not met are dropped by configRows().
var configSettingsRows = []configRow{
	{name: "Agent Mode", settingsKey: "agentMode",
		values: []cfgVal{
			cfgv("default", ""), cfgv("accept-edits", "accept-edits"),
			cfgv("auto", "auto"), cfgv("plan", "plan"),
		},
		description: "'default': standard behavior. 'accept-edits': auto-approve file edits, prompt for commands. 'auto': auto-approve edits and safe commands, prompt on irreversible actions. 'plan': research and plan without making changes"},
	{name: "Animation Speed", settingsKey: "runningLightSpeed",
		values: []cfgVal{
			cfgv("medium", ""), cfgv("slow", "slow"), cfgv("off", "off"), cfgv("fast", "fast"),
		},
		description: "Speed of the running light animation"},
	{name: "Artifact Review", settingsKey: "artifactReviewPolicy",
		values: []cfgVal{
			cfgv("asks for review", ""), cfgv("always proceeds", "always-proceed"),
			cfgv("agent decides", "agent-decides"),
		},
		description: "Agent always asks for review."},
	{name: "Color Scheme", settingsKey: "colorScheme",
		values: []cfgVal{
			cfgv("terminal", ""), cfgv("light", "light"),
			cfgv("solarized light", "solarized light"),
			cfgv("colorblind-friendly light", "colorblind-friendly light"),
			cfgv("dark", "dark"), cfgv("solarized dark", "solarized dark"),
			cfgv("colorblind-friendly dark", "colorblind-friendly dark"),
			cfgv("tokyo night", "tokyo night"),
		},
		description: ""},
	{name: "Conversation Picker Grouping", settingsKey: "pickerGrouping",
		values: []cfgVal{
			cfgv("none", ""), cfgv("grouped", "grouped"),
		},
		description: "How the /resume picker organizes conversations. Press ctrl+f in the picker to switch view for one session without changing this."},
	{name: "Copy on Select", settingsKey: "copyOnSelect",
		values:      []cfgVal{cfgb("on", nil), cfgb("off", false)},
		description: "When enabled, releasing a mouse text selection copies it to the system clipboard. Only active in alt-screen mode. When disabled, the artifact viewer uses native terminal selection instead."},
	{name: "Editor", settingsKey: "editor",
		values: []cfgVal{
			cfgv("auto ($EDITOR)", ""), cfgv("vim", "vim"), cfgv("emacs", "emacs"),
		},
		description: "Preferred editor for viewing artifacts"},
	{name: "Editor Mode", settingsKey: "editorMode",
		values:      []cfgVal{cfgv("default", ""), cfgv("vim", "vim")},
		description: "Configure how you edit the prompt (default or vim)"},
	{name: "Editor Mode \u203a Insert First", settingsKey: "vimInsertFirst",
		values:      []cfgVal{cfgb("off", nil), cfgb("on", true)},
		description: "Place vim in insert mode when the prompt gains focus.",
		showWhenKey: "editorMode", showWhenVal: "vim"},
	{name: "Enable Telemetry", settingsKey: "enableTelemetry",
		// agy 1.4.3 defaults this row to off on a clean settings.json
		// (live capture at 120x40, 2026-10-20); the 1.4.1 default was on.
		values:      []cfgVal{cfgb("off", nil), cfgb("on", true)},
		description: "When toggled on, Pigasus CLI collects usage data to help Google enhance performance and features",
		memoryOnly:  true},
	{name: "Non-Workspace Access", settingsKey: "allowNonWorkspaceAccess",
		values:      []cfgVal{cfgb("off", nil), cfgb("on", true)},
		description: "Allow agent to access files outside workspace directories"},
	{name: "Notifications", settingsKey: "notifications",
		values:      []cfgVal{cfgb("off", nil), cfgb("on", true)},
		description: "Bell + desktop notification on tool confirmation and agent completion"},
	{name: "Queued Messages", settingsKey: "queuedMessages",
		values: []cfgVal{
			cfgv("Queue", ""), cfgv("Send Immediately", "send-immediately"),
		},
		description: "Configure when follow-up messages are sent."},
	{name: "Rendering Mode", settingsKey: "altScreenMode",
		values: []cfgVal{
			cfgv("native terminal (inline)", ""), cfgv("no flickering (altscreen)", "always"),
		},
		description: "no flickering (altscreen) is ideal for long conversations but requires holding Shift/Option for native text selection. Native Terminal experience (inline) preserves native terminal behavior but may truncate long conversations and flicker when toggling views."},
	{name: "Sandbox Mode", settingsKey: "enableTerminalSandbox",
		values:      []cfgVal{cfgb("off", nil), cfgb("on", true)},
		description: "Enables terminal sandbox restrictions."},
	{name: "Show Active Tasks", settingsKey: "hideActiveItems",
		values:      []cfgVal{cfgb("on", nil), cfgb("off", true)},
		description: "Show active subagents and background tasks below the input box"},
	{name: "Show Feedback Survey", settingsKey: "showFeedbackSurvey",
		values:      []cfgVal{cfgb("on", nil), cfgb("off", false)},
		description: "Show feedback survey"},
	{name: "Show Tips", settingsKey: "showTips",
		values:      []cfgVal{cfgb("on", nil), cfgb("off", false)},
		description: "Show helpful tips while the agent is generating"},
	{name: "Tool Permission", settingsKey: "toolPermission",
		values:      []cfgVal{cfgv("always-proceed", ""), cfgv("strict", "strict")},
		description: "Controls whether terminal commands require your approval before running."},
	{name: "Use AI Credits", settingsKey: "useG1Credits",
		values:      []cfgVal{cfgb("off", nil), cfgb("on", true)},
		description: "Use your AI credits for model calls once your plan's quota runs out"},
	{name: "Verbosity", settingsKey: "verbosity",
		values: []cfgVal{
			cfgv("medium", ""), cfgv("low", "low"), cfgv("high", "high"),
		},
		description: "Controls how much detail is shown for the agent's tool calls, commands, and thoughts."},
}

// configRows returns the catalogue filtered by the dynamic gates — today
// only the "Editor Mode › Insert First" child row, which the reference
// draws immediately after Editor Mode while editorMode == "vim"
// (probe_ref_walk2.py row 8).
func configRows() []configRow {
	out := make([]configRow, 0, len(configSettingsRows)+1)
	for _, r := range configSettingsRows {
		if r.showWhenKey != "" {
			if raw, _ := auth.SettingsField(r.showWhenKey); rawString(raw) != r.showWhenVal {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// rawString renders a settings.json leaf as the string form used for
// gating comparisons.
func rawString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "on"
		}
		return "off"
	}
	return ""
}

// configSettingRaw returns the JSON leaf exactly as REF would compare it:
// the stored value, or nil when the key is absent (the default state).
func configSettingRaw(rowIdx int) any {
	rows := configRows()
	if rowIdx < 0 || rowIdx >= len(rows) {
		return nil
	}
	raw, err := auth.SettingsField(rows[rowIdx].settingsKey)
	if err != nil {
		return nil
	}
	return raw
}

// configSettingValue returns the display string for one /config row —
// what the picker paints in the value column.
func configSettingValue(rowIdx int) string {
	rows := configRows()
	if rowIdx < 0 || rowIdx >= len(rows) {
		return ""
	}
	return configDisplayFor(rows[rowIdx], configSettingRaw(rowIdx))
}

// configDisplayFor maps a stored leaf back to the display form of the
// matching cycle value. A value not present in the list (e.g. a
// `toolPermission` explicitly written as "always-proceed" while the
// default is absent, or a hand-edited colour) falls back to the first
// entry, which is the reference's own behaviour.
func configDisplayFor(row configRow, raw any) string {
	// Cycle 241 (AGY 1.4.1): Business accounts see the `Enable Telemetry`
	// row painted as `off (disabled)` regardless of what settings.json
	// (or, since it's memoryOnly, what m.panel.configMemory) holds. The
	// check sits in this lowest-level display function so the test,
	// configRowValueAt, configSettingValue, and the live panel render all
	// agree on the Business-account locked display.
	if isTelemetryRow(row) && IsBusinessAccount() {
		return telemetryDisabledValue
	}
	for _, v := range row.values {
		if jsonLeafEqual(v.stored, raw) {
			return v.display
		}
	}
	// The raw value names one of the *stored* strings even though the
	// list's default entry stores nil (Tool Permission).
	if s, ok := raw.(string); ok {
		for _, v := range row.values {
			if v.display == s {
				return v.display
			}
		}
	}
	return row.values[0].display
}

// jsonLeafEqual compares two settings.json leaves for cycle matching.
// nil (key absent) only equals nil.
func jsonLeafEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a == b
}

// configVisibleRowsFor returns how many /config rows the reference draws
// at this terminal height. Measured with probe_ref_height.py:
//
//	h=20 → 3    h=24 → 6    h=28 → 9    h=32 → 12    h=40 → 18    h≥44 → all
//
// i.e. `height*3/4 - 12`, clamped to [1, len(catalogue)] — the same 3/4
// shape as helpListWindowFor, offset by the /config chrome (title,
// search box, description, keyboard legend). Returns 10 for an unknown
// height (before the first WindowSizeMsg) so the byte-exact snapshots and
// the unit tests are unchanged.
func configVisibleRowsFor(height int) int {
	if height <= 0 {
		return 10
	}
	n := height*3/4 - 12
	if total := len(configRows()); n > total {
		n = total
	}
	if n < 1 {
		n = 1
	}
	return n
}

// panelConfigLines renders the /config settings picker. Layout recovered
// from /tmp/agycmp/panels/ppref.config.raw.txt:
//   - bold "Settings" title
//   - "Search: ▮" + 20-cell `─` underline
//   - the catalogue rows; name padded to col 29, value at col 31.
//     Cursor row has both name + value in blue (38;5;69).
//   - selected-row description in dim grey 238
//   - `[W+1 of 20 items]` counter in dim grey 241 — only while the list
//     is windowed; the reference drops it when every row fits
//     (probe_ref_scroll.py at height 50 paints all 20 with no counter).
//   - Keyboard: ↑/↓ Navigate  ←/→ Change  enter Select  esc Clear Search/Exit
//
// Cycle 196 deepened the picker: values are read live from settings.json
// (auth.SettingsField) on every render, so ←/→ flips the value and
// persists before the next paint. Cycle 197 corrected the catalogue
// (all 21 rows, real keys, real value lists — see configRow) and made
// the window height-derived via configVisibleRowsFor.
func (m *runMode) panelConfigLines() []string {
	rows := configRowsFiltered(m.panel.configQuery)
	if m.panel.configSubmenu {
		return m.panelConfigSubmenuLines(rows)
	}
	visible := configVisibleRowsFor(m.height)
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
	if len(rows) == 0 {
		lines = append(lines, "  No matching settings found.", "")
		lines = append(lines, "", panelKeyboardHint("esc", "Clear Search/Exit"))
		return lines
	}
	cursor := m.panel.cursor
	if cursor < 0 || cursor >= len(rows) {
		cursor = 0
	}
	// Adjust window so cursor is always visible
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	if cursor < m.panel.window {
		m.panel.window = cursor
	}
	if cursor >= m.panel.window+visible {
		m.panel.window = cursor - visible + 1
	}
	for i := m.panel.window; i < m.panel.window+visible && i < len(rows); i++ {
		row := rows[i]
		value := m.configRowValue(row)
		pad := configNamePad(row.name, 29)
		if i == cursor {
			// Cycle 208: two measured cursor-row forms — see
			// configCursorRowIsSingleSpan.
			if configCursorRowIsSingleSpan(row.name) {
				lines = append(lines,
					cBlueOpen+"> "+row.name+strings.Repeat(" ", pad)+value+cBlueClose)
			} else {
				lines = append(lines,
					cBlueOpen+"> "+row.name+cBlueClose+
						strings.Repeat(" ", pad)+
						cBlueOpen+value+cBlueClose)
			}
		} else {
			lines = append(lines, "  "+row.name+strings.Repeat(" ", pad)+value)
		}
	}
	desc := rows[cursor].description
	// Cycle 241 (AGY 1.4.1): for the locked `Enable Telemetry` row the
	// reference substitutes a Business-account-specific description so the
	// user understands *why* the row is non-togglable. The literal text is
	// pinned in business_account.go.
	if isTelemetryRow(rows[cursor]) && IsBusinessAccount() {
		desc = telemetryBusinessDescription()
	}
	lines = append(lines, "")
	for _, wrapped := range wrapPanelText(desc, m.width) {
		lines = append(lines, "  "+panelGray238+wrapped+panelGray238C)
	}
	lines = append(lines, "")
	// The counter slot is always present: the reference blanks it when
	// every row fits rather than collapsing the line (live REF capture
	// at height 50: rows, '', desc, '', '', '', Keyboard). Cycle 208: a
	// *windowed* list carries one more blank than a complete one before
	// the counter (REF at height 24: rows, '', desc, '', '', [W+1 of N],
	// '', Keyboard).
	if visible < len(rows) {
		// The description block above already ends with one blank row;
		// the counter follows it directly (live 1.4.3 at 120x40).
		lines = append(lines,
			cDimGrayOpen+fmt.Sprintf("  [%d-%d of %d items]",
				m.panel.window+1, m.panel.window+visible, len(rows))+cDimGrayClose)
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, "", panelKeyboardHint("↑/↓", "Navigate", "←/→", "Change",
		"enter", "Select", "esc", "Clear Search/Exit"))
	return lines
}

// wrapPanelText word-wraps a /config description to width-2 content
// columns; the caller adds the two-space indent. agy 1.4.3 wraps the
// description across as many terminal rows as needed (the Agent Mode
// text fills exactly 120 columns and continues on the next row at
// 120x40), while bubbletea's renderer clips a single long line. width <= 0
// means unset (unit tests) and leaves the text unwrapped.
func wrapPanelText(s string, width int) []string {
	if width <= 2 {
		return []string{s}
	}
	avail := width - 2
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{s}
	}
	out := []string{}
	line := words[0]
	for _, word := range words[1:] {
		if utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= avail {
			line += " " + word
			continue
		}
		out = append(out, line)
		line = word
	}
	return append(out, line)
}

// cycleConfigValue advances the value of the setting the cursor is on
// by `dir` (+1 for right, -1 for left) through its cycle list, then
// persists through auth: a non-nil `stored` is written, a nil `stored`
// *deletes* the key (the reference's representation of the default).
func (m *runMode) cycleConfigValue(dir int) {
	m.cycleConfigValueFiltered(configRows(), dir)
}

// configRowValue returns the display string for a configRow, honouring
// in-memory-only rows and business account locks.
func (m *runMode) configRowValue(row configRow) string {
	if isTelemetryRow(row) && IsBusinessAccount() {
		return telemetryDisabledValue
	}
	if row.memoryOnly {
		if v, ok := m.panel.configMemory[row.settingsKey]; ok {
			return v
		}
		return row.values[0].display
	}
	raw, err := auth.SettingsField(row.settingsKey)
	if err != nil {
		return configDisplayFor(row, nil)
	}
	return configDisplayFor(row, raw)
}

// configRowValueAt is the rendered value of one /config row, honouring the
// in-memory-only rows the reference never persists.
func (m *runMode) configRowValueAt(rowIdx int) string {
	rows := configRows()
	if rowIdx < 0 || rowIdx >= len(rows) {
		return ""
	}
	return m.configRowValue(rows[rowIdx])
}

