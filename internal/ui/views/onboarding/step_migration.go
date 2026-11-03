package onboarding

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
)

// MigrationItem represents one detected legacy asset available for migration.
type MigrationItem struct {
	ID          string // Unique identifier, e.g. "ext:<name>", "settings", "keyring"
	Label       string // Primary label displayed in checklist
	Description string // Optional detail description
	Selected    bool   // Whether checkbox is checked
}

const stepNameMigration = "Migrate legacy Gemini CLI assets:"

// formatExtensionsLabel formats the checklist label for a detected legacy extension.
func formatExtensionsLabel(name string) string {
	return fmt.Sprintf("Convert legacy extension %q to native plugin", name)
}

// DetectGeminiAssets inspects the given Gemini home directory (typically ~/.gemini)
// and returns all detected legacy configuration and extension assets.
func DetectGeminiAssets(geminiHome string) []MigrationItem {
	if strings.TrimSpace(geminiHome) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		geminiHome = filepath.Join(home, ".gemini")
	}

	var items []MigrationItem

	// 1. Check for legacy extensions in ~/.gemini/extensions/*/gemini-extension.json
	extsDir := filepath.Join(geminiHome, "extensions")
	if entries, err := os.ReadDir(extsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			manifestPath := filepath.Join(extsDir, e.Name(), "gemini-extension.json")
			if fi, err := os.Stat(manifestPath); err == nil && !fi.IsDir() {
				items = append(items, MigrationItem{
					ID:          "ext:" + e.Name(),
					Label:       formatExtensionsLabel(e.Name()),
					Description: "Extension manifest and custom commands",
					Selected:    true,
				})
			}
		}
	}

	// 2. Check for legacy settings in ~/.gemini/settings.json
	settingsPath := filepath.Join(geminiHome, "settings.json")
	if fi, err := os.Stat(settingsPath); err == nil && !fi.IsDir() {
		items = append(items, MigrationItem{
			ID:          "settings",
			Label:       "Align global settings and preferences (~/.gemini/settings.json)",
			Description: "Visual parameters, model preferences, and execution rules",
			Selected:    true,
		})
	}

	// 3. Check for legacy token files
	tokenPath := filepath.Join(geminiHome, "jetski-standalone-oauth-token")
	accountsPath := filepath.Join(geminiHome, "google_accounts.json")
	hasToken := false
	if fi, err := os.Stat(tokenPath); err == nil && !fi.IsDir() {
		hasToken = true
	} else if fi, err := os.Stat(accountsPath); err == nil && !fi.IsDir() {
		hasToken = true
	}
	if hasToken {
		items = append(items, MigrationItem{
			ID:          "keyring",
			Label:       "Migrate active session tokens into operating system keyring",
			Description: "Secure token migration for Google authentication",
			Selected:    true,
		})
	}

	return items
}

func renderMigrationLines(items []MigrationItem, cursor int, width int) []string {
	var lines []string
	lines = append(lines, stepNameMigration, "")
	intro := "Legacy configuration profiles were detected on your system."
	prompt := "Choose which assets you want to migrate into Antigravity CLI:"
	if width > 0 {
		lines = append(lines, wrapProseLines(intro, width, "  ")...)
		lines = append(lines, wrapProseLines(prompt, width, "  ")...)
	} else {
		lines = append(lines, "  "+intro, "  "+prompt)
	}
	lines = append(lines, "")

	for i, item := range items {
		check := termsCheckboxOff
		if item.Selected {
			check = termsCheckboxOn
		}
		pointer := " "
		if i == cursor {
			pointer = "▸"
		}
		itemLine := fmt.Sprintf("%s [%s] %s", pointer, check, item.Label)
		if width > 0 {
			lines = append(lines, wrapCheckboxLines(item.Label, width, fmt.Sprintf("  %s [%s] ", pointer, check))...)
			if item.Description != "" {
				desc := "        " + ansiDim + item.Description + ansiReset
				lines = append(lines, desc)
			}
		} else {
			lines = append(lines, "  "+itemLine)
			if item.Description != "" {
				lines = append(lines, "        "+ansiDim+item.Description+ansiReset)
			}
		}
	}

	return lines
}

func renderMigrationWithDims(w io.Writer, items []MigrationItem, cursor int, width, height, scroll int) error {
	lines := renderMigrationLines(items, cursor, width)
	footer := "                       [ Previous ]    [ Next ]"
	if width > 0 && len(footer) > width {
		btn := "[ Previous ]  [ Next ]"
		if len(btn) <= width {
			pad := (width - len(btn)) / 2
			footer = strings.Repeat(" ", pad) + btn
		} else {
			footer = btn
		}
	}
	if height > 0 {
		area := max(1, height-3)
		lines = tuiviewport.ScrollWindow(len(lines), scroll, area).SliceRows(lines)
	}
	for _, l := range lines {
		if _, err := fmt.Fprintln(w, l); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, footer)
	return err
}
