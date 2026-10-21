// slash_regression.go — cycle 208: the four panels that cycles 196/197/
// 201/202 declared "done" (/config, /model, /effort, /feedback) were
// re-driven against the live reference (agy 1.4.0, Mach-O arm64) at
// 220×50 and 220×24 through a private tmux socket. This file holds the
// fixes for the divergences that run turned up; slash_panels.go keeps
// only one-line call sites so the diff stays small.
//
// Every rule below is a *measurement*, not an inference. The captures
// live in the cycle-208 probe directory (fin_*.raw.txt / our_*.raw.txt):
//
//	/config  row 13 `Editor Mode › Insert First` — the reference pads the
//	         name column by RUNE count; Go's len() counts bytes and the
//	         `›` is three of them, so the clone was two cells short.
//	/config  a windowed list (visible < len(rows)) puts an extra blank
//	         line between the description and the `[W+1 of N items]`
//	         counter that a non-windowed list does not.
//	/model   the effort slider + the `←/→ Effort` legend exist ONLY while
//	         the cursor sits on the three-tier Gemini row; the cursor row
//	         is padded to column 30 only when it carries the `(current)`
//	         annotation.
//	/effort  the active level's ◉ ends the coloured run — the 14 cells
//	         that follow it are dim, not coloured.
//	/effort  Enter applies the highlighted level: unchanged → the panel
//	         closes silently, changed → `⎿  Effort set to <level>`, the
//	         banner tier and the status suffix follow, and the reference
//	         persists the model *label* to settings.json.
//	/feedback stage 2 is the "Review Submission Data" form: the legend is
//	         ` ctrl+s Submit Feedback   esc Back`, and Enter does not
//	         submit.
package main

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
)

// panelGray241 is the dim grey the reference uses for the secondary half
// of the /feedback stage-2 legend (`esc Back`) and the title's parenthetical.
const (
	panelGray241  = "\x1b[38;2;101;123;131m"
	panelGray241C = "\x1b[39m"
)

// effortLevelNames maps the slider index onto the word the reference
// prints in `⎿  Effort set to <word>`.
var effortLevelNames = [3]string{"low", "medium", "high"}

// effortLevelName returns the word for a slider index, clamped to the
// three known levels.
func effortLevelName(level int) string {
	if level < effortLow || level > effortHigh {
		level = effortHigh
	}
	return effortLevelNames[level]
}

// parseEffortWord parses the user's typed level into a canonical word.
// It accepts "low", "medium", "high" (case-insensitive); anything else —
// including the cycle-101 invalid input "extreme" — is rejected so the
// caller can echo the matching error and leave the run untouched.
//
// The returned word is the lowercase canonical form, identical to the
// CLI's --effort vocabulary (so the flag set accepts it without further
// normalisation).
func parseEffortWord(input string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "low":
		return "low", true
	case "medium":
		return "medium", true
	case "high":
		return "high", true
	}
	return "", false
}

// effortIndexForName returns the slider index for a canonical level
// word. The mapping is the same one effortLevelName uses in reverse; an
// out-of-vocab word falls back to high so the picker never indexes off
// the end of the bar.
func effortIndexForName(name string) int {
	switch name {
	case "low":
		return effortLow
	case "medium":
		return effortMedium
	}
	return effortHigh
}

// statusEffortSuffix is the right-hand status row's " · <effort>". The
// reference derives it from the *live* effort (verified: after Enter on
// /effort at medium the row reads `Gemini 3.8 Flash · medium`), so the
// clone can no longer use a constant.
func (m *runMode) statusEffortSuffix() string {
	// MiniMax M3 has no effort tiers — the reference never prints
	// " · high" / " · medium" for that model, so the suffix is empty.
	if isMiniMax(m.statusModel) {
		return ""
	}
	return " \u00b7 " + effortLevelName(m.currentEffort())
}

// effortBarSegments builds the two coloured runs of the /effort slider
// for `level`: the lit `on` run (7-cell lead, every completed segment,
// then the active ◉) and the dim `off` run (the 14 cells that follow the
// ◉, the remaining markers and the trailing 7 cells).
//
// Cycle 208 correction: the previous code kept the 14 cells after the
// active marker inside the lit run, so the colour boundary sat one
// segment too far right at low/medium (REF `…◉\x1b[38;2;88;110;117m──…` vs clone
// `…◉──…\x1b[38;2;88;110;117m○`).
func effortBarSegments(level int) (on, off string) {
	if level < effortLow || level > effortHigh {
		level = effortHigh
	}
	marker := func(i int) string {
		switch {
		case i < level:
			return "●"
		case i == level:
			return "◉"
		}
		return "○"
	}
	var lit, dim strings.Builder
	lit.WriteString(strings.Repeat(" ", 7))
	for i := 0; i < 3; i++ {
		if i <= level {
			lit.WriteString(marker(i))
		} else {
			dim.WriteString(marker(i))
		}
		if i < 2 {
			// The connecting bar is lit only while it runs *towards* the
			// active marker.
			if i < level {
				lit.WriteString(strings.Repeat("━", 14))
			} else {
				dim.WriteString(strings.Repeat("─", 14))
			}
		}
	}
	dim.WriteString(strings.Repeat(" ", 7))
	return lit.String(), dim.String()
}

