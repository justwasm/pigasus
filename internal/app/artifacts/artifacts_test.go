package artifacts

import (
	"os"
	"path/filepath"
	"testing"
)

// TestList_MatchesTheReferenceEntry pins the checkpoint's artifact entry:
// the stem names the artifact, the path is a file:// URI, and the last-edit
// time drops the sidecar's sub-second precision.
func TestList_MatchesTheReferenceEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	conv := "db19bb83-c308-4d59-875d-e44089ff56f5"
	dir := filepath.Join(home, "brain", conv)
	if err := os.MkdirAll(filepath.Join(dir, "scratch"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("mock_remote_agent_plan.md", "body")
	write("mock_remote_agent_plan.md.metadata.json", `{
  "summary":  "plan",
  "updatedAt":  "2026-11-07T13:55:31.228605Z",
  "userFacing":  true
}`)
	write("full_agent_bridge_plan.md", "body")
	write("full_agent_bridge_plan.md.metadata.json", `{"updatedAt":"2026-11-07T16:01:33.305705Z","userFacing":true}`)
	// Not artifacts: a hidden file, a file with no sidecar, a sidecar-only
	// entry, and one the artifact layer marked not user-facing.
	write(".secret.md", "x")
	write("notes.md", "x")
	write("userFacing_false.md", "x")
	write("userFacing_false.md.metadata.json", `{"updatedAt":"2026-11-07T16:01:33Z","userFacing":false}`)

	got := List(conv)
	want := []struct {
		name, path, edited string
	}{
		{
			name:   "full_agent_bridge_plan",
			path:   "file://" + filepath.Join(dir, "full_agent_bridge_plan.md"),
			edited: "2026-11-07T16:01:33Z",
		},
		{
			name:   "mock_remote_agent_plan",
			path:   "file://" + filepath.Join(dir, "mock_remote_agent_plan.md"),
			edited: "2026-11-07T13:55:31Z",
		},
	}
	if len(got) != len(want) {
		t.Fatalf("List = %+v, want %d entries", got, len(want))
	}
	for i := range want {
		if got[i].Name != want[i].name || got[i].Path != want[i].path || got[i].LastEdited != want[i].edited {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestList_MissingConversationIsEmpty keeps a checkpoint from failing when the
// conversation has no directory yet.
func TestList_MissingConversationIsEmpty(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	if got := List("missing"); got != nil {
		t.Fatalf("List = %+v, want nil", got)
	}
	if got := List(""); got != nil {
		t.Fatalf("List(\"\") = %+v, want nil", got)
	}
}
