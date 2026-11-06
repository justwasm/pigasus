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

package tui

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/policy/permissions"
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
	if m.overlays.panel.kind != panelregistry.Plugin {
		t.Errorf("the open panel = %v, want panelregistry.Plugin", m.overlays.panel.kind)
	}
	// The panel carries the canonical name (A-31).
	if m.overlays.panel.cmd != "/plugin" {
		t.Errorf("the open panel's command = %q, want %q", m.overlays.panel.cmd, "/plugin")
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
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("/plugins list opened the %v panel, want no panel", m.overlays.panel.kind)
	}
	got := termcolor.StripSGR(strings.Join(m.engine.response, "\n"))
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
	got := termcolor.StripSGR(strings.Join(m.engine.response, "\n"))
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
		got := termcolor.StripSGR(strings.Join(m.engine.response, "\n"))
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

func TestChainedSlashCommands_BoostPlanGoal(t *testing.T) {
	m := newSlashModel(t)
	handled, cmd := m.handleSlashCommand("/boost /plan /goal refactor engine")
	if !handled {
		t.Fatalf("expected /boost /plan /goal to be handled")
	}
	if cmd == nil {
		t.Fatalf("expected tea.Cmd batch for streaming turn")
	}
	// Verify state modifiers: ModePlan
	if m.session.mode != permissions.ModePlan {
		t.Errorf("m.session.mode = %v, want ModePlan", m.session.mode)
	}
	// Verify state modifiers: GoalState
	if m.session.activeGoal == nil || m.session.activeGoal.GoalText != "refactor engine" {
		t.Errorf("activeGoal = %v, want 'refactor engine'", m.session.activeGoal)
	}
	// Verify display echo
	if len(m.engine.response) == 0 {
		t.Fatalf("expected response echo")
	}
	echoRow := m.engine.response[0]
	wantEcho := "> /boost /plan /goal refactor engine"
	if echoRow != wantEcho {
		t.Errorf("echoRow = %q, want %q", echoRow, wantEcho)
	}
	// Verify session history prompt composition
	if len(m.engine.session) == 0 {
		t.Fatalf("expected session message")
	}
	prompt := m.engine.session[0].Content
	if !strings.Contains(prompt, "<ORCHESTRATOR>") {
		t.Errorf("prompt missing <ORCHESTRATOR>, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "refactor engine") {
		t.Errorf("prompt missing task, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Help me create a plan") {
		t.Errorf("prompt missing PlanPrompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Help me set a clear goal") {
		t.Errorf("prompt missing GoalPrompt, got:\n%s", prompt)
	}
}

func TestChainedSlashCommands_PlanGoalWithoutTask(t *testing.T) {
	m := newSlashModel(t)
	handled, cmd := m.handleSlashCommand("/plan /goal")
	if !handled {
		t.Fatalf("expected /plan /goal to be handled")
	}
	if cmd == nil {
		t.Fatalf("expected tea.Cmd batch for streaming turn")
	}
	if m.session.mode != permissions.ModePlan {
		t.Errorf("m.session.mode = %v, want ModePlan", m.session.mode)
	}
	if m.session.activeGoal == nil || m.session.activeGoal.GoalText != "Help me set a clear goal for this session" {
		t.Errorf("activeGoal = %v, want default goal text", m.session.activeGoal)
	}
	if len(m.engine.response) == 0 || m.engine.response[0] != "> /plan /goal" {
		t.Errorf("echoRow = %v, want '> /plan /goal'", m.engine.response)
	}
}

func TestChainedSlashCommands_QueuesWhileStreaming(t *testing.T) {
	m := newSlashModel(t)
	m.engine.streaming = true

	input := "/boost /plan /goal refactor engine"
	if !m.slashQueuesWhileStreaming(input) {
		t.Fatalf("expected chained commands to queue while streaming")
	}

	m.composer.input.SetValue(input)
	cmd := m.submitPrompt()
	if cmd != nil {
		t.Errorf("expected nil cmd when queued, got %v", cmd)
	}
	if len(m.session.store.StreamQueue) != 1 || m.session.store.StreamQueue[0] != input {
		t.Errorf("expected %q in StreamQueue, got %v", input, m.session.store.StreamQueue)
	}
}
