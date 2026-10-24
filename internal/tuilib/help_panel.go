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
	"time"

	"github.com/justwasm/pigasus/internal/branding"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/palette"
)

const (
	// HelpHeaderOpen / HelpHeaderClose wrap the overlay's section headers.
	HelpHeaderOpen  = "\x1b[1m\x1b[38;2;88;110;117m"
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

	// TabBarHintSuffix is the text after the tab labels.
	TabBarHintSuffix = "  (←/→ or tab to cycle)"
)

// HelpKeyboardHint is the footer the COMMANDS and SHORTCUTS tabs render.
var HelpKeyboardHint = "\x1b[1m" + "Keyboard: " + "\x1b[0m" +
	HelpBlueOpen + "↑/↓" + HelpBlueClose + " " +
	HelpDimGrayOpen + "Navigate" + HelpDimGrayClose + "  " +
	HelpBlueOpen + "←/→" + HelpBlueClose + " " +
	HelpDimGrayOpen + "Switch View" + HelpDimGrayClose + "  " +
	HelpBlueOpen + "esc" + HelpBlueClose + " " +
	HelpDimGrayOpen + "Close" + HelpDimGrayClose

// FormatLogTS is the timestamp suffix of the CLI's log file name.
func FormatLogTS() string {
	return time.Now().Format("20060102_150405")
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
	b.WriteString(HelpDimGrayOpen + "Version " + cli.ReferenceVersion + HelpDimGrayClose + "\n")
	b.WriteString(HelpDimGrayOpen + email)
	if tier != "" {
		b.WriteString(" (" + tier + ")")
	}
	b.WriteString(HelpDimGrayClose + "\n")
	b.WriteString(HelpDimGrayOpen + "Workspace:      " + cwd + HelpDimGrayClose + "\n")
	b.WriteString(HelpDimGrayOpen + "Project Config: ~/.gemini/config/projects/default-cli-project.json" + HelpDimGrayClose + "\n")
	b.WriteString(HelpDimGrayOpen + "Log File:       ~/.gemini/antigravity-cli/log/cli-" + FormatLogTS() + ".log" + HelpDimGrayClose + "\n")
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

// StripTier removes a parenthesised tier suffix from a model display name.
func StripTier(model string) string {
	if i := strings.LastIndex(model, " ("); i >= 0 {
		if model[len(model)-1] == ')' {
			return model[:i]
		}
	}
	return model
}

// RenderTabBar emits the cycle-85 styled tab bar.
func RenderTabBar(active int) string {
	return RenderTabBarTabs(active, []string{"general", "commands", "shortcuts"})
}

// RenderTabBarTabs emits the tab bar for an arbitrary tab list.
func RenderTabBarTabs(active int, tabs []string) string {
	var b strings.Builder
	b.WriteString(palette.BlueOpen)
	b.WriteString(branding.Current().CLIName)
	b.WriteString(palette.BlueClose)
	b.WriteString("   ")
	for i, name := range tabs {
		if i > 0 {
			b.WriteString("    ")
		}
		if i == active {
			b.WriteString(TabBarActiveOpen)
			b.WriteString(name)
			b.WriteString(TabBarActiveClose)
		} else {
			b.WriteString(TabBarInactiveOpen)
			b.WriteString(name)
			b.WriteString(TabBarInactiveClose)
		}
	}
	b.WriteString(" ")
	b.WriteString(TabBarHintOpen)
	b.WriteString(TabBarHintSuffix)
	b.WriteString(TabBarHintClose)
	return b.String()
}
