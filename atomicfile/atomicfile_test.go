package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteFile_CreatesWithTheGivenMode pins the mode of a file that
// did not exist.
func TestWriteFile_CreatesWithTheGivenMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.json")
	if err := WriteFile(path, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600", perm)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"a":1}` {
		t.Fatalf("content = %q", data)
	}
}

// TestWriteFile_ReplacesAWiderMode is the guarantee the wrapper exists
// for: renameio.WriteFile keeps the mode the target had, and these
// files hold credentials.
func TestWriteFile_ReplacesAWiderMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600", perm)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("content = %q", data)
	}
}

// TestWriteFile_LeavesNoTempFile pins the cleanup: a failed write must
// not litter the directory.
func TestWriteFile_LeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.json")
	for i := 0; i < 3; i++ {
		if err := WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "secret.json" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("directory holds %v, want just the target", names)
	}
}
