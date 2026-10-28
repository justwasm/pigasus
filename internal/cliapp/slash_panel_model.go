package cliapp

import (
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/modelpicker"
	"github.com/justwasm/pigasus/internal/models"
)

// panelEffortLines renders the /effort panel. The slider geometry was
// recovered by driving the reference with ←/→ (/tmp/agycmp/effort_probe.py):
// three 15-cell groups, the active one replaced by a single ◉, and only
// the segment up to and including the active marker coloured.
func (m *runMode) panelEffortLines() []string {
	return rebuildEffortSlider(m.panel.cursor, m.currentEffort(), true)
}

// rebuildEffortSlider renders the /effort slider geometry with the
// active level set explicitly. Cycle 197 reuses this from
// panelModelLines so the /model picker can host its own slider at
// m.panel.effortCursor (independent of the model cursor).
//
// `curEff` is the user's currently-active effort; when `annotate` is
// true and `level` equals it, the description carries the trailing
// "  (current)" — the standalone /effort panel does that, the /model
// embed does not (ppref.model.raw.txt:25).
func effortSliderBar(level int, colours [3]string) string {
	return modelpicker.EffortSliderBar(level, colours)
}

func effortSliderLabels(level int, colours [3]string) string {
	return modelpicker.EffortSliderLabels(level, colours)
}

func rebuildEffortSlider(level, curEff int, annotate bool) []string {
	return modelpicker.RebuildEffortSlider(level, curEff, annotate)
}

// rebuildEffortSliderTiers renders the slider for a row that offers its
// own tiers — the /model embed's slider, whose width follows the row's
// tier count.
func rebuildEffortSliderTiers(level, curEff int, tiers []string, annotate bool) []string {
	return modelpicker.RebuildEffortSliderN(level, curEff, tiers, annotate)
}

// modelRowType is one picker row REF renders between the Search field
// and the effort slider.
//
// The row model itself lives in internal/modelpicker; this alias keeps
// the panel's own vocabulary (and its tests) readable.
type modelRowType = modelpicker.Row

// modelRows is the static, always-present portion of the /model picker.
var modelRows = modelpicker.Rows

// modelRowsView returns the full picker view: modelRows + any
// conditional 5.5 entries + the models the user configured. The picker
// always renders modelRows first so cursor indices 0–4 are stable across
// ADC state changes; the 5.5 rows are appended at the end when present,
// and the configured rows follow them.
func modelRowsView() []modelRowType {
	return modelpicker.RowsView()
}

// modelRows returns the rows the /model panel is showing right now: the
// full list while the search box is empty, the filtered list after that.
// Every cursor-indexed helper (the renderer, the effort slider, the
// selection) goes through it, so the cursor always points at a row that
// is on screen.
func (m *runMode) modelRows() []modelRowType {
	return modelRowsFiltered(m.panel.modelQuery)
}

// modelRowsFiltered returns the picker rows that match the search query,
// most relevant first. An empty query returns the rows unchanged.
//
// The reference matches the *display label* only: typing "cl-opus" — a
// subsequence of the catalogue id, not of the label — matches nothing
// (agy 1.4.5, live). The match is a case-insensitive *subsequence*, not
// a substring: "clauson" matches "Claude Sonnet 4.6 (Thinking)".
//
// The order is the reference's relevance order, read off live captures:
//
//   - a row the user cannot select (dim) sorts last;
//   - then the rows whose match lands on a word boundary ("Claude
//     Sonnet…" wins the query "c" over a mid-word match);
//   - then the shorter label;
//   - then the earlier first match.
//
// Each key was measured, not guessed: the query "s" puts "Claude Sonnet
// 4.6 (Thinking)" first (its "s" starts a word) and orders the rest by
// label length, and the query "4" puts "Gemini 4 Argon" last although its
// label is the shortest (it is the dim, unselectable row).
func modelRowsFiltered(query string) []modelRowType {
	return modelpicker.RowsFiltered(query)
}

