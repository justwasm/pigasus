// pluginimport_test.go — cycle 364 (agy 1.4.11): `agy plugin import`.
//
// 【一手】 every expectation below was measured against the reference
// binary (sha256 09cefed6…) with a synthetic $HOME; the transcript lives in
// docs/cycles/cycle-364-agy-1.4.11-alignment/deliverable.md.
package pluginimport

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testRoots builds a fake home and returns the roots that point into it.
func testRoots(t *testing.T) (Roots, string) {
	t.Helper()
	home := t.TempDir()
	roots := Roots{
		GeminiHome:    filepath.Join(home, ".gemini"),
		ConfigHome:    filepath.Join(home, ".gemini", "config"),
		ExtensionsDir: filepath.Join(home, ".gemini", "extensions"),
		ClaudeHome:    filepath.Join(home, ".claude"),
		ClaudePlugins: filepath.Join(home, ".claude", "plugins"),
	}
	return roots, home
}

// writeFile writes a file below dir, creating the parents.
func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// claudePlugin writes a Claude Code plugin directory.
func claudePlugin(t *testing.T, roots Roots, name, manifest string) string {
	t.Helper()
	dir := filepath.Join(roots.ClaudePlugins, name)
	writeFile(t, dir, ".claude-plugin/plugin.json", manifest)
	return dir
}

// run imports from the given sources and returns stdout and stderr.
func run(t *testing.T, roots Roots, force bool, sources ...Source) (string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	opts := Options{Roots: roots, Force: force, Out: &out, ErrOut: &errOut}
	if err := Run(opts, sources); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return out.String(), errOut.String()
}

