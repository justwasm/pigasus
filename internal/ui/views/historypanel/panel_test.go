package historypanel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func sampleEntries() []Entry {
	now := time.Now().UnixMilli()
	return []Entry{
		{Text: "first prompt", Workspace: "/ws/a", Timestamp: now - 3600000},
		{Text: "second prompt /help", Workspace: "/ws/b", Timestamp: now - 1800000},
		{Text: "third prompt multiline\nline2\nline3", Workspace: "/ws/a", Timestamp: now - 60000},
		{Text: "git status", Workspace: "/ws/a", Timestamp: now - 1000},
	}
}

func TestPanelEmptyState(t *testing.T) {
	p := New(Seams{
		Entries: func() []Entry { return nil },
	})
	lines := p.Lines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Prompt History") {
		t.Errorf("expected title, got:\n%s", joined)
	}
	if !strings.Contains(joined, "No prompt history found") {
		t.Errorf("expected empty message, got:\n%s", joined)
	}
}

func TestPanelFilteredEntriesAndScope(t *testing.T) {
	entries := sampleEntries()
	var picked string
	var closed bool

	p := New(Seams{
		Entries:          func() []Entry { return entries },
		CurrentWorkspace: func() string { return "/ws/a" },
		Height:           func() int { return 30 },
		Pick: func(text string) tea.Cmd {
			picked = text
			return nil
		},
		Close: func() tea.Cmd {
			closed = true
			return nil
		},
	})

	// Default scope is ScopeWorkspace: /ws/a has 3 entries ("git status", "third...", "first...")
	if got := p.RowCount(); got != 3 {
		t.Fatalf("expected 3 entries in /ws/a, got %d", got)
	}

	// Newest is first ("git status")
	cur, ok := p.currentEntry()
	if !ok || cur.Text != "git status" {
		t.Errorf("currentEntry = %+v, want 'git status'", cur)
	}

	// Navigate Down
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	if p.Cursor() != 1 {
		t.Errorf("cursor = %d, want 1", p.Cursor())
	}
	cur, _ = p.currentEntry()
	if !strings.HasPrefix(cur.Text, "third prompt") {
		t.Errorf("cursor 1 = %q, want 'third prompt...'", cur.Text)
	}

	// Test Pick with Enter
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !strings.HasPrefix(picked, "third prompt") {
		t.Errorf("picked = %q, want 'third prompt...'", picked)
	}

	// Toggle Scope with Tab -> ScopeAll: should have 4 entries
	p.HandleKey(tea.Key{Code: tea.KeyTab})
	if p.Scope() != ScopeAll {
		t.Errorf("expected ScopeAll after Tab, got %v", p.Scope())
	}
	if got := p.RowCount(); got != 4 {
		t.Errorf("expected 4 entries in ScopeAll, got %d", got)
	}

	// Search filter
	p.HandleKey(tea.Key{Text: "help"})
	if p.Query() != "help" {
		t.Errorf("query = %q, want 'help'", p.Query())
	}
	if got := p.RowCount(); got != 1 {
		t.Errorf("expected 1 match for 'help', got %d", got)
	}
	cur, _ = p.currentEntry()
	if cur.Text != "second prompt /help" {
		t.Errorf("expected 'second prompt /help', got %q", cur.Text)
	}

	// Backspace in search
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	if p.Query() != "hel" {
		t.Errorf("query after backspace = %q, want 'hel'", p.Query())
	}

	// Esc clears query first
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.Query() != "" {
		t.Errorf("query after esc = %q, want empty", p.Query())
	}

	// Esc again closes
	closed = false
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !closed {
		t.Errorf("expected closed = true after second esc")
	}
}

func TestPanelRenderLines(t *testing.T) {
	entries := sampleEntries()
	p := New(Seams{
		Entries:          func() []Entry { return entries },
		CurrentWorkspace: func() string { return "/ws/a" },
		Height:           func() int { return 30 },
	})

	lines := p.Lines()
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "Prompt History") {
		t.Errorf("missing title")
	}
	if !strings.Contains(joined, "Scope: ") {
		t.Errorf("missing scope line")
	}
	if !strings.Contains(joined, "Search: ") {
		t.Errorf("missing search line")
	}
	if !strings.Contains(joined, "Preview:") {
		t.Errorf("missing preview section")
	}
	if !strings.Contains(joined, "Navigate") || !strings.Contains(joined, "Select") {
		t.Errorf("missing legend hint")
	}
}
