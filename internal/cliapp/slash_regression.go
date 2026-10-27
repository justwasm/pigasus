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
package cliapp

import (
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/configpanel"
	"github.com/justwasm/pigasus/internal/diffaction"
	"github.com/justwasm/pigasus/internal/editorkit"
	"github.com/justwasm/pigasus/internal/modelcatalog"
	"github.com/justwasm/pigasus/internal/modelpicker"
)

// panelGray241 is the dim grey the reference uses for the secondary half
// of the /feedback stage-2 legend (`esc Back`) and the title's parenthetical.
const (
	panelGray241  = "\x1b[38;2;101;123;131m"
	panelGray241C = "\x1b[39m"
)

// effortLevelName returns the word for a slider index, clamped to the
// three known levels.
func effortLevelName(level int) string {
	return modelpicker.EffortLevelName(level)
}

// effortIndexForName returns the slider index for a canonical level
// word.
func effortIndexForName(name string) int {
	return modelpicker.EffortIndexForName(name)
}

// statusEffortSuffix is the right-hand status row's " · <effort>". The
// reference derives it from the *live* effort (verified: after Enter on
// /effort at medium the row reads `Gemini 3.8 Flash · medium`), so the
// clone can no longer use a constant.
func (m *runMode) statusEffortSuffix() string {
	// A model the reference publishes no effort tiers for carries no
	// suffix: the reference never prints " · high" / " · medium" for it,
	// and the tiers belong to the reference's own catalogue.
	if !hasEffortTiers(m.statusModel) {
		return ""
	}
	return " \u00b7 " + effortLevelName(m.currentEffort())
}

// hasEffortTiers reports whether a model carries the reference's effort
// tiers (low / medium / high).
func hasEffortTiers(name string) bool {
	return modelpicker.HasEffortTiers(name)
}

// effortBarSegments builds the two coloured runs of the /effort slider
// for `level`.
func effortBarSegments(level int) (on, off string) {
	return modelpicker.EffortBarSegments(level)
}

// modelRowHasEffortSlider reports whether the /model cursor is on a row
// that has more than one effort. Only the three-tier Gemini row does; the
// reference hides the whole slider block (and the `←/→ Effort` legend
// entry) on Claude/GPT-OSS/Argon (fin_model_down.raw.txt rows 10–11).
func (m *runMode) modelRowHasEffortSlider() bool {
	idx := m.panel.cursor
	rows := m.modelRows()
	if idx < 0 || idx >= len(rows) {
		idx = 0
	}
	return rows[idx].Slug == "gemini-3.8-flash"
}

// configNamePad is the padding between a /config row's name and its value.
// The reference aligns the value column at 29 characters counted in
// *runes*; the nested row `Editor Mode › Insert First` is 26 runes but 28
// bytes, so a byte count leaves it two cells short (fin config capture,
// row 13).
func configNamePad(name string, col int) int {
	return configpanel.NamePad(name, col)
}

// configCursorFieldCols is the name-field width at which the /config
// cursor row switches from "blue name span + uncoloured pad + blue value
// span" to one blue span covering the whole row. Measured across all 21
// rows in two independent runs (harness/cfg_cursor.py): every name of 16
// or more runes renders as a single span, every name of 15 or fewer keeps
// the pad uncoloured. The mechanism behind the 16-cell boundary was not
// recovered — see the cycle-208 report's 存疑 section.
const configCursorFieldCols = configpanel.CursorFieldCols

