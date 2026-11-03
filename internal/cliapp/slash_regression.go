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
	"github.com/justwasm/pigasus/internal/ui/tuilib"

	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	settings "github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/models"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/ui/editor/editorkit"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
)

// statusEffortSuffix is the right-hand status row's " · <effort>". The
// reference derives it from the *live* effort (verified: after Enter on
// /effort at medium the row reads `Gemini 3.8 Flash · medium`), so the
// clone can no longer use a constant.
//
// The word is the tier the model actually runs at, so a model that
// declares a tier the reference's own three-tier slider cannot name
// ("xhigh", "max") still says the right thing.
func (m *runMode) statusEffortSuffix() string {
	// A model the reference publishes no effort tiers for carries no
	// suffix: the reference never prints " · high" / " · medium" for it,
	// and the tiers belong to the reference's own catalogue.
	if !modelpicker.HasEffortTiers(m.statusModel) {
		return ""
	}
	return " \u00b7 " + m.currentEffortName()
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
//
// The slider's index is an index into the *model's own* list of tiers, so
// the word comes from that list and not from the vocabulary: a model that
// publishes "max" but not "xhigh" has its own order, and the tier the
// marker sits on has to be the one the model publishes.
func (m *runMode) applyEffortSelection(level int) tea.Cmd {
	name := models.TierNameAt(m.effortTiers(), level)
	if name == "" {
		name = effortpanel.EffortLevelName(level)
	}
	return m.applyEffortLevel(name, level)
}

// applyEffortLevel is the shared plumbing the picker Enter and the
// /effort <level> shortcut both call into. Cycle effort-picker:
//
//   - `name` is the canonical lowercase word ("low" | "medium" | "high");
//     callers should run printmode.ParseEffortWord first when the word came from
//     user input.
//   - `level` is the slider index for change-detection; pass the
//     same value the name came from so the persistence path is one
//     branch.
//   - The function never returns an error: invalid input has already been
//     rejected by the caller; persistence errors are swallowed because the
//     user-typed echo must survive a read-only settings file.
//
// The reference refuses the change *before* it looks at the word: a model
// with a fixed reasoning budget has no adjustable effort, and the
// reference answers
//
//	Effort isn't adjustable for <model>.
//
// (【反编译】 agy 1.4.12, commands.(*effortCommand).Execute.func1 at
// 0x102b091c0: the adjustable flag is read at 0x102b09248 and the
// command branches to the message before it parses its argument). The
// clone's own word validation happens in the /effort handler, which
// checks the word first — a divergence that is recorded in the cycle
// report rather than silently changed here.
func (m *runMode) applyEffortLevel(name string, level int) tea.Cmd {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = effortpanel.EffortLevelName(level)
	}
	model := m.effortModel()
	if !modelcatalog.EffortAdjustableFor(model) {
		m.slashRespond("/effort", models.NotAdjustableMessage(models.BaseLabel(model)))
		return nil
	}
	before := model
	label := modelcatalog.EffortLabelFor(model, name)
	changed := label != "" && label != before

	// The session's effort lives on the flag set (currentEffort reads it).
	if m.flags != nil {
		_ = m.flags.Set("effort", name)
	}

	// The picker path closes the panel; the /effort <level> path has
	// nothing open to close. The early-return-after-mutation pattern
	// keeps both callers' semantics identical.
	if m.panel.kind == panelregistry.Effort {
		m.panel = slashPanel{}
	}
	if !changed {
		return nil
	}
	if strings.TrimSpace(m.model) == "" {
		m.model = before
		m.statusModel = tuilib.StripTier(before)
	}
	m.model = label
	m.statusModel = tuilib.StripTier(label)
	m.syncBackendForModel()
	_ = settings.SetField("model", label)
	m.response = append(m.response, tuilib.SlashEchoLines("/effort", "Effort set to "+name)...)
	return nil
}

// vcsDiffFunc is the test seam for diffFeedback. Production code reads it
// directly; tests override it with a function that returns canned output so
// the color.ui = always regression can be exercised without configuring a
// real repository.
var vcsDiffFunc = diffaction.VCSDiff

// gitHeadFunc is the test seam for the AGY 1.3.3 line 52 fallback: it runs
// `git -C <workspace> diff HEAD`. Production code reads runGit through it;
// tests override it with a function that returns canned output.
var gitHeadFunc = func(dir string) (string, error) {
	return diffaction.RunGit(dir, "diff", "HEAD")
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

// editPromptCmd implements ctrl+g: open the configured editor on the current
// prompt and read the result back.
//
// Recovered from the changelog: "Fixed pressing `Ctrl+G` to edit the prompt in a
// full-screen terminal editor such as `vim` hanging with `Vim: Warning: Output
// is not to a terminal`; external editors now run against a real terminal
// stdout", and "Added `ctrl+g` on the artifact view to open $EDITOR". The prompt
// is written to a scratch file, edited, and read back on exit.
func (m *runMode) editPromptCmd() tea.Cmd {
	name, args, notice, err := editorkit.ResolveEditor(settings.String("editor"))
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
