// help_panel.go — the /help overlay's pure renderers.
//
// The overlay's tab bar, the GENERAL tab's description / metadata / quick
// reference blocks, the warning cards and the two path/name helpers are
// all pure functions of their arguments (plus the environment for the
// $HOME rewrite). They live here so cmd/wagyu/tui_help.go keeps only the
// runMode-coupled plumbing: which tab is active, where the list window
// starts and what the cursor row is.
package tuilib

import (
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/core/branding"
	"github.com/justwasm/pigasus/internal/core/logts"
	"github.com/justwasm/pigasus/internal/core/version"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/palette"
)

// The palette's colours follow the selected colour scheme, so these
// aliases are refreshed whenever the scheme changes.
var (
	// HelpHeaderOpen / HelpHeaderClose wrap the overlay's section headers.
	HelpHeaderOpen  = "\x1b[1m" + palette.SubtleOpen
	HelpHeaderClose = "\x1b[0m"

	// TabBarActiveOpen / TabBarActiveClose wrap the active tab's label.
	TabBarActiveOpen  = "\x1b[1;4m" + palette.BlueOpen
	TabBarActiveClose = "\x1b[0m"

	// TabBarInactiveOpen / TabBarInactiveClose wrap an inactive tab's label.
	TabBarInactiveOpen  = palette.SeparatorOpen
	TabBarInactiveClose = palette.SeparatorClose

	// TabBarHintOpen / TabBarHintClose wrap the "cycle the tabs" hint.
	TabBarHintOpen  = palette.SeparatorOpen
	TabBarHintClose = palette.SeparatorClose
)

const (
	// TabBarHintSuffix is the text after the tab labels.
	TabBarHintSuffix = "  (←/→ or tab to cycle)"
)

// HelpKeyboardHint is the footer the COMMANDS and SHORTCUTS tabs render.
var HelpKeyboardHint = buildHelpKeyboardHint()

// buildHelpKeyboardHint renders the footer's text with the current
// palette colours.
func buildHelpKeyboardHint() string {
	return "\x1b[1m" + "Keyboard: " + "\x1b[0m" +
		HelpBlueOpen + "↑/↓" + HelpBlueClose + " " +
		HelpDimGrayOpen + "Navigate" + HelpDimGrayClose + "  " +
		HelpBlueOpen + "←/→" + HelpBlueClose + " " +
		HelpDimGrayOpen + "Switch View" + HelpDimGrayClose + "  " +
		HelpBlueOpen + "esc" + HelpBlueClose + " " +
		HelpDimGrayOpen + "Close" + HelpDimGrayClose
}

// RenderHelpGeneralDescription writes the overlay's one-paragraph pitch.
func RenderHelpGeneralDescription(b *strings.Builder) {
	b.WriteString(HelpDimGrayOpen)
	b.WriteString(branding.Current().CLIName + " understands your codebase, makes edits with your permission,")
	b.WriteString(HelpDimGrayClose)
	b.WriteString("\n")
	b.WriteString(HelpDimGrayOpen)
	b.WriteString("and executes commands — right from your terminal.")
	b.WriteString(HelpDimGrayClose)
	b.WriteString("\n\n")
}

// RenderHelpGeneralMetadata writes the version / account / workspace /
// project-config / log-file / conversation block.
func RenderHelpGeneralMetadata(b *strings.Builder, tier, cwd, email, conversationID string) {
	b.WriteString(HelpDimGrayOpen + "Version " + version.Reference + HelpDimGrayClose + "\n")
	b.WriteString(HelpDimGrayOpen + email)
	if tier != "" {
		b.WriteString(" (" + tier + ")")
	}
	b.WriteString(HelpDimGrayClose + "\n")
	b.WriteString(HelpDimGrayOpen + "Workspace:      " + cwd + HelpDimGrayClose + "\n")
	b.WriteString(HelpDimGrayOpen + "Project Config: ~/.gemini/config/projects/default-cli-project.json" + HelpDimGrayClose + "\n")
	b.WriteString(HelpDimGrayOpen + "Log File:       ~/.gemini/antigravity-cli/log/cli-" + logts.FormatLogTS() + ".log" + HelpDimGrayClose + "\n")
	if conversationID != "" {
		b.WriteString(HelpDimGrayOpen + "Conversation:   " + conversationID + HelpDimGrayClose + "\n")
	}
	b.WriteString("\n")
}