// configCursorRowIsSingleSpan reports which of the two measured cursor-row
// forms the reference uses for this row.
func configCursorRowIsSingleSpan(name string) bool {
	return configpanel.CursorRowIsSingleSpan(name)
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
//     callers should run printmode.ParseEffortWord first when the word came from
//     user input.
//   - `level` is the slider index (0..2) for change-detection; pass the
//     same value the name came from so the persistence path is one
//     branch.
//   - The function never returns an error: invalid input has already been
//     rejected by the caller; persistence errors are swallowed because the
//     user-typed echo must survive a read-only settings file.
func resolveEffortBeforeModel() string {
	before, _ := modelcatalog.ResolveEffectiveModel("")
	if !hasEffortTiers(before) {
		// The model resolved to a label the reference publishes no tier
		// for — a model the user configured. The reference cannot run
		// one, so the (running) fallback comes through instead, and
		// applying a tier is then a real change.
		//
		// In practice: when settings.json pins a configured model, the
		// reference is already running Gemini 3.8 Flash (High), and we
		// want `before` to be that label.
		return modelcatalog.DefaultModel
	}
	return before
}

func (m *runMode) resolveTargetEffortLabel(name string, level int, before string) string {
	label := m.modelLabelForEffort(level)
	if label != "" {
		return label
	}
	// Catalogue miss — derive from the fallback slug so the
	// (current) model's effort tier still resolves.
	base := panelModelBaseSlug(before)
	if base != "" {
		want := base + "-" + name
		for _, e := range cli.ReferenceModels() {
			if e.ID == want {
				return e.Label
			}
		}
	}
	return ""
}

func (m *runMode) applyEffortLevel(name string, level int) tea.Cmd {
	before := resolveEffortBeforeModel()
	label := m.resolveTargetEffortLabel(name, level, before)
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

// detectVCS returns the version-control system that owns dir, or "" when there
// is none. Recovered: the reference supports Git, Mercurial and Jujutsu for
// /diff (the 1.3.4 changelog names all three, and the enum carries
// VCS_TYPE_GIT / VCS_TYPE_JJ).
func detectVCS(dir string) string {
	return diffaction.DetectVCS(dir)
}

func pathExists(p string) bool {
	return diffaction.PathExists(p)
}

// vcsDiff runs the working-copy diff. Pagers and colour are disabled the way
// the reference does it: the 1.3.4 changelog records that "agent commands now
// set JJ_PAGER=cat alongside PAGER and GIT_PAGER", and that /diff must not be
// fooled by repositories configured to always colour their output. The 1.3.6
// changelog line 7 adds the editor-disabling trio (GIT_EDITOR / HGEDITOR /
// JJ_EDITOR = false) so a future /commit-style slash that fails through to a
// real `git commit` cannot hang on a hidden vim session.
func vcsDiff(vcs, dir string) (string, error) {
	return diffaction.VCSDiff(vcs, dir)
}

// stripANSI removes ANSI CSI escape sequences from s. The returned string
// preserves all other bytes, including embedded newlines and surrounding
// whitespace, so a caller's TrimSpace / Split pipeline keeps working.
func stripANSI(s string) string {
	return diffaction.StripANSI(s)
}

// vcsDiffFunc is the test seam for diffFeedback. Production code reads it
// directly; tests override it with a function that returns canned output so
// the color.ui = always regression can be exercised without configuring a
// real repository.
var vcsDiffFunc = vcsDiff

// gitHeadFunc is the test seam for the AGY 1.3.3 line 52 fallback: it runs
// `git -C <workspace> diff HEAD`. Production code reads runGit through it;
// tests override it with a function that returns canned output.
var gitHeadFunc = func(dir string) (string, error) {
	return runGit(dir, "diff", "HEAD")
}

// diffFeedback renders /diff: the workspace's uncommitted changes.
func diffFeedback(workspace string, maxCells int) string {
	diffaction.VCSDiffFunc = vcsDiffFunc
	diffaction.GitHeadFuncSeam = gitHeadFunc
	return diffaction.DiffFeedback(workspace, maxCells)
}

// editorFinishedMsg is delivered after an external editor process exits.
//
// tempPath is set when the editor was editing a scratch file that must be read
// back (ctrl+g prompt editing); it is empty for /open, which edits a real file
// in place.
type editorFinishedMsg struct {
	err      error
	tempPath string
}

// splitCommand splits an editor setting such as "code --wait" into a name and
// arguments.
func splitCommand(s string) (string, []string) {
	return editorkit.SplitCommand(s)
}

// editPromptCmd implements ctrl+g: open the configured editor on the current
// prompt and read the result back.
//
// Recovered from the changelog: "Fixed pressing `Ctrl+G` to edit the prompt in a
// full-screen terminal editor such as `vim` hanging with `Vim: Warning: Output
// is not to a terminal`; external editors now run against a real terminal
// stdout", and "Added `ctrl+g` on the artifact view to open $EDITOR". The prompt
// is written to a scratch file, edited, and read back on exit.
func (m *runMode) editPromptCmd() tea.Cmd {
	name, args, notice, err := editorkit.ResolveEditor(auth.SettingsString("editor"))
	if err != nil {
		m.response = append(m.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if _, err := exec.LookPath(name); err != nil {
		m.response = append(m.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if notice != "" {
		m.response = append(m.response, notice)
	}
	path, err := editorkit.PromptScratchFile(m.input.Value())
	if err != nil {
		m.response = append(m.response, "Failed to create a temporary file: "+err.Error())
		return nil
	}
	c := exec.Command(name, append(args, path)...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err, tempPath: path}
	})
}

// openFeedback resolves /open into a command to run. It returns the line(s) to
// show before the editor starts and the command itself (nil when the file
// cannot be opened).
//
// Recovered: `Usage: /open <file_path>`, `File not found: %s`,
// `Opening file: %s`, `No editor configured; using %s` and
// `Failed to resolve editor: %v`. The caller emits
// `Editor exited with error: %v` when the process returns non-zero.
func openFeedback(arg, configuredEditor, workspace string) (string, *exec.Cmd) {
	return editorkit.OpenFeedback(arg, configuredEditor, workspace, pathExists)
}
