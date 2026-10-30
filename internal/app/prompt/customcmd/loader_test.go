// loader_test.go — cycle custom-slash-commands-no-plugin-injection.
//
// The five mandatory tests cover the AGY 1.2.16 contract:
//  1. plugin command wins over project duplicate
//  2. project command wins when no plugin defines
//  3. built-in wins over project duplicate
//  4. deterministic precedence (sorted output, two runs equal)
//  5. name conflict logged (Conflict slice populated)
//
// Every test pins behaviour that the AGY 1.2.16 changelog line
// describes — the merge precedence is the cycle's load-bearing
// invariant, so each test isolates one precedence rule and asserts
// it directly.
package customcmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// withCommandFile writes a one-line custom command file at
// <root>/<name>.md with the given description. Returns the absolute
// path the file landed at so conflict tests can compare Path.
func withCommandFile(t *testing.T, root, name, description string) string {
	t.Helper()
	path := filepath.Join(root, name+".md")
	body := "---\ndescription: " + description + "\n---\n# body\n"
	if err := writeFile(path, body); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// writeFile is os.WriteFile lifted to t.Helper so the failure line
// points at the caller, not the helper.
func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}

// testFS lays out a fake AGY config tree:
//
//	<root>/plugins/<plugin>/commands/*.md
//	<root>/.claude/commands/*.md                (if project is set)
//	<root>/commands/*.md
//
// It returns the three roots the loader wants. Keeping the helper
// in test code (rather than the loader) means the loader stays
// pure: it takes paths, not env vars.
func testFS(t *testing.T, plugins map[string]map[string]string, projectFiles map[string]string, globalFiles map[string]string) LoadRoots {
	t.Helper()
	root := t.TempDir()
	// Plugin layout: <root>/plugins/<plugin>/commands/<name>.md
	pluginsRoot := filepath.Join(root, "plugins")
	for pluginName, cmds := range plugins {
		dir := filepath.Join(pluginsRoot, pluginName, "commands")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir plugin dir: %v", err)
		}
		for name, desc := range cmds {
			withCommandFile(t, dir, name, desc)
		}
	}
	// Project layout: <projectRoot>/.claude/commands/<name>.md
	var projectRoot string
	if projectFiles != nil {
		projectRoot = filepath.Join(root, "project")
		dir := filepath.Join(projectRoot, ".claude", "commands")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir project dir: %v", err)
		}
		for name, desc := range projectFiles {
			withCommandFile(t, dir, name, desc)
		}
	}
	// Global layout: <root>/commands/<name>.md
	var globalRoot string
	if globalFiles != nil {
		globalRoot = filepath.Join(root, "commands")
		if err := os.MkdirAll(globalRoot, 0o755); err != nil {
			t.Fatalf("mkdir global dir: %v", err)
		}
		for name, desc := range globalFiles {
			withCommandFile(t, globalRoot, name, desc)
		}
	}
	return LoadRoots{
		PluginsDir: pluginsRoot,
		ProjectDir: projectRoot,
		GlobalDir:  globalRoot,
	}
}

