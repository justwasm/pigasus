package voice

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLiveSessionStore_SaveLoadListFind(t *testing.T) {
	tempDir := t.TempDir()
	SetLiveSessionDirOverride(tempDir)
	defer SetLiveSessionDirOverride("")

	// 1. Initially empty
	list, err := ListLiveSessions()
	if err != nil {
		t.Fatalf("ListLiveSessions failed on empty dir: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(list))
	}

	// 2. Save Session 1
	s1 := &LiveSessionData{
		ID:        "20261117-100000-aaaa",
		Title:     "Session 1: Initial exploration",
		Workspace: "/workspace/proj-a",
		Model:     "gemini-2.5-flash",
		Voice:     "Aoede",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		Turns: []LiveTurnRecord{
			{Role: "User", Content: "Hello assistant"},
			{Role: "Agent", Content: "Hi, how can I help you today?"},
		},
		Logs: []string{
			"=== LiveChat ===",
			"User: Hello assistant",
			"Agent: Hi, how can I help you today?",
		},
	}
	if err := SaveLiveSession(s1); err != nil {
		t.Fatalf("SaveLiveSession s1 failed: %v", err)
	}

	// Wait 10ms to ensure distinct timestamp for s2
	time.Sleep(10 * time.Millisecond)

	// 3. Save Session 2 (More recent, different workspace)
	s2 := &LiveSessionData{
		ID:        "20261117-120000-bbbb",
		Title:     "Session 2: Debugging bug",
		Workspace: "/workspace/proj-b",
		Model:     "gemini-2.5-flash",
		Voice:     "Puck",
		CreatedAt: time.Now().Add(-1 * time.Hour),
		Turns: []LiveTurnRecord{
			{Role: "User", Content: "Run go test please"},
			{Role: "Agent", Content: "Tests passed successfully."},
		},
		Logs: []string{
			"=== LiveChat ===",
			"User: Run go test please",
			"Agent: Tests passed successfully.",
		},
	}
	if err := SaveLiveSession(s2); err != nil {
		t.Fatalf("SaveLiveSession s2 failed: %v", err)
	}

	// 4. ListLiveSessions (Ordered by UpdatedAt descending)
	list, err = ListLiveSessions()
	if err != nil {
		t.Fatalf("ListLiveSessions failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(list))
	}
	if list[0].ID != s2.ID {
		t.Errorf("expected newest session first (%s), got %s", s2.ID, list[0].ID)
	}
	if list[1].ID != s1.ID {
		t.Errorf("expected older session second (%s), got %s", s1.ID, list[1].ID)
	}

	// 5. Workspace Filtering
	projAList, err := ListLiveSessions("/workspace/proj-a")
	if err != nil {
		t.Fatalf("ListLiveSessions with filter failed: %v", err)
	}
	if len(projAList) != 1 || projAList[0].ID != s1.ID {
		t.Fatalf("expected only s1 for proj-a filter, got %+v", projAList)
	}

	// 6. FindLiveSession
	// 6a. Exact ID
	f1, err := FindLiveSession(s1.ID)
	if err != nil {
		t.Fatalf("FindLiveSession exact failed: %v", err)
	}
	if f1.Title != s1.Title || len(f1.Turns) != 2 {
		t.Errorf("FindLiveSession data mismatch: %+v", f1)
	}

	// 6b. Index "1" (should be s2 because it's newest)
	fLatest, err := FindLiveSession("1")
	if err != nil {
		t.Fatalf("FindLiveSession '1' failed: %v", err)
	}
	if fLatest.ID != s2.ID {
		t.Errorf("expected session #1 to be %s, got %s", s2.ID, fLatest.ID)
	}

	// 6c. Keyword "latest"
	fLatestWord, err := FindLiveSession("latest")
	if err != nil {
		t.Fatalf("FindLiveSession 'latest' failed: %v", err)
	}
	if fLatestWord.ID != s2.ID {
		t.Errorf("expected 'latest' to be %s, got %s", s2.ID, fLatestWord.ID)
	}

	// 6d. Prefix match "20261117-10"
	fPrefix, err := FindLiveSession("20261117-10")
	if err != nil {
		t.Fatalf("FindLiveSession prefix failed: %v", err)
	}
	if fPrefix.ID != s1.ID {
		t.Errorf("expected prefix to match %s, got %s", s1.ID, fPrefix.ID)
	}

	// 6e. Ambiguous prefix
	_, err = FindLiveSession("20261117")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguous prefix error, got %v", err)
	}
}

func TestNativeLiveSession_SetConversationHistory(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	sess := NewNativeLiveSession(mockBackend, nil)

	turns := []LiveTurnRecord{
		{Role: "User", Content: "Hello from earlier"},
		{Role: "Agent", Content: "Welcome back!"},
	}

	sess.SetConversationHistory(turns)
	got := sess.ConversationHistory()
	if len(got) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(got))
	}
	if got[0].Content != "Hello from earlier" {
		t.Errorf("turn 0 content = %q, want 'Hello from earlier'", got[0].Content)
	}
	if got[1].Content != "Welcome back!" {
		t.Errorf("turn 1 content = %q, want 'Welcome back!'", got[1].Content)
	}
}

func TestLiveSessionStore_ConcurrentSaveSameID(t *testing.T) {
	tempDir := t.TempDir()
	SetLiveSessionDirOverride(tempDir)
	defer SetLiveSessionDirOverride("")

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			err := SaveLiveSession(&LiveSessionData{
				ID:    "concurrent-session-1",
				Title: fmt.Sprintf("Concurrent Save %d", idx),
				Model: "models/gemini-3.8-live",
				Voice: "Fola",
				Turns: []LiveTurnRecord{
					{Role: "User", Content: fmt.Sprintf("Turn %d", idx)},
				},
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent SaveLiveSession failed: %v", err)
	}

	loaded, err := LoadLiveSession("concurrent-session-1")
	if err != nil {
		t.Fatalf("LoadLiveSession after concurrent saves failed: %v", err)
	}
	if loaded.ID != "concurrent-session-1" {
		t.Errorf("loaded ID = %q, want concurrent-session-1", loaded.ID)
	}
}