// A Claude Code plugin with every component reports them in the
// reference's order and stages them.
func TestClaudeImportReportsEveryComponent(t *testing.T) {
	roots, home := testRoots(t)
	dir := claudePlugin(t, roots, "full", `{"name":"full","version":"1.2.3","description":"Full.","author":{"name":"Ada"}}`)
	writeFile(t, dir, "commands/hello.md", "hello\n")
	writeFile(t, dir, "skills/sa/SKILL.md", "s\n")
	writeFile(t, dir, "agents/ag.md", "a\n")
	writeFile(t, dir, ".mcp.json", `{"mcpServers":{"srv":{"command":"node"}}}`)

	out, errOut := run(t, roots, false, SourceClaude)
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
	want := "  [ok]    full\n" +
		"          ✔ skills      : 1 installed (sa)\n" +
		"          ✔ agents      : 1 installed (ag)\n" +
		"          ✔ commands    : 1 installed (converted to skills) (hello)\n" +
		"          ✔ mcpServers  : 1 installed (srv)\n" +
		"\nStaged to " + roots.ConfigHome + "\n"
	if out != want {
		t.Fatalf("stdout =\n%q\nwant\n%q", out, want)
	}
	// The command became a skill.
	staged := filepath.Join(roots.ConfigHome, "plugins", "full", "skills", "full-cmd-hello", "SKILL.md")
	body, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("read %s: %v", staged, err)
	}
	if string(body) != "hello\n" {
		t.Errorf("the converted skill holds %q", body)
	}
	// The CLI's own manifest carries name, version, description and author.
	raw, err := os.ReadFile(filepath.Join(roots.ConfigHome, "plugins", "full", "plugin.json"))
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	if got, want := string(raw), "{\n  \"author\": \"Ada\",\n  \"description\": \"Full.\",\n  \"name\": \"full\",\n  \"version\": \"1.2.3\"\n}\n"; got != want {
		t.Errorf("plugin.json =\n%q\nwant\n%q", got, want)
	}
	// Claude Code's file name is gone; the CLI's own file carries the
	// servers.
	if _, err := os.Stat(filepath.Join(roots.ConfigHome, "plugins", "full", ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf("the staged plugin still carries .mcp.json")
	}
	raw, err = os.ReadFile(filepath.Join(roots.ConfigHome, "plugins", "full", "mcp_config.json"))
	if err != nil {
		t.Fatalf("read mcp_config.json: %v", err)
	}
	if !strings.Contains(string(raw), `"command": "node"`) {
		t.Errorf("mcp_config.json = %q", raw)
	}
	// The manifest records the components in the reference's own order.
	raw, err = os.ReadFile(filepath.Join(roots.ConfigHome, "import_manifest.json"))
	if err != nil {
		t.Fatalf("read import_manifest.json: %v", err)
	}
	var doc struct {
		Imports []struct {
			Name       string   `json:"name"`
			Source     string   `json:"source"`
			Components []string `json:"components"`
		} `json:"imports"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse import_manifest.json: %v", err)
	}
	if len(doc.Imports) != 1 {
		t.Fatalf("imports = %d, want 1", len(doc.Imports))
	}
	if doc.Imports[0].Source != "claude-code" {
		t.Errorf("source = %q, want claude-code", doc.Imports[0].Source)
	}
	if got, want := strings.Join(doc.Imports[0].Components, ","), "skills,agents,mcpServers,commands"; got != want {
		t.Errorf("components = %q, want %q", got, want)
	}
	_ = home
}

// The Gemini CLI importer reads ~/.gemini/extensions.
func TestGeminiImport(t *testing.T) {
	roots, _ := testRoots(t)
	writeFile(t, filepath.Join(roots.ExtensionsDir, "gext"), "gemini-extension.json", `{"name":"gext","version":"1.0.0","description":"G."}`)
	writeFile(t, filepath.Join(roots.ExtensionsDir, "gext", "skills", "gs"), "SKILL.md", "s\n")

	out, _ := run(t, roots, false, SourceGemini)
	want := "  [ok]    gext\n" +
		"          ✔ skills      : 1 installed (gs)\n" +
		"\nStaged to " + roots.ConfigHome + "\n"
	if out != want {
		t.Fatalf("stdout =\n%q\nwant\n%q", out, want)
	}
}

// The bare command runs the Gemini importer first and the Claude importer
// second, each with its own "Staged to" line.
func TestImportRunsBothSources(t *testing.T) {
	roots, _ := testRoots(t)
	writeFile(t, filepath.Join(roots.ExtensionsDir, "gext"), "gemini-extension.json", `{"name":"gext"}`)
	claudePlugin(t, roots, "cp", `{"name":"cp"}`)

	out, _ := run(t, roots, false, SourceGemini, SourceClaude)
	want := "  [ok]    gext\n" +
		"\nStaged to " + roots.ConfigHome + "\n" +
		"  [ok]    cp\n" +
		"\nStaged to " + roots.ConfigHome + "\n"
	if out != want {
		t.Fatalf("stdout =\n%q\nwant\n%q", out, want)
	}
}

// A source that finds nothing says so.
func TestImportReportsAnEmptySource(t *testing.T) {
	roots, _ := testRoots(t)
	out, _ := run(t, roots, false, SourceGemini, SourceClaude)
	want := "No gemini extensions found.\nNo claude extensions found.\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

// A plugin the manifest already lists is skipped; --force re-imports it.
func TestImportSkipsAndForces(t *testing.T) {
	roots, _ := testRoots(t)
	claudePlugin(t, roots, "cp", `{"name":"cp"}`)
	run(t, roots, false, SourceClaude)

	out, _ := run(t, roots, false, SourceClaude)
	if !strings.Contains(out, "  [skip]  cp (already imported, use --force to re-import)\n") {
		t.Fatalf("stdout = %q", out)
	}
	out, _ = run(t, roots, true, SourceClaude)
	if !strings.Contains(out, "  [ok]    cp\n") {
		t.Fatalf("stdout = %q", out)
	}
}

// A record's scope decides whether it is skipped.
func TestImportSkipsProjectAndLocalScopes(t *testing.T) {
	roots, home := testRoots(t)
	dir := claudePlugin(t, roots, "mkt/user", `{"name":"user"}`)
	claudePlugin(t, roots, "mkt/project", `{"name":"project"}`)
	claudePlugin(t, roots, "mkt/local", `{"name":"local"}`)
	writeFile(t, roots.ClaudePlugins, "installed_plugins.json",
		`{"plugins":{"mkt":[`+
			`{"name":"user","scope":"user","installPath":"`+dir+`"},`+
			`{"name":"project","scope":"project","installPath":"`+filepath.Join(home, ".claude/plugins/mkt/project")+`"},`+
			`{"name":"local","scope":"local","installPath":"`+filepath.Join(home, ".claude/plugins/mkt/local")+`"}`+
			`]}}`)

	out, _ := run(t, roots, false, SourceClaude)
	if !strings.Contains(out, "  [ok]    user\n") {
		t.Errorf("stdout = %q", out)
	}
	if strings.Contains(out, "project") || strings.Contains(out, "local") {
		t.Errorf("a project or local scoped record was imported:\n%s", out)
	}
}

// The MCP conversion rules, as measured.
func TestMCPConversion(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		want     string
		warnings []string
	}{
		{name: "http", file: `{"mcpServers":{"s":{"type":"http","url":"u","headers":{"A":"b"}}}}`,
			// The comparison marshals the decoded file, so the expected
			// value is written in the map's own (sorted) key order.
			want: `{"mcpServers":{"s":{"headers":{"A":"b"},"serverUrl":"u"}}}`},
		{name: "http without url", file: `{"mcpServers":{"s":{"type":"http"}}}`,
			warnings: []string{`MCP server "s" skipped: an http server needs a url`}},
		{name: "sse", file: `{"mcpServers":{"s":{"type":"sse","url":"u"}}}`,
			warnings: []string{`MCP server "s" skipped: the legacy SSE transport isn't supported`}},
		{name: "stdio without command", file: `{"mcpServers":{"s":{"type":"stdio"}}}`,
			warnings: []string{`MCP server "s" skipped: a stdio server needs a command`}},
		{name: "unknown type", file: `{"mcpServers":{"s":{"type":"other","url":"u"}}}`,
			warnings: []string{`MCP server "s" skipped: unsupported type "other"`}},
		{name: "url without type", file: `{"mcpServers":{"s":{"url":"u"}}}`,
			want: `{"mcpServers":{"s":{"serverUrl":"u"}}}`},
		{name: "command without type", file: `{"mcpServers":{"s":{"command":"c"}}}`,
			want: `{"mcpServers":{"s":{"command":"c"}}}`},
		{name: "neither", file: `{"mcpServers":{"s":{}}}`,
			warnings: []string{`MCP server "s" skipped: it has neither a command nor a url`}},
		{name: "not an object", file: `{"mcpServers":{"s":1}}`,
			warnings: []string{`MCP server "s" skipped: it isn't a JSON object`}},
		{name: "no wrapper", file: `{"s":{"command":"c"}}`,
			want: `{"mcpServers":{"s":{"command":"c"}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roots, _ := testRoots(t)
			dir := claudePlugin(t, roots, "cp", `{"name":"cp"}`)
			writeFile(t, dir, ".mcp.json", tc.file)
			out, _ := run(t, roots, false, SourceClaude)
			for _, w := range tc.warnings {
				if !strings.Contains(out, "          ⚠ "+w+"\n") {
					t.Errorf("stdout misses the warning %q:\n%s", w, out)
				}
			}
			if tc.want == "" {
				if _, err := os.Stat(filepath.Join(roots.ConfigHome, "plugins", "cp", "mcp_config.json")); !os.IsNotExist(err) {
					t.Errorf("mcp_config.json was written although no server survived")
				}
				return
			}
			raw, err := os.ReadFile(filepath.Join(roots.ConfigHome, "plugins", "cp", "mcp_config.json"))
			if err != nil {
				t.Fatalf("read mcp_config.json: %v", err)
			}
			var got any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("parse mcp_config.json: %v", err)
			}
			blob, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal mcp_config.json: %v", err)
			}
			if string(blob) != tc.want {
				t.Errorf("mcp_config.json =\n%s\nwant\n%s", blob, tc.want)
			}
		})
	}
}

// ${CLAUDE_PLUGIN_ROOT} expands to the staged plugin directory.
func TestPluginRootExpandsToTheStagedDirectory(t *testing.T) {
	roots, _ := testRoots(t)
	dir := claudePlugin(t, roots, "cp", `{"name":"cp"}`)
	writeFile(t, dir, ".mcp.json", `{"mcpServers":{"s":{"command":"node","args":["${CLAUDE_PLUGIN_ROOT}/s.js"],"env":{"X":"${CLAUDE_PLUGIN_ROOT}"}}}}`)
	run(t, roots, false, SourceClaude)
	raw, err := os.ReadFile(filepath.Join(roots.ConfigHome, "plugins", "cp", "mcp_config.json"))
	if err != nil {
		t.Fatalf("read mcp_config.json: %v", err)
	}
	staged := filepath.Join(roots.ConfigHome, "plugins", "cp")
	if !strings.Contains(string(raw), staged+"/s.js") {
		t.Errorf("mcp_config.json = %q", raw)
	}
	if !strings.Contains(string(raw), `"X": "`+staged+`"`) {
		t.Errorf("mcp_config.json = %q", raw)
	}
}

// The object form of a marketplace parses but yields no plugins.
func TestInstalledPluginsObjectFormYieldsNothing(t *testing.T) {
	roots, home := testRoots(t)
	dir := claudePlugin(t, roots, "mkt/alpha", `{"name":"alpha"}`)
	writeFile(t, roots.ClaudePlugins, "installed_plugins.json",
		`{"plugins":{"mkt":{"alpha":[{"installPath":"`+dir+`"}]}}}`)
	out, _ := run(t, roots, false, SourceClaude)
	if out != "No claude extensions found.\n" {
		t.Fatalf("stdout = %q", out)
	}
	_ = home
}

// A malformed installed_plugins.json is the reference's own error.
func TestInstalledPluginsParseError(t *testing.T) {
	roots, _ := testRoots(t)
	writeFile(t, roots.ClaudePlugins, "installed_plugins.json", "@@@")
	var out, errOut bytes.Buffer
	opts := Options{Roots: roots, Out: &out, ErrOut: &errOut}
	err := Run(opts, []Source{SourceClaude})
	if err == nil {
		t.Fatal("Run did not fail")
	}
	want := "error importing from claude: error discovering extensions: parsing " +
		filepath.Join(roots.ClaudePlugins, "installed_plugins.json") + ": invalid character '@' looking for beginning of value"
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

// A top-level plugin directory is imported as well.
func TestTopLevelPluginDirsAreImported(t *testing.T) {
	roots, _ := testRoots(t)
	claudePlugin(t, roots, "top", `{"name":"top"}`)
	out, _ := run(t, roots, false, SourceClaude)
	if !strings.Contains(out, "  [ok]    top\n") {
		t.Fatalf("stdout = %q", out)
	}
}
