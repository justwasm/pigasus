// cycle383_slash_test.go — the slash-command divergences the
// official-docs survey closed (cycle 383, slice A).
//
// Four findings, four facts:
//
//   - A-6  `/plugin` has the alias `/plugins`
//     (official-docs/cli/reference.md:41, official-docs/slash-commands.md:159).
//   - A-8  the command is `/teamwork-preview`, with `/teamwork` as its
//     alias (official-docs/cli/reference.md:50, official-docs/slash-commands.md:34).
//   - A-28 `/plugin`'s unknown-subcommand message is the reference's own
//     format string, "Unknown subcommand %q." 【strings】 agy 1.4.11.
//   - A-31 the panel exit notice echoes the command's *canonical* name,
//     so every alias of a command closes with the same bytes.
//
// The alias rule has one owner (internal/ui/slashcommands), so these
// tests exercise the host's *use* of that rule, not the rule itself —
// the rule is pinned in internal/ui/slashcommands/canonical_test.go.

package cliapp

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
)

// TestPluginsAliasOpensThePanel pins the bare `/plugins`: it opens the
// same Plugins Manager panel as `/plugin`.
//
// A-6: the reference's own table lists `/plugins` as the alias of
// `/plugin` (official-docs/cli/reference.md:41).
func TestPluginsAliasOpensThePanel(t *testing.T) {
	m := newSlashModel(t)
	if !m.openSlashPanelArgs("plugins", "") {
		t.Fatal("openSlashPanelArgs(plugins, \"\") = false, want true")
	}
	if m.panel.kind != panelregistry.Plugin {
		t.Errorf("the open panel = %v, want panelregistry.Plugin", m.panel.kind)
	}
	// The panel carries the canonical name (A-31).
	if m.panel.cmd != "/plugin" {
		t.Errorf("the open panel's command = %q, want %q", m.panel.cmd, "/plugin")
	}
}

// TestPluginsAliasDispatchesTheVerb pins the argument-carrying form:
// `/plugins list` is `/plugin list` and dispatches the verb instead of
// opening the panel with the argument swallowed.
//
// The rule lives in openSlashPanelArgs, so the alias has to resolve
// before the "a command with an argument tail is not a panel" rules are
// applied.
func TestPluginsAliasDispatchesTheVerb(t *testing.T) {
	m := newSlashModel(t)
	handled, _ := m.dispatchSlashLine("/plugins list")
	if !handled {
		t.Fatal("/plugins list was not handled")
	}
	if m.panel.kind != panelregistry.None {
		t.Errorf("/plugins list opened the %v panel, want no panel", m.panel.kind)
	}
	got := termcolor.StripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(got, "No plugins installed.") {
		t.Errorf("/plugins list did not answer the list verb; transcript:\n%s", got)
	}
}

// TestPluginUnknownSubcommandIsTheReferenceString pins the reference's
// own format string: "Unknown subcommand %q." — the verb is quoted and
// the sentence ends with a period.
//
// Evidence 【strings】: the agy 1.4.11 string table carries the literal
// "Unknown subcommand %q.".
func TestPluginUnknownSubcommandIsTheReferenceString(t *testing.T) {
	m := newSlashModel(t)
	handled, _ := m.handleSlashCommand("/plugin bogus")
	if !handled {
		t.Fatal("/plugin bogus was not handled")
	}
	got := termcolor.StripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(got, `Unknown subcommand "bogus".`) {
		t.Errorf("the unknown-subcommand message is not the reference's; transcript:\n%s", got)
	}
	if strings.Contains(got, "Unknown subcommand:") {
		t.Errorf("the clone's own wording survived; transcript:\n%s", got)
	}
}

// TestPanelExitNoticeEchoesTheCanonicalName pins A-31: the exit notice
// names the command, not the spelling the user typed.
//
// The reference's binary carries one literal per command ("Exited
// /config command"), not one per alias, so `/settings` and `/config`
// close with the same bytes.
func TestPanelExitNoticeEchoesTheCanonicalName(t *testing.T) {
	for _, typed := range []string{"config", "settings"} {
		m := newSlashModel(t)
		if !m.openSlashPanel(typed) {
			t.Fatalf("openSlashPanel(%q) = false, want true", typed)
		}
		m.closePanel()
		got := termcolor.StripSGR(strings.Join(m.response, "\n"))
		if !strings.Contains(got, "Exited /config command") {
			t.Errorf("%s closed with the wrong notice; transcript:\n%s", typed, got)
		}
		if strings.Contains(got, "Exited /"+typed+" command") && typed != "config" {
			t.Errorf("%s closed with its own spelling; transcript:\n%s", typed, got)
		}
	}
}

// TestTeamworkPreviewIsTheCommand pins A-8 at the host's seam: both
// spellings reach the one handler, and the table is keyed by the
// canonical name.
func TestTeamworkPreviewIsTheCommand(t *testing.T) {
	if _, ok := slashHandlers["teamwork-preview"]; !ok {
		t.Error("the host has no handler for /teamwork-preview")
	}
	if _, ok := slashHandlers["teamwork"]; ok {
		t.Error("the host's table carries the alias /teamwork as a second key")
	}
}