// panelModelLines renders the /model picker. Byte-for-byte layout
// recovered from /tmp/agycmp/panels/ppref.model.raw.txt lines 12–28:
//
//	Switch Model
//	                        ← blank
//	  Search: ▮
//	          ────────────────────
//	                        ← blank
//	> Gemini 3.8 Flash             (current)      ← pad to col 30, gray238
//	  Claude Sonnet 4.6 (Thinking)
//	  Claude Opus 4.6 (Thinking)
//	  GPT-OSS 120B (Medium)
//	  MiniMax M3                    MiniMax M3 via gateway (openagy wrapper)
//	  Gemini 4 Argon                             ← dim gray 241
//	                        ← blank
//	  Effort  ◂        ●━━━...━◉        ▸        ← /effort bar +8 cells
//	                  low          medium          high
//	            Deepest reasoning…                ← +8 cells, no "(current)"
//	                        ← blank
//	Keyboard: ↑/↓ Navigate  ←/→ Effort  enter Select  esc Go Back
//
// Cycle 197 deepens: the `(current)` annotation is computed live from
// settings.json's "model" key (slug or label — both resolve), `←/→`
// advances m.panel.effortCursor (Gemini only; the other rows are fixed
// single-effort), and `enter` persists the selection and closes.
func renderModelPickerRow(r modelRowType, i, cursor int, isCurrent bool) string {
	return modelpicker.RenderModelPickerRow(r, i, cursor, isCurrent)
}

func (m *runMode) panelModelLines() []string {
	lines := m.panelModelHeader()
	rows := m.modelRows()
	if len(rows) == 0 {
		return m.panelModelEmpty(lines)
	}
	cursor := m.panel.cursor
	if cursor < 0 || cursor >= len(rows) {
		cursor = 0
	}
	currentSlug := resolveCatalogueID(m.panelCurrentModelLabel())
	for i, r := range rows {
		isCurrent := r.Slug != "" && r.CatalogueID(m.modelEffortCursor(r)) == currentSlug
		lines = append(lines, renderModelPickerRow(r, i, cursor, isCurrent))
	}
	return m.panelModelFooter(lines)
}

// currentModelRow returns the picker row the cursor is on.
func (m *runMode) currentModelRow() (modelRowType, bool) {
	rows := m.modelRows()
	idx := m.panel.cursor
	if idx < 0 || idx >= len(rows) {
		return modelRowType{}, false
	}
	return rows[idx], true
}

// modelEffortCursor returns the slider index a row shows.
//
// The running model's own tier wins when it belongs to the row: the
// reference's slider parks on the tier the session runs at. A row that
// is not the running model parks on its own default — the reference's
// default is high.
//
// The index is into *that row's* tier list, so a row with five tiers and
// a row with three never share a number.
func (m *runMode) modelEffortCursor(r modelRowType) int {
	if len(r.Tiers) <= 1 {
		return 0
	}
	current := m.panelCurrentModelLabel()
	curSlug := resolveCatalogueID(current)
	if curSlug != "" && models.BaseModelID(curSlug) == models.BaseModelID(r.Slug) {
		if i := r.TierIndex(models.TierFromLabel(current)); i >= 0 {
			return i
		}
	}
	if i := r.TierIndex(r.DefaultTier); i >= 0 {
		return i
	}
	return len(r.Tiers) - 1
}

// setModelCursor moves the /model picker's cursor and re-seeds the
// effort slider for the row it lands on.
//
// The re-seed matters because the slider's position is an index into the
// row's tier list: the cursor leaving the three-tier Gemini row for a
// configured model's five tiers has to move the slider too, or the
// marker would sit on the wrong tier.
//
// A row without a slider leaves the position alone — the slider is not
// on screen there, and the reference keeps the position so that coming
// back to the row restores it.
func (m *runMode) setModelCursor(idx int) {
	m.panel.cursor = idx
	r, ok := m.currentModelRow()
	if !ok || !r.HasEffortSlider() {
		return
	}
	m.panel.effortCursor = m.modelEffortCursor(r)
}