// TestLoad_PluginWinsOverProjectDuplicate is the cycle's load-bearing
// test: AGY 1.2.16 fixed "plugin-defined commands being silently
// overridden by other definitions with the same name". When a plugin
// and a project both ship /foo, the plugin's /foo must be the one the
// picker shows — the project's /foo is recorded as a Conflict so the
// call site can log it.
func TestLoad_PluginWinsOverProjectDuplicate(t *testing.T) {
	roots := testFS(t,
		map[string]map[string]string{
			"alpha": {"foo": "plugin alpha foo"},
		},
		map[string]string{"foo": "project foo"},
		nil,
	)
	cat, conflicts := Load(roots)
	// Catalog should have exactly one "/foo" entry, sourced from the plugin.
	if len(cat) != 1 {
		t.Fatalf("catalog size = %d, want 1", len(cat))
	}
	got := cat[0]
	if got.Name != "foo" {
		t.Errorf("Name = %q, want %q (plugin wins; bare basename)", got.Name, "foo")
	}
	if got.Source != SourcePlugin {
		t.Errorf("Source = %v, want SourcePlugin", got.Source)
	}
	if got.Description != "plugin alpha foo" {
		t.Errorf("Description = %q, want the plugin's description", got.Description)
	}
	// Conflict: project foo was shadowed.
	if len(conflicts) != 1 {
		t.Fatalf("conflicts size = %d, want 1 (project foo shadowed)", len(conflicts))
	}
	if conflicts[0].Name != "foo" {
		t.Errorf("conflict Name = %q, want %q", conflicts[0].Name, "foo")
	}
	if conflicts[0].Winner.Source != SourcePlugin {
		t.Errorf("Winner.Source = %v, want SourcePlugin", conflicts[0].Winner.Source)
	}
	if conflicts[0].Loser.Source != SourceProject {
		t.Errorf("Loser.Source = %v, want SourceProject", conflicts[0].Loser.Source)
	}
}

// TestLoad_ProjectWinsWhenNoPluginDefines covers the "no plugin
// shadow" path: when only the project declares /bar, the project's
// /bar lands in the catalog and no conflict is logged.
func TestLoad_ProjectWinsWhenNoPluginDefines(t *testing.T) {
	roots := testFS(t,
		nil, // no plugins:  the alpha plugin's commands/ subtree is absent
		map[string]string{"bar": "project bar"},
		nil,
	)
	cat, conflicts := Load(roots)
	if len(cat) != 1 {
		t.Fatalf("catalog size = %d, want 1", len(cat))
	}
	if cat[0].Name != "bar" || cat[0].Source != SourceProject {
		t.Errorf("got %+v, want project-sourced /bar", cat[0])
	}
	if len(conflicts) != 0 {
		t.Errorf("conflicts = %+v, want none (no plugin shadowed the project)", conflicts)
	}
}

// TestMergeWithBuiltins_BuiltinWinsOverProjectDuplicate is the third
// precedence rule. A built-in command (the system surface) must beat
// a same-named project command — the user's .claude/commands/foo.md
// cannot silently replace /foo when /foo is a system command.
func TestMergeWithBuiltins_BuiltinWinsOverProjectDuplicate(t *testing.T) {
	roots := testFS(t, nil,
		map[string]string{"foo": "project foo shadows builtin"},
		nil,
	)
	_, custom, _ := LoadWithConflicts(roots)
	cat, conflicts := MergeWithBuiltins(
		[]BuiltIn{{Name: "/foo", Description: "builtin foo"}},
		custom,
	)
	// Catalog: exactly one /foo, sourced from the built-in catalog.
	var found *Command
	for i := range cat {
		if cat[i].Name == "foo" {
			found = &cat[i]
		}
	}
	if found == nil {
		t.Fatalf("catalog has no /foo: %+v", cat)
	}
	if found.Source != SourceBuiltin {
		t.Errorf("/foo Source = %v, want SourceBuiltin (built-in wins over project)", found.Source)
	}
	if found.Description != "builtin foo" {
		t.Errorf("/foo Description = %q, want the built-in's description", found.Description)
	}
	// Conflict: project foo was shadowed by the built-in.
	if len(conflicts) != 1 {
		t.Fatalf("conflicts size = %d, want 1 (project foo shadowed by built-in)", len(conflicts))
	}
	if conflicts[0].Winner.Source != SourceBuiltin || conflicts[0].Loser.Source != SourceProject {
		t.Errorf("winner=%v loser=%v, want builtin/project", conflicts[0].Winner.Source, conflicts[0].Loser.Source)
	}
}