// RenderHelpGeneralQuickRef writes the "Quick Reference" section.
func RenderHelpGeneralQuickRef(b *strings.Builder) {
	b.WriteString(HelpHeaderOpen + "Quick Reference" + HelpHeaderClose + "\n")
	b.WriteString("  " + HelpBlueOpen + "/" + HelpBlueClose + "  " + HelpDimGrayOpen + "Type / to see available commands" + HelpDimGrayClose + "\n\n")
}

// RenderHelpGeneral writes the whole GENERAL tab.
func RenderHelpGeneral(b *strings.Builder, model, tier, cwd, email, conversationID string) {
	RenderHelpGeneralDescription(b)
	RenderHelpGeneralMetadata(b, tier, cwd, email, conversationID)
	RenderHelpGeneralQuickRef(b)
	b.WriteString("\x1b[1mKeyboard: \x1b[0m")
	b.WriteString(HelpBlueOpen + "↑/↓" + HelpBlueClose + " " + HelpDimGrayOpen + "Navigate" + HelpDimGrayClose + "  ")
	b.WriteString(HelpBlueOpen + "←/→" + HelpBlueClose + " " + HelpDimGrayOpen + "Switch View" + HelpDimGrayClose + "  ")
	b.WriteString(HelpBlueOpen + "esc" + HelpBlueClose + " " + HelpDimGrayOpen + "Close" + HelpDimGrayClose + "\n\n")
}

// RenderAgentWarning is the reference's ⚠ Agent Not Found card.
func RenderAgentWarning(body string) string {
	var b strings.Builder
	b.WriteString(palette.WarningOpen)
	b.WriteString("⚠ Agent Not Found")
	b.WriteString(palette.WarningClose)
	b.WriteString("\n")
	b.WriteString(palette.WarningBlueOpen)
	b.WriteString("  ⎿  ")
	b.WriteString(palette.WarningBlueClose)
	b.WriteString(palette.WarningBodyOpen)
	b.WriteString(body)
	b.WriteString(palette.WarningBodyClose)
	b.WriteString("\n")
	return b.String()
}

// RenderModelWarning is the reference-style ⚠ Model Not Configured card.
func RenderModelWarning(body string) string {
	var b strings.Builder
	b.WriteString(palette.WarningOpen)
	b.WriteString("⚠ Model Not Configured")
	b.WriteString(palette.WarningClose)
	b.WriteString("\n")
	b.WriteString(palette.WarningBlueOpen)
	b.WriteString("  ⎿  ")
	b.WriteString(palette.WarningBlueClose)
	b.WriteString(palette.WarningBodyOpen)
	b.WriteString(body)
	b.WriteString(palette.WarningBodyClose)
	b.WriteString("\n")
	return b.String()
}

// RenderWarning emits the cycle-81 fallback warning block.
func RenderWarning(from, to string) string {
	var b strings.Builder
	b.WriteString(palette.WarningOpen)
	b.WriteString("⚠ Warning")
	b.WriteString(palette.WarningClose)
	b.WriteString("\n")
	b.WriteString(palette.WarningBlueOpen)
	b.WriteString("  ⎿  ")
	b.WriteString(palette.WarningBlueClose)
	b.WriteString(palette.WarningBodyOpen)
	b.WriteString("\"")
	b.WriteString(from)
	b.WriteString("\" is no longer available. Using \"")
	b.WriteString(to)
	b.WriteString("\".")
	b.WriteString(palette.WarningBodyClose)
	b.WriteString("\n")
	return b.String()
}

