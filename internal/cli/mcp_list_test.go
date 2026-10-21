// Tests for the v1.3.5 `mcp list` subcommand.
//
// Config lives at $HOME/.gemini/config/mcp_config.json (or
// $GEMINI_CONFIG_HOME/mcp_config.json) with schema:
//
//	{"mcpServers": {"<name>": {"command": ..., "args": [...],
//	                            "disabled": bool, "serverUrl": ...}}}
//
// `mcp list` prints an aligned 4-column table:
//
//	NAME   TYPE   STATUS   COMMAND/URL
//
// Column widths are NAME=max(4,maxlen)+2, TYPE=max(4,maxlen)+2,
// STATUS=max(6,maxlen)+2 (verified byte-for-byte against the reference:
// a config with a 15-char name yields NAME width 17, a "disabled" row
// yields STATUS width 10). Names are sorted ascending.
//
// When the config is missing or empty the handler prints
// "No MCP servers configured." on stdout and exits 0.
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newParityJetski returns a JetskiCLI writing results to out and
// usage/errors to errb.
func newParityJetski(out, errb *bytes.Buffer) *JetskiCLI {
	j := NewJetski("agy", "1.3.5")
	j.Out = out
	j.ErrOut = errb
	return j
}

// writeMCPConfig points $HOME at a fresh temp dir and writes the given
// mcpServers JSON (the full document body).
func writeMCPConfig(t *testing.T, doc string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".gemini", "config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mcp_config.json"), []byte(doc), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestMCPList_Empty(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no config file at all
	var out, errb bytes.Buffer
	if err := runMCPList(newParityJetski(&out, &errb), nil); err != nil {
		t.Fatalf("runMCPList: %v", err)
	}
	if got, want := out.String(), "No MCP servers configured.\n"; got != want {
		t.Errorf("stdout: got %q, want %q", got, want)
	}
}

func TestMCPList_Table(t *testing.T) {
	writeMCPConfig(t, `{"mcpServers":{
		"foo":{"command":"bar","args":[],"disabled":false},
		"dummy":{"serverUrl":"http://localhost:8080","disabled":false},
		"runner":{"command":"node","args":["server.js","--port=8080"],"disabled":false},
		"off":{"serverUrl":"https://example.com","disabled":true},
		"chrome-devtools":{"command":"npx","args":["chrome-devtools-mcp@latest"],"disabled":false}
	}}`)

	var out, errb bytes.Buffer
	if err := runMCPList(newParityJetski(&out, &errb), nil); err != nil {
		t.Fatalf("runMCPList: %v", err)
	}
	want := "NAME             TYPE   STATUS    COMMAND/URL\n" +
		"chrome-devtools  stdio  enabled   npx chrome-devtools-mcp@latest\n" +
		"dummy            http   enabled   http://localhost:8080\n" +
		"foo              stdio  enabled   bar\n" +
		"off              http   disabled  https://example.com\n" +
		"runner           stdio  enabled   node server.js --port=8080\n"
	if got := out.String(); got != want {
		t.Errorf("mcp list output drift\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestMCPList_BadFlag(t *testing.T) {
	writeMCPConfig(t, `{"mcpServers":{}}`)
	var out, errb bytes.Buffer
	err := runMCPList(newParityJetski(&out, &errb), []string{"--json"})
	if err == nil {
		t.Fatalf("mcp list --json should fail")
	}
	got := errb.String()
	if !strings.Contains(got, "Error: flags provided but not defined: -json") {
		t.Errorf("stderr missing flag error\n--- got ---\n%s", got)
	}
	if !strings.HasPrefix(got, "Usage: agy mcp list [flags]") {
		t.Errorf("stderr should start with the list usage\n--- got ---\n%s", got)
	}
}

// TestMCPList_Dispatch exercises the full Parser path for `mcp list`.
func TestMCPList_Dispatch(t *testing.T) {
	writeMCPConfig(t, `{"mcpServers":{"foo":{"command":"bar","disabled":false}}}`)
	var out, errb bytes.Buffer
	p := NewParser(StandardFlags(), StandardSubcommands(), "agy", "recovered", "recovered")
	p.SetOut(&out)
	p.SetErr(&errb)
	if err := p.ExecuteArgs([]string{"mcp", "list"}); err != nil {
		t.Fatalf("mcp list: %v", err)
	}
	want := "NAME  TYPE   STATUS   COMMAND/URL\n" +
		"foo   stdio  enabled  bar\n"
	if got := out.String(); got != want {
		t.Errorf("mcp list dispatch output drift\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if errb.Len() != 0 {
		t.Errorf("mcp list stderr should be empty, got %q", errb.String())
	}
}

// TestMCPList_GeminiConfigHome verifies $GEMINI_CONFIG_HOME overrides
// $HOME for the config location.
func TestMCPList_GeminiConfigHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, "mcp_config.json"),
		[]byte(`{"mcpServers":{"x":{"command":"y"}}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var out, errb bytes.Buffer
	if err := runMCPList(newParityJetski(&out, &errb), nil); err != nil {
		t.Fatalf("runMCPList: %v", err)
	}
	if !strings.Contains(out.String(), "x     stdio  enabled  y") {
		t.Errorf("expected server x row in table, got:\n%s", out.String())
	}
}
