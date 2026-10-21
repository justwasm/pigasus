package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAgent(t *testing.T, dir, file, name, description, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	text := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscover_WorkspaceAgentsAreSortedAndUseFrontmatterName(t *testing.T) {
	workspace, home := t.TempDir(), t.TempDir()
	dir := filepath.Join(workspace, ".agents", "agents")
	writeAgent(t, dir, "1.md", "zebra", "Z", "Z body")
	writeAgent(t, dir, "2.md", "alpha", "A", "A body")
	writeAgent(t, dir, "folder-name.md", "not-the-folder", "N", "N body")

	got := DiscoverWith(DiscoverOptions{Workspace: workspace, Home: home})
	if len(got) != 3 {
		t.Fatalf("got %d agents, want 3: %+v", len(got), got)
	}
	var names []string
	for _, a := range got {
		names = append(names, a.Name)
	}
	if strings.Join(names, ",") != "alpha,not-the-folder,zebra" {
		t.Errorf("names = %v, want sorted frontmatter names", names)
	}
	if got[1].Body != "N body" || got[1].Description != "N" {
		t.Errorf("body/description = %q/%q", got[1].Body, got[1].Description)
	}
}

func TestDiscover_SkipsInvalidFilesSilently(t *testing.T) {
	workspace, home := t.TempDir(), t.TempDir()
	dir := filepath.Join(workspace, ".agents", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"nofm.md":     "no frontmatter\n",
		"noname.md":   "---\ndescription: d\n---\nbody\n",
		"nodesc.md":   "---\nname: nodesc\n---\nbody\n",
		"badyaml.md":  "---\nname: [x\n---\nbody\n",
		"ok.md":       "---\nname: ok\ndescription: d\n---\nbody\n",
		"ignored.txt": "not markdown",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := DiscoverWith(DiscoverOptions{Workspace: workspace, Home: home})
	if len(got) != 1 || got[0].Name != "ok" {
		t.Fatalf("got %+v, want only ok", got)
	}
}

func TestDiscover_UserAndWorkspaceScopesAndPrecedence(t *testing.T) {
	workspace, home := t.TempDir(), t.TempDir()
	writeAgent(t, filepath.Join(home, ".gemini", "config", "agents"), "home.md", "homey", "H", "from home")
	writeAgent(t, filepath.Join(workspace, ".agents", "agents"), "work.md", "worky", "W", "from work")
	writeAgent(t, filepath.Join(home, ".gemini", "config", "agents"), "dup.md", "same", "H", "user body")
	writeAgent(t, filepath.Join(workspace, ".agents", "agents"), "dup2.md", "same", "W", "workspace body")

	got := DiscoverWith(DiscoverOptions{Workspace: workspace, Home: home})
	var names []string
	for _, a := range got {
		names = append(names, a.Name)
	}
	if strings.Join(names, ",") != "homey,same,worky" {
		t.Fatalf("names = %v", names)
	}
	for _, a := range got {
		if a.Name == "same" && a.Body != "workspace body" {
			t.Errorf("workspace agent should win: %+v", a)
		}
	}
}

func TestDiscover_ExclusionsByFileStem(t *testing.T) {
	workspace, home := t.TempDir(), t.TempDir()
	dir := filepath.Join(workspace, ".agents", "agents")
	writeAgent(t, dir, "bar.md", "baz", "B", "body")
	writeAgent(t, dir, "keep.md", "kept", "K", "body")
	if err := os.WriteFile(filepath.Join(workspace, ".agents", "agents.json"),
		[]byte(`{"exclude":["bar"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := DiscoverWith(DiscoverOptions{Workspace: workspace, Home: home})
	if len(got) != 1 || got[0].Name != "kept" {
		t.Fatalf("got %+v, want only kept (exclusion is the file stem, not name)", got)
	}
}

func TestDiscover_PluginAgents(t *testing.T) {
	workspace, home := t.TempDir(), t.TempDir()
	userPlugins := filepath.Join(home, ".gemini", "config", "plugins")
	userPlugin := filepath.Join(userPlugins, "p")
	if err := os.MkdirAll(userPlugin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userPlugin, "plugin.json"), []byte(`{"name":"p"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeAgent(t, filepath.Join(userPlugin, "agents"), "f.md", "from-plugin", "P", "body")

	// A folder without plugin.json is not a plugin.
	noManifest := filepath.Join(userPlugins, "no-manifest")
	if err := os.MkdirAll(noManifest, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAgent(t, filepath.Join(noManifest, "agents"), "x.md", "ghost", "G", "body")

	// Workspace plugins load too.
	wsPlugin := filepath.Join(workspace, ".agents", "plugins", "w")
	if err := os.MkdirAll(wsPlugin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsPlugin, "plugin.json"), []byte(`{"name":"w"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeAgent(t, filepath.Join(wsPlugin, "agents"), "w.md", "workspace-plugin", "W", "body")

	got := DiscoverWith(DiscoverOptions{Workspace: workspace, Home: home})
	var names []string
	for _, a := range got {
		names = append(names, a.Name)
	}
	if strings.Join(names, ",") != "from-plugin,workspace-plugin" {
		t.Fatalf("names = %v", names)
	}

	// User config.json disables a plugin; a missing entry defaults to enabled.
	cfg := filepath.Join(home, ".gemini", "config", "config.json")
	if err := os.WriteFile(cfg, []byte(`{"plugins":{"p":{"enabled":false}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got = DiscoverWith(DiscoverOptions{Workspace: workspace, Home: home})
	if len(got) != 1 || got[0].Name != "workspace-plugin" {
		t.Fatalf("disabled plugin still loaded: %+v", got)
	}

	// The plugin's agents.json excludes by stem, like the other scopes.
	if err := os.WriteFile(filepath.Join(wsPlugin, "agents.json"), []byte(`{"exclude":["w"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got = DiscoverWith(DiscoverOptions{Workspace: workspace, Home: home})
	if len(got) != 0 {
		t.Fatalf("plugin exclusion ignored: %+v", got)
	}
}

func TestValidateAgent(t *testing.T) {
	workspace, home := t.TempDir(), t.TempDir()
	writeAgent(t, filepath.Join(workspace, ".agents", "agents"), "foo.md", "foo", "F", "body")
	t.Chdir(workspace)
	t.Setenv("HOME", home)

	if err := ValidateAgent(""); err != nil {
		t.Errorf("empty: %v", err)
	}
	if err := ValidateAgent("default"); err != nil {
		t.Errorf("default: %v", err)
	}
	if err := ValidateAgent("foo"); err != nil {
		t.Errorf("foo: %v", err)
	}
	if err := ValidateAgent("Foo"); err == nil {
		t.Error("agent matching is case-sensitive; Foo must fail")
	}
	err := ValidateAgent("nosuch")
	var nf *AgentNotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("type = %T", err)
	}
	want := `error: unknown agent "nosuch" (available: foo)`
	if err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
	// Lookup returns the body for prompt assembly.
	a, ok := Lookup("foo")
	if !ok || a.Body != "body" || a.Description != "F" {
		t.Errorf("Lookup = %+v, %v", a, ok)
	}
}

func TestValidateAgent_NoCustomAgentsMessage(t *testing.T) {
	workspace, home := t.TempDir(), t.TempDir()
	t.Chdir(workspace)
	t.Setenv("HOME", home)
	err := ValidateAgent("nosuch")
	if err == nil {
		t.Fatal("want error")
	}
	want := `error: unknown agent "nosuch" (no custom agents are available)`
	if err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestParseAgentFile_TrimsBodyAndRequiresFrontmatter(t *testing.T) {
	a, ok := parseAgentFile([]byte("---\nname: x\ndescription: d\n---\n\n  body  \n"))
	if !ok || a.Name != "x" || a.Body != "body" {
		t.Errorf("parsed = %+v, %v", a, ok)
	}
	if _, ok := parseAgentFile([]byte("---\nname: x\n---\nbody")); ok {
		t.Error("missing description must be rejected")
	}
	if _, ok := parseAgentFile([]byte("no frontmatter")); ok {
		t.Error("missing frontmatter must be rejected")
	}
}

func TestDiscover_AntigravityHomeMovesUserConfigTree(t *testing.T) {
	workspace, root := t.TempDir(), t.TempDir()
	agentHome := filepath.Join(root, ".gemini", "antigravity-cli")
	writeAgent(t, filepath.Join(root, ".gemini", "config", "agents"), "a.md", "from-config", "C", "body")
	t.Chdir(workspace)
	t.Setenv("HOME", filepath.Join(root, "unused-home"))
	t.Setenv("ANTIGRAVITY_HOME", agentHome)

	got := DiscoverWith(DiscoverOptions{Workspace: workspace})
	if len(got) != 1 || got[0].Name != "from-config" {
		t.Fatalf("got %+v, want the agent under $ANTIGRAVITY_HOME's sibling config tree", got)
	}
}
