package agentturn

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestACPRegistry_BuiltinAgents(t *testing.T) {
	for _, id := range []string{"agy-acp", "pigasus-acp", "codex-acp", "claude-acp"} {
		m, ok := BuiltinACPRegistry[id]
		if !ok {
			t.Fatalf("missing builtin agent %q", id)
		}
		if m.ID != id {
			t.Errorf("agent id = %q, want %q", m.ID, id)
		}
		if m.Name == "" {
			t.Errorf("agent %q has empty name", id)
		}
	}
}

func TestACPRegistry_LoadManifest(t *testing.T) {
	dir := t.TempDir()
	manifestJSON := `{
		"id": "test-agent",
		"name": "Test Agent",
		"version": "1.0.0",
		"description": "A test ACP agent",
		"distribution": {
			"binary": {
				"all": {
					"cmd": "test-agent-bin",
					"args": ["serve", "--stdio"]
				}
			},
			"npx": {
				"package": "@test/agent-acp",
				"args": ["--acp"]
			}
		}
	}`

	filePath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(filePath, []byte(manifestJSON), 0644); err != nil {
		t.Fatal(err)
	}

	// Test loading by file path
	m1, err := LoadACPAgentManifest(filePath)
	if err != nil {
		t.Fatalf("LoadACPAgentManifest file failed: %v", err)
	}
	if m1.ID != "test-agent" || m1.Name != "Test Agent" {
		t.Errorf("unexpected manifest content: %+v", m1)
	}

	// Test loading by directory path
	m2, err := LoadACPAgentManifest(dir)
	if err != nil {
		t.Fatalf("LoadACPAgentManifest dir failed: %v", err)
	}
	if m2.ID != "test-agent" || m2.Distribution.NPX == nil || m2.Distribution.NPX.Package != "@test/agent-acp" {
		t.Errorf("unexpected directory manifest content: %+v", m2)
	}
}

func TestACPRegistry_ResolveACPAgent_BuiltinAliases(t *testing.T) {
	// Agy alias resolution
	for _, alias := range []string{"agy", "agy-acp", "agy-acp-server", "antigravity"} {
		cmd, args, _, err := ResolveACPAgent(alias)
		if err != nil {
			t.Fatalf("ResolveACPAgent(%q) failed: %v", alias, err)
		}
		if cmd == "" {
			t.Errorf("ResolveACPAgent(%q) cmd is empty", alias)
		}
		if len(args) == 0 {
			t.Errorf("ResolveACPAgent(%q) args is empty", alias)
		}
	}

	// Pigasus alias resolution
	for _, alias := range []string{"pigasus", "pigasus-acp", "pig-acp"} {
		cmd, args, _, err := ResolveACPAgent(alias)
		if err != nil {
			t.Fatalf("ResolveACPAgent(%q) failed: %v", alias, err)
		}
		if cmd == "" {
			t.Errorf("ResolveACPAgent(%q) cmd is empty", alias)
		}
		if len(args) == 0 || args[0] != "acp" {
			t.Errorf("ResolveACPAgent(%q) args = %v, want ['acp']", alias, args)
		}
	}

	// Codex resolution
	_, _, _, errCodex := ResolveACPAgent("codex-acp")
	// If codex-acp or npx is installed on machine it succeeds; if neither, returns descriptive error
	if errCodex != nil && !testing.Short() {
		t.Logf("codex-acp resolution returned expected hint when binary/npx not in test env: %v", errCodex)
	}

	// Claude resolution
	_, _, _, errClaude := ResolveACPAgent("claude-acp")
	if errClaude != nil && !testing.Short() {
		t.Logf("claude-acp resolution returned expected hint when binary/npx not in test env: %v", errClaude)
	}
}

func TestACPRegistry_ResolveACPAgent_ManifestFile(t *testing.T) {
	dir := t.TempDir()
	manifestJSON := `{
		"id": "custom-agent",
		"name": "Custom Agent",
		"version": "1.0.0",
		"distribution": {
			"binary": {
				"all": {
					"cmd": "echo",
					"args": ["hello-acp"]
				}
			}
		}
	}`
	filePath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(filePath, []byte(manifestJSON), 0644); err != nil {
		t.Fatal(err)
	}

	cmd, args, _, err := ResolveACPAgent(filePath)
	if err != nil {
		t.Fatalf("ResolveACPAgent on manifest file failed: %v", err)
	}
	if filepath.Base(cmd) != "echo" {
		t.Errorf("cmd = %q, want echo", cmd)
	}
	if len(args) != 1 || args[0] != "hello-acp" {
		t.Errorf("args = %v, want [hello-acp]", args)
	}
}

func TestACPRegistry_ResolveACPAgent_Errors(t *testing.T) {
	if _, _, _, err := ResolveACPAgent(""); err == nil {
		t.Error("expected error on empty agent")
	}

	if _, _, _, err := ResolveACPAgent("non-existent-agent-12345"); err == nil {
		t.Error("expected error on non-existent agent")
	}
}

func TestSpawnACPRemote_EarlyExitCloses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Spawn a command that exits immediately
	_, err := SpawnACPRemote(ctx, "echo", []string{"invalid acp stream"}, t.TempDir(), "1")
	if err == nil {
		t.Fatal("expected SpawnACPRemote to fail on echo non-jsonrpc output")
	}
}
