// configpaths_test.go — the MCP configuration path set and the merge of
// the files it names.
//
// The path set is the one owner of "where the MCP servers live": the
// `agy mcp add` writer and the runtime that reads the file back both ask
// this package, so the two cannot drift apart.
package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile writes body to path, creating the directory.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestGlobalConfigPathHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEMINI_CONFIG_HOME", "")
	got := GlobalConfigPath()
	want := filepath.Join(home, ".gemini", "config", "mcp_config.json")
	if got != want {
		t.Errorf("GlobalConfigPath = %q, want %q", got, want)
	}
}

func TestGlobalConfigPathGeminiConfigHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", dir)
	got := GlobalConfigPath()
	want := filepath.Join(dir, "mcp_config.json")
	if got != want {
		t.Errorf("GlobalConfigPath = %q, want %q", got, want)
	}
}

func TestWorkspaceConfigPath(t *testing.T) {
	ws := t.TempDir()
	got := WorkspaceConfigPath(ws)
	want := filepath.Join(ws, ".agents", "mcp_config.json")
	if got != want {
		t.Errorf("WorkspaceConfigPath = %q, want %q", got, want)
	}
	if got := WorkspaceConfigPath(""); got != "" {
		t.Errorf("WorkspaceConfigPath(\"\") = %q, want the empty path", got)
	}
}

func TestReferenceConfigPath(t *testing.T) {
	dir := t.TempDir()
	got := ReferenceConfigPath(dir)
	want := filepath.Join(dir, "mcp.json")
	if got != want {
		t.Errorf("ReferenceConfigPath = %q, want %q", got, want)
	}
	if got := ReferenceConfigPath(""); got != "" {
		t.Errorf("ReferenceConfigPath(\"\") = %q, want the empty path", got)
	}
}

// TestConfigPathsOrder pins the merge order: the documented global file,
// the reference binary's own file, then the workspace file.
func TestConfigPathsOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEMINI_CONFIG_HOME", "")
	agentDir := filepath.Join(home, ".gemini", "antigravity-cli")
	ws := t.TempDir()

	sources := ConfigPaths(agentDir, ws)
	if len(sources) != 3 {
		t.Fatalf("ConfigPaths = %d sources, want 3: %+v", len(sources), sources)
	}
	want := []struct {
		path  string
		scope ConfigScope
	}{
		{filepath.Join(home, ".gemini", "config", "mcp_config.json"), ScopeGlobal},
		{filepath.Join(agentDir, "mcp.json"), ScopeReference},
		{filepath.Join(ws, ".agents", "mcp_config.json"), ScopeWorkspace},
	}
	for i, w := range want {
		if sources[i].Path != w.path {
			t.Errorf("source %d path = %q, want %q", i, sources[i].Path, w.path)
		}
		if sources[i].Scope != w.scope {
			t.Errorf("source %d scope = %q, want %q", i, sources[i].Scope, w.scope)
		}
	}
}

// TestConfigPathsWithoutWorkspace: a host that cannot supply a workspace
// gets the two global files and no workspace file.
func TestConfigPathsWithoutWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GEMINI_CONFIG_HOME", "")
	agentDir := filepath.Join(home, ".gemini", "antigravity-cli")

	sources := ConfigPaths(agentDir, "")
	if len(sources) != 2 {
		t.Fatalf("ConfigPaths = %d sources, want 2: %+v", len(sources), sources)
	}
	for _, src := range sources {
		if src.Scope == ScopeWorkspace {
			t.Errorf("the workspace file must not be read without a workspace: %+v", src)
		}
	}
}

// TestLoadMergedConfigReadsBothRootKeys: the documented `mcpServers` file
// and the reference's own `servers` file both load.
func TestLoadMergedConfigReadsBothRootKeys(t *testing.T) {
	dir := t.TempDir()
	docPath := filepath.Join(dir, "mcp_config.json")
	refPath := filepath.Join(dir, "mcp.json")
	writeFile(t, docPath, `{"mcpServers":{"doc":{"command":"node","args":["doc.js"],"env":{"K":"V"},"headers":{"H":"V"}}}}`)
	writeFile(t, refPath, `{"servers":{"ref":{"type":"stdio","command":"node","args":["ref.js"],"enabled":true}}}`)

	merged, err := LoadMergedConfig([]ConfigSource{
		{Path: docPath, Scope: ScopeGlobal, RootKey: RootMCPServers},
		{Path: refPath, Scope: ScopeReference, RootKey: RootServers},
	})
	if err != nil {
		t.Fatalf("LoadMergedConfig: %v", err)
	}
	if len(merged.Servers) != 2 {
		t.Fatalf("merged servers = %d, want 2: %+v", len(merged.Servers), merged.Servers)
	}
	doc := merged.Servers["doc"]
	if doc.Command != "node" {
		t.Errorf("doc.command = %q, want node", doc.Command)
	}
	if doc.Env["K"] != "V" {
		t.Errorf("doc.env = %v, want the documented object", doc.Env)
	}
	if doc.Headers["H"] != "V" {
		t.Errorf("doc.headers = %v, want the documented object", doc.Headers)
	}
	ref := merged.Servers["ref"]
	if ref.Command != "node" {
		t.Errorf("ref.command = %q, want node", ref.Command)
	}
	specs := merged.Specs()
	if len(specs) != 2 {
		t.Fatalf("specs = %d, want 2 (both servers are enabled)", len(specs))
	}
}

