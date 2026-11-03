// mcp_add_test.go — the `agy mcp add` writer.
//
// The writer owns the documented file's shape: the root member
// `mcpServers`, and the object-shaped `env` and `headers` the
// documentation describes ("Key-value map of environment variables",
// "Custom HTTP request headers").
//
// The reference's own semantics are pinned here byte for byte: an entry
// the command names is *replaced*, a root member the writer does not own
// survives, a malformed file is refused rather than overwritten, and the
// two flags that belong to the other transport are rejected.
//
// 【一手】 every expectation below was observed against the reference
// binary (1.4.12) under a temporary HOME.
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// mcpConfigFile reads the MCP configuration file the writer maintains.
func mcpConfigFile(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(os.Getenv("GEMINI_CONFIG_HOME"), "mcp_config.json"))
	if err != nil {
		t.Fatalf("read the config: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse the config: %v (%s)", err, raw)
	}
	return doc
}

// mcpEntryNames returns the member names of a server entry, sorted.
func mcpEntryNames(entry map[string]any) []string {
	names := make([]string, 0, len(entry))
	for name := range entry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestMCPAddWritesEnvAndHeadersAsObjects: the two flags are repeatable and
// their pairs are written as JSON objects, not as arrays.
//
// The two belong to the two transports: `env` to a stdio server,
// `headers` to an http one. 【一手】 the reference binary (1.4.12)
// refuses the other pairing outright.
func TestMCPAddWritesEnvAndHeadersAsObjects(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_HOME", t.TempDir())
	var out, errb bytes.Buffer
	j := newParityJetski(&out, &errb)

	err := runMCPAdd(j, []string{
		"--env", "GITHUB_TOKEN=xxx",
		"--env", "OTHER=a=b",
		"gh", "docker", "run", "-i", "ghcr.io/github/mcp",
	})
	if err != nil {
		t.Fatalf("runMCPAdd: %v", err)
	}
	doc := mcpConfigFile(t)
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("the document has no mcpServers root: %+v", doc)
	}
	entry, ok := servers["gh"].(map[string]any)
	if !ok {
		t.Fatalf("the document has no gh entry: %+v", servers)
	}
	env, ok := entry["env"].(map[string]any)
	if !ok {
		t.Fatalf("env is not an object: %#v", entry["env"])
	}
	if env["GITHUB_TOKEN"] != "xxx" {
		t.Errorf("env[GITHUB_TOKEN] = %#v, want xxx", env["GITHUB_TOKEN"])
	}
	if env["OTHER"] != "a=b" {
		t.Errorf("env[OTHER] = %#v, want a=b (the split is on the first =)", env["OTHER"])
	}

	out.Reset()
	errb.Reset()
	if err := runMCPAdd(j, []string{
		"--header", "Authorization: Bearer TOKEN",
		"--header", "X-Trace=1",
		"api", "https://example.com/mcp",
	}); err != nil {
		t.Fatalf("runMCPAdd: %v", err)
	}
	doc = mcpConfigFile(t)
	servers = doc["mcpServers"].(map[string]any)
	entry, ok = servers["api"].(map[string]any)
	if !ok {
		t.Fatalf("the document has no api entry: %+v", servers)
	}
	headers, ok := entry["headers"].(map[string]any)
	if !ok {
		t.Fatalf("headers is not an object: %#v", entry["headers"])
	}
	if headers["Authorization"] != "Bearer TOKEN" {
		t.Errorf("headers[Authorization] = %#v, want Bearer TOKEN", headers["Authorization"])
	}
	if headers["X-Trace"] != "1" {
		t.Errorf("headers[X-Trace] = %#v, want 1", headers["X-Trace"])
	}
}

