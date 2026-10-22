package main

import (
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
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
	on, off := effortBarSegments(level)
	leftArrow := panelGray238 + "◂" + panelGray238C
	if level > effortLow {
		leftArrow = cBlueOpen + "◂" + cBlueClose
	}
	rightArrow := cBlueOpen + "▸" + cBlueClose
	if level == effortHigh {
		rightArrow = panelGray238 + "▸" + panelGray238C
	}
	return "  " + leftArrow + " " + colours[level] + on +
		panelGray238 + off + panelGray238C + " " + rightArrow
}

func effortSliderLabels(level int, colours [3]string) string {
	groups := [3]string{"      low      ", "    medium     ", "     high      "}
	var labels strings.Builder
	labels.WriteString("    ")
	if level > effortLow {
		labels.WriteString(panelGray238)
		for i := 0; i < level; i++ {
			labels.WriteString(groups[i])
		}
	}
	labels.WriteString("\x1b[1m")
	labels.WriteString(colours[level])
	labels.WriteString(groups[level])
	labels.WriteString("\x1b[0m")
	if level < effortHigh {
		labels.WriteString(panelGray238)
		for i := level + 1; i < 3; i++ {
			labels.WriteString(groups[i])
		}
		labels.WriteString(panelGray238C)
	}
	return labels.String()
}

func rebuildEffortSlider(level, curEff int, annotate bool) []string {
	if level < 0 || level > 2 {
		level = effortHigh
	}
	colours := [3]string{panelEffLowOpen, panelEffMedOpen, panelEffHighOpen}
	slider := effortSliderBar(level, colours)
	labels := effortSliderLabels(level, colours)

	desc := "    \x1b[3m" + panelGray238 + effortDescriptions[level]
	if annotate && level == curEff {
		desc += "  (current)"
	}
	desc += "\x1b[0m"

	return []string{
		panelTitleOpen + "Set Effort" + panelTitleClose,
		"",
		slider,
		labels,
		desc,
		"",
		panelKeyboardHint("←/→", "Effort", "enter", "Select", "esc", "Go Back"),
	}
}

// modelRowType is one picker row REF renders between the Search field
// and the effort slider. `display` is the label-column text REF renders:
// the Gemini row drops the "(High)" tier (the tier lives on the effort
// slider instead), the dim "Gemini 4 Argon" row is drawn in gray 241
// (unavailable on this account — see panelModelLines). `slug` is the
// catalogue ID prefix; the effort suffix is appended from the slider.
type modelRowType struct {
	display string // label column, verbatim from REF
	slug    string // catalogue base ID (no effort suffix)
	dim     bool   // row is drawn in dim gray 241
	note    string // optional trailing description (custom registry models)
}

// modelRows is the static, always-present portion of the /model picker.
// Order is the live 1.4.3 list captured at 120x40, with the clone-only
// MiniMax M3 row kept before the unavailable Argon placeholder: Gemini
// 3.8 Flash, Claude Sonnet 4.6, Claude Opus 4.6, GPT-OSS 120B,
// MiniMax M3, Gemini 4 Argon. The reference does not advertise MiniMax
// (it remains selectable with --model minimax-m3), but the agw gateway
// serves it and the picker deliberately keeps it available.
// Cycle model-catalog-claude-55 adds Claude Opus 5.5 / Claude Sonnet 5.5
// as a conditional suffix via modelRows55(); the picker iterates the
// concatenation (see modelRowsView).
var modelRows = []modelRowType{
	{"Gemini 3.8 Flash", "gemini-3.8-flash", false, ""},
	{"Claude Sonnet 4.6 (Thinking)", "claude-sonnet-4-6", false, ""},
	{"Claude Opus 4.6 (Thinking)", "claude-opus-4-6-thinking", false, ""},
	{"GPT-OSS 120B (Medium)", "gpt-oss-120b-medium", false, ""},
	{"MiniMax M3", "minimax-m3", false, "MiniMax M3 via agw (openagy wrapper)"},
	{"Gemini 4 Argon", "", true, ""}, // slug unknown (not in `agy models`); cannot be selected
}

