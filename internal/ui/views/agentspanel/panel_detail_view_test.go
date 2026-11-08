package agentspanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
)

func TestAgentsPanel_SubagentDetailView(t *testing.T) {
	sa := &subagent.Subagent{
		ID:     "sub-1",
		Name:   "worker-1",
		Title:  "Analyze Repository",
		State:  "running",
		Model:  "Gemini 3.8 Flash (High)",
		Effort: "high",
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
	// AGY 1.5.3: the detail view carries the full model and effort.
	if !strings.Contains(lines, "Model: Gemini 3.8 Flash (High) (effort: high)") {
		t.Errorf("expected model and effort in detail view, got:\n%s", lines)
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

func TestAgentsPanel_SubagentDetailView_LineWrapping(t *testing.T) {
	longOutput := "\tfunc SomeLongMethodWithTabIndent() {\n\t\tfmt.Println(\"a very long line that easily exceeds thirty columns by a lot\")\n\t}"
	sa := &subagent.Subagent{
		ID:     "sub-wrap",
		Name:   "worker-wrap",
		Title:  "Task with very long title that also exceeds thirty columns",
		State:  "running",
		Result: longOutput,
	}

	width := 35
	p := New(Seams{
		Subagents: func() []*subagent.Subagent {
			return []*subagent.Subagent{sa}
		},
		Size: func() (int, int) {
			return width, 24
		},
	}, nil)

	p.SetCursor(1)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.DetailMode() {
		t.Fatal("expected DetailMode")
	}

	lines := p.Lines()
	// Exclude the bottom blank row and keyboard footer hint
	contentLines := lines[:len(lines)-2]
	for i, l := range contentLines {
		plain := ansi.Strip(l)
		if n := len([]rune(plain)); n > width {
			t.Errorf("line %d exceeds width %d (len %d): %q", i, width, n, plain)
		}
		if strings.Contains(l, "\t") {
			t.Errorf("line %d contains unexpanded tab: %q", i, l)
		}
	}
}

// TestAgentsPanel_SubagentRowsShowModelAndOneTip pins the AGY 1.5.3
// list changes: the model sits next to the subagent's name and the
// panel prints one tip line instead of a per-row "[view details]".
func TestAgentsPanel_SubagentRowsShowModelAndOneTip(t *testing.T) {
	sa := &subagent.Subagent{
		ID:    "sub-1",
		Name:  "worker-1",
		State: "running",
		Model: "Gemini 3.8 Flash (High)",
	}
	p := New(Seams{
		Size:      func() (int, int) { return 120, 40 },
		Subagents: func() []*subagent.Subagent { return []*subagent.Subagent{sa} },
	}, nil)
	plain := ansi.Strip(strings.Join(p.Lines(), "\n"))
	if !strings.Contains(plain, "worker-1 · Gemini 3.8 Flash (High)") {
		t.Errorf("subagent row does not carry the model:\n%s", plain)
	}
	if got := strings.Count(plain, "Tip: Press enter on a subagent to view details."); got != 1 {
		t.Errorf("tip line count = %d, want 1:\n%s", got, plain)
	}
	if strings.Contains(plain, "[view details]") {
		t.Errorf("the panel repeats a per-row [view details]:\n%s", plain)
	}
}