// TestMCPAddWritesTheDocumentedEntry: the entry carries the documented
// transport member and the documented `disabled` flag.
func TestMCPAddWritesTheDocumentedEntry(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_HOME", t.TempDir())
	var out, errb bytes.Buffer
	j := newParityJetski(&out, &errb)

	if err := runMCPAdd(j, []string{"api", "https://example.com/mcp"}); err != nil {
		t.Fatalf("runMCPAdd: %v", err)
	}
	doc := mcpConfigFile(t)
	servers := doc["mcpServers"].(map[string]any)
	entry := servers["api"].(map[string]any)
	if entry["serverUrl"] != "https://example.com/mcp" {
		t.Errorf("serverUrl = %#v, want the endpoint", entry["serverUrl"])
	}
	if entry["disabled"] != false {
		t.Errorf("disabled = %#v, want false", entry["disabled"])
	}
	if _, ok := entry["env"]; ok {
		t.Errorf("env must be absent when no --env was given: %+v", entry)
	}
}

// TestMCPAddReplacesTheTargetEntry: a re-add replaces the entry the
// command names.
//
// The reference does not merge the new entry into the one the file
// holds: the entry is written from scratch, so the documented members
// the `agy mcp add` flags cannot express — `cwd`, `authProviderType`,
// `oauth`, `enabledTools`, `disabledTools` and `timeoutSeconds` — and
// any member beside them that the file carried are dropped.
//
// 【一手】 the reference binary (1.4.12), under a temporary HOME: a file
// whose `gh` entry carried all six documented members and a member the
// schema does not list was rewritten by `agy mcp add gh node server.js`
// with an entry carrying only `args`, `command` and `disabled`.
func TestMCPAddReplacesTheTargetEntry(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_HOME", t.TempDir())
	existing := `{"mcpServers":{"gh":{
		"command":"old","args":["old.js"],
		"cwd":"/work",
		"authProviderType":"google_credentials",
		"oauth":{"clientId":"cid","clientSecret":"secret"},
		"enabledTools":["a"],
		"disabledTools":["b"],
		"timeoutSeconds":42,
		"futureMember":{"nested":true}
	}}}`
	if err := os.WriteFile(filepath.Join(os.Getenv("GEMINI_CONFIG_HOME"), "mcp_config.json"),
		[]byte(existing), 0o644); err != nil {
		t.Fatalf("write the config: %v", err)
	}

	var out, errb bytes.Buffer
	j := newParityJetski(&out, &errb)
	if err := runMCPAdd(j, []string{"gh", "node", "server.js"}); err != nil {
		t.Fatalf("runMCPAdd: %v", err)
	}

	doc := mcpConfigFile(t)
	servers := doc["mcpServers"].(map[string]any)
	entry, ok := servers["gh"].(map[string]any)
	if !ok {
		t.Fatalf("the document has no gh entry: %+v", servers)
	}

	// The entry carries the transport members and the flag, and nothing
	// else: the six documented members and the unknown member are gone.
	want := []string{"args", "command", "disabled"}
	if got := mcpEntryNames(entry); !reflect.DeepEqual(got, want) {
		t.Errorf("the entry carries %v, want %v", got, want)
	}
	if entry["command"] != "node" {
		t.Errorf("command = %#v, want node", entry["command"])
	}
	args, ok := entry["args"].([]any)
	if !ok || len(args) != 1 || args[0] != "server.js" {
		t.Errorf("args = %#v, want [server.js]", entry["args"])
	}
	if entry["disabled"] != false {
		t.Errorf("disabled = %#v, want false", entry["disabled"])
	}
}

