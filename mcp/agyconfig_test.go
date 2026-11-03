// agyconfig_test.go — the two configuration file shapes.
//
// The clone reads two shapes of MCP configuration file: the reference
// binary's own `mcp.json` (the root member `servers`) and the documented
// `mcp_config.json` (the root member `mcpServers`). Both are read from
// every file, because both exist in the wild.
package mcp

import (
	"path/filepath"
	"testing"
)

// TestLoadConfigFileDocumentedShape: the documented file carries the root
// member `mcpServers`, the object-shaped `env` and `headers`, and the
// documented `serverUrl`.
func TestLoadConfigFileDocumentedShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	writeFile(t, path, `{"mcpServers":{
		"sqlite-explorer":{"command":"node","args":["/usr/local/bin/sqlite-mcp-server.js"],
			"env":{"SQLITE_DB_PATH":"/var/data/app.db"}},
		"my-remote-server":{"serverUrl":"https://api.example.com/mcp/",
			"headers":{"Authorization":"Bearer TOKEN"},
			"timeoutSeconds":600}}}`)

	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	servers := cfg.AllServers()
	if len(servers) != 2 {
		t.Fatalf("servers = %d, want 2: %+v", len(servers), servers)
	}
	stdio := servers["sqlite-explorer"]
	if stdio.Command != "node" {
		t.Errorf("command = %q, want node", stdio.Command)
	}
	if stdio.Env["SQLITE_DB_PATH"] != "/var/data/app.db" {
		t.Errorf("env = %v, want the documented object", stdio.Env)
	}
	if !stdio.Enabled {
		t.Error("a documented entry with no `disabled` member must be enabled")
	}
	remote := servers["my-remote-server"]
	if remote.Endpoint() != "https://api.example.com/mcp/" {
		t.Errorf("endpoint = %q, want the documented serverUrl", remote.Endpoint())
	}
	if remote.Headers["Authorization"] != "Bearer TOKEN" {
		t.Errorf("headers = %v, want the documented object", remote.Headers)
	}
	if remote.TimeoutSeconds != 600 {
		t.Errorf("timeoutSeconds = %d, want 600", remote.TimeoutSeconds)
	}
	if remote.NormaliseType() != "http" {
		t.Errorf("type = %q, want http (a serverUrl means a remote server)", remote.NormaliseType())
	}
}

// TestLoadConfigFileReferenceShape: the reference's own file carries the
// root member `servers` and the `enabled` flag.
func TestLoadConfigFileReferenceShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	writeFile(t, path, `{"servers":{
		"fs":{"type":"stdio","command":"npx","args":["-y","server-filesystem","/tmp"],
			"env":{"TOKEN":"x"},"enabled":true,"alwaysLoad":["read_file"]},
		"web":{"type":"http","url":"https://example.invalid/mcp","enabled":true},
		"off":{"type":"stdio","command":"x","enabled":false}}}`)

	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	servers := cfg.AllServers()
	if len(servers) != 3 {
		t.Fatalf("servers = %d, want 3", len(servers))
	}
	if !servers["fs"].Enabled {
		t.Error("fs is not enabled")
	}
	if servers["off"].Enabled {
		t.Error("off must be disabled")
	}
	specs := cfg.Specs()
	if len(specs) != 2 {
		t.Fatalf("specs = %d, want 2 (the disabled server is skipped)", len(specs))
	}
}

// TestLoadConfigFileBothRootKeys: a file may carry both root members.
func TestLoadConfigFileBothRootKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	writeFile(t, path, `{"servers":{"ref":{"command":"a","enabled":true}},
		"mcpServers":{"doc":{"command":"b","disabled":false}}}`)

	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	servers := cfg.AllServers()
	if len(servers) != 2 {
		t.Fatalf("servers = %d, want 2: %+v", len(servers), servers)
	}
	if servers["ref"].Command != "a" || servers["doc"].Command != "b" {
		t.Errorf("servers = %+v, want both root members read", servers)
	}
}

// TestDocumentedEntryDisabled: the documented `disabled` flag suspends a
// server, and it is the inverse of the reference's `enabled` flag.
func TestDocumentedEntryDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	writeFile(t, path, `{"mcpServers":{"off":{"command":"x","disabled":true}}}`)

	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if cfg.AllServers()["off"].Enabled {
		t.Error("a documented `disabled: true` entry must not be enabled")
	}
	if specs := cfg.Specs(); len(specs) != 0 {
		t.Errorf("specs = %d, want 0 (the server is suspended)", len(specs))
	}
}

