package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The TUI passed no SystemPrompt before cycle agy-system-prompt, so the model
// got the conversation with no instructions. This checks the run-mode helper
// that now supplies the reference's prompt, skills included.
func TestRunModeSystemPromptIsTheReferencePrompt(t *testing.T) {
	root := t.TempDir()
	agentHome := filepath.Join(root, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(agentHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_HOME", agentHome)

	pluginDir := filepath.Join(root, ".gemini", "config", "plugins", "demo-plugin")
	skillDir := filepath.Join(pluginDir, "skills", "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo-plugin"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: demo-skill\ndescription: A demo skill for the TUI prompt test.\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := filepath.Join(root, "ws")
	m := &runMode{cwd: ws, conversationID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}
	got := m.systemPrompt()

	for _, want := range []string{
		"<identity>\nYou are Antigravity, a powerful agentic AI coding assistant",
		"Command Working Directory: " + ws,
		"Conversation ID: aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		"- demo-skill (" + filepath.Join(skillDir, "SKILL.md") + "): A demo skill for the TUI prompt test.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("TUI system prompt is missing %q", want)
		}
	}
	if strings.Contains(got, "operating inside pig") {
		t.Fatal("TUI system prompt is PiG's default, not the reference's")
	}
}

// A --agent markdown agent swaps in the reference's six-section prompt rather
// than editing the default ten-section one. Captured from agy 1.4.3.
func TestRunModeSystemPrompt_CustomAgent(t *testing.T) {
	root := t.TempDir()
	agentHome := filepath.Join(root, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(agentHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_HOME", agentHome)
	ws := filepath.Join(root, "ws")
	m := &runMode{cwd: ws, conversationID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", agentBody: "You are foo."}
	got := m.systemPrompt()

	if !strings.HasPrefix(got, "<System Prompt>\nYou are foo.\n</System Prompt>\n<user_information>") {
		t.Fatalf("custom agent prompt prefix = %.160q", got)
	}
	for _, absent := range []string{"<identity>", "<subagents>", "<slash_commands>", "<conversation_transcript>"} {
		if strings.Contains(got, absent) {
			t.Errorf("custom agent prompt contains %s", absent)
		}
	}
	if strings.HasSuffix(got, "\n") {
		t.Error("custom agent prompt has a trailing newline")
	}
}
