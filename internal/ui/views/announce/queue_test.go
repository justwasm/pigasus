package announce

// queue_test.go — the announcement queue's own behaviour.
//
// The queue moved here from internal/cliapp (cycle 381, tranche 53):
// the visible cards, the session-hidden id and the persistence failure
// are the queue's state, and the host drives it.
//
// The tests point $ANTIGRAVITY_HOME at a tempdir so the persistent
// side never touches ~/.gemini/antigravity-cli/settings.json on the
// host — the same pattern catalog_test.go uses.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestQueue_LoadSeedsFromCatalog is the first-launch path: a fresh
// home carries no dismissed list, so the queue holds the whole
// catalogue in display order.
func TestQueue_LoadSeedsFromCatalog(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	var q Queue
	q.Load()
	if len(q.Cards) != len(Catalog()) {
		t.Fatalf("queue holds %d cards, want %d", len(q.Cards), len(Catalog()))
	}
	if q.SessionHiddenID != "" {
		t.Errorf("session-hidden id = %q, want \"\"", q.SessionHiddenID)
	}
	if q.SaveError != nil {
		t.Errorf("save error = %v, want nil", q.SaveError)
	}
	head := q.Active()
	if head == nil || head.ID != Catalog()[0].ID {
		t.Fatalf("head = %v, want %q", head, Catalog()[0].ID)
	}
}

// TestQueue_LoadHonoursDismissedList is the restart path: a card
// dismissed in an earlier session is not in the queue again.
func TestQueue_LoadHonoursDismissedList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	if err := SaveDismissed(Catalog()[0].ID); err != nil {
		t.Fatalf("SaveDismissed: %v", err)
	}
	var q Queue
	q.Load()
	if len(q.Cards) != len(Catalog())-1 {
		t.Fatalf("queue holds %d cards, want %d", len(q.Cards), len(Catalog())-1)
	}
	for _, c := range q.Cards {
		if c.ID == Catalog()[0].ID {
			t.Errorf("dismissed card %q is back in the queue", c.ID)
		}
	}
}

// TestQueue_DismissAdvancesAndPersists is the Esc contract: the head
// of the queue advances and the dismissal reaches settings.json.
func TestQueue_DismissAdvancesAndPersists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	var q Queue
	q.Load()
	want := Catalog()[1].ID
	q.Dismiss()
	head := q.Active()
	if head == nil || head.ID != want {
		t.Fatalf("head after dismiss = %v, want %q", head, want)
	}
	if q.SaveError != nil {
		t.Errorf("save error = %v, want nil", q.SaveError)
	}
	body, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatalf("settings.json missing: %v", err)
	}
	if !strings.Contains(string(body), Catalog()[0].ID) {
		t.Errorf("dismissed id not persisted:\n%s", body)
	}
}

// TestQueue_DismissOnEmptyQueueIsNoop pins the boundary: Esc on an
// empty queue must not panic and must not write a file.
func TestQueue_DismissOnEmptyQueueIsNoop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	var q Queue
	q.Dismiss()
	if q.Active() != nil {
		t.Error("empty queue surfaced a card")
	}
	if _, err := os.Stat(filepath.Join(home, "settings.json")); err == nil {
		t.Error("dismissing an empty queue wrote settings.json")
	}
}

// TestQueue_DismissKeepsGoingWhenPersistenceFails: a persistence
// failure must not stop the queue — the head still advances for the
// rest of the session, and the failure is reported to the host.
func TestQueue_DismissKeepsGoingWhenPersistenceFails(t *testing.T) {
	// A regular file where the home's parent should be: the mkdir
	// inside SaveDismissed fails, so the dismissal cannot be written.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(blocker, "home"))
	var q Queue
	q.Cards = VisibleCards(nil)
	q.Dismiss()
	head := q.Active()
	if head == nil || head.ID != Catalog()[1].ID {
		t.Fatalf("head after failed dismiss = %v, want %q", head, Catalog()[1].ID)
	}
	if q.SaveError == nil {
		t.Fatal("save error = nil, want the persistence failure")
	}
	msg := q.SaveErrorMessage()
	if !strings.Contains(msg, "Could not save announcement dismissal") {
		t.Errorf("save error message = %q, want the warning row", msg)
	}
}

// TestQueue_HideForSessionDoesNotPersist is the submit contract: the
// card is hidden for the rest of the session and nothing is written.
func TestQueue_HideForSessionDoesNotPersist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	var q Queue
	q.Load()
	hidden := Catalog()[0].ID
	q.HideForSession()
	if q.SessionHiddenID != hidden {
		t.Errorf("session-hidden id = %q, want %q", q.SessionHiddenID, hidden)
	}
	head := q.Active()
	if head == nil || head.ID != Catalog()[1].ID {
		t.Fatalf("head after hide = %v, want %q", head, Catalog()[1].ID)
	}
	if _, err := os.Stat(filepath.Join(home, "settings.json")); err == nil {
		t.Error("hiding for the session wrote settings.json")
	}
}

// TestQueue_HideForSessionOnEmptyQueueIsNoop pins the boundary.
func TestQueue_HideForSessionOnEmptyQueueIsNoop(t *testing.T) {
	var q Queue
	q.HideForSession()
	if q.SessionHiddenID != "" {
		t.Errorf("session-hidden id = %q, want \"\"", q.SessionHiddenID)
	}
}

// TestQueue_RowIsEmptyWithoutACard pins the empty-queue row.
func TestQueue_RowIsEmptyWithoutACard(t *testing.T) {
	var q Queue
	if got := q.Row(); got != "" {
		t.Errorf("row = %q, want \"\"", got)
	}
}

// TestQueue_SaveErrorMessageIsEmptyWithoutAFailure pins the boundary.
func TestQueue_SaveErrorMessageIsEmptyWithoutAFailure(t *testing.T) {
	var q Queue
	if got := q.SaveErrorMessage(); got != "" {
		t.Errorf("save error message = %q, want \"\"", got)
	}
}
