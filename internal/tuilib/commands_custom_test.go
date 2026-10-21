// commands_custom_test.go — cycle custom-slash-commands-no-plugin-injection.
//
// The picker integration test pins the contract that
// FilterSlashCommandsWithCustom merges the hardcoded built-in
// catalog with customcmd-loaded plugin / project / global commands
// under the AGY 1.2.16 precedence (built-in > plugin > project >
// global). The merge is the user-visible surface of the cycle:
//
//   - /foo typed in the prompt must resolve to the same command
//     every run, regardless of which on-disk root declared it.
//   - A plugin command "foo" must not be silently shadowed by a
//     project command "foo".
//   - A built-in command "foo" must not be silently shadowed by any
//     custom command.
//
// The FilterSlashCommands surface stays unchanged so the existing
// cycle-83 / cycle-146 / cycle-184 picker tests keep pinning their
// invariants; the new helper is an additive path the picker calls
// once custom commands are loaded.
package tuilib

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/customcmd"
)

// findCommand returns the first SlashCommand with the given name or
// nil if no such entry exists.
func findCommand(rows []SlashCommand, name string) *SlashCommand {
	for i := range rows {
		if rows[i].Name == name {
			return &rows[i]
		}
	}
	return nil
}

// TestFilterSlashCommandsWithCustom_NilFallsBackToBuiltins pins the
// backward-compat contract: when the loader returns no custom
// commands, the picker behaves exactly like the existing
// FilterSlashCommands. This keeps the cycle-83 / cycle-146 / cycle-184
// invariants unchanged on a machine with no plugins, no project
// .claude/, and no global commands/.
func TestFilterSlashCommandsWithCustom_NilFallsBackToBuiltins(t *testing.T) {
	withNil := FilterSlashCommandsWithCustom("", nil)
	base := FilterSlashCommands("")
	if len(withNil) != len(base) {
		t.Fatalf("len with nil = %d, len base = %d", len(withNil), len(base))
	}
	for i := range withNil {
		if withNil[i].Name != base[i].Name || withNil[i].Description != base[i].Description {
			t.Errorf("row %d differs: %q vs %q", i, withNil[i].Name, base[i].Name)
		}
	}
}

// TestFilterSlashCommandsWithCustom_BuiltinShadowsProject pins the
// top-of-the-stack precedence: a built-in /foo must beat a project
// /foo. The picker surfaces the built-in's description; the
// project's command is dropped.
func TestFilterSlashCommandsWithCustom_BuiltinShadowsProject(t *testing.T) {
	project := []customcmd.Command{{
		Name:        "help", // built-in: /help; project: "help"
		Description: "PROJECT help shadow",
		Source:      customcmd.SourceProject,
	}}
	rows := FilterSlashCommandsWithCustom("", project)
	got := findCommand(rows, "/help")
	if got == nil {
		t.Fatalf("/help not in catalog; got %d rows", len(rows))
	}
	if got.Description == "PROJECT help shadow" {
		t.Errorf("project's /help shadowed the built-in; want the built-in description")
	}
}

// TestFilterSlashCommandsWithCustom_PluginShadowsProject is the
// cycle's load-bearing test. A plugin's /foo must surface in the
// picker; the project's /foo (same name, lower precedence) must be
// dropped.
func TestFilterSlashCommandsWithCustom_PluginShadowsProject(t *testing.T) {
	custom := []customcmd.Command{
		{Name: "deploy", Description: "plugin deploy", Source: customcmd.SourcePlugin},
		{Name: "deploy", Description: "project deploy", Source: customcmd.SourceProject},
		{Name: "deploy", Description: "global deploy", Source: customcmd.SourceGlobal},
	}
	rows := FilterSlashCommandsWithCustom("", custom)
	got := findCommand(rows, "/deploy")
	if got == nil {
		t.Fatalf("/deploy not in catalog; got %d rows", len(rows))
	}
	if got.Description != "plugin deploy" {
		t.Errorf("Description = %q, want %q (plugin wins over project + global)", got.Description, "plugin deploy")
	}
	// No duplicate "/deploy" row should appear — the merge drops the
	// project + global losers, not just shadows them.
	count := 0
	for _, r := range rows {
		if r.Name == "/deploy" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("/deploy appears %d times in catalog, want 1", count)
	}
}