// TestDocumentedEntryOverridesReferenceEntry: the documented root member
// wins over the reference's own for the same name.
func TestDocumentedEntryOverridesReferenceEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	writeFile(t, path, `{"servers":{"x":{"command":"reference","enabled":true}},
		"mcpServers":{"x":{"command":"documented","disabled":false}}}`)

	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if got := cfg.AllServers()["x"].Command; got != "documented" {
		t.Errorf("command = %q, want the documented entry", got)
	}
}

// TestLegacyArrayEnvAndHeaders: an older build of the clone wrote `env`
// and `headers` as arrays of "KEY=value" strings. The file is the user's
// own, so the array form is read as the object it stands for — a file
// that cannot be decoded is dropped whole.
func TestLegacyArrayEnvAndHeaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	writeFile(t, path, `{"mcpServers":{"x":{"command":"node",
		"env":["GITHUB_TOKEN=xxx","OTHER=a=b"],
		"headers":["Authorization: Bearer TOKEN"]}}}`)

	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	entry := cfg.AllServers()["x"]
	if entry.Env["GITHUB_TOKEN"] != "xxx" {
		t.Errorf("env = %v, want the array read as an object", entry.Env)
	}
	if entry.Env["OTHER"] != "a=b" {
		t.Errorf("env[OTHER] = %q, want a=b", entry.Env["OTHER"])
	}
	if entry.Headers["Authorization: Bearer TOKEN"] != "" {
		t.Errorf("headers = %v, want the array read as an object", entry.Headers)
	}
}

// TestServerFileDocumentedMembers: the six documented members decode.
func TestServerFileDocumentedMembers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	writeFile(t, path, `{"mcpServers":{"x":{
		"cwd":"/work",
		"authProviderType":"google_credentials",
		"oauth":{"clientId":"cid","clientSecret":"secret"},
		"enabledTools":["a","b"],
		"disabledTools":["c"],
		"timeoutSeconds":42,
		"command":"node"}}}`)

	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	entry := cfg.AllServers()["x"]
	if entry.CWD != "/work" {
		t.Errorf("cwd = %q, want /work", entry.CWD)
	}
	if entry.AuthProviderType != "google_credentials" {
		t.Errorf("authProviderType = %q, want google_credentials", entry.AuthProviderType)
	}
	if entry.OAuth["clientId"] != "cid" {
		t.Errorf("oauth = %v, want the documented object", entry.OAuth)
	}
	if len(entry.EnabledTools) != 2 || entry.EnabledTools[0] != "a" {
		t.Errorf("enabledTools = %v, want [a b]", entry.EnabledTools)
	}
	if len(entry.DisabledTools) != 1 || entry.DisabledTools[0] != "c" {
		t.Errorf("disabledTools = %v, want [c]", entry.DisabledTools)
	}
	if entry.TimeoutSeconds != 42 {
		t.Errorf("timeoutSeconds = %d, want 42", entry.TimeoutSeconds)
	}
}

// TestSpecCarriesTheDocumentedMembers: the connection spec carries the
// documented members on.
func TestSpecCarriesTheDocumentedMembers(t *testing.T) {
	entry := ServerFile{
		Command:          "node",
		CWD:              "/work",
		AuthProviderType: "google_credentials",
		OAuth:            map[string]any{"clientId": "cid"},
		EnabledTools:     []string{"a"},
		DisabledTools:    []string{"b"},
		TimeoutSeconds:   42,
		Enabled:          true,
	}
	spec := entry.Spec("x")
	if spec.CWD != "/work" {
		t.Errorf("spec.CWD = %q, want /work", spec.CWD)
	}
	if spec.AuthProviderType != "google_credentials" {
		t.Errorf("spec.AuthProviderType = %q, want google_credentials", spec.AuthProviderType)
	}
	if spec.OAuth["clientId"] != "cid" {
		t.Errorf("spec.OAuth = %v, want the documented object", spec.OAuth)
	}
	if len(spec.EnabledTools) != 1 || spec.EnabledTools[0] != "a" {
		t.Errorf("spec.EnabledTools = %v, want [a]", spec.EnabledTools)
	}
	if len(spec.DisabledTools) != 1 || spec.DisabledTools[0] != "b" {
		t.Errorf("spec.DisabledTools = %v, want [b]", spec.DisabledTools)
	}
	if spec.TimeoutSeconds != 42 {
		t.Errorf("spec.TimeoutSeconds = %d, want 42", spec.TimeoutSeconds)
	}
}