// modelRowPad is the space count between a /model row's label and its
// `(current)` annotation so the annotation starts at column 30
// (2 = the `  `/`> ` gutter). Rune-based, so a row with a multi-byte
// label pads correctly.
func modelRowPad(display string) int {
	pad := 30 - 2 - utf8.RuneCountInString(display)
	if pad < 1 {
		pad = 1
	}
	return pad
}

// modelRowHasEffortSlider reports whether the /model cursor is on a row
// that has more than one effort. Only the three-tier Gemini row does; the
// reference hides the whole slider block (and the `←/→ Effort` legend
// entry) on Claude/GPT-OSS/Argon (fin_model_down.raw.txt rows 10–11).
func (m *runMode) modelRowHasEffortSlider() bool {
	idx := m.panel.cursor
	rows := modelRowsView()
	if idx < 0 || idx >= len(rows) {
		idx = 0
	}
	return rows[idx].slug == "gemini-3.8-flash"
}

// configNamePad is the padding between a /config row's name and its value.
// The reference aligns the value column at 29 characters counted in
// *runes*; the nested row `Editor Mode › Insert First` is 26 runes but 28
// bytes, so a byte count leaves it two cells short (fin config capture,
// row 13).
func configNamePad(name string, col int) int {
	pad := col - utf8.RuneCountInString(name)
	if pad < 1 {
		pad = 1
	}
	return pad
}

// configCursorFieldCols is the name-field width at which the /config
// cursor row switches from "blue name span + uncoloured pad + blue value
// span" to one blue span covering the whole row. Measured across all 21
// rows in two independent runs (harness/cfg_cursor.py): every name of 16
// or more runes renders as a single span, every name of 15 or fewer keeps
// the pad uncoloured. The mechanism behind the 16-cell boundary was not
// recovered — see the cycle-208 report's 存疑 section.
const configCursorFieldCols = 16

// configCursorRowIsSingleSpan reports which of the two measured cursor-row
// forms the reference uses for this row.
func configCursorRowIsSingleSpan(name string) bool {
	return utf8.RuneCountInString(name) >= configCursorFieldCols
}

// modelLabelForEffort returns the catalogue display label of the model the
// current session would run at `level`, or "" when the current model has
// no tiered catalogue entry (a custom name, or a model whose id takes no
// effort suffix).
func (m *runMode) modelLabelForEffort(level int) string {
	label := m.panelCurrentModelLabel()
	base := panelModelBaseSlug(label)
	if base == "" || base == label {
		return "" // not in the catalogue — no tier to name
	}
	want := base + "-" + effortLevelName(level)
	for _, e := range cli.ReferenceModels() {
		if e.ID == want {
			return e.Label
		}
	}
	return ""
}

// applyEffortSelection is the /effort panel's Enter action. Measured
// against the reference:
//
//	Enter at the already-active level  → panel closes, no transcript row,
//	                                     nothing written to settings.json
//	Enter at a different level        → `> /effort` + `⎿  Effort set to
//	                                     <level>`, the banner tier and the
//	                                     status suffix move to the new
//	                                     level, and settings.json's "model"
//	                                     becomes the model *label* (e.g.
//	                                     "Gemini 3.8 Flash (Medium)").
//
// The "unchanged ⇒ silent" branch is why the reference leaves an
// unavailable settings model (e.g. "MiniMax-M3") in place when you apply
// the fallback's own tier.
func (m *runMode) applyEffortSelection(level int) tea.Cmd {
	name := effortLevelName(level)
	return m.applyEffortLevel(name, level)
}