// modelRows55 returns the conditional Claude Opus 5.5 / Claude Sonnet
// 5.5 picker rows. Empty unless cli.Claude55Available() is true —
// the AGY 1.3.6 changelog line 1 contract ("Claude models now appear
// in /model only if your Google Cloud project can use them").
//
// Returning a fresh slice on each call lets tests toggle the env
// vars between renders without restarting the binary, and avoids
// mutating the caller's backing array.
func modelRows55() []modelRowType {
	if !cli.Claude55Available() {
		return nil
	}
	return []modelRowType{
		{"Claude Opus 5.5", "claude-opus-5-5", false, ""},
		{"Claude Sonnet 5.5", "claude-sonnet-5-5", false, ""},
	}
}

// modelRowsView returns the full picker view: modelRows + any
// conditional 5.5 entries. The picker always renders modelRows first
// so cursor indices 0–6 are stable across ADC state changes; the 5.5
// rows are appended at the end (indices 7, 8) when present.
func modelRowsView() []modelRowType {
	extra := modelRows55()
	if len(extra) == 0 {
		return modelRows
	}
	out := make([]modelRowType, 0, len(modelRows)+len(extra))
	out = append(out, modelRows...)
	out = append(out, extra...)
	return out
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
//	  MiniMax M3                    MiniMax M3 via agw (openagy wrapper)
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
	var row string
	ink := ""
	if r.dim {
		ink = cDimGrayOpen
	}
	if i == cursor {
		row = cBlueOpen + "> " + r.display
		if isCurrent {
			row += strings.Repeat(" ", modelRowPad(r.display))
		}
		row += cBlueClose
		if isCurrent {
			row += panelGray238 + " (current)" + panelGray238C
		}
	} else {
		row = "  " + ink + r.display
		if ink != "" {
			row += cDimGrayClose
		}
		if isCurrent {
			row += strings.Repeat(" ", modelRowPad(r.display)) +
				panelGray238 + " (current)" + panelGray238C
		}
	}
	if r.note != "" {
		row += strings.Repeat(" ", modelRowPad(r.display)+2) +
			"\x1b[38;5;60m" + r.note + "\x1b[39m"
	}
	return row
}

func (m *runMode) panelModelLines() []string {
	lines := []string{
		panelTitleOpen + "Switch Model" + panelTitleClose,
		"",
		"  Search: " + searchCursorBlock,
		"          " + searchUnderline,
		"",
	}
	cursor := m.panel.cursor
	rows := modelRowsView()
	if cursor < 0 || cursor >= len(rows) {
		cursor = 0
	}
	currentSlug := resolveCatalogueID(m.panelCurrentModelLabel())
	for i, r := range rows {
		isCurrent := r.slug != "" && r.catalogueID(m.panel.effortCursor) == currentSlug
		lines = append(lines, renderModelPickerRow(r, i, cursor, isCurrent))
	}
	// Cycle 208: the slider (and its legend entry) only exist while the
	// cursor is on the three-tier Gemini row.
	if !m.modelRowHasEffortSlider() {
		lines = append(lines, "",
			panelKeyboardHint("↑/↓", "Navigate",
				"enter", "Select", "esc", "Go Back"))
		return lines
	}
	level := m.panel.effortCursor
	if level < 0 || level > 2 {
		level = 1
	}
	eff := rebuildEffortSlider(level, effortLow, false)
	lines = append(lines, "",
		"  Effort  "+eff[2][2:],
		"        "+eff[3],
		"            "+stripLeadingSpaces(eff[4], 4),
		"",
		panelKeyboardHint("↑/↓", "Navigate", "←/→", "Effort",
			"enter", "Select", "esc", "Go Back"))
	return lines
}

