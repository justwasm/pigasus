// renderer.go - cycle 256: the renderer's optimization/bypass
// behaviour, exposed as a public, testable seam.
//
// AGY 1.1.19's render path emits only dirty rows on each frame
// and falls back to the full viewport only when the consumer
// explicitly disables the optimization. chasm.land/bubbletea/v2
// owns the actual diff implementation inside cursed_renderer.go;
// we don't fork it. What cycle 256 ships is:
//
//  1. A boolean escapeSequenceOptimizationsEnabled() (in
//     escape_optimizations.go) that reads
//     AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS.
//
//  2. applyRenderer(prev, current, optimizationsEnabled) -
//     public test surface for "given two frames and a gate,
//     what does the renderer emit?". The contract:
//
//     optimizationsEnabled AND prev == current : ""    (no-op)
//     otherwise                                : current (full redraw)
//
//     When the env var bypasses optimizations,
//     optimizationsEnabled is false and applyRenderer always
//     returns `current`. The cycle-256 deliverable's
//     "What the fix does" section lists every call site
//     currently consulting this seam; the View() in tui_run.go
//     does not yet route through applyRenderer because that
//     would require either wrapping or replacing the
//     bubbletea/v2 renderer (out of scope for cycle 256's
//     env-var gap; documented in 硬约束).
//
//  3. shouldConsultRenderer(mode) - the print-mode bypass pin.
//     Print mode (-p / --print) writes plain text to stdout and
//     never reaches bubbletea's View() pipeline, so the
//     AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS env var is
//     irrelevant there. The cycle-256 spec says
//     "print mode (`-p`) doesn't trigger the renderer code path
//     (it's a TUI concern); pin in test." shouldConsultRenderer
//     is the public pin.
package cliapp

import (
	"strings"

	"github.com/justwasm/pigasus/internal/mdrender"
	"github.com/justwasm/pigasus/internal/termcolor"
	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// applyRenderer returns the bytes the renderer should emit given
// the previous frame, the current frame, and whether the
// dirty-rectangle / diff optimizations are enabled.
//
// When optimizations are on AND the previous and current frame
// are identical (byte-for-byte), applyRenderer returns the empty
// string to signal "no draw needed". The match is exact and
// untrimmed: the renderer downstream always sees the literal
// viewport bytes, and a single trailing newline difference is
// still a redraw. The cycle 256 deliverable documents this
// decision against any future cycle that wants a trimmed
// comparison (trimming would let optimisations elide frame
// transitions bubbletea treats as different for layout reasons
// but humans view as identical - kept simple here).
//
// When optimizations are off, applyRenderer returns the full
// current view regardless of whether it differs from prev. This
// is the AGY 1.1.19 "bypass the renderer's ... optimizations"
// behaviour: full redraw every frame.
//
// When optimizations are on AND the views differ, applyRenderer
// returns the full current view (the diff itself would be
// returned in a future cycle - cycle 256 exercises the gate
// only, not the diff algorithm).
func applyRenderer(prev, current string, optimizationsEnabled bool) string {
	if optimizationsEnabled && prev == current {
		return ""
	}
	return current
}

// mode constants for shouldConsultRenderer. Held as named
// strings rather than an enum so the print-mode dispatcher in
// cmd/wagyu/main.go can pass through values verbatim without a
// conversion step.
const (
	modePrint = "print"
	modeDashP = "-p"
)

// shouldConsultRenderer reports whether the renderer / dirty-rect
// optimization seam applies for a given run mode. Returns false
// for print mode (and its `-p` shorthand) and true otherwise.
//
// match is case-insensitive and whitespace-trimmed so callers do
// not have to normalize mode strings before calling; the test
// fixture in renderer_test.go pins this contract for every
// spelling the print dispatcher accepts (cmd/wagyu/main.go
// normalizes "-p" and "--print" to a single print path long
// before this gate fires in production, but the gate still has
// to be robust to the literals a future cycle might emit).
//
// The default (unknown mode) returns true - "be conservative
// and apply the gate" matches the AGY 1.1.19 reading of the
// spec.
func shouldConsultRenderer(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case modePrint, modeDashP:
		return false
	}
	return true
}

const envDisableEscapeSequenceOptimizations = tuilib.EnvDisableEscapeSequenceOptimizations

func escapeSequenceOptimizationsBypassed() bool {
	return tuilib.EscapeSequenceOptimizationsBypassed()
}

func escapeSequenceOptimizationsEnabled() bool {
	return tuilib.EscapeSequenceOptimizationsEnabled()
}

func renderMarkdown(text string, width int) string {
	return mdrender.RenderMarkdown(text, width)
}

func styleInlineBackticks(line string) string {
	return mdrender.StyleInlineBackticks(line)
}