// TestLoadMergedConfigWorkspaceOverridesGlobal: the workspace file wins
// for a server both files name.
func TestLoadMergedConfigWorkspaceOverridesGlobal(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.json")
	workspacePath := filepath.Join(dir, "workspace.json")
	writeFile(t, globalPath, `{"mcpServers":{"shared":{"command":"global","disabled":false}}}`)
	writeFile(t, workspacePath, `{"mcpServers":{"shared":{"command":"workspace","disabled":false}}}`)

	merged, err := LoadMergedConfig([]ConfigSource{
		{Path: globalPath, Scope: ScopeGlobal, RootKey: RootMCPServers},
		{Path: workspacePath, Scope: ScopeWorkspace, RootKey: RootMCPServers},
	})
	if err != nil {
		t.Fatalf("LoadMergedConfig: %v", err)
	}
	if got := merged.Servers["shared"].Command; got != "workspace" {
		t.Errorf("shared.command = %q, want the workspace's entry", got)
	}
	if len(merged.Servers) != 1 {
		t.Errorf("merged servers = %d, want 1", len(merged.Servers))
	}
}

// TestLoadMergedConfigDropsABrokenFile: a file that cannot be parsed is
// dropped, and the servers of the other files still load.
func TestLoadMergedConfigDropsABrokenFile(t *testing.T) {
	dir := t.TempDir()
	goodPath := filepath.Join(dir, "good.json")
	badPath := filepath.Join(dir, "bad.json")
	writeFile(t, goodPath, `{"mcpServers":{"ok":{"command":"node"}}}`)
	writeFile(t, badPath, `{not json`)

	merged, err := LoadMergedConfig([]ConfigSource{
		{Path: badPath, Scope: ScopeGlobal, RootKey: RootMCPServers},
		{Path: goodPath, Scope: ScopeWorkspace, RootKey: RootMCPServers},
	})
	if err == nil {
		t.Error("LoadMergedConfig: want the parse error")
	}
	if len(merged.Servers) != 1 {
		t.Errorf("merged servers = %d, want the one good file's server", len(merged.Servers))
	}
}

// TestLoadMergedConfigMissingFileIsEmpty: a file that does not exist is
// the "no servers configured" state, not an error.
func TestLoadMergedConfigMissingFileIsEmpty(t *testing.T) {
	merged, err := LoadMergedConfig([]ConfigSource{
		{Path: filepath.Join(t.TempDir(), "nope.json"), Scope: ScopeGlobal, RootKey: RootMCPServers},
	})
	if err != nil {
		t.Fatalf("LoadMergedConfig: %v", err)
	}
	if len(merged.Servers) != 0 {
		t.Errorf("merged servers = %d, want 0", len(merged.Servers))
	}
	if specs := merged.Specs(); len(specs) != 0 {
		t.Errorf("specs = %d, want 0", len(specs))
	}
}

// TestMergedConfigDisableAllMCP: the reference's switch gates every
// server, whichever file carries it.
func TestMergedConfigDisableAllMCP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	writeFile(t, path, `{"servers":{"a":{"command":"x","enabled":true}},"disableAllMCP":true}`)

	merged, err := LoadMergedConfig([]ConfigSource{{Path: path, Scope: ScopeReference, RootKey: RootServers}})
	if err != nil {
		t.Fatalf("LoadMergedConfig: %v", err)
	}
	if !merged.DisableAllMCP {
		t.Error("DisableAllMCP was lost")
	}
	if specs := merged.Specs(); len(specs) != 0 {
		t.Errorf("specs = %d, want 0 when MCP is disabled", len(specs))
	}
}