// TestMCPAddKeepsTheOtherEntriesAndRootMembers: the writer replaces the
// entry the command names and nothing else.
//
// 【一手】 the reference binary (1.4.12): a file that carried a sibling
// entry, the reference binary's own `servers` root, a `disableAllMCP`
// switch and an unknown root member kept every one of them; the sibling
// entry kept a member the documented schema does not list.
func TestMCPAddKeepsTheOtherEntriesAndRootMembers(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_HOME", t.TempDir())
	existing := `{"mcpServers":{
		"gh":{"command":"old"},
		"sibling":{"command":"sib","cwd":"/other","futureMember":{"nested":true}}
	},
	"servers":{"other":{"type":"stdio","command":"x","enabled":true}},
	"disableAllMCP":false,
	"unknownRoot":{"keep":true}}`
	if err := os.WriteFile(filepath.Join(os.Getenv("GEMINI_CONFIG_HOME"), "mcp_config.json"),
		[]byte(existing), 0o644); err != nil {
		t.Fatalf("write the config: %v", err)
	}

	var out, errb bytes.Buffer
	j := newParityJetski(&out, &errb)
	if err := runMCPAdd(j, []string{"gh", "node", "server.js"}); err != nil {
		t.Fatalf("runMCPAdd: %v", err)
	}

	doc := mcpConfigFile(t)
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("the document has no mcpServers root: %+v", doc)
	}
	sibling, ok := servers["sibling"].(map[string]any)
	if !ok {
		t.Fatalf("the sibling entry is gone: %+v", servers)
	}
	if sibling["command"] != "sib" || sibling["cwd"] != "/other" {
		t.Errorf("the sibling entry lost a member: %+v", sibling)
	}
	future, ok := sibling["futureMember"].(map[string]any)
	if !ok {
		t.Fatalf("the sibling entry lost its unknown member: %+v", sibling)
	}
	if future["nested"] != true {
		t.Errorf("futureMember = %#v, want the user's own member", future)
	}
	if _, ok := doc["servers"]; !ok {
		t.Errorf("the reference's own root member was dropped: %+v", doc)
	}
	if _, ok := doc["disableAllMCP"]; !ok {
		t.Errorf("the disableAllMCP switch was dropped: %+v", doc)
	}
	keep, ok := doc["unknownRoot"].(map[string]any)
	if !ok {
		t.Fatalf("the unknown root member was dropped: %+v", doc)
	}
	if keep["keep"] != true {
		t.Errorf("unknownRoot = %#v, want the user's own member", keep)
	}
}

// TestMCPAddWritesOnlyTheDocumentedMembers: the written entry carries the
// documented schema and nothing else.
//
// The reference binary's own `mcp.json` members — `type`, `url`,
// `enabled` and `alwaysLoad` — belong to that file, not to the documented
// `mcp_config.json`.
func TestMCPAddWritesOnlyTheDocumentedMembers(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_HOME", t.TempDir())
	var out, errb bytes.Buffer
	j := newParityJetski(&out, &errb)

	if err := runMCPAdd(j, []string{"api", "https://example.com/mcp"}); err != nil {
		t.Fatalf("runMCPAdd: %v", err)
	}
	doc := mcpConfigFile(t)
	servers := doc["mcpServers"].(map[string]any)
	entry, ok := servers["api"].(map[string]any)
	if !ok {
		t.Fatalf("the document has no api entry: %+v", servers)
	}
	for _, name := range []string{"type", "url", "enabled", "alwaysLoad"} {
		if _, ok := entry[name]; ok {
			t.Errorf("the entry carries the reference's own member %q: %+v", name, entry)
		}
	}
}

// TestMCPAddRefusesEnvOnAnHTTPServer: `env` belongs to the stdio
// transport, so the reference refuses the flag on an http server.
//
// 【一手】 the reference binary (1.4.12):
//
//	$ agy mcp add --env FOO=bar api https://example.com/mcp
//	Error: http server type does not accept --env flags      (exit 1)
func TestMCPAddRefusesEnvOnAnHTTPServer(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_HOME", t.TempDir())
	var out, errb bytes.Buffer
	j := newParityJetski(&out, &errb)

	err := runMCPAdd(j, []string{"--env", "FOO=bar", "api", "https://example.com/mcp"})
	if err == nil {
		t.Fatalf("the command was accepted: %s", out.String())
	}
	if code, ok := ExitCodeOf(err); !ok || code != 1 {
		t.Errorf("the exit code is %d (%v), want 1", code, err)
	}
	want := "Error: http server type does not accept --env flags\n"
	if got := errb.String(); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("GEMINI_CONFIG_HOME"), "mcp_config.json")); !os.IsNotExist(err) {
		t.Errorf("the file was written although the command was refused")
	}
}