// catalogueID returns the full catalogue ID a picker row represents at
// the given effort cursor. Only the three-tier Gemini encodes the
// cursor; the other rows are fixed IDs.
func (r modelRowType) catalogueID(effortCursor int) string {
	if r.slug == "gemini-3.8-flash" {
		return r.slug + "-" + effortSlugForModelCursor(r.slug, effortCursor)
	}
	return r.slug
}

// resolveCatalogueID maps a settings.json "model" value (catalogue
// slug or display label) to the canonical catalogue slug. Returns ""
// for custom models such as "MiniMax-M3".
func resolveCatalogueID(val string) string {
	val = strings.TrimSpace(val)
	for _, e := range cli.ReferenceModels() {
		if e.ID == val || e.Label == val {
			return e.ID
		}
	}
	return ""
}

// stripLeadingSpaces drops up to n leading spaces (used to re-indent
// the borrowed /effort slider rows for the /model embed).
func stripLeadingSpaces(s string, n int) string {
	for n > 0 && len(s) > 0 && s[0] == ' ' {
		s = s[1:]
		n--
	}
	return s
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
	for _, m := range cli.ReferenceModels() {
		if m.Label == label {
			id := m.ID
			for _, suf := range []string{"-high", "-medium", "-low", "-thinking"} {
				if strings.HasSuffix(id, suf) {
					return strings.TrimSuffix(id, suf)
				}
			}
			return id
		}
	}
	return label
}

// effortSlugForModelCursor maps (base, cursor) to the effort slug that
// belongs in the catalogue ID. For Gemini (low/medium/high) the cursor
// directly indexes; for Claude/GPT-OSS the cursor is forced to "medium".
func effortSlugForModelCursor(base string, cursor int) string {
	switch base {
	case "gemini-3.8-flash":
		switch cursor {
		case 0:
			return "low"
		case 2:
			return "high"
		default:
			return "medium"
		}
	default:
		return "medium"
	}
}

// syncBackendForModel rebuilds the transport after a model change. The
// selected family decides the endpoint: catalogue Gemini/Claude/GPT-OSS
// go to CloudCode, MiniMax to the agw gateway. A nil factory (unit tests
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
// Only the three-tier Gemini row is multi-effort; the other rows are
// fixed and ←/→ remain inert on them — calling catalogueID(c) on a
// non-Gemini row would still yield the row's fixed slug, but we never
// want to overwrite the user's pinned model just because their cursor
// wandered onto Claude Sonnet and they pressed an arrow.
func (m *runMode) cycleModelEffort(dir int) {
	idx := m.panel.cursor
	rows := modelRowsView()
	if idx < 0 || idx >= len(rows) {
		return
	}
	row := rows[idx]
	if row.slug != "gemini-3.8-flash" {
		return
	}
	// 3-tier wrap: low → medium → high → low → … in either direction.
	c := m.panel.effortCursor + dir
	if c < 0 {
		c = 2
	} else if c > 2 {
		c = 0
	}
	m.panel.effortCursor = c
	slug := row.catalogueID(c)
	_ = auth.SetSettingsField("model", slug)
	if entry, ok := catalogueEntry(slug); ok {
		m.model = entry.Label
		m.statusModel = stripTier(entry.Label)
	} else {
		// Defensive: the Gemini-low/medium/high IDs are always in the
		// catalogue (cli.ReferenceModels pins all three), so this
		// branch is unreachable in practice. Falling back to the slug
		// mirrors selectModelEffort's custom-name fallback so we don't
		// leak a half-applied state.
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
	rows := modelRowsView()
	if idx < 0 || idx >= len(rows) {
		return ""
	}
	row := rows[idx]
	if row.slug == "" {
		return "" // not selectable on this account
	}
	slug := row.catalogueID(m.panel.effortCursor)
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
	for _, m := range cli.ReferenceModels() {
		if m.ID == slug {
			return m, true
		}
	}
	return cli.ModelEntry{}, false
}

