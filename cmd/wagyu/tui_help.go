package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/branding"
	"github.com/justwasm/pigasus/internal/tuilib"
)

func renderHelp(tab int, model, tier, cwd, email, conversationID string, m *runMode) string {
	tabs := helpTabs(m)
	if tab < 0 || tab >= len(tabs) {
		tab = 0
	}
	var b strings.Builder
	b.WriteString(renderTabBarTabs(tab, tabs))
	b.WriteString("\n")
	switch tab {
	case 0:
		renderHelpGeneral(&b, model, tier, shortenCwd(cwd), email, conversationID)
	case 1:
		renderHelpCommands(&b, m)
	case 2:
		renderHelpShortcuts(&b, m)
	case 3:
		renderHelpVim(&b, m)
	}
	return b.String()
}

const (
	helpHeaderOpen  = "\x1b[1m\x1b[38;2;88;110;117m"
	helpHeaderClose = "\x1b[0m"
	cHelpDescOpen   = "\x1b[38;2;88;110;117m"
	cHelpDescClose  = "\x1b[39m"
)

var helpKeyboardHint = "\x1b[1m" + "Keyboard: " + "\x1b[0m" +
	helpBlueOpen + "\u2191/\u2193" + helpBlueClose + " " +
	helpDimGrayOpen + "Navigate" + helpDimGrayClose + "  " +
	helpBlueOpen + "\u2190/\u2192" + helpBlueClose + " " +
	helpDimGrayOpen + "Switch View" + helpDimGrayClose + "  " +
	helpBlueOpen + "esc" + helpBlueClose + " " +
	helpDimGrayOpen + "Close" + helpDimGrayClose

func formatLogTS() string {
	return time.Now().Format("20060102_150405")
}

func renderHelpGeneralDescription(b *strings.Builder) {
	b.WriteString(helpDimGrayOpen)
	b.WriteString(branding.Current().CLIName + " understands your codebase, makes edits with your permission,")
	b.WriteString(helpDimGrayClose)
	b.WriteString("\n")
	b.WriteString(helpDimGrayOpen)
	b.WriteString("and executes commands \u2014 right from your terminal.")
	b.WriteString(helpDimGrayClose)
	b.WriteString("\n\n")
}

func renderHelpGeneralMetadata(b *strings.Builder, tier, cwd, email, conversationID string) {
	b.WriteString(helpDimGrayOpen + "Version " + cliVersion + helpDimGrayClose + "\n")
	b.WriteString(helpDimGrayOpen + email)
	if tier != "" {
		b.WriteString(" (" + tier + ")")
	}
	b.WriteString(helpDimGrayClose + "\n")
	b.WriteString(helpDimGrayOpen + "Workspace:      " + cwd + helpDimGrayClose + "\n")
	b.WriteString(helpDimGrayOpen + "Project Config: ~/.gemini/config/projects/default-cli-project.json" + helpDimGrayClose + "\n")
	b.WriteString(helpDimGrayOpen + "Log File:       ~/.gemini/antigravity-cli/log/cli-" + formatLogTS() + ".log" + helpDimGrayClose + "\n")
	if conversationID != "" {
		b.WriteString(helpDimGrayOpen + "Conversation:   " + conversationID + helpDimGrayClose + "\n")
	}
	b.WriteString("\n")
}

func renderHelpGeneralQuickRef(b *strings.Builder) {
	b.WriteString(helpHeaderOpen + "Quick Reference" + helpHeaderClose + "\n")
	b.WriteString("  " + helpBlueOpen + "/" + helpBlueClose + "  " + helpDimGrayOpen + "Type / to see available commands" + helpDimGrayClose + "\n\n")
}

func renderHelpGeneral(b *strings.Builder, model, tier, cwd, email, conversationID string) {
	renderHelpGeneralDescription(b)
	renderHelpGeneralMetadata(b, tier, cwd, email, conversationID)
	renderHelpGeneralQuickRef(b)
	b.WriteString("\x1b[1mKeyboard: \x1b[0m")
	b.WriteString(helpBlueOpen + "\u2191/\u2193" + helpBlueClose + " " + helpDimGrayOpen + "Navigate" + helpDimGrayClose + "  ")
	b.WriteString(helpBlueOpen + "\u2190/\u2192" + helpBlueClose + " " + helpDimGrayOpen + "Switch View" + helpDimGrayClose + "  ")
	b.WriteString(helpBlueOpen + "esc" + helpBlueClose + " " + helpDimGrayOpen + "Close" + helpDimGrayClose + "\n\n")
}

func renderHelpCommands(b *strings.Builder, m *runMode) {
	b.WriteString(helpHeaderOpen + "Available Commands" + helpHeaderClose + "\n\n")
	cmds := tuilib.SortedSlashCommands()
	total := len(cmds)
	m.helpListClampWindow(total)
	start := m.helpWindow
	visible := helpListVisible(total, start, m.helpListWindowSize())
	for i := 0; i < visible; i++ {
		idx := start + i
		row := buildCommandRow(cmds[idx], idx == m.helpCursor, m.width)
		b.WriteString(row + "\n")
	}
	from := start + 1
	to := start + visible
	fmt.Fprintf(b, "%s  [%d-%d of %d items]%s\n\n", helpDimGrayOpen, from, to, total, helpDimGrayClose)
	b.WriteString(helpKeyboardHint + "\n\n")
}

func renderHelpShortcuts(b *strings.Builder, m *runMode) {
	b.WriteString(helpHeaderOpen + "Keyboard Shortcuts" + helpHeaderClose + "\n")
	b.WriteString("  " + helpBlueOpen + "/keybindings" + helpBlueClose + " " + helpDimGrayOpen + "to customize" + helpDimGrayClose + "\n\n")
	total := len(helpShortcuts)
	m.helpListClampWindow(total)
	start := m.helpWindow
	visible := helpListVisible(total, start, m.helpListWindowSize())
	for i := 0; i < visible; i++ {
		idx := start + i
		b.WriteString(buildShortcutRow(helpShortcuts[idx], idx == m.helpCursor) + "\n")
	}
	from := start + 1
	to := start + visible
	fmt.Fprintf(b, "%s  [%d-%d of %d items]%s\n\n", helpDimGrayOpen, from, to, total, helpDimGrayClose)
	b.WriteString(helpKeyboardHint + "\n\n")
}