// TestMCPAddRefusesAHeaderOnAStdioServer: `headers` belongs to the http
// transport.
//
// 【一手】 the reference binary (1.4.12):
//
//	$ agy mcp add --header "A: b" fs npx
//	Error: stdio server type does not accept --header flags   (exit 1)
func TestMCPAddRefusesAHeaderOnAStdioServer(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_HOME", t.TempDir())
	var out, errb bytes.Buffer
	j := newParityJetski(&out, &errb)

	err := runMCPAdd(j, []string{"--header", "A: b", "fs", "npx"})
	if err == nil {
		t.Fatalf("the command was accepted: %s", out.String())
	}
	if code, ok := ExitCodeOf(err); !ok || code != 1 {
		t.Errorf("the exit code is %d (%v), want 1", code, err)
	}
	want := "Error: stdio server type does not accept --header flags\n"
	if got := errb.String(); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

// TestMCPAddRefusesAnUnknownServerType: the two documented names are the
// only ones accepted.
//
// 【一手】 the reference binary (1.4.12):
//
//	$ agy mcp add --type sse fs npx
//	Error: invalid server type "sse": must be 'stdio' or 'http'  (exit 1)
func TestMCPAddRefusesAnUnknownServerType(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_HOME", t.TempDir())
	var out, errb bytes.Buffer
	j := newParityJetski(&out, &errb)

	err := runMCPAdd(j, []string{"--type", "sse", "fs", "npx"})
	if err == nil {
		t.Fatalf("the command was accepted: %s", out.String())
	}
	want := "Error: invalid server type \"sse\": must be 'stdio' or 'http'\n"
	if got := errb.String(); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

// TestMCPAddAcceptsTheSpelledServerTypes: the type name is trimmed and
// lowercased before it is compared, so `--type HTTP` is the http
// transport.
//
// 【一手】 the reference binary (1.4.12): `--type HTTP`, `--type Http`
// and `--type " stdio"` are all accepted.
func TestMCPAddAcceptsTheSpelledServerTypes(t *testing.T) {
	for _, tc := range []struct {
		typ  string
		kind string
	}{
		{"HTTP", "http"},
		{"Http", "http"},
		{" stdio", "stdio"},
	} {
		t.Run(tc.typ, func(t *testing.T) {
			t.Setenv("GEMINI_CONFIG_HOME", t.TempDir())
			var out, errb bytes.Buffer
			j := newParityJetski(&out, &errb)

			if err := runMCPAdd(j, []string{"--type", tc.typ, "fs", "npx"}); err != nil {
				t.Fatalf("runMCPAdd: %v (%s)", err, errb.String())
			}
			want := "Added MCP server \"fs\" (" + tc.kind + ")\n"
			if got := out.String(); got != want {
				t.Errorf("stdout = %q, want %q", got, want)
			}
		})
	}
}

// TestMCPAddRefusesToOverwriteAMalformedFile: a file the writer cannot
// parse is refused, not overwritten.
//
// 【一手】 the reference binary (1.4.12):
//
//	Error: failed to parse <path>: failed to parse JSON config: invalid character 'n' looking for beginning of object key string
func TestMCPAddRefusesToOverwriteAMalformedFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", dir)
	path := filepath.Join(dir, "mcp_config.json")
	if err := os.WriteFile(path, []byte("{ not json"), 0o644); err != nil {
		t.Fatalf("write the config: %v", err)
	}

	var out, errb bytes.Buffer
	j := newParityJetski(&out, &errb)

	err := runMCPAdd(j, []string{"gh", "node", "server.js"})
	if err == nil {
		t.Fatalf("the command was accepted: %s", out.String())
	}
	want := "Error: failed to parse " + path + ": failed to parse JSON config: invalid character 'n' looking for beginning of object key string\n"
	if got := errb.String(); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read the config: %v", readErr)
	}
	if string(raw) != "{ not json" {
		t.Errorf("the malformed file was overwritten: %q", raw)
	}
}

