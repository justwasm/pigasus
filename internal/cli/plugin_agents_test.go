package cli

// plugin_agents_test.go — cycle 234: agents.json round-trip on the CLI
// subcommand handlers (validate + install). Mirrors the TUI-side test rig
// in cmd/wagyu/slash_cycle234_test.go so a future split between the two
// loaders cannot drift: both surfaces read the same JSON manifest and
// report the same counts.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedPluginTree writes a minimal plugin.json manifest and the named
// agents-sidecar artefacts under root. Skills directories are created
// with empty SKILL.md files so legacyListSkills picks them up.
func seedPluginTree(t *testing.T, root, name, version string, skills []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	for _, s := range skills {
		dir := filepath.Join(root, "skills", s)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatalf("mkdir skill: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"),
			[]byte("---\nname: "+s+"\ndescription: "+s+" skill\n---\n# "+s+"\n"), 0600); err != nil {
			t.Fatalf("write SKILL.md: %v", err)
		}
	}
	manifest := map[string]any{"name": name, "version": version, "description": name + " plugin."}
	raw, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), raw, 0600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// seedPluginAgentsJSON writes an agents.json at the plugin root.
func seedAgentsJSON(t *testing.T, root string, names []string) {
	t.Helper()
	entries := make([]map[string]any, 0, len(names))
	for _, n := range names {
		entries = append(entries, map[string]any{"name": n, "path": "agents/" + n + ".md"})
	}
	raw, _ := json.Marshal(map[string]any{"agents": entries})
	if err := os.WriteFile(filepath.Join(root, "agents.json"), raw, 0600); err != nil {
		t.Fatalf("write agents.json: %v", err)
	}
}

// seedPluginAgentsMarkdown writes <root>/agents/<name>.md files.
func seedPluginAgentsMarkdown(t *testing.T, root string, names []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0700); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(root, "agents", n+".md"),
			[]byte("---\nname: "+n+"\ndescription: "+n+" agent\n---\n# "+n+"\n"), 0600); err != nil {
			t.Fatalf("write %s.md: %v", n, err)
		}
	}
}

// captureStdout captures whatever the closure writes to stdout and returns
// the resulting string. The install / validate handlers print to j.Out.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	done := make(chan string, 1)
	go func() {
		var buf strings.Builder
		tmp := make([]byte, 4096)
		for {
			n, err := r.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
			}
			if err != nil {
				break
			}
		}
		done <- buf.String()
	}()
	fn()
	w.Close()
	return <-done
}

// j with no parser flags attached — the subcommand handlers don't read
// flags, just args.
func newTestJ() *JetskiCLI { return NewJetski("test", "") }

// TestCLI_ValidateListsAgents: `agy plugin validate ./plugin` counts flat
// `agents/*.md` files with valid frontmatter in the summary line. Root
// `agents.json` is ignored by the reference loader and does not contribute.
func TestCLI_ValidateListsAgents(t *testing.T) {
	root := t.TempDir()
	seedPluginTree(t, root, "with-agents", "1.0.0", []string{"s1"})
	seedPluginAgentsMarkdown(t, root, []string{"agent-a", "agent-b"})

	out := captureStdout(t, func() {
		j := newTestJ()
		if err := runPluginValidate(j, []string{root}); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})
	if !strings.Contains(out, "skills 1 · agents 2 · rules 0") {
		t.Errorf("validate output missing agents count: %q", out)
	}
	if !strings.Contains(out, "✔ Validation passed") {
		t.Errorf("validate output missing pass footer: %q", out)
	}
}

// TestCLI_ValidateMissingAgentsShowsZero: a plugin without agents files
// reports `agents 0` in the component summary.
func TestCLI_ValidateMissingAgentsShowsZero(t *testing.T) {
	root := t.TempDir()
	seedPluginTree(t, root, "no-agents", "1.0.0", nil)

	out := captureStdout(t, func() {
		j := newTestJ()
		if err := runPluginValidate(j, []string{root}); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})
	if !strings.Contains(out, "agents 0 · rules 0") {
		t.Errorf("validate should print agents 0: %q", out)
	}
}

// TestCLI_ValidateMalformedAgentsJsonReported: a root agents.json does not
// contribute components, but the reference still parses it and reports a
// malformed file — the run exits 1 because the finding is an error.
func TestCLI_ValidateMalformedAgentsJsonReported(t *testing.T) {
	root := t.TempDir()
	seedPluginTree(t, root, "broken", "1.0.0", nil)
	if err := os.WriteFile(filepath.Join(root, "agents.json"),
		[]byte(`{"agents": [{"name": "x"`), 0600); err != nil {
		t.Fatalf("write broken: %v", err)
	}

	var out strings.Builder
	j := newTestJ()
	j.Out = &out
	err := runPluginValidate(j, []string{root})
	if err == nil {
		t.Fatal("validate accepted malformed agents.json")
	}
	want := "✗ agents.json: line 1, column 25: parsing object after value: unexpected EOF. The loader ignores this file"
	if !strings.Contains(out.String(), want) {
		t.Errorf("missing %q in:\n%s", want, out.String())
	}
}