// The palette's colours follow the selected colour scheme, so these
// aliases are refreshed whenever the scheme changes.
var (
	cBlueOpen              = palette.BlueOpen
	cBlueBoldOpen          = palette.BlueBoldOpen
	cUserFgOpen            = palette.UserFgOpen
	cUserFgBoldOpen        = palette.UserFgBoldOpen
	cDimGrayOpen           = palette.DimGrayOpen
	cWarningOpen           = palette.WarningOpen
	cWarningBlueOpen       = palette.WarningBlueOpen
	cWarningBodyOpen       = palette.WarningBodyOpen
	cSeparatorOpen         = palette.SeparatorOpen
	cSubtleOpen            = palette.SubtleOpen
	cToolAddOpen           = palette.ToolAddOpen
	cToolRemoveOpen        = palette.ToolRemoveOpen
	cToolBulletOpen        = palette.ToolBulletOpen
	cToolBulletClose       = palette.ToolBulletClose
	cToolBulletRunningOpen = palette.ToolBulletRunningOpen
	cToolBulletErrOpen     = palette.ToolBulletErrOpen
	cToolVerbOpen          = palette.ToolVerbOpen
	cToolVerbClose         = palette.ToolVerbClose
	cToolArgsOpen          = palette.ToolArgsOpen
	cToolArgsClose         = palette.ToolArgsClose
	cToolArgsOuterOpen     = palette.ToolArgsOuterOpen
	cToolArgsOuterClose    = palette.ToolArgsOuterClose
	cToolHintOpen          = palette.ToolHintOpen
	cToolHintClose         = palette.ToolHintClose
	cToolFailedSuffixOpen  = palette.ToolFailedSuffixOpen
	cToolFailedSuffixClose = palette.ToolFailedSuffixClose
	cDetailBulletOpen      = palette.DetailBulletOpen
	cDiffNumOpen           = palette.DiffNumOpen
	cDiffStripeOpen        = palette.DiffStripeOpen
	cDiffAddTintDark       = palette.DiffAddTintDark
	cDiffRemoveTintDark    = palette.DiffRemoveTintDark
	cDiffAddTintLight      = palette.DiffAddTintLight
	cDiffRemoveTintLight   = palette.DiffRemoveTintLight
	cDiffAddTintOpen       = palette.DiffAddTintOpen
	cDiffRemoveTintOpen    = palette.DiffRemoveTintOpen
	cThoughtBulletOpen     = palette.ThoughtBulletOpen
	cThoughtBulletClose    = palette.ThoughtBulletClose
	cBodyOpen              = palette.BodyOpen
	cBodyClose             = palette.BodyClose
	cSpinnerBaseOpen       = palette.SpinnerBaseOpen
	cSpinnerHiOpen         = palette.SpinnerHiOpen
	cInlineCodeOpen        = palette.InlineCodeOpen
	cInlineCodeClose       = palette.InlineCodeClose
	cBoldOpen              = palette.BoldOpen
	cBoldClose             = palette.BoldClose
	cBlueClose             = palette.BlueClose
	cBlueBoldClose         = palette.BlueBoldClose
	cDimGrayClose          = palette.DimGrayClose
	cWarningClose          = palette.WarningClose
	cWarningBlueClose      = palette.WarningBlueClose
	cWarningBodyClose      = palette.WarningBodyClose
	cSeparatorClose        = palette.SeparatorClose
	cToolAddClose          = palette.ToolAddClose
	cToolRemoveClose       = palette.ToolRemoveClose
	cSyntaxKeywordOpen     = palette.SyntaxKeywordOpen
	cSyntaxStringOpen      = palette.SyntaxStringOpen
	cSyntaxNumberOpen      = palette.SyntaxNumberOpen
	cSyntaxCommentOpen     = palette.SyntaxCommentOpen
	cSyntaxTypeOpen        = palette.SyntaxTypeOpen
	cSyntaxTokenClose      = palette.SyntaxTokenClose
)

func init() {
	palette.OnSchemeChange(refreshPaletteAliases)
}

