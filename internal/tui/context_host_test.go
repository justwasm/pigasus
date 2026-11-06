// context_host_test.go — the host's side of the /context usage panel.
//
// The panel itself is internal/ui/views/contextpanel: it owns the rows
// and the layout. These tests pin the wiring — the facts the host
// gathers for the categories the reference's documentation names
// (official-docs/cli/commands/context.md:32-42).

package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/contextpanel"
)

// TestContextStateRulesLeaveTheSystemPrompt — the rules are reported on
// a row of their own, so they leave the system prompt's count.
//
// The rules are the workspace's own rule files; the prompt carries them
// in its <user_rules> section, and the base count the host measures
// includes them.
func TestContextStateRulesLeaveTheSystemPrompt(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# rule\nalways be brief.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.session.cwd = dir
	m.engine.session = append(m.engine.session, backend.Message{Role: "user", Content: "hello"})

	st := m.contextState()
	if st.Rules <= 0 {
		t.Fatalf("rules = %d, want the workspace's own rule file", st.Rules)
	}
	base, _, _ := m.promptSectionTokens()
	if want := base - st.Rules; st.SystemPrompt != want {
		t.Errorf("system prompt = %d, want %d (the base without the rules)", st.SystemPrompt, want)
	}
}

// TestContextStateCheckpointBufferIsOutsideTheUsage — the checkpoint's
// tokens stand in for turns that have already been compacted: they are
// reported on a row of their own, outside the grid, and they leave the
// user row.
func TestContextStateCheckpointBufferIsOutsideTheUsage(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	block := compaction.ResumingBlock([]string{"do the thing"}, "<summary>done</summary>")
	m.engine.session = append(m.engine.session, backend.Message{Role: "user", Content: block})

	st := m.contextState()
	if st.CheckpointBuffer <= 0 {
		t.Fatalf("checkpoint buffer = %d, want the session's own resume block", st.CheckpointBuffer)
	}
	raw := contextpanel.UserMessageTokens(m.engine.session)
	if want := raw - st.CheckpointBuffer; st.User != want {
		t.Errorf("user = %d, want %d (the checkpoint's tokens leave the user row)", st.User, want)
	}
	if !strings.Contains(strings.Join(contextpanel.Lines(st), "\n"), "Checkpoint buffer") {
		t.Error("the panel rendered no checkpoint row")
	}
}

// TestContextStateSystemMessagesAreTheirOwnCategory — a
// system-generated message is reported on the "System messages" row and
// is not part of the user row.
func TestContextStateSystemMessagesAreTheirOwnCategory(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.engine.session = append(m.engine.session,
		backend.Message{Role: "user", Content: "hello"},
		backend.Message{Role: "system", Content: "a background task finished"},
	)

	st := m.contextState()
	if st.SystemMessages <= 0 {
		t.Fatalf("system messages = %d, want the session's own system message", st.SystemMessages)
	}
	if want := contextpanel.UserMessageTokens(m.engine.session); st.User != want {
		t.Errorf("user = %d, want %d (the system message is not a user message)", st.User, want)
	}
	if !strings.Contains(strings.Join(contextpanel.Lines(st), "\n"), "System messages:") {
		t.Error("the panel rendered no system-messages row")
	}
}
