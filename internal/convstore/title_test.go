package convstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// errTestStopScan is the callback sentinel readTitleFromIndex returns
// once it has found the row it wants; db.Scan treats any non-nil error
// as "stop walking".
var errTestStopScan = errors.New("test: stop scan")

// TestWriteSummariesTitleUpdatesRow seeds an index row, calls
// WriteSummariesTitle, and re-reads the row to confirm the title
// change landed. The cycle-resume code path runs this immediately
// after SaveConversation so the /resume picker shows the
// auto-generated title (rather than the first-message preview that
// SaveConversation derives by default).
func TestWriteSummariesTitleUpdatesRow(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "a1a1a1a1-b2b2-4000-8000-000000000501"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeConversationSummaryRow(
		filepath.Join(StoreDir(), "conversation_summaries.db"), id,
		"file://"+filepath.Clean(ws), now); err != nil {
		t.Fatal(err)
	}
	if err := WriteSummariesTitle(id, "auto-title-1"); err != nil {
		t.Fatalf("WriteSummariesTitle: %v", err)
	}
	got := readTitleFromIndex(t, id)
	if got != "auto-title-1" {
		t.Errorf("title = %q, want %q", got, "auto-title-1")
	}
	// Empty title is a no-op: the row keeps whatever title it had.
	if err := WriteSummariesTitle(id, ""); err != nil {
		t.Errorf("empty title should not error: %v", err)
	}
	if got := readTitleFromIndex(t, id); got != "auto-title-1" {
		t.Errorf("empty title should leave row untouched, got %q", got)
	}
}

// TestWriteSummariesTitleMissingRowIsNoop runs the helper against a
// fresh index where no row exists. The function is documented to be
// a no-op on missing rows so the cycle-resume caller does not have
// to special-case "first ever save".
func TestWriteSummariesTitleMissingRowIsNoop(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)
	if err := os.MkdirAll(StoreDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteSummariesTitle("ghost-id-no-such-row", "should-not-create"); err != nil {
		t.Errorf("missing-row WriteSummariesTitle should be a no-op, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(StoreDir(), "conversation_summaries.db")); err == nil {
		t.Errorf("WriteSummariesTitle should not have created the db")
	}
}

// TestWriteCacheTitleUpdatesEntry mirrors the index test for the
// metadata cache. The cache lives at
// cache/conversation_metadata.json; only the matching id is updated.
func TestWriteCacheTitleUpdatesEntry(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)
	if err := os.MkdirAll(filepath.Join(StoreDir(), "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := "b2b2b2b2-c3c3-4000-8000-000000000601"
	cache := map[string]any{
		"conversations": map[string]any{
			id: map[string]any{
				"summary": map[string]any{
					"Title":   "preview-from-save",
					"Preview": "preview-from-save",
				},
				"last_modified_time": time.Now().UTC().Format(time.RFC3339Nano),
			},
		},
	}
	data, _ := json.MarshalIndent(cache, "", "  ")
	if err := os.WriteFile(filepath.Join(StoreDir(), "cache", "conversation_metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteCacheTitle(id, "auto-title-2"); err != nil {
		t.Fatalf("WriteCacheTitle: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(StoreDir(), "cache", "conversation_metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"Title": "auto-title-2"`) {
		t.Errorf("cache title not updated: %s", out)
	}
}

// TestWriteCacheTitleMissingEntryIsNoop runs the helper against a
// cache file that does not contain the id. Documented no-op; a missing
// id should not produce an error or create the entry.
func TestWriteCacheTitleMissingEntryIsNoop(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)
	if err := os.MkdirAll(filepath.Join(StoreDir(), "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteCacheTitle("ghost-id", "nope"); err != nil {
		t.Errorf("missing-entry WriteCacheTitle should be a no-op, got %v", err)
	}
	// No file should have been written.
	if _, err := os.Stat(filepath.Join(StoreDir(), "cache", "conversation_metadata.json")); err == nil {
		t.Errorf("WriteCacheTitle should not have created the cache file")
	}
}

// readTitleFromIndex walks the index db with the existing convstore
// Scan/Row machinery so the test exercises the same read path the
// picker does, rather than re-implementing it.
func readTitleFromIndex(t *testing.T, id string) string {
	t.Helper()
	db, err := Open(filepath.Join(StoreDir(), "conversation_summaries.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got string
	_ = db.Scan("conversation_summaries", func(r *Row) error {
		if r.Text("conversation_id") != id {
			return nil
		}
		got = r.Text("title")
		return errTestStopScan
	})
	return got
}
