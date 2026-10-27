package cliapp

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/configpanel"
	"github.com/justwasm/pigasus/internal/seasonal"
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
//
// The catalogue itself lives in internal/configpanel; these aliases keep
// the panel's own vocabulary.
type cfgVal = configpanel.Val

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
type configRow = configpanel.Row

// configSettingsRows is the static catalogue. Rows whose showWhen gate is
// not met are dropped by configRows().
var configSettingsRows = configpanel.SettingsRows

// configRows returns the catalogue filtered by the dynamic gates — today
// only the "Editor Mode › Insert First" child row, which the reference
// draws immediately after Editor Mode while editorMode == "vim"
// (probe_ref_walk2.py row 8).
func configRows() []configRow {
	out := make([]configRow, 0, len(configSettingsRows)+1)
	for _, r := range configSettingsRows {
		if r.ShowWhenKey != "" {
			if raw, _ := auth.SettingsField(r.ShowWhenKey); rawString(raw) != r.ShowWhenVal {
				continue
			}
		}
		out = append(out, r)
	}
	// The seasonal row is drawn only while a seasonal event is in
	// season, so it is not part of the static catalogue.
	if ev := seasonalEvent(); ev != nil {
		out = insertSeasonalRow(out, seasonalConfigRow(ev))
	}
	return out
}

// seasonalConfigRow is the "Seasonal Themes" row.
//
// 【一手】 agy 1.4.7's /config (out/changelog-147/ref/config.txt):
//
//	Seasonal Themes              on                 seasonalThemesOptIn
//
// The row's help line is verbatim in seasonal.RowDescription, its two
// stops read "off" and "on", and switching it on writes the event's own
// id into settings.json ("seasonalThemesOptIn": "halloween-2026").
// Switching it off deletes the key again — the reference's own
// "a setting at its default is absent" convention.
func seasonalConfigRow(ev *seasonal.Event) configRow {
	return configRow{
		Name:        seasonal.RowName,
		SettingsKey: seasonal.OptInKey,
		Values: []cfgVal{
			configpanel.V("off", ""),
			configpanel.V("on", ev.ID),
		},
		Description: seasonal.RowDescription,
	}
}

// insertSeasonalRow puts the seasonal row where the reference draws it:
// between "Sandbox Mode" and "Show Active Tasks".
func insertSeasonalRow(rows []configRow, row configRow) []configRow {
	for i, r := range rows {
		if r.Name != "Show Active Tasks" {
			continue
		}
		out := make([]configRow, 0, len(rows)+1)
		out = append(out, rows[:i]...)
		out = append(out, row)
		out = append(out, rows[i:]...)
		return out
	}
	return append(rows, row)
}

// rawString renders a settings.json leaf as the string form used for
// gating comparisons.
func rawString(v any) string {
	return configpanel.RawString(v)
}

// configSettingRaw returns the JSON leaf exactly as REF would compare it:
// the stored value, or nil when the key is absent (the default state).
func configSettingRaw(rowIdx int) any {
	rows := configRows()
	if rowIdx < 0 || rowIdx >= len(rows) {
		return nil
	}
	raw, err := auth.SettingsField(rows[rowIdx].SettingsKey)
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
	locked := ""
	if IsBusinessAccount() {
		locked = telemetryDisabledValue
	}
	return configpanel.DisplayFor(row, raw, locked)
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
	return configpanel.VisibleRowsFor(height, len(configRows()))
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
func configAdjustWindow(m *runMode, cursor, visible, total int) {
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	if cursor < m.panel.window {
		m.panel.window = cursor
	}
	if cursor >= m.panel.window+visible {
		m.panel.window = cursor - visible + 1
	}
}

func configRenderItemRow(row configRow, value string, isCursor bool) string {
	pad := configNamePad(row.Name, 29)
	if !isCursor {
		return "  " + row.Name + strings.Repeat(" ", pad) + value
	}
	if configCursorRowIsSingleSpan(row.Name) {
		return cBlueOpen + "> " + row.Name + strings.Repeat(" ", pad) + value + cBlueClose
	}
	return cBlueOpen + "> " + row.Name + cBlueClose +
		strings.Repeat(" ", pad) +
		cBlueOpen + value + cBlueClose
}

func configDescriptionLines(desc string, width int) []string {
	var lines []string
	lines = append(lines, "")
	for _, wrapped := range wrapPanelText(desc, width) {
		lines = append(lines, "  "+panelGray238+wrapped+panelGray238C)
	}
	lines = append(lines, "")
	return lines
}

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
		lines = append(lines, "  No matching settings found.", "", "",
			panelKeyboardHint("esc", "Clear Search/Exit"))
		return lines
	}
	cursor := m.panel.cursor
	if cursor < 0 || cursor >= len(rows) {
		cursor = 0
	}
	configAdjustWindow(m, cursor, visible, len(rows))
	for i := m.panel.window; i < m.panel.window+visible && i < len(rows); i++ {
		lines = append(lines, configRenderItemRow(rows[i], m.configRowValue(rows[i]), i == cursor))
	}
	desc := rows[cursor].Description
	if isTelemetryRow(rows[cursor]) && IsBusinessAccount() {
		desc = telemetryBusinessDescription()
	}
	lines = append(lines, configDescriptionLines(desc, m.width)...)
	if visible < len(rows) {
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
	return configpanel.WrapText(s, width)
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
	if row.MemoryOnly {
		if v, ok := m.panel.configMemory[row.SettingsKey]; ok {
			return v
		}
		return row.Values[0].Display
	}
	raw, err := auth.SettingsField(row.SettingsKey)
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
