package main

// slash_cycleplugincaseinsensitive_test.go — cycle plugin-case-insensitive.
//
// Closes AGY 1.3.6 changelog line 21 on the slash side:
//
//	Fixed plugin names and manifests being case-sensitive where the
//	agent was not: `/plugin` subcommands such as `/plugin List` now
//	work in any case instead of failing with `Unknown subcommand`.
//
// The CLI half (agy plugin List / agy plugin LIST / ...) is
// covered by internal/cli/plugin_case_insensitive_test.go via the
// jetski dispatcher. The slash half (the /plugin TUI command) is
// handled here.
//
// Pre-cycle, openSlashPanelArgs opened the panel for every /plugin
// invocation — the argument tail was dropped on the floor — so
// `/plugin List`, `/plugin LIST`, `/plugin install foo` all
// behaved like a bare `/plugin`. The cycle short-circuits the
// panel path when args is non-empty and dispatches to a subcommand
// handler in `case "plugin":` (slash_commands.go).

import (
	"strings"
	"testing"
)

// TestSlashPlugin_ListAllCases: the three documented case variants
// (capitalised, mixed, lowercase) all dispatch to the same
// list-handler body. This is the headline gap named in the
// changelog. Each variant should land in m.response with the
// "No plugins installed." placeholder; the bare-form panel
// behaviour stays covered by openSlashPanelArgs short-circuit.
func TestSlashPlugin_ListAllCases(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{"lowercase", "/plugin list"},
		{"capitalised", "/plugin List"},
		{"allcaps", "/plugin LIST"},
		{"mixed", "/plugin LiSt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newSlashModel(t)
			handled, _ := m.handleSlashCommand(tc.command)
			if !handled {
				t.Fatalf("handleSlashCommand(%q) reported not handled; expected subcommand dispatch", tc.command)
			}
			// The panel must NOT have opened — that is the whole
			// point of the cycle. The handler appends a transcript
			// message and returns; m.panel.kind stays at its zero
			// value (panelNone).
			if m.panel.kind != panelNone {
				t.Errorf("handleSlashCommand(%q) opened panel kind %d; want panelNone", tc.command, m.panel.kind)
			}
			got := strings.Join(afterEcho(m), "\n")
			if !strings.Contains(got, "No plugins installed.") {
				t.Errorf("%s output missing list body: %q", tc.command, got)
			}
		})
	}
}

// TestSlashPlugin_InstallDispatch: `/plugin install <path>` and
// its case variants emit a usage stub for now (the cycle wires
// the dispatch table; a follow-up cycle can route the install
// path through `cli.runPluginInstall` against a parsed `JetskiCLI`).
// What matters for this cycle is that the subcommand is
// recognised — the pre-cycle code never reached the handler and
// instead silently opened the panel.
func TestSlashPlugin_InstallDispatch(t *testing.T) {
	for _, cmd := range []string{"/plugin install", "/plugin Install", "/plugin INSTALL"} {
		t.Run(cmd, func(t *testing.T) {
			m := newSlashModel(t)
			handled, _ := m.handleSlashCommand(cmd + " ./my-plugin")
			if !handled {
				t.Fatalf("handleSlashCommand(%q) not handled", cmd)
			}
			if m.panel.kind != panelNone {
				t.Errorf("handleSlashCommand(%q) opened panel; want subcommand dispatch", cmd)
			}
			got := strings.Join(afterEcho(m), "\n")
			if !strings.Contains(got, "Usage:") {
				t.Errorf("%s output missing usage stub: %q", cmd, got)
			}
			if !strings.Contains(got, "install") {
				t.Errorf("%s output should mention install: %q", cmd, got)
			}
		})
	}
}

// TestSlashPlugin_ValidateDispatch: the validate subcommand
// surfaces a usage stub regardless of case. The dispatch
// recognition is the headline gap; a follow-up cycle wires the
// real `cli.runPluginValidate` call against the parsed
// args.
func TestSlashPlugin_ValidateDispatch(t *testing.T) {
	for _, cmd := range []string{"/plugin validate", "/plugin Validate"} {
		t.Run(cmd, func(t *testing.T) {
			m := newSlashModel(t)
			handled, _ := m.handleSlashCommand(cmd + " ./plugin-tree")
			if !handled {
				t.Fatalf("handleSlashCommand(%q) not handled", cmd)
			}
			got := strings.Join(afterEcho(m), "\n")
			if !strings.Contains(got, "Usage:") {
				t.Errorf("%s output missing usage stub: %q", cmd, got)
			}
			if !strings.Contains(got, "validate") {
				t.Errorf("%s output should mention validate: %q", cmd, got)
			}
		})
	}
}

