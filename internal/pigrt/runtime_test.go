package pigrt_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/pigrt"
)

// fauxModel builds a model whose Provider is PiG's recorded test provider and
// whose ProviderID is "test-faux". The clone's own provider (gateway) is exercised
// elsewhere; here the point is only that a Session built by New accepts a
// caller-supplied ai.Provider, which is the seam the gateway transport rides in on.
//
// "test-faux" is also the one ProviderID the coding runtime does not demand
// credentials for (coding.modelRuntimeRequiresAuth), so a test host without an
// agy login can still open a session.
func fauxModel() *ai.Model {
	return &ai.Model{
		Type:         ai.ModelTypeChat,
		ID:           "faux-1",
		DisplayName:  "faux-1",
		Provider:     &ai.TestFauxProvider{},
		ProviderMeta: ai.ProviderMetadata{ProviderID: "test-faux", API: "test-faux"},
		Input:        []string{"text"},
	}
}

func TestRuntimeRunsFirstTurn(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt, err := pigrt.New(ctx, pigrt.Options{
		CWD:           dir,
		AgentDir:      filepath.Join(dir, "agent"),
		Model:         fauxModel(),
		SystemPrompt:  "You are a test.",
		NoSession:     true,
		MCPConfigPath: filepath.Join(dir, "no-such-mcp.json"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	var streamed strings.Builder
	msgs, err := rt.Run(ctx, "What is 20+22?", func(delta string) {
		streamed.WriteString(delta)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := streamed.String(); got != "42" {
		t.Fatalf("streamed text = %q, want %q", got, "42")
	}
	if len(msgs) == 0 {
		t.Fatal("Run returned no messages")
	}
}

func setupDemoSkill(t *testing.T, dir string) (string, string, string) {
	t.Helper()
	agentHome := filepath.Join(dir, ".gemini", "antigravity-cli")
	pluginDir := filepath.Join(dir, ".gemini", "config", "plugins", "demo-plugin")
	skillDir := filepath.Join(pluginDir, "skills", "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo-plugin"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: demo-skill\ndescription: A demo skill for the loader test.\n---\n\nDo the demo thing.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(dir, ".gemini", "skills")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, ".gemini", "config", "plugins", "demo-plugin", "skills")
	return agentHome, want, shared
}

func TestRuntimeDiscoversAgySkills(t *testing.T) {
	dir := t.TempDir()
	agentHome, want, shared := setupDemoSkill(t, dir)

	paths := pigrt.AgySkillPaths(agentHome, dir)
	found, sharedFound := false, false
	for _, p := range paths {
		if p == want {
			found = true
		}
		if p == shared {
			sharedFound = true
		}
	}
	if !found {
		t.Fatalf("AgySkillPaths(%q) = %v, missing %q", agentHome, paths, want)
	}
	if !sharedFound {
		t.Fatalf("AgySkillPaths(%q) = %v, missing the shared library %q", agentHome, paths, shared)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt, err := pigrt.New(ctx, pigrt.Options{
		CWD:           dir,
		AgentDir:      agentHome,
		Model:         fauxModel(),
		NoSession:     true,
		MCPConfigPath: filepath.Join(dir, "none.json"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	names := rt.SkillNames()
	foundSkill := false
	for _, n := range names {
		if n == "demo-skill" {
			foundSkill = true
		}
	}
	if !foundSkill {
		t.Fatalf("SkillNames() = %v, missing demo-skill (diagnostics: %+v)", names, rt.SkillDiagnostics())
	}
}

func TestLoadAgyMCPTranslate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	doc := `{
  "servers": {
    "zeta": {"type": "http", "url": "https://example.invalid/mcp", "enabled": true},
    "alpha": {"type": "stdio", "command": "/bin/echo", "args": ["hi"], "env": {"B": "2", "A": "1"}, "enabled": true},
    "off": {"type": "stdio", "command": "/bin/false", "enabled": false}
  }
}`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := pigrt.LoadAgyMCP(path)
	if len(loaded.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", loaded.Errors)
	}
	if len(loaded.Servers) != 3 {
		t.Fatalf("got %d servers, want 3", len(loaded.Servers))
	}
	// Sorted by name, so the on-disk order does not leak into captures.
	if loaded.Servers[0].Name != "alpha" || loaded.Servers[1].Name != "off" || loaded.Servers[2].Name != "zeta" {
		t.Fatalf("servers not name-sorted: %v %v %v",
			loaded.Servers[0].Name, loaded.Servers[1].Name, loaded.Servers[2].Name)
	}
	alpha := loaded.Servers[0].Config
	if alpha.Command != "/bin/echo" || len(alpha.Args) != 1 || alpha.Args[0] != "hi" {
		t.Fatalf("stdio server not translated: %+v", alpha)
	}
	if alpha.Enabled == nil || !*alpha.Enabled {
		t.Fatalf("enabled flag lost: %+v", alpha.Enabled)
	}
	if off := loaded.Servers[1].Config; off.Enabled == nil || *off.Enabled {
		t.Fatalf("disabled server lost its flag: %+v", off.Enabled)
	}
	zeta := loaded.Servers[2].Config
	if zeta.URL != "https://example.invalid/mcp" || zeta.Type != "http" {
		t.Fatalf("http server not translated: %+v", zeta)
	}
}

func TestLoadAgyMCPMissingFileIsEmpty(t *testing.T) {
	loaded := pigrt.LoadAgyMCP(filepath.Join(t.TempDir(), "absent.json"))
	if len(loaded.Servers) != 0 || len(loaded.Errors) != 0 {
		t.Fatalf("missing file should be an empty, error-free config: %+v", loaded)
	}
}
