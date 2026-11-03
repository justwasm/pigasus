package convstore_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
)

func storeTestFixture(t *testing.T, ids ...string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "transcript.db"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	for _, id := range ids {
		if err := os.WriteFile(filepath.Join(dir, "conversations", id+".db"), fixture, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func writeTestCache(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cache", name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	fixtureID = "cafe0000-0000-4000-8000-000000000001"
	otherID   = "cafe0000-0000-4000-8000-000000000009"
)

func TestResolveSelection_FlagExisting(t *testing.T) {
	storeTestFixture(t, fixtureID)
	var warn strings.Builder
	sel := convstore.ResolveSelection(fixtureID, false, "/tmp", &warn)
	if sel.ID != fixtureID || sel.Source != "flag" {
		t.Fatalf("selection = %+v", sel)
	}
	if warn.String() != "" {
		t.Fatalf("unexpected warning: %q", warn.String())
	}
	if sel.Store == nil || sel.Store.ID != fixtureID {
		t.Errorf("store not loaded: %+v", sel.Store)
	}
	if sel.PriorUserTurns != 2 {
		t.Errorf("PriorUserTurns = %d, want 2", sel.PriorUserTurns)
	}
}

func TestResolveSelection_FlagMissing(t *testing.T) {
	storeTestFixture(t, fixtureID)
	var warn strings.Builder
	sel := convstore.ResolveSelection("cafe0000-dead-beef-0000-000000000000", false, "/tmp", &warn)
	if sel.ID != "" || sel.Store != nil {
		t.Fatalf("expected empty selection, got %+v", sel)
	}
	if !strings.Contains(warn.String(), "warning: conversation") {
		t.Errorf("missing warning: %q", warn.String())
	}
}

func TestResolveSelection_ContinueFromCache(t *testing.T) {
	dir := storeTestFixture(t, fixtureID)
	writeTestCache(t, dir, "last_conversations.json", `{"/workspace/demo": "`+fixtureID+`"}`)
	sel := convstore.ResolveSelection("", true, "/workspace/demo", io.Discard)
	if sel.ID != fixtureID || sel.Source != "continue" {
		t.Fatalf("selection = %+v, want %s via continue", sel, fixtureID)
	}
}

func TestResolveSelection_ContinueAncestorFallback(t *testing.T) {
	dir := storeTestFixture(t, fixtureID)
	writeTestCache(t, dir, "last_conversations.json", `{"/workspace": "`+fixtureID+`"}`)
	sel := convstore.ResolveSelection("", true, "/workspace/sub/dir", io.Discard)
	if sel.ID != fixtureID || sel.Source != "continue" {
		t.Fatalf("selection = %+v, want %s via continue", sel, fixtureID)
	}
}

func TestResolveSelection_IncompleteUUIDWarnsAndHints(t *testing.T) {
	storeTestFixture(t, fixtureID)
	var warn strings.Builder
	// fixtureID is "f65cfba7-0331-4091-a128-bd5a48529248".
	// Truncate by 1 character: 35 chars.
	truncated := fixtureID[:len(fixtureID)-1]
	sel := convstore.ResolveSelection(truncated, false, "/tmp", &warn)
	if sel.ID != "" || sel.Store != nil {
		t.Fatalf("expected empty selection, got %+v", sel)
	}

	got := warn.String()
	if !strings.Contains(got, fmt.Sprintf("warning: conversation %q not found", truncated)) {
		t.Errorf("expected not-found warning, got: %s", got)
	}
	if !strings.Contains(got, "is not a complete UUID (length is 35, expected 36") {
		t.Errorf("expected incomplete UUID warning, got: %s", got)
	}
	if !strings.Contains(got, fmt.Sprintf("hint: did you mean --conversation=%s ?", fixtureID)) {
		t.Errorf("expected did-you-mean hint with %s, got: %s", fixtureID, got)
	}
}