// ShortenCwd replaces the leading $HOME with "~".
func ShortenCwd(cwd string) string {
	if pwd := os.Getenv("PWD"); pwd != "" {
		if fi1, err1 := os.Stat(pwd); err1 == nil {
			if fi2, err2 := os.Stat(cwd); err2 == nil && os.SameFile(fi1, fi2) {
				cwd = pwd
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return cwd
	}
	if cwd == home {
		return "~"
	}
	if strings.HasPrefix(cwd, home+"/") {
		return "~" + cwd[len(home):]
	}
	return cwd
}

// StripTier removes the effort tier from a model display name:
// "Gemini 3.8 Flash (High)" -> "Gemini 3.8 Flash".
//
// Only a model whose effort is adjustable has a tier to strip. Every
// other bracket belongs to the model's own name and stays: Claude's
// "(Thinking)", a configured model's "(1017)" tag, GPT-OSS's single
// "(Medium)". The rule — and the reason it lives in the catalogue — is
// catalog.StatusLabel's; the status row's " · <tier>" segment is the
// other half of the same answer.
func StripTier(model string) string {
	name, _ := catalog.StatusLabel(model)
	return name
}

// RenderTabBar emits the cycle-85 styled tab bar.
func RenderTabBar(active int) string {
	return RenderTabBarTabs(active, []string{"general", "commands", "shortcuts"})
}

// TabSpan is one tab label's rune-column range in a rendered tab bar.
//
// The range is half-open: Start is the label's first column and End is
// the column after its last, so a click at x belongs to the tab whose
// [Start, End) contains it.
type TabSpan struct {
	Index      int
	Start, End int
}

// RenderTabBarTabs emits the tab bar for an arbitrary tab list.
func RenderTabBarTabs(active int, tabs []string) string {
	line, _ := RenderTabBarTabsSpans(active, tabs)
	return line
}

// RenderTabBarTabsSpans emits the tab bar and reports where each label
// sits, so a panel can turn a click into a tab.
//
// The two answers come from one walk. A bar whose hit regions were
// computed by a second copy of this arithmetic would drift the first
// time a separator changed width — the same class of bug as a render
// and a scroll bound that disagree about how tall a head is.
func RenderTabBarTabsSpans(active int, tabs []string) (string, []TabSpan) {
	return RenderTabBarTabsSpansWidth(active, tabs, 0)
}

// RenderTabBarTabsSpansWidth is RenderTabBarTabsSpans for a bar that has
// to fit a terminal width.
//
// The brand and the labels are what the bar is for; the "(←/→ or tab to
// cycle)" hint is the first thing dropped when they do not fit, because a
// hint pushed off the right edge is not read anyway and the bar still
// cycles from the keys it names. A width of 0 means "no limit", which is
// what the unwrapped callers pass.
//
// A label that does not fit whole is not painted at all, and the walk
// stops there. Painting half a word would leave a fragment the user can
// see and cannot click — the span for it is not reported, because a click
// on `Wi` must not switch to a view whose name it cannot read. Stopping
// rather than skipping keeps the visible labels a prefix of the tab list,
// so the bar never shows `Logs  Trajectory` with `Wire` missing from the
// middle.
//
// Dropping the hint, and dropping the labels, is decided before each part
// is written, so the spans reported here are the spans of the line this
// call returns: a click can never land on a label that was not painted.
func RenderTabBarTabsSpansWidth(active int, tabs []string, width int) (string, []TabSpan) {
	var b strings.Builder
	col := 0
	write := func(s string, width int) {
		b.WriteString(s)
		col += width
	}
	brand := branding.Current().CLIName
	write(palette.BlueOpen, 0)
	write(brand, ansi.StringWidth(brand))
	write(palette.BlueClose, 0)
	write("   ", 3)
	spans := make([]TabSpan, 0, len(tabs))
	for i, name := range tabs {
		sepWidth := 0
		if i > 0 {
			sepWidth = 4
		}
		w := ansi.StringWidth(name)
		if width > 0 && col+sepWidth+w > width {
			// This label cannot be painted whole. Stop rather than paint
			// a fragment: the labels that made it are a prefix of the
			// list, and every one of them is clickable.
			break
		}
		if i > 0 {
			write("    ", sepWidth)
		}
		spans = append(spans, TabSpan{Index: i, Start: col, End: col + w})
		if i == active {
			write(TabBarActiveOpen, 0)
			write(name, w)
			write(TabBarActiveClose, 0)
			continue
		}
		write(TabBarInactiveOpen, 0)
		write(name, w)
		write(TabBarInactiveClose, 0)
	}
	// The hint is the bar's other droppable part. Deciding after the walk
	// rather than truncating the finished line keeps the escape sequences
	// whole: cutting a styled string mid-span is what leaves a colour on.
	if width > 0 {
		if need := col + 1 + ansi.StringWidth(TabBarHintSuffix); need > width {
			return b.String(), spans
		}
	}
	write(" ", 1)
	write(TabBarHintOpen, 0)
	write(TabBarHintSuffix, ansi.StringWidth(TabBarHintSuffix))
	write(TabBarHintClose, 0)
	return b.String(), spans
}
