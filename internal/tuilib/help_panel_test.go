package tuilib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/branding"
)

// TestRenderHelpGeneralMetadata pins the GENERAL tab's metadata block: the
// version, the account line (with and without a tier), the workspace, the
// project config path, the log file and the optional conversation line.
func TestRenderHelpGeneralMetadata(t *testing.T) {
	var b strings.Builder
	RenderHelpGeneralMetadata(&b, "Antigravity Starter Quota", "~/x", "u@t", "uuid")
	got := b.String()
	for _, want := range []string{
		"Version ",
		"u@t (Antigravity Starter Quota)",
		"Workspace:      ~/x",
		"Project Config: ~/.gemini/config/projects/default-cli-project.json",
		"Log File:       ~/.gemini/antigravity-cli/log/cli-",
		"Conversation:   uuid",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("metadata missing %q:\n%s", want, got)
		}
	}

	// No tier: the parenthetical suffix disappears.
	var b2 strings.Builder
	RenderHelpGeneralMetadata(&b2, "", "~/x", "u@t", "")
	if strings.Contains(b2.String(), "u@t (") {
		t.Errorf("empty tier still rendered a suffix:\n%s", b2.String())
	}
	if strings.Contains(b2.String(), "Conversation:") {
		t.Errorf("empty conversation id still rendered a line:\n%s", b2.String())
	}
}

// TestRenderHelpGeneralDescription pins the overlay's pitch paragraph.
func TestRenderHelpGeneralDescription(t *testing.T) {
	var b strings.Builder
	RenderHelpGeneralDescription(&b)
	got := b.String()
	if !strings.Contains(got, branding.Current().CLIName+" understands your codebase") {
		t.Errorf("description missing the CLI name:\n%s", got)
	}
	if !strings.Contains(got, "right from your terminal.") {
		t.Errorf("description missing the closing line:\n%s", got)
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Errorf("description should end with a blank line, got %q", got)
	}
}

// TestRenderHelpGeneralQuickRef pins the Quick Reference section.
func TestRenderHelpGeneralQuickRef(t *testing.T) {
	var b strings.Builder
	RenderHelpGeneralQuickRef(&b)
	got := b.String()
	if !strings.Contains(got, "Quick Reference") {
		t.Errorf("quick ref missing the header:\n%s", got)
	}
	if !strings.Contains(got, "Type / to see available commands") {
		t.Errorf("quick ref missing the hint:\n%s", got)
	}
}

// TestRenderHelpGeneral pins the whole GENERAL tab: description, metadata,
// quick reference and the keyboard line.
func TestRenderHelpGeneral(t *testing.T) {
	var b strings.Builder
	RenderHelpGeneral(&b, "Gemini 3.8 Flash (High)", "Starter", "~/x", "u@t", "uuid")
	got := b.String()
	for _, want := range []string{
		"understands your codebase",
		"Version ",
		"Workspace:      ~/x",
		"Quick Reference",
		"Keyboard: ",
		"Switch View",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("general tab missing %q:\n%s", want, got)
		}
	}
}

// TestRenderTabBarTabs pins the tab bar: the brand, the tab labels, the
// active tab's bold+underline, and the cycle hint.
func TestRenderTabBarTabs(t *testing.T) {
	got := RenderTabBarTabs(1, []string{"general", "commands", "shortcuts"})
	if !strings.Contains(got, branding.Current().CLIName) {
		t.Errorf("tab bar missing the brand:\n%s", got)
	}
	if !strings.Contains(got, TabBarHintSuffix) {
		t.Errorf("tab bar missing the hint:\n%s", got)
	}
	if !strings.Contains(got, TabBarActiveOpen+"commands") {
		t.Errorf("tab bar did not mark the active tab:\n%s", got)
	}
	if strings.Contains(got, TabBarActiveOpen+"general") {
		t.Errorf("tab bar marked an inactive tab active:\n%s", got)
	}
}

// TestRenderTabBar pins the three-tab default bar.
func TestRenderTabBar(t *testing.T) {
	got := RenderTabBar(0)
	for _, want := range []string{"general", "commands", "shortcuts"} {
		if !strings.Contains(got, want) {
			t.Errorf("tab bar missing %q:\n%s", want, got)
		}
	}
}

// TestRenderWarning pins the cycle-81 fallback warning card.
func TestRenderWarning(t *testing.T) {
	got := RenderWarning("old-model", "new-model")
	for _, want := range []string{"⚠ Warning", "\"old-model\" is no longer available. Using \"new-model\"."} {
		if !strings.Contains(got, want) {
			t.Errorf("warning missing %q:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("warning should end with a newline, got %q", got)
	}
}

// TestRenderAgentWarning pins the ⚠ Agent Not Found card.
func TestRenderAgentWarning(t *testing.T) {
	got := RenderAgentWarning("no such agent")
	for _, want := range []string{"⚠ Agent Not Found", "no such agent"} {
		if !strings.Contains(got, want) {
			t.Errorf("agent warning missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "  ⎿  ") {
		t.Errorf("agent warning missing the ⎿ marker:\n%s", got)
	}
}

// TestStripTier pins the parenthesised-tier suffix removal.
func TestStripTier(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Gemini 3.8 Flash (High)", "Gemini 3.8 Flash"},
		{"Claude Sonnet 4.6 (Thinking)", "Claude Sonnet 4.6"},
		{"Gemini 3.8 Flash", "Gemini 3.8 Flash"},
		{"Model (Tier)", "Model"},
		{"Model (unclosed", "Model (unclosed"},
	} {
		if got := StripTier(tc.in); got != tc.want {
			t.Errorf("StripTier(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestShortenCwd pins the $HOME rewrite.
func TestShortenCwd(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}
	if got := ShortenCwd(home); got != "~" {
		t.Errorf("ShortenCwd(%q) = %q, want %q", home, got, "~")
	}
	if got := ShortenCwd(filepath.Join(home, "x")); got != "~/x" {
		t.Errorf("ShortenCwd(%q) = %q, want %q", filepath.Join(home, "x"), got, "~/x")
	}
	if got := ShortenCwd("/nowhere/else"); got != "/nowhere/else" {
		t.Errorf("ShortenCwd(/nowhere/else) = %q, want it unchanged", got)
	}
}

// TestFormatLogTS pins the log-file timestamp's shape.
func TestFormatLogTS(t *testing.T) {
	got := FormatLogTS()
	if len(got) != len("20060102_150405") {
		t.Errorf("FormatLogTS() = %q, want 15 characters", got)
	}
	if strings.ContainsAny(got, "-:/. ") {
		t.Errorf("FormatLogTS() = %q, want only digits and one underscore", got)
	}
}
