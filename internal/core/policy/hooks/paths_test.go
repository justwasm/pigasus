package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGlobalPath pins the global document's path.
//
// The documentation names it twice: ~/.gemini/config/hooks.json. It is
// the reference's own tree, not the clone's configuration home.
func TestGlobalPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".gemini", "config", "hooks.json")
	if got := GlobalPath(); got != want {
		t.Errorf("GlobalPath() = %q, want %q", got, want)
	}
}

// TestWorkspacePath pins the workspace document's path: the
// documentation names ".agents/hooks.json" at the project root.
func TestWorkspacePath(t *testing.T) {
	want := filepath.Join("/tmp/project", ".agents", "hooks.json")
	if got := WorkspacePath("/tmp/project"); got != want {
		t.Errorf("WorkspacePath() = %q, want %q", got, want)
	}
	if got := WorkspacePath(""); got != "" {
		t.Errorf("WorkspacePath(\"\") = %q, want the empty string", got)
	}
}

// TestLoadReadsBothDocuments pins the loader: the global document first,
// the workspace document second, and a document that is not there is not
// an error.
func TestLoadReadsBothDocuments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	global := filepath.Join(home, ".gemini", "config")
	if err := os.MkdirAll(global, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(global, "hooks.json"), `{"global": {"SessionStart": [{"command": "./g.sh"}]}}`)

	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, ".agents", "hooks.json"),
		`{"local": {"SessionStart": [{"command": "./l.sh"}]}}`)

	reg := Load(workspace)
	if len(reg.Entries) != 2 {
		t.Fatalf("Load read %d entries, want 2: %+v", len(reg.Entries), reg.Entries)
	}
	if reg.Entries[0].Hook != "global" || reg.Entries[1].Hook != "local" {
		t.Errorf("the global document does not come first: %+v", reg.Entries)
	}
	if len(reg.Problems) != 0 {
		t.Errorf("Load reported problems: %v", reg.Problems)
	}
	if len(reg.Sources) != 2 {
		t.Errorf("Load read %d sources, want 2", len(reg.Sources))
	}
}

// TestLoadWithoutAnyDocument pins the loader's answer when there is no
// hooks.json at all: an empty registry and no problem.
func TestLoadWithoutAnyDocument(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reg := Load(t.TempDir())
	if reg.Len() != 0 {
		t.Errorf("Load read %d entries, want 0", reg.Len())
	}
	if len(reg.Problems) != 0 {
		t.Errorf("a missing document is not an error, got %v", reg.Problems)
	}
}

// TestLoadFileReadsOneDocument pins the single-document loader.
func TestLoadFileReadsOneDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.json")
	writeFile(t, path, `{"one": {"Stop": [{"command": "./s.sh"}]}}`)
	reg := LoadFile(path)
	if reg.Len() != 1 {
		t.Fatalf("LoadFile read %d entries, want 1", reg.Len())
	}
	if reg.Entries[0].Event != EventStop {
		t.Errorf("entry = %+v", reg.Entries[0])
	}
	// A file that is not there is not an error.
	missing := LoadFile(filepath.Join(dir, "nope.json"))
	if missing.Len() != 0 || len(missing.Problems) != 0 {
		t.Errorf("LoadFile on a missing file = %+v", missing)
	}
}

// writeFile writes a file, creating the directories above it.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