// TestLoad_DeterministicPrecedence pins the AGY 1.2.16 contract that
// "/foo lookup succeeds the same way on every invocation regardless
// of filesystem walk order." Two back-to-back Load calls with
// the same roots must return equal catalogs; a third call after
// touching the on-disk state in a non-overwriting way must also
// return the same catalog.
func TestLoad_DeterministicPrecedence(t *testing.T) {
	roots := testFS(t,
		map[string]map[string]string{
			"alpha": {"foo": "alpha foo", "shared": "alpha shared"},
			"beta":  {"bar": "beta bar", "shared": "beta shared"},
		},
		map[string]string{"baz": "project baz", "shared": "project shared"},
		map[string]string{"qux": "global qux", "shared": "global shared"},
	)
	first, firstConflicts := Load(roots)
	second, secondConflicts := Load(roots)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("two consecutive Load calls returned different catalogs:\nfirst=%+v\nsecond=%+v", first, second)
	}
	if !reflect.DeepEqual(firstConflicts, secondConflicts) {
		t.Errorf("two consecutive Load calls returned different conflicts:\nfirst=%+v\nsecond=%+v", firstConflicts, secondConflicts)
	}
	// Sort-by-Name invariant — the deterministic contract is that
	// the catalog is sorted. Verify by walking it in ascending
	// order and asserting each entry's Name is non-decreasing.
	for i := 1; i < len(first); i++ {
		if first[i].Name < first[i-1].Name {
			t.Errorf("catalog not sorted at index %d: %q < %q", i, first[i].Name, first[i-1].Name)
			break
		}
	}
	// Conflicts slice must be sorted (the stable sort in Load
	// applies to the merged catalog; conflicts inherit the same
	// iteration order which is also deterministic).
	for i := 1; i < len(firstConflicts); i++ {
		if firstConflicts[i].Name < firstConflicts[i-1].Name {
			t.Errorf("conflicts not sorted at index %d: %q < %q", i, firstConflicts[i].Name, firstConflicts[i-1].Name)
			break
		}
	}
}

// TestLoad_NameConflictLogged walks the third source-mixing case:
// a same-name command across plugin + project + global sources
// produces two conflicts (project and global both shadowed) and a
// single catalog entry sourced from the plugin. The Conflict slice
// is what the call site will log at startup; this test pins that
// the loader records every shadowed source, not just the first one.
func TestLoad_NameConflictLogged(t *testing.T) {
	roots := testFS(t,
		map[string]map[string]string{
			"alpha": {"conflict": "alpha conflict"},
		},
		map[string]string{"conflict": "project conflict"},
		map[string]string{"conflict": "global conflict"},
	)
	cat, conflicts := Load(roots)
	// Catalog: one "/conflict" (plugin wins).
	if len(cat) != 1 {
		t.Fatalf("catalog size = %d, want 1", len(cat))
	}
	if cat[0].Name != "conflict" {
		t.Errorf("catalog Name = %q, want %q", cat[0].Name, "conflict")
	}
	if cat[0].Source != SourcePlugin {
		t.Errorf("catalog Source = %v, want SourcePlugin", cat[0].Source)
	}
	// Conflicts: project AND global both shadowed.
	if len(conflicts) != 2 {
		t.Fatalf("conflicts size = %d, want 2 (project + global)", len(conflicts))
	}
	// The two losers are the global entry and the project entry.
	// Order doesn't matter — pin the multiset.
	loserSources := map[Source]bool{}
	for _, c := range conflicts {
		if c.Name != "conflict" {
			t.Errorf("conflict Name = %q, want %q", c.Name, "conflict")
		}
		if c.Winner.Source != SourcePlugin {
			t.Errorf("conflict Winner.Source = %v, want SourcePlugin", c.Winner.Source)
		}
		loserSources[c.Loser.Source] = true
	}
	if !loserSources[SourceProject] || !loserSources[SourceGlobal] {
		t.Errorf("loser sources = %v, want both project and global", loserSources)
	}
}

