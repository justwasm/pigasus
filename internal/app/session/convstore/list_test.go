package convstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestProjectFromURIsBasename pins the basename extraction that the
// /resume picker relies on for its right-hand project column. The
// cwd-blank rule is exercised by the parity harness (zzz and x@26 on
// this machine), not here, because it requires the real working
// directory and a conversation started from it.
func TestProjectFromURIsBasename(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		// Plain JSON array of one URI.
		{`["file:///tmp/agycmp"]`, "agycmp"},
		// Bare URI, no array wrapper (the index column shape).
		{`file:///workspace/openagy`, "openagy"},
		// Trailing slash stripped before basename.
		{`["file:///tmp/agycmp/"]`, "agycmp"},
		// No `file://` marker → empty.
		{`["/tmp/agycmp"]`, ""},
		// Empty / malformed → empty.
		{``, ""},
		{`[]`, ""},
	}
	for _, tc := range cases {
		got := projectFromURIs(tc.raw)
		if got != tc.want {
			t.Errorf("projectFromURIs(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestListConversationsIn_KeepsAllBeyond500(t *testing.T) {
	root := t.TempDir()
	convDir := filepath.Join(root, "conversations")
	cacheDir := filepath.Join(root, "cache")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	const total = 505
	convs := make(map[string]cacheConvEntry, total)
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("conv-%04d", i)
		if err := os.WriteFile(filepath.Join(convDir, id+".db"), []byte("sqlite"), 0o644); err != nil {
			t.Fatal(err)
		}
		ts := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		convs[id] = cacheConvEntry{
			Summary: cacheConvSummary{
				Title:     fmt.Sprintf("Conversation %d", i),
				Preview:   fmt.Sprintf("prompt %d", i),
				NumSteps:  2,
				UpdatedAt: ts,
			},
			LastModifiedTime: ts,
		}
	}
	raw, err := json.Marshal(map[string]any{"conversations": convs})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "conversation_metadata.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ListConversationsIn(root)
	if err != nil {
		t.Fatalf("ListConversationsIn: %v", err)
	}
	if len(got) != total {
		t.Fatalf("ListConversationsIn returned %d conversations, want all %d (no 500-conversation pruning)", len(got), total)
	}
	entries, err := os.ReadDir(convDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != total {
		t.Fatalf("conversations dir has %d files, want %d", len(entries), total)
	}
}