// TestMCPAddRemovesAndDisables: the other subcommands read and write the
// same file.
func TestMCPAddRemovesAndDisables(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_HOME", t.TempDir())
	var out, errb bytes.Buffer
	j := newParityJetski(&out, &errb)

	if err := runMCPAdd(j, []string{"srv", "node", "server.js"}); err != nil {
		t.Fatalf("runMCPAdd: %v", err)
	}
	if err := runMCPSetEnabled(false)(j, []string{"srv"}); err != nil {
		t.Fatalf("mcp disable: %v", err)
	}
	doc := mcpConfigFile(t)
	servers := doc["mcpServers"].(map[string]any)
	entry := servers["srv"].(map[string]any)
	if entry["disabled"] != true {
		t.Errorf("disabled = %#v, want true", entry["disabled"])
	}
	if err := runMCPRemove(j, []string{"srv"}); err != nil {
		t.Fatalf("mcp remove: %v", err)
	}
	doc = mcpConfigFile(t)
	servers = doc["mcpServers"].(map[string]any)
	if len(servers) != 0 {
		t.Errorf("servers = %+v, want none", servers)
	}
}

// TestMCPEnableRemovesTheFlagAndDropsAnEmptiedEntry: resuming a server
// removes the documented `disabled` flag, and a server whose entry
// carries nothing else is dropped from the file.
//
// 【一手】 the reference binary (1.4.12): `agy mcp enable` on an entry
// that carried only `{"disabled": true}` removed the entry; on an entry
// that carried a member too, it kept the member and removed the flag.
func TestMCPEnableRemovesTheFlagAndDropsAnEmptiedEntry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", dir)
	existing := `{"mcpServers":{
		"only":{"disabled":true},
		"withMore":{"command":"c","disabled":true,"futureMember":{"nested":true}}
	}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp_config.json"), []byte(existing), 0o644); err != nil {
		t.Fatalf("write the config: %v", err)
	}

	var out, errb bytes.Buffer
	j := newParityJetski(&out, &errb)
	if err := runMCPSetEnabled(true)(j, []string{"only"}); err != nil {
		t.Fatalf("mcp enable: %v", err)
	}
	if err := runMCPSetEnabled(true)(j, []string{"withMore"}); err != nil {
		t.Fatalf("mcp enable: %v", err)
	}

	doc := mcpConfigFile(t)
	servers := doc["mcpServers"].(map[string]any)
	if _, ok := servers["only"]; ok {
		t.Errorf("the emptied entry survived: %+v", servers)
	}
	entry, ok := servers["withMore"].(map[string]any)
	if !ok {
		t.Fatalf("the entry is gone: %+v", servers)
	}
	if _, ok := entry["disabled"]; ok {
		t.Errorf("the disabled flag survived: %+v", entry)
	}
	if entry["command"] != "c" {
		t.Errorf("the other members were touched: %+v", entry)
	}
	future, ok := entry["futureMember"].(map[string]any)
	if !ok {
		t.Fatalf("the unknown member was dropped: %+v", entry)
	}
	if future["nested"] != true {
		t.Errorf("futureMember = %#v, want the user's own member", future)
	}
}

// TestMCPDisableCreatesTheEntry: suspending a server the file does not
// know creates the entry.
//
// 【一手】 the reference binary (1.4.12): `agy mcp disable zzz` on a
// file that did not know `zzz` wrote the entry `{"disabled": true}` and
// exited 0.
func TestMCPDisableCreatesTheEntry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", dir)

	var out, errb bytes.Buffer
	j := newParityJetski(&out, &errb)
	if err := runMCPSetEnabled(false)(j, []string{"zzz"}); err != nil {
		t.Fatalf("mcp disable: %v", err)
	}
	want := "Disabled MCP server \"zzz\"\n"
	if got := out.String(); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	doc := mcpConfigFile(t)
	servers := doc["mcpServers"].(map[string]any)
	entry, ok := servers["zzz"].(map[string]any)
	if !ok {
		t.Fatalf("the entry was not created: %+v", servers)
	}
	if entry["disabled"] != true {
		t.Errorf("disabled = %#v, want true", entry["disabled"])
	}
}