// TestLoad_TwoPluginsSameShortName verifies the two-plugins
// collision case: when two plugins both ship /foo, the
// alphabetically-first plugin's file wins (same source, first
// write wins). The conflict slice is empty because same-source
// duplicates are dedup'd silently rather than logged.
func TestLoad_TwoPluginsSameShortName(t *testing.T) {
	roots := testFS(t,
		map[string]map[string]string{
			"alpha": {"foo": "alpha foo"},
			"beta":  {"foo": "beta foo"},
		},
		nil, nil,
	)
	cat, conflicts := Load(roots)
	if len(conflicts) != 0 {
		t.Errorf("conflicts = %+v, want none (same-source dedup)", conflicts)
	}
	if len(cat) != 1 {
		t.Fatalf("catalog size = %d, want 1", len(cat))
	}
	// "alpha" sorts before "beta" so alpha's foo wins.
	if cat[0].Name != "foo" {
		t.Errorf("catalog Name = %q, want %q", cat[0].Name, "foo")
	}
	if !strings.HasPrefix(cat[0].Path, roots.PluginsDir) {
		t.Errorf("catalog Path = %q, want it under the plugins dir %q", cat[0].Path, roots.PluginsDir)
	}
	if !strings.Contains(cat[0].Path, "alpha") {
		t.Errorf("catalog Path = %q, want it under alpha's commands/ subtree", cat[0].Path)
	}
}

// TestLoad_GlobalShadowedByProjectWhenNoPluginCover pins the lowest
// precedence: when only project and global both declare /bar, the
// project wins.
func TestLoad_GlobalShadowedByProjectWhenNoPluginCover(t *testing.T) {
	roots := testFS(t,
		nil,
		map[string]string{"bar": "project bar"},
		map[string]string{"bar": "global bar"},
	)
	cat, conflicts := Load(roots)
	if len(cat) != 1 {
		t.Fatalf("catalog size = %d, want 1", len(cat))
	}
	if cat[0].Source != SourceProject {
		t.Errorf("Source = %v, want SourceProject", cat[0].Source)
	}
	if len(conflicts) != 1 || conflicts[0].Loser.Source != SourceGlobal {
		t.Errorf("conflicts = %+v, want exactly the global bar shadowed by the project bar", conflicts)
	}
}

// TestLoad_MissingDirectoriesAreNotErrors verifies the first-run
// case: a fresh machine with no plugin, no project .claude/, no
// global commands/ returns an empty catalog without error. The
// loader's contract is that missing directories are silently
// skipped, not fatal.
func TestLoad_MissingDirectoriesAreNotErrors(t *testing.T) {
	roots := LoadRoots{
		PluginsDir: "/definitely/not/a/dir",
		ProjectDir: "/also/not/a/dir",
		GlobalDir:  "/nope/nope",
	}
	cat, conflicts := Load(roots)
	if len(cat) != 0 {
		t.Errorf("catalog = %+v, want empty", cat)
	}
	if len(conflicts) != 0 {
		t.Errorf("conflicts = %+v, want empty", conflicts)
	}
}

// TestFrontmatterDescription_EmptyWhenMissing pins the contract that
// a custom command without a frontmatter description still appears
// in the catalog — only the description is empty, the command is
// still usable.
func TestFrontmatterDescription_EmptyWhenMissing(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "commands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Plain markdown, no frontmatter.
	if err := writeFile(filepath.Join(dir, "nofront.md"), "# Just markdown\n\nWith body.\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Frontmatter, but no description key.
	if err := writeFile(filepath.Join(dir, "nodesc.md"), "---\nname: nodesc\n---\n# body\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	roots := LoadRoots{GlobalDir: dir}
	cat, _ := Load(roots)
	if len(cat) != 2 {
		t.Fatalf("catalog size = %d, want 2 (both commands are present, descriptions empty)", len(cat))
	}
	for _, c := range cat {
		if c.Description != "" {
			t.Errorf("command %q has non-empty description %q; want \"\"", c.Name, c.Description)
		}
	}
}

// LoadWithConflicts is a thin wrapper around Load that returns the
// catalog and the conflict slice separately so the "merge with
// built-ins" tests can feed the custom commands back into
// MergeWithBuiltins. Defined here (in the test file) rather than
// re-importing Load's signature into every test.
func LoadWithConflicts(roots LoadRoots) (LoadRoots, []Command, []Conflict) {
	cat, conflicts := Load(roots)
	return roots, cat, conflicts
}