// panelModelHeader renders the picker's title and search box.
//
// The search box shows the typed text in front of the cursor block,
// exactly like the /config panel's (agy 1.4.5, live: "  Search:
// claude" + the reverse-video cell).
func (m *runMode) panelModelHeader() []string {
	searchContent := searchCursorBlock
	if m.panel.modelQuery != "" {
		searchContent = m.panel.modelQuery + searchCursorBlock
	}
	return []string{
		panelTitleOpen + "Switch Model" + panelTitleClose,
		"",
		"  Search: " + searchContent,
		"          " + searchUnderline,
		"",
	}
}

// panelModelEmpty renders the picker's no-match state.
func (m *runMode) panelModelEmpty(lines []string) []string {
	// The reference's empty-result line, byte for byte (agy 1.4.5,
	// live): the foreground is reset before the two spaces, the
	// message itself is 256-colour 60.
	return append(lines,
		"\x1b[39m  \x1b[38;5;60mNo models match \""+m.panel.modelQuery+"\"\x1b[39m",
		"",
		panelKeyboardHint("esc", "Clear search"))
}

// panelModelFooter renders the picker's legend and, when the cursor is
// on a row that has more than one effort, the embedded slider.
func (m *runMode) panelModelFooter(lines []string) []string {
	// While a search is active the reference's last legend entry reads
	// "esc Clear search" instead of "esc Go Back" (agy 1.4.5, live) —
	// Esc then clears the query and keeps the panel open.
	back := "Go Back"
	if m.panel.modelQuery != "" {
		back = "Clear search"
	}
	// Cycle 208: the slider (and its legend entry) only exist while the
	// cursor is on a row that offers more than one effort. The
	// reference's own picker has exactly one such row (the three-tier
	// Gemini); a configured model that declares tiers gets the same
	// slider, widened to fit its own tier count.
	row, ok := m.currentModelRow()
	if !ok || !row.HasEffortSlider() {
		return append(lines, "",
			panelKeyboardHint("↑/↓", "Navigate",
				"enter", "Select", "esc", back))
	}
	level := m.panel.effortCursor
	if level < 0 || level > len(row.Tiers)-1 {
		level = m.modelEffortCursor(row)
	}
	eff := rebuildEffortSliderTiers(level, effortLow, row.Tiers, false)
	return append(lines, "",
		"  Effort  "+eff[2][2:],
		"        "+eff[3],
		"            "+stripLeadingSpaces(eff[4], 4),
		"",
		panelKeyboardHint("↑/↓", "Navigate", "←/→", "Effort",
			"enter", "Select", "esc", back))
}

// resolveCatalogueID maps a settings.json "model" value (catalogue
// slug or display label) to the canonical catalogue slug. Returns ""
// for a name no catalogue carries.
func resolveCatalogueID(val string) string {
	return modelpicker.ResolveCatalogueID(val)
}

// stripLeadingSpaces drops up to n leading spaces (used to re-indent
// the borrowed /effort slider rows for the /model embed).
func stripLeadingSpaces(s string, n int) string {
	return modelpicker.StripLeadingSpaces(s, n)
}

// panelModelBaseSlug returns the catalogue ID prefix for a panel row's
// display label. Cycle 197: the panel row stores the label
// ("Gemini 3.8 Flash (High)") and the catalogue ID
// ("gemini-3.8-flash-high") is what REF persists to settings.json.
// For labels that don't carry an effort suffix (e.g. "Claude Sonnet
// 4.6 (Thinking)"), the base is the catalogue ID with the trailing
// "-thinking" stripped.
//
// Note: this is distinct from print_slash.modelBaseSlug which strips
// the effort from an *ID*. The picker works in label-space and looks
// up the matching ID first.
func panelModelBaseSlug(label string) string {
	return modelpicker.PanelModelBaseSlug(label)
}

// syncBackendForModel rebuilds the transport after a model change. The
// selected family decides the endpoint: catalogue Gemini/Claude/GPT-OSS
// go to CloudCode, MiniMax to the gateway. A nil factory (unit tests
// constructing runMode directly) leaves the backend untouched.
func (m *runMode) syncBackendForModel() {
	if m.backendFor == nil || strings.TrimSpace(m.model) == "" {
		return
	}
	if b := m.backendFor(m.model); b != nil {
		m.backend = b
	}
	// Cycle 292: a new selection puts the reference's
	// <USER_SETTINGS_CHANGE> block on the next user turn.
	m.wireSettingsPending = true
}