// refreshPaletteAliases re-reads the palette's colours after the
// selected colour scheme changed.
func refreshPaletteAliases() {
	cBlueOpen = palette.BlueOpen
	cBlueBoldOpen = palette.BlueBoldOpen
	cUserFgOpen = palette.UserFgOpen
	cUserFgBoldOpen = palette.UserFgBoldOpen
	cDimGrayOpen = palette.DimGrayOpen
	cWarningOpen = palette.WarningOpen
	cWarningBlueOpen = palette.WarningBlueOpen
	cWarningBodyOpen = palette.WarningBodyOpen
	cSeparatorOpen = palette.SeparatorOpen
	cSubtleOpen = palette.SubtleOpen
	panelGray238 = palette.SubtleOpen
	cToolAddOpen = palette.ToolAddOpen
	cToolRemoveOpen = palette.ToolRemoveOpen
	cToolBulletOpen = palette.ToolBulletOpen
	cToolBulletClose = palette.ToolBulletClose
	cToolBulletRunningOpen = palette.ToolBulletRunningOpen
	cToolBulletErrOpen = palette.ToolBulletErrOpen
	cToolVerbOpen = palette.ToolVerbOpen
	cToolVerbClose = palette.ToolVerbClose
	cToolArgsOpen = palette.ToolArgsOpen
	cToolArgsClose = palette.ToolArgsClose
	cToolArgsOuterOpen = palette.ToolArgsOuterOpen
	cToolArgsOuterClose = palette.ToolArgsOuterClose
	cToolHintOpen = palette.ToolHintOpen
	cToolHintClose = palette.ToolHintClose
	cToolFailedSuffixOpen = palette.ToolFailedSuffixOpen
	cToolFailedSuffixClose = palette.ToolFailedSuffixClose
	cDetailBulletOpen = palette.DetailBulletOpen
	cDiffNumOpen = palette.DiffNumOpen
	cDiffStripeOpen = palette.DiffStripeOpen
	cDiffAddTintDark = palette.DiffAddTintDark
	cDiffRemoveTintDark = palette.DiffRemoveTintDark
	cDiffAddTintLight = palette.DiffAddTintLight
	cDiffRemoveTintLight = palette.DiffRemoveTintLight
	cDiffAddTintOpen = palette.DiffAddTintOpen
	cDiffRemoveTintOpen = palette.DiffRemoveTintOpen
	cThoughtBulletOpen = palette.ThoughtBulletOpen
	cThoughtBulletClose = palette.ThoughtBulletClose
	cBodyOpen = palette.BodyOpen
	cBodyClose = palette.BodyClose
	cSpinnerBaseOpen = palette.SpinnerBaseOpen
	cSpinnerHiOpen = palette.SpinnerHiOpen
	cInlineCodeOpen = palette.InlineCodeOpen
	cInlineCodeClose = palette.InlineCodeClose
	cBoldOpen = palette.BoldOpen
	cBoldClose = palette.BoldClose
	cBlueClose = palette.BlueClose
	cBlueBoldClose = palette.BlueBoldClose
	cDimGrayClose = palette.DimGrayClose
	cWarningClose = palette.WarningClose
	cWarningBlueClose = palette.WarningBlueClose
	cWarningBodyClose = palette.WarningBodyClose
	cSeparatorClose = palette.SeparatorClose
	cToolAddClose = palette.ToolAddClose
	cToolRemoveClose = palette.ToolRemoveClose
	cSyntaxKeywordOpen = palette.SyntaxKeywordOpen
	cSyntaxStringOpen = palette.SyntaxStringOpen
	cSyntaxNumberOpen = palette.SyntaxNumberOpen
	cSyntaxCommentOpen = palette.SyntaxCommentOpen
	cSyntaxTypeOpen = palette.SyntaxTypeOpen
	cSyntaxTokenClose = palette.SyntaxTokenClose

	// The help overlay's own aliases, declared in tui_help.go.
	helpHeaderOpen = tuilib.HelpHeaderOpen
	helpHeaderClose = tuilib.HelpHeaderClose
	cHelpDescOpen = tuilib.HelpDescOpen
	cHelpDescClose = tuilib.HelpDescClose
	helpBlueOpen = tuilib.HelpBlueOpen
	helpBlueClose = tuilib.HelpBlueClose
	helpDimGrayOpen = tuilib.HelpDimGrayOpen
	helpDimGrayClose = tuilib.HelpDimGrayClose
	tabBarBrandOpen = tuilib.HelpBlueOpen
	tabBarBrandClose = tuilib.HelpBlueClose
	tabBarActiveOpen = tuilib.TabBarActiveOpen
	tabBarActiveClose = tuilib.TabBarActiveClose
	tabBarInactiveOpen = tuilib.TabBarInactiveOpen
	tabBarInactiveClose = tuilib.TabBarInactiveClose
	tabBarHintOpen = tuilib.TabBarHintOpen
	tabBarHintClose = tuilib.TabBarHintClose
	helpKeyboardHint = tuilib.HelpKeyboardHint

	// The transcript's own bullets, declared in tui_tools.go.
	stepBulletOpen = cToolBulletOpen + "\u25cf" + cToolBulletClose
	stepBulletErrOpen = cToolBulletErrOpen + "\u25cf" + cToolBulletClose
	thoughtBulletOpen = cThoughtBulletOpen + "\u25b8" + cThoughtBulletClose
	detailBulletOpen = cDetailBulletOpen + "  \u23bf  " + "\x1b[m"

	// The picker's own colours, declared in picker_view.go.
	pickerNameHi = cBlueOpen
	pickerDescDim = cSubtleOpen
	pickerLabelDim = cDimGrayOpen
	pickerKeyOpen = "\x1b[1m" + cBlueOpen

	// The bash block's body colours, declared in tui_bash_mode.go.
	bashBodyOKOpen = palette.WarningBodyOpen
	bashBodyFailOpen = palette.ToolRemoveBodyOpen
}

func cRule(width int) string {
	return palette.Rule(width)
}

func turnSeparator(width int) string {
	w := 60
	if width > 0 && width-3 < w {
		w = width - 3
		if w < 10 {
			w = width
		}
	}
	return palette.Rule(w)
}

func statusModeColor(mode string) string {
	return palette.StatusModeColor(mode)
}

// stripSGR removes every ANSI SGR sequence.
func stripSGR(s string) string {
	return termcolor.StripSGR(s)
}
