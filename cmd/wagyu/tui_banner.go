package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/justwasm/pigasus/internal/branding"
)

func renderBanner(w io.Writer, email, model, tier, cwd string) {
	artRows := renderLogoLines()
	fmt.Fprintf(w, "%s  "+cBlueBoldOpen+"%s "+cliVersion+"\x1b[0m\n", artRows[0], branding.Current().CLIName)
	emailLine := email
	if tier != "" {
		emailLine += " (" + tier + ")"
	}
	fmt.Fprintf(w, logoLines[1]+"\n", artRows[1], emailLine)
	fmt.Fprintf(w, logoLines[2]+"\n", artRows[2], model)
	fmt.Fprintf(w, logoLines[3]+"\n", artRows[3], shortenCwd(cwd))
	if !envHideLogoOn() {
		fmt.Fprintln(w, artRows[4])
	} else {
		fmt.Fprintln(w)
	}
}

// shortenCwd replaces the leading $HOME with "~" so the banner
// displays "~/GitHub/openagy" instead of "/Users/gear/GitHub/openagy".
func shortenCwd(cwd string) string {
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

// stripTier removes a parenthesised tier suffix from a model display name.
func stripTier(model string) string {
	if i := strings.LastIndex(model, " ("); i >= 0 {
		if model[len(model)-1] == ')' {
			return model[:i]
		}
	}
	return model
}

// renderAgentWarning is the reference's ⚠ Agent Not Found card.
func renderAgentWarning(body string) string {
	var b strings.Builder
	b.WriteString(cWarningOpen)
	b.WriteString("⚠ Agent Not Found")
	b.WriteString(cWarningClose)
	b.WriteString("\n")
	b.WriteString(cWarningBlueOpen)
	b.WriteString("  ⎿  ")
	b.WriteString(cWarningBlueClose)
	b.WriteString(cWarningBodyOpen)
	b.WriteString(body)
	b.WriteString(cWarningBodyClose)
	b.WriteString("\n")
	return b.String()
}

// renderWarning emits the cycle-81 fallback warning block.
func renderWarning(from, to string) string {
	var b strings.Builder
	b.WriteString(cWarningOpen)
	b.WriteString("⚠ Warning")
	b.WriteString(cWarningClose)
	b.WriteString("\n")
	b.WriteString(cWarningBlueOpen)
	b.WriteString("  ⎿  ")
	b.WriteString(cWarningBlueClose)
	b.WriteString(cWarningBodyOpen)
	b.WriteString("\"")
	b.WriteString(from)
	b.WriteString("\" is no longer available. Using \"")
	b.WriteString(to)
	b.WriteString("\".")
	b.WriteString(cWarningBodyClose)
	b.WriteString("\n")
	return b.String()
}

const (
	tabBarBrandOpen     = cBlueOpen
	tabBarBrandClose    = cBlueClose
	tabBarActiveOpen    = "\x1b[1;4m" + cBlueOpen
	tabBarActiveClose   = "\x1b[0m"
	tabBarInactiveOpen  = cSeparatorOpen
	tabBarInactiveClose = cSeparatorClose
	tabBarHintOpen      = cSeparatorOpen
	tabBarHintClose     = cSeparatorClose
	tabBarHintSuffix    = "  (\u2190/\u2192 or tab to cycle)"
)

// renderTabBar emits the cycle-85 styled tab bar.
func renderTabBar(active int) string {
	return renderTabBarTabs(active, []string{"general", "commands", "shortcuts"})
}

// renderTabBarTabs emits the tab bar for an arbitrary tab list.
func renderTabBarTabs(active int, tabs []string) string {
	var b strings.Builder
	b.WriteString(tabBarBrandOpen)
	b.WriteString(branding.Current().CLIName)
	b.WriteString(tabBarBrandClose)
	b.WriteString("   ")
	for i, name := range tabs {
		if i > 0 {
			b.WriteString("    ") // 4 spaces between tabs
		}
		if i == active {
			b.WriteString(tabBarActiveOpen)
			b.WriteString(name)
			b.WriteString(tabBarActiveClose)
		} else {
			b.WriteString(tabBarInactiveOpen)
			b.WriteString(name)
			b.WriteString(tabBarInactiveClose)
		}
	}
	b.WriteString(" ")
	b.WriteString(tabBarHintOpen)
	b.WriteString(tabBarHintSuffix)
	b.WriteString(tabBarHintClose)
	return b.String()
}