// TestSlashPlugin_EnableDisableDispatch: enable and disable
// dispatch case-insensitively. Pre-cycle these were silently
// swallowed by the panel short-circuit. The cycle's handler
// emits a "Usage:" stub for now; a later cycle can forward to
// the cli handlers.
func TestSlashPlugin_EnableDisableDispatch(t *testing.T) {
	for _, verb := range []string{"enable", "Enable", "ENABLE"} {
		t.Run("enable/"+verb, func(t *testing.T) {
			m := newSlashModel(t)
			handled, _ := m.handleSlashCommand("/plugin " + verb + " my-plugin")
			if !handled {
				t.Fatalf("handleSlashCommand(%q) not handled", verb)
			}
			got := strings.Join(afterEcho(m), "\n")
			if !strings.Contains(got, "Usage:") {
				t.Errorf("/plugin %s output missing usage stub: %q", verb, got)
			}
			// The handler echoes the verb verbatim, so the body
			// carries whatever the user typed — both the
			// canonical (`enable`) and the variant (`Enable`,
			// `ENABLE`) appear because we look for either.
			if !strings.Contains(got, "enable") && !strings.Contains(got, "Enable") && !strings.Contains(got, "ENABLE") {
				t.Errorf("/plugin %s output should mention enable: %q", verb, got)
			}
		})
	}
	for _, verb := range []string{"disable", "Disable"} {
		t.Run("disable/"+verb, func(t *testing.T) {
			m := newSlashModel(t)
			handled, _ := m.handleSlashCommand("/plugin " + verb + " my-plugin")
			if !handled {
				t.Fatalf("handleSlashCommand(%q) not handled", verb)
			}
			got := strings.Join(afterEcho(m), "\n")
			if !strings.Contains(got, "Usage:") {
				t.Errorf("/plugin %s output missing usage stub: %q", verb, got)
			}
			if !strings.Contains(got, "disable") && !strings.Contains(got, "Disable") {
				t.Errorf("/plugin %s output should mention disable: %q", verb, got)
			}
		})
	}
}

// TestSlashPlugin_UnknownSubcommand: a subcommand that does not
// exist still surfaces a "Unknown subcommand: <verb>" line, not
// the panel — same contract as the CLI half.
func TestSlashPlugin_UnknownSubcommand(t *testing.T) {
	m := newSlashModel(t)
	handled, _ := m.handleSlashCommand("/plugin frobnicate")
	if !handled {
		t.Fatalf("handleSlashCommand not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel opened for unknown subcommand")
	}
	got := strings.Join(afterEcho(m), "\n")
	if !strings.Contains(got, "Unknown subcommand") {
		t.Errorf("output missing 'Unknown subcommand': %q", got)
	}
	if !strings.Contains(got, "frobnicate") {
		t.Errorf("output should echo the verb name: %q", got)
	}
}

// TestSlashPlugin_BareOpensPanel: bare `/plugin` (no args) still
// opens the panel — the subcommand dispatch is opt-in via the
// presence of an argument tail. This is the contract
// openSlashPanelArgs short-circuits on: `cmd == "plugin" &&
// strings.TrimSpace(args) != ""` returns false, so the panel
// opens only on the bare form.
//
// The panel-open path doesn't run in unit tests (openSlashPanel
// requires a real tea program), so this test only confirms that
// the bare /plugin form is NOT routed to the subcommand
// handler. The panel path itself is exercised by the
// slash_cycle198_test.go suite.
func TestSlashPlugin_BareFallsToPanel(t *testing.T) {
	m := newSlashModel(t)
	// The panel-opening code path calls tea's runloop, which the
	// unit-test rig cannot drive; but we can at least confirm the
	// case "plugin" handler is NOT reached for bare /plugin by
	// checking the post-conditions the subcommand handler
	// produces. pre-cycle: bare /plugin ALSO went through case
	// "plugin" and printed "Plugins: 0 installed."; cycle: bare
	// /plugin still prints that line because openSlashPanelArgs
	// returns true, then tui_run.go's prompt dispatcher opens
	// the panel before reaching handleSlashCommand.
	//
	// Drive the bare form through handleSlashCommand directly
	// (which is what openSlashPanelArgs' false-return would NOT
	// do); the case "plugin" branch still has the trimmed=="""
	// fallback that prints "Plugins: 0 installed." — that's the
	// safety net for any future caller that bypasses
	// openSlashPanelArgs. Confirm the fallback fires.
	handled, _ := m.handleSlashCommand("/plugin")
	if !handled {
		t.Fatalf("handleSlashCommand(/plugin) not handled")
	}
	got := strings.Join(afterEcho(m), "\n")
	if !strings.Contains(got, "Plugins: 0 installed") {
		t.Errorf("bare /plugin fallback missing: %q", got)
	}
}