// cycleModelEffort advances `m.panel.effortCursor` by `dir` (+1 / -1)
// with wrap-around (low ⇆ high across the boundary, medium stays in
// the middle), then persists the new (model, effort) catalogue slug to
// settings.json and updates m.model / m.statusModel so the status row
// reflects the change on the very next frame.
//
// Cycle `model-left-right-effort` deepens cycle 197: the prior clamp
// behaviour only moved the slider — settings.json was written only on
// Enter, so a user that cycled the effort and then closed the panel
// without selecting (Esc, or selecting a different row) lost the
// preview. Now ←/→ is the change-of-record: settings.json sees the
// same slug `selectModelEffort` would write on Enter, the status row
// picks up the new tier on the next render, and the picker slider
// redraws because panelModelLines reads m.panel.effortCursor live.
//
// Only a row that offers more than one effort is multi-effort; the
// other rows are fixed and ←/→ remain inert on them — calling
// catalogueID(c) on a fixed row would still yield the row's fixed slug,
// but we never want to overwrite the user's pinned model just because
// their cursor wandered onto Claude Sonnet and they pressed an arrow.
//
// The wrap is over the row's *own* tiers: the reference's Gemini row has
// three (low ⇆ high), a configured model may have five.
func (m *runMode) cycleModelEffort(dir int) {
	row, ok := m.currentModelRow()
	if !ok || !row.HasEffortSlider() {
		return
	}
	// Wrap: the lowest tier ⇆ the highest one in either direction.
	c := m.panel.effortCursor + dir
	if c < 0 {
		c = len(row.Tiers) - 1
	} else if c > len(row.Tiers)-1 {
		c = 0
	}
	m.panel.effortCursor = c
	slug := row.CatalogueID(c)
	_ = auth.SetSettingsField("model", slug)
	if entry, ok := catalogueEntry(slug); ok {
		m.model = entry.Label
		m.statusModel = stripTier(entry.Label)
	} else {
		// Defensive: the tier ids are always in the catalogue
		// (cli.ReferenceModels pins Gemini's three, and the configured
		// models' own tier rows are listed there too), so this branch
		// is unreachable in practice. Falling back to the slug mirrors
		// selectModelEffort's custom-name fallback so we don't leak a
		// half-applied state.
		m.model = slug
		m.statusModel = stripTier(slug)
	}
	m.syncBackendForModel()
}

// selectModelEffort persists the highlighted (model, effort) as the
// new settings.json "model" key, updates m.model so the status row and
// the banner reflect the selection, and returns the display label to
// echo. Cycle 197.
//
// Persisted shape: the catalogue ID (e.g. "gemini-3.8-flash-medium") —
// the same form `agy -p /model` prints as its first column and the
// form modelAvailable() matches. m.model is set to the *label* so the
// status row renders "Gemini 3.8 Flash · medium" rather than the raw
// slug. Returns "" when the row has no catalogue ID (Gemini 4 Argon).
func (m *runMode) selectModelEffort() string {
	idx := m.panel.cursor
	rows := m.modelRows()
	if idx < 0 || idx >= len(rows) {
		return ""
	}
	row := rows[idx]
	if row.Slug == "" {
		return "" // not selectable on this account
	}
	slug := row.CatalogueID(m.panel.effortCursor)
	_ = auth.SetSettingsField("model", slug)
	if entry, ok := catalogueEntry(slug); ok {
		m.model = entry.Label
		m.statusModel = stripTier(entry.Label)
		m.syncBackendForModel()
		return entry.Label
	}
	// Custom-name row (MiniMax M3 etc.): the slug IS the label.
	m.model = slug
	m.statusModel = stripTier(slug)
	m.syncBackendForModel()
	return slug
}

// catalogueEntry looks up the catalogue by slug. Used by
// selectModelEffort to derive the display label from a chosen row's
// catalogue ID — a name like "minimax-m3" maps to "MiniMax M3".
func catalogueEntry(slug string) (cli.ModelEntry, bool) {
	return modelpicker.CatalogueEntry(slug)
}