// TestFilterSlashCommandsWithCustom_ProjectShadowsGlobal pins the
// third precedence rule: a project /foo beats a global /foo when no
// plugin declares /foo.
func TestFilterSlashCommandsWithCustom_ProjectShadowsGlobal(t *testing.T) {
	custom := []customcmd.Command{
		{Name: "lint", Description: "global lint", Source: customcmd.SourceGlobal},
		{Name: "lint", Description: "project lint", Source: customcmd.SourceProject},
	}
	rows := FilterSlashCommandsWithCustom("", custom)
	got := findCommand(rows, "/lint")
	if got == nil {
		t.Fatalf("/lint not in catalog; got %d rows", len(rows))
	}
	if got.Description != "project lint" {
		t.Errorf("Description = %q, want %q (project wins over global)", got.Description, "project lint")
	}
}

// TestFilterSlashCommandsWithCustom_Deterministic verifies the AGY
// 1.2.16 contract that two picker calls return rows in a stable
// order. Built-in + custom rows interleave by sorted Name; no
// shuffling across runs.
func TestFilterSlashCommandsWithCustom_Deterministic(t *testing.T) {
	custom := []customcmd.Command{
		{Name: "alpha", Description: "alpha plugin", Source: customcmd.SourcePlugin},
		{Name: "zeta", Description: "zeta project", Source: customcmd.SourceProject},
		{Name: "mid", Description: "mid global", Source: customcmd.SourceGlobal},
	}
	first := FilterSlashCommandsWithCustom("", custom)
	second := FilterSlashCommandsWithCustom("", custom)
	if len(first) != len(second) {
		t.Fatalf("len first=%d, len second=%d", len(first), len(second))
	}
	for i := range first {
		if first[i].Name != second[i].Name || first[i].Description != second[i].Description {
			t.Errorf("row %d differs across calls: %q vs %q", i, first[i].Name, second[i].Name)
		}
	}
	// Spot-check the contract: alpha, mid and zeta each appear
	// exactly once in the merged catalog. The picker overlay can
	// therefore render them in a stable place, and the picker tests
	// asserting "no duplicate /foo rows" still pass.
	for _, want := range []string{"/alpha", "/mid", "/zeta"} {
		count := 0
		for _, r := range first {
			if r.Name == want {
				count++
			}
		}
		if count != 1 {
			t.Errorf("%s appears %d times, want 1", want, count)
		}
	}
}

// TestFilterSlashCommandsWithCustom_FilterAppliesAfterMerge pins
// the picker's substring filter: typing "/dep" matches "/deploy"
// (the plugin-shadowed command) but does NOT match built-in entries
// that happen to contain "dep" as a substring (none in this
// catalog). The merged catalog and the substring filter cooperate
// so custom commands show up in the picker overlay just like
// built-ins.
func TestFilterSlashCommandsWithCustom_FilterAppliesAfterMerge(t *testing.T) {
	custom := []customcmd.Command{
		{Name: "deploy", Description: "plugin deploy", Source: customcmd.SourcePlugin},
	}
	rows := FilterSlashCommandsWithCustom("/dep", custom)
	if findCommand(rows, "/deploy") == nil {
		t.Errorf("filter %q did not surface /deploy; rows: %v", "/dep", commandNames(rows))
	}
	rowsEmpty := FilterSlashCommandsWithCustom("/nothing-matches-this", custom)
	if len(rowsEmpty) != 0 {
		t.Errorf("filter %q returned %d rows, want 0", "/nothing-matches-this", len(rowsEmpty))
	}
}

// TestFilterSlashCommandsWithCustom_ChromeDevtoolsPluginPrefixStillFiltered
// pins the cycle-146 invariant: chrome-devtools-plugin:* built-in
// rows are still excluded from the picker overlay even after the
// merge. The custom command loader does not change that filter.
func TestFilterSlashCommandsWithCustom_ChromeDevtoolsPluginPrefixStillFiltered(t *testing.T) {
	rows := FilterSlashCommandsWithCustom("", nil)
	for _, r := range rows {
		if strings.Contains(r.Name, "chrome-devtools-plugin:") {
			t.Errorf("chrome-devtools-plugin:* built-in leaked into picker: %q", r.Name)
		}
	}
}

// commandNames is a small helper for test failure messages; not a
// production helper.
func commandNames(rows []SlashCommand) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out
}
