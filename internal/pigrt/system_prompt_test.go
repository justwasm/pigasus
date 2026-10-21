package pigrt_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/pigrt"
)

// newPromptRuntime opens a session in a temp agent home with one discoverable
// skill, so the prompt's <skills> section is not empty.
func newPromptRuntime(t *testing.T, opts pigrt.Options) *pigrt.Runtime {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	rt, err := pigrt.New(ctx, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return rt
}

func writeDemoSkill(t *testing.T, agentHome string) string {
	t.Helper()
	pluginDir := filepath.Join(filepath.Dir(agentHome), "config", "plugins", "demo-plugin")
	dir := filepath.Join(pluginDir, "skills", "demo-skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo-plugin"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(dir, "SKILL.md")
	body := "---\nname: demo-skill\ndescription: A demo skill for the prompt test.\n---\n\nDo the demo thing.\n"
	if err := os.WriteFile(md, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return md
}

// The reference sends the Antigravity system prompt; before cycle
// agy-system-prompt an unset SystemPrompt meant PiG's own "pig" preamble, a
// different product's instructions. This pins the default.
func TestRuntimeDefaultsToAgySystemPrompt(t *testing.T) {
	dir := t.TempDir()
	agentHome := filepath.Join(dir, ".gemini", "antigravity-cli")
	md := writeDemoSkill(t, agentHome)

	rt := newPromptRuntime(t, pigrt.Options{
		CWD:            dir,
		AgentDir:       agentHome,
		Model:          fauxModel(),
		ConversationID: "11111111-2222-3333-4444-555555555555",
		NoSession:      true,
		MCPConfigPath:  filepath.Join(dir, "no-such-mcp.json"),
	})

	got := rt.SystemPrompt()
	if !strings.HasPrefix(got, "<identity>\nYou are Antigravity, a powerful agentic AI coding assistant") {
		t.Fatalf("prompt does not open with the reference identity:\n%.120s", got)
	}
	for _, want := range []string{
		"</identity>\n<artifacts>",
		"<communication_style>",
		"<slash_commands>",
		"<subagents>",
		"- research: Research subagent",
		"- demo-skill (" + md + "): A demo skill for the prompt test.",
		"<user_information>\nThe USER's OS version is ",
		"Command Working Directory: " + dir,
		"Conversation ID: 11111111-2222-3333-4444-555555555555",
		"Artifact Directory Path: " + filepath.Join(agentHome, "brain", "11111111-2222-3333-4444-555555555555"),
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt is missing %q", want)
		}
	}
	if strings.Contains(got, "operating inside pig") {
		t.Fatal("prompt is PiG's default, not the reference's")
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatal("prompt has a trailing newline; the reference does not")
	}
	if rt.ConversationID() != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("ConversationID() = %q", rt.ConversationID())
	}
}

// PigPrompt is the escape hatch: it restores PiG's own coding prompt for hosts
// that need its tool-guidance sections.
func TestRuntimePigPromptEscapeHatch(t *testing.T) {
	dir := t.TempDir()
	rt := newPromptRuntime(t, pigrt.Options{
		CWD:           dir,
		AgentDir:      filepath.Join(dir, ".gemini", "antigravity-cli"),
		Model:         fauxModel(),
		PigPrompt:     true,
		NoSession:     true,
		MCPConfigPath: filepath.Join(dir, "no-such-mcp.json"),
	})
	got := rt.SystemPrompt()
	if !strings.Contains(got, "expert coding assistant operating inside pig") {
		t.Fatalf("PigPrompt did not restore PiG's preamble:\n%.200s", got)
	}
	if strings.Contains(got, "<identity>") {
		t.Fatal("PigPrompt still produced the reference prompt")
	}
}

// A caller-supplied prompt still wins over the default.
func TestRuntimeExplicitSystemPromptWins(t *testing.T) {
	dir := t.TempDir()
	rt := newPromptRuntime(t, pigrt.Options{
		CWD:           dir,
		AgentDir:      filepath.Join(dir, ".gemini", "antigravity-cli"),
		Model:         fauxModel(),
		SystemPrompt:  "You are a test.",
		NoSession:     true,
		MCPConfigPath: filepath.Join(dir, "no-such-mcp.json"),
	})
	if got := rt.SystemPrompt(); got != "You are a test." {
		t.Fatalf("SystemPrompt() = %q, want %q", got, "You are a test.")
	}
}