// applyEffortLevel is the shared plumbing the picker Enter and the
// /effort <level> shortcut both call into. Cycle effort-picker:
//
//   - `name` is the canonical lowercase word ("low" | "medium" | "high");
//     callers should run parseEffortWord first when the word came from
//     user input.
//   - `level` is the slider index (0..2) for change-detection; pass the
//     same value the name came from so the persistence path is one
//     branch.
//   - The function never returns an error: invalid input has already been
//     rejected by the caller; persistence errors are swallowed because the
//     user-typed echo must survive a read-only settings file.
func (m *runMode) applyEffortLevel(name string, level int) tea.Cmd {
	// Persist against the *effective* model the session started with.
	//
	// Cycle 99 / 99b: when settings.json pins "MiniMax-M3" (a model
	// the reference catalogue no longer offers), the (current)
	// running model is the catalogue fallback "Gemini 3.8 Flash
	// (High)" — not the configured value. That is what the panel
	// should treat as "before" for change-detection.
	//
	// Cycle 208: applying the same level as the running model is a
	// no-op — the panel closes silently and settings.json is left
	// untouched. The reference's "MiniMax-M3" warning stays in place
	// so the banner keeps reading "Gemini 3.8 Flash (High)" until
	// the user picks a catalogue model explicitly.
	before, _ := resolveEffectiveModel("")
	if isMiniMax(before) {
		// The configured-only model resolved to its label; re-resolve
		// without MiniMax short-circuit so the (running) fallback
		// comes through instead. We do this by clearing the
		// settings.json key temporarily, which would persist; avoid
		// that by checking the catalogue directly.
		//
		// In practice: when settings.json pins MiniMax, the reference
		// is already running Gemini 3.8 Flash (High), and we want
		// `before` to be that label so applying Medium is a real
		// change.
		before = "Gemini 3.8 Flash (High)"
	}
	label := m.modelLabelForEffort(level)
	if label == "" {
		// Catalogue miss — derive from the fallback slug so the
		// (current) model's effort tier still resolves.
		base := panelModelBaseSlug(before)
		if base != "" {
			want := base + "-" + name
			for _, e := range cli.ReferenceModels() {
				if e.ID == want {
					label = e.Label
					break
				}
			}
		}
	}
	changed := label != "" && label != before

	// The session's effort lives on the flag set (currentEffort reads it).
	if m.flags != nil {
		_ = m.flags.Set("effort", name)
	}

	// The picker path closes the panel; the /effort <level> path has
	// nothing open to close. The early-return-after-mutation pattern
	// keeps both callers' semantics identical.
	if m.panel.kind == panelEffort {
		m.panel = slashPanel{}
	}
	if !changed {
		return nil
	}
	if strings.TrimSpace(m.model) == "" {
		m.model = before
		m.statusModel = stripTier(before)
	}
	m.model = label
	m.statusModel = stripTier(label)
	m.syncBackendForModel()
	_ = auth.SetSettingsField("model", label)
	m.response = append(m.response, slashEchoLines("/effort", "Effort set to "+name)...)
	return nil
}

// feedbackCategoryLabel drops the `N. ` picker index from a category row:
// the reference's stage-2 `Feedback:` line echoes the label alone
// (fin_fb_stage2.raw.txt has "Feedback: Code doesn't work (e.g., syntax
// errors, bad logic)") while stage 1 lists it as "1. Code doesn't work…".
func feedbackCategoryLabel(cat string) string {
	i := strings.Index(cat, ". ")
	if i <= 0 || i > 3 {
		return cat
	}
	for _, r := range cat[:i] {
		if r < '0' || r > '9' {
			return cat
		}
	}
	return cat[i+2:]
}

// feedbackStage2Lines renders the /feedback free-text form. Until cycle
// 208 this was marked 【推测】: the earlier probes could not reach REF's
// second stage. It has now been captured (fin_fb_stage2.raw.txt):
//
//	\x1b[1m\x1b[39mReview Submission Data\x1b[0m  \x1b[38;2;101;123;131m(You can always provide manual feedback via /feedback)\x1b[39m
//
//	Your feedback helps improve Antigravity CLI. The following sanitized data will be sent:
//
//	Feedback: <category in brand blue>
//
//	Additional Details (optional):
//	<buffer><cursor block>
//	(3 blank rows)
//	\x1b[7m\x1b[38;2;133;153;0m ctrl+s Submit Feedback \x1b[0m  \x1b[38;2;101;123;131mesc Back\x1b[39m
func (m *runMode) feedbackStage2Lines() []string {
	cursor := m.panel.cursor
	if cursor < 0 || cursor >= len(feedbackCategories) {
		cursor = 0
	}
	return []string{
		panelTitleOpen + "Review Submission Data" + panelTitleClose + "  " +
			panelGray241 + "(You can always provide manual feedback via /feedback)" + panelGray241C,
		"",
		"Your feedback helps improve Pigasus CLI. The following sanitized data will be sent:",
		"",
		"Feedback: " + cBlueOpen + feedbackCategoryLabel(feedbackCategories[cursor]) + cBlueClose,
		"",
		"Additional Details (optional):",
		m.panel.text + searchCursorBlock,
		"",
		"",
		"",
		"\x1b[7m\x1b[38;2;133;153;0m ctrl+s Submit Feedback \x1b[0m  " +
			panelGray241 + "esc Back" + panelGray241C,
	}
}
