package agentspanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
)

func TestAgentsPanel_SubagentDetailView(t *testing.T) {
	sa := &subagent.Subagent{
		ID:    "sub-1",
		Name:  "worker-1",
		Title: "Analyze Repository",
		State: "running",
	}

	p := New(Seams{
		Subagents: func() []*subagent.Subagent {
			return []*subagent.Subagent{sa}
		},
		HasSession: func() bool { return false },
	}, nil)

	// Available agents: index 0 (default).
	// Subagent row: index 1.
	p.SetCursor(1)

	// Press Enter to open Subagent Detail View
	handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled || !p.DetailMode() {
		t.Fatalf("expected Enter to open DetailMode")
	}

	lines := strings.Join(p.Lines(), "\n")
	if !strings.Contains(lines, "Subagent Detail: worker-1") {
		t.Errorf("expected detail title, got:\n%s", lines)
	}
	if !strings.Contains(lines, "Task Title:") || !strings.Contains(lines, "Analyze Repository") {
		t.Errorf("expected task title in detail view, got:\n%s", lines)
	}

	// Press Esc to exit detail mode and return to list
	handledEsc, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handledEsc || p.DetailMode() {
		t.Fatalf("expected Esc to close DetailMode")
	}
}

func TestAgentsPanel_SwitchConversationNotice(t *testing.T) {
	// 1. Without active session -> "create a new conversation"
	pNoSession := New(Seams{
		HasSession: func() bool { return false },
	}, nil)
	pNoSession.SetCursor(0)
	pNoSession.HandleKey(tea.Key{Code: tea.KeyEnter})
	pNoSession.prepared = true
	linesNoSession := strings.Join(pNoSession.Lines(), "\n")
	if !strings.Contains(linesNoSession, "[ Switch will create a new conversation on exit ]") {
		t.Errorf("expected new conversation notice, got:\n%s", linesNoSession)
	}

	// 2. With active session -> "fork the current conversation"
	pWithSession := New(Seams{
		HasSession: func() bool { return true },
	}, nil)
	pWithSession.SetCursor(0)
	pWithSession.HandleKey(tea.Key{Code: tea.KeyEnter})
	pWithSession.prepared = true
	linesWithSession := strings.Join(pWithSession.Lines(), "\n")
	if !strings.Contains(linesWithSession, "[ Switch will fork the current conversation on exit ]") {
		t.Errorf("expected fork conversation notice, got:\n%s", linesWithSession)
	}
}
