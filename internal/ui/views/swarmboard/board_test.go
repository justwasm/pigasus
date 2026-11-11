package swarmboard

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestBoard_OpenCloseToggle(t *testing.T) {
	b := NewBoard(nil)
	if b.Active() {
		t.Fatal("expected newly created board to be inactive")
	}

	b.Open()
	if !b.Active() {
		t.Fatal("expected board to be active after Open")
	}

	b.Close()
	if b.Active() {
		t.Fatal("expected board to be inactive after Close")
	}

	b.Toggle()
	if !b.Active() {
		t.Fatal("expected board to be active after Toggle")
	}
}

func TestBoard_ApplySnapshotAndRender(t *testing.T) {
	b := NewBoard(nil)
	b.SetSize(80, 24)
	b.Open()

	snap := &SwarmSnapshot{
		Title: "The Parser Rewrite",
		Members: []Member{
			{Name: "coordinator", State: "working", Model: "gemini-2.5-pro", IsCoordinator: true},
			{Name: "parser", State: "working", Model: "gemini-2.5-flash"},
			{Name: "verifier", State: "waiting", Model: "gemini-2.5-flash"},
		},
		Posts: []Post{
			{
				Seq:       1,
				Author:    "coordinator",
				Body:      "@parser You own src/parser.rs. Make the three failing cases in parser_test.rs pass.",
				CreatedAt: time.Now(),
				Mentions:  []string{"parser"},
			},
			{
				Seq:       2,
				Author:    "parser",
				Body:      "@verifier Diff is ready. Please verify.",
				CreatedAt: time.Now(),
				Mentions:  []string{"verifier"},
			},
		},
		IsDone: false,
	}

	b.ApplySnapshot(snap)

	if b.Title() != "The Parser Rewrite" {
		t.Errorf("Title = %q, want %q", b.Title(), "The Parser Rewrite")
	}
	if b.PostCount() != 2 {
		t.Errorf("PostCount = %d, want 2", b.PostCount())
	}

	v := b.View()
	if !strings.Contains(v, "The Parser Rewrite") {
		t.Error("View does not contain title")
	}
	if !strings.Contains(v, "ROSTER") {
		t.Error("View does not contain ROSTER")
	}
	if !strings.Contains(v, "POSTS") {
		t.Error("View does not contain POSTS")
	}
	if !strings.Contains(v, "MESSAGE") {
		t.Error("View does not contain MESSAGE")
	}
	if !strings.Contains(v, "parser") {
		t.Error("View does not contain parser member")
	}
}

func TestBoard_KeyNavigation(t *testing.T) {
	var openedConv string
	b := NewBoard(func(convID string) {
		openedConv = convID
	})
	b.SetSize(80, 24)
	b.Open()

	snap := &SwarmSnapshot{
		Title: "Test Swarm",
		Members: []Member{
			{Name: "parser", State: "working", ConversationID: "conv-parser"},
			{Name: "verifier", State: "waiting", ConversationID: "conv-verifier"},
		},
		Posts: []Post{
			{Seq: 1, Author: "parser", Body: "Post 1", Mentions: []string{"verifier"}},
			{Seq: 2, Author: "verifier", Body: "Post 2", Mentions: []string{"parser"}},
			{Seq: 3, Author: "parser", Body: "Post 3", Mentions: []string{"coordinator"}},
		},
	}
	b.ApplySnapshot(snap)

	// 1. Tab switches pane
	if b.focusPane != PaneRoster {
		t.Fatalf("expected initial focusPane to be PaneRoster(0), got %d", b.focusPane)
	}
	b.HandleKey(tea.Key{Code: tea.KeyTab})
	if b.focusPane != PanePosts {
		t.Fatalf("expected focusPane to be PanePosts(1), got %d", b.focusPane)
	}
	b.HandleKey(tea.Key{Code: tea.KeyTab})
	if b.focusPane != PaneMessage {
		t.Fatalf("expected focusPane to be PaneMessage(2), got %d", b.focusPane)
	}
	b.HandleKey(tea.Key{Code: tea.KeyTab})
	if b.focusPane != PaneRoster {
		t.Fatalf("expected focusPane to cycle back to PaneRoster(0), got %d", b.focusPane)
	}

	// 2. Zoom ('z')
	b.HandleKey(tea.Key{Code: 'z', Text: "z"})
	if !b.zoomed {
		t.Fatal("expected zoomed=true after 'z'")
	}
	b.HandleKey(tea.Key{Code: 'z', Text: "z"})
	if b.zoomed {
		t.Fatal("expected zoomed=false after second 'z'")
	}

	// 3. Step posts ('[' and ']')
	b.focusPane = PanePosts
	b.postsCursor = 1
	b.HandleKey(tea.Key{Code: '[', Text: "["})
	if b.postsCursor != 0 {
		t.Fatalf("expected postsCursor=0 after '[', got %d", b.postsCursor)
	}
	b.HandleKey(tea.Key{Code: ']', Text: "]"})
	if b.postsCursor != 1 {
		t.Fatalf("expected postsCursor=1 after ']', got %d", b.postsCursor)
	}

	// 4. Filter ('p')
	b.focusPane = PaneRoster
	b.rosterCursor = 0 // "parser"
	b.HandleKey(tea.Key{Code: 'p', Text: "p"})
	if b.filterMember != "parser" {
		t.Fatalf("expected filterMember='parser', got %q", b.filterMember)
	}
	// Toggle filter again to clear
	b.HandleKey(tea.Key{Code: 'p', Text: "p"})
	if b.filterMember != "" {
		t.Fatalf("expected filterMember='', got %q", b.filterMember)
	}

	// 5. Open subagent ('o')
	b.focusPane = PaneRoster
	b.rosterCursor = 1 // "verifier"
	b.HandleKey(tea.Key{Code: 'o', Text: "o"})
	if openedConv != "conv-verifier" {
		t.Fatalf("expected openedConv='conv-verifier', got %q", openedConv)
	}

	// 6. Toggle message ('Enter')
	b.HandleKey(tea.Key{Code: tea.KeyEnter})
	if b.showMessage {
		t.Fatal("expected showMessage=false after Enter")
	}
	b.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !b.showMessage {
		t.Fatal("expected showMessage=true after second Enter")
	}

	// 7. Esc dismisses
	b.HandleKey(tea.Key{Code: tea.KeyEsc})
	if b.Active() {
		t.Fatal("expected board to be closed after Esc")
	}
}