// TestCLI_ValidateUnionMarkdownAndJson: only agents/*.md contributes; the
// root agents.json entries are ignored even when they collide by name.
func TestCLI_ValidateUnionMarkdownAndJson(t *testing.T) {
	root := t.TempDir()
	seedPluginTree(t, root, "merge", "1.0.0", nil)
	seedPluginAgentsMarkdown(t, root, []string{"shared", "md-only"})
	seedAgentsJSON(t, root, []string{"shared", "json-only"})

	out := captureStdout(t, func() {
		j := newTestJ()
		if err := runPluginValidate(j, []string{root}); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})
	if !strings.Contains(out, "agents 2 · rules 0") {
		t.Errorf("validate should count only the two markdown agents: %q", out)
	}
}

// TestCLI_InstallSummaryListsAgents: `agy plugin install ./plugin` prints
// the summary the changelog line 13 fixed — `Installed plugin "name"`
// followed by per-component counts (skills + agents). Pre-cycle-234 the
// handler always failed; cycle 234 wires the real installer.
func TestCLI_InstallSummaryListsAgents(t *testing.T) {
	// The plugin source.
	src := filepath.Join(t.TempDir(), "src-plugin")
	seedPluginTree(t, src, "summary", "1.0.0", []string{"s1", "s2"})
	seedAgentsJSON(t, src, []string{"a1", "a2", "a3"})

	// Point GEMINI_CONFIG_HOME at a tmp dir so the install lands there.
	cfgHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", cfgHome)

	out := captureStdout(t, func() {
		j := newTestJ()
		if err := runPluginInstall(j, []string{src}); err != nil {
			t.Fatalf("install: %v", err)
		}
	})
	if !strings.Contains(out, `Installed plugin "summary"`) {
		t.Errorf("install summary missing: %q", out)
	}
	if !strings.Contains(out, "skills      : 2 processed") {
		t.Errorf("skills count missing: %q", out)
	}
	if !strings.Contains(out, "agents      : 3 processed") {
		t.Errorf("agents count missing: %q", out)
	}

	// And the on-disk state should match: a copy under plugins/ + an
	// enabled=true row in config.json.
	dst := filepath.Join(cfgHome, "plugins", "summary")
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("installed plugin dir missing: %v", err)
	}
	cfg := filepath.Join(cfgHome, "config.json")
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("config.json missing: %v", err)
	}
	if !strings.Contains(string(data), `"summary"`) ||
		!strings.Contains(string(data), `"enabled": true`) {
		t.Errorf("config.json missing enabled row: %s", data)
	}
}

// TestCLI_InstallSkippedWhenNoAgents: a plugin that ships no agents at
// all still installs successfully and prints `agents      : skipped
// (not found)` instead of the count line.
func TestCLI_InstallSkippedWhenNoAgents(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	seedPluginTree(t, src, "no-agents", "1.0.0", []string{"s1"})

	cfgHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", cfgHome)

	out := captureStdout(t, func() {
		j := newTestJ()
		if err := runPluginInstall(j, []string{src}); err != nil {
			t.Fatalf("install: %v", err)
		}
	})
	if !strings.Contains(out, `Installed plugin "no-agents"`) {
		t.Errorf("install summary missing: %q", out)
	}
	if !strings.Contains(out, "agents      : skipped (not found)") {
		t.Errorf("agents skipped line missing: %q", out)
	}
}

// TestCLI_InstallRejectsMalformedAgentsJson: a broken agents.json fails
// the install — same as validate. The plugin never lands in plugins/,
// the config.json never gets an enabled row.
func TestCLI_InstallRejectsMalformedAgentsJson(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	seedPluginTree(t, src, "broken", "1.0.0", nil)
	if err := os.WriteFile(filepath.Join(src, "agents.json"),
		[]byte(`{"agents": [`), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfgHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", cfgHome)

	j := newTestJ()
	err := runPluginInstall(j, []string{src})
	if err == nil {
		t.Fatalf("install accepted malformed agents.json")
	}
	if !strings.Contains(err.Error(), "invalid agents.json") {
		t.Errorf("error = %q, want invalid agents.json", err.Error())
	}
	if _, err := os.Stat(filepath.Join(cfgHome, "plugins", "broken")); err == nil {
		t.Errorf("plugin dir created despite invalid agents.json")
	}
}

// TestCLI_ListAgentsFlag: `agy plugin list --json` should include the
// agents list per plugin. This is the third consumer of legacyListAgents
// in the same code path as the validate/install output.
func TestCLI_ListAgentsFlag(t *testing.T) {
	dir := legacyPluginsDir()
	if dir == "" {
		t.Skip("GEMINI_CONFIG_HOME/HOME unavailable")
	}
	// Plant two plugins: one with agents.json, one with no agents.
	p1 := filepath.Join(dir, "list-agents-a")
	p2 := filepath.Join(dir, "list-agents-b")
	for _, p := range []string{p1, p2} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}
	manifest := func(name string) []byte {
		raw, _ := json.Marshal(map[string]any{
			"name": name, "version": "1.0.0", "description": name,
		})
		return raw
	}
	if err := os.WriteFile(filepath.Join(p1, "plugin.json"), manifest("list-agents-a"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(p2, "plugin.json"), manifest("list-agents-b"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(p1, "agents.json"),
		[]byte(`{"agents":[{"name":"a1"},{"name":"a2"}]}`), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(p1)
		_ = os.RemoveAll(p2)
	}()

	got := legacyLoadPluginAgents(p1)
	if strings.Join(got, ",") != "a1,a2" {
		t.Errorf("legacyLoadPluginAgents(p1) = %v, want [a1 a2]", got)
	}
	got = legacyLoadPluginAgents(p2)
	if got != nil {
		t.Errorf("legacyLoadPluginAgents(p2) = %v, want nil", got)
	}
}
