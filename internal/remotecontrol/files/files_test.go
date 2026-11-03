// Package files — files_test.go: the provider seam's own tests.
//
// The provider is the domain's fact: the in-memory fixture's happy
// paths, the production scope's workspace / --add-dir / sandbox
// resolution, and the sentinels the two dashboard methods map to
// their JSON-RPC codes.
//
// The dispatcher's round-trip against this domain lives in
// internal/remotecontrol/files_test.go, because the dispatcher is
// the host's.
package files

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// memProvider is a small in-memory Provider fixture the
// tests below drive. It exposes an "allowed" directory
// with a few files (text, binary, a 100-KiB text file
// for the truncation test) plus an isolated "secret"
// directory the dispatcher must reject via the production
// policy. The fixture deliberately has no policy of its
// own — the rejection tests pair MemProvider with a WorkdirScope
// whose Workspace is the allowed dir.
func memProvider(t *testing.T) *MemProvider {
	t.Helper()
	bin := []byte{0x00, 0x01, 0x02, 0x03, 0x00, 0x05}
	big := make([]byte, 100*1024)
	for i := range big {
		big[i] = byte('A' + (i % 26))
	}
	return NewMemProvider(map[string][]MemEntry{
		"/allowed": {
			{Name: "main.go", IsDir: false, Data: []byte("package main\n\nfunc main() {}\n")},
			{Name: "binary.bin", IsDir: false, Data: bin},
			{Name: "big.txt", IsDir: false, Data: big},
			{Name: "sub", IsDir: true, Data: nil},
		},
		"/secret": {
			{Name: "secret.txt", IsDir: false, Data: []byte("top secret\n")},
		},
	})
}

// dispatchFile sends a ReadRemoteAgentFile or
// ReadRemoteAgentDir request through the dispatcher and
// returns the (result, error) pair. The helper centralises
// the boilerplate every test would otherwise repeat.

// TestWorkdirScope_AllowList covers the --add-dir
// path: a path inside an allowed dir is permitted
// even when outside the workspace.
func TestWorkdirScope_AllowList(t *testing.T) {
	s := &WorkdirScope{
		Workspace: "/workspace",
		Allow:     []string{"/extra"},
	}
	canon, err := s.ResolvePolicy("/extra/data.txt")
	if err != nil {
		t.Fatalf("allow-list path: error = %v", err)
	}
	if canon != "/extra/data.txt" {
		t.Errorf("canonical = %q, want /extra/data.txt", canon)
	}
	// /elsewhere still rejected.
	if _, err := s.ResolvePolicy("/elsewhere/x"); !errors.Is(err, ErrPathNotAllowed) {
		t.Errorf("elsewhere: err = %v, want ErrPathNotAllowed", err)
	}
	// workspace subdir still allowed.
	canon, err = s.ResolvePolicy("/workspace/src/main.go")
	if err != nil {
		t.Fatalf("workspace subdir: error = %v", err)
	}
	if canon != "/workspace/src/main.go" {
		t.Errorf("canonical = %q, want /workspace/src/main.go", canon)
	}
}

// TestWorkdirScope_RelativePath covers the
// "relative path" path: "src/main.go" resolves
// against the workspace.

// TestWorkdirScope_RelativePath covers the
// "relative path" path: "src/main.go" resolves
// against the workspace.
func TestWorkdirScope_RelativePath(t *testing.T) {
	s := &WorkdirScope{Workspace: "/workspace"}
	canon, err := s.ResolvePolicy("src/main.go")
	if err != nil {
		t.Fatalf("relative path: error = %v", err)
	}
	if canon != "/workspace/src/main.go" {
		t.Errorf("canonical = %q, want /workspace/src/main.go", canon)
	}
}

// TestWorkdirScope_EmptyPath covers the "empty path"
// path: returns ErrPathNotAllowed.

// TestWorkdirScope_EmptyPath covers the "empty path"
// path: returns ErrPathNotAllowed.
func TestWorkdirScope_EmptyPath(t *testing.T) {
	s := &WorkdirScope{Workspace: "/workspace"}
	if _, err := s.ResolvePolicy(""); !errors.Is(err, ErrPathNotAllowed) {
		t.Errorf("empty path: err = %v, want ErrPathNotAllowed", err)
	}
}

// TestMemFS_ReadFile_Binary covers the MemProvider-level
// binary-detection path: bytes with NULs are
// classified as binary.

// TestMemFS_ReadFile_Binary covers the MemProvider-level
// binary-detection path: bytes with NULs are
// classified as binary.
func TestMemFS_ReadFile_Binary(t *testing.T) {
	m := NewMemProvider(map[string][]MemEntry{
		"/x": {
			{Name: "b", IsDir: false, Data: []byte{0x00, 0x01, 0x02}},
		},
	})
	data, total, truncated, isBinary, err := m.ReadFile("/x/b", 1024, 0)
	if err != nil {
		t.Fatalf("MemProvider.ReadFile: %v", err)
	}
	if !isBinary {
		t.Errorf("isBinary = false, want true")
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if truncated {
		t.Errorf("truncated = true, want false")
	}
	if len(data) != 3 {
		t.Errorf("data length = %d, want 3", len(data))
	}
}

// TestMemFS_ReadFile_Directory covers the
// MemProvider-level "not a regular file" path: reading a
// directory returns ErrNotRegularFile.

// TestMemFS_ReadFile_Directory covers the
// MemProvider-level "not a regular file" path: reading a
// directory returns ErrNotRegularFile.
func TestMemFS_ReadFile_Directory(t *testing.T) {
	m := NewMemProvider(map[string][]MemEntry{
		"/x": {
			{Name: "sub", IsDir: true, Data: nil},
		},
	})
	_, _, _, _, err := m.ReadFile("/x/sub", 1024, 0)
	if !errors.Is(err, ErrNotRegularFile) {
		t.Errorf("MemProvider.ReadFile on directory: err = %v, want ErrNotRegularFile", err)
	}
}

// TestMemFS_ReadFile_OffsetBeyondSize covers the
// MemProvider-level offset > size path.

// TestMemFS_ReadFile_OffsetBeyondSize covers the
// MemProvider-level offset > size path.
func TestMemFS_ReadFile_OffsetBeyondSize(t *testing.T) {
	m := NewMemProvider(map[string][]MemEntry{
		"/x": {
			{Name: "f", IsDir: false, Data: []byte("hi")},
		},
	})
	_, total, _, _, err := m.ReadFile("/x/f", 1024, 100)
	if err == nil {
		t.Fatalf("offset beyond size: got nil error")
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
}

// TestWorkdirScope_ReadFile covers the production
// ReadFile path against a real temporary file. The
// test creates a tmp file, configures a WorkdirScope
// whose workspace is the tmp dir's parent, and
// exercises the read / truncation / binary paths.

// TestWorkdirScope_ReadFile covers the production
// ReadFile path against a real temporary file. The
// test creates a tmp file, configures a WorkdirScope
// whose workspace is the tmp dir's parent, and
// exercises the read / truncation / binary paths.
func TestWorkdirScope_ReadFile(t *testing.T) {
	dir := t.TempDir()
	// text file
	textPath := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(textPath, []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// binary file
	binPath := filepath.Join(dir, "bin.bin")
	if err := os.WriteFile(binPath, []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// big file
	big := make([]byte, 8*1024)
	for i := range big {
		big[i] = byte('A' + (i % 26))
	}
	bigPath := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(bigPath, big, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := &WorkdirScope{Workspace: dir}

	// text → utf-8
	data, total, truncated, isBinary, err := s.ReadFile(textPath, 1024, 0)
	if err != nil {
		t.Fatalf("text ReadFile: %v", err)
	}
	if isBinary || truncated {
		t.Errorf("text: isBinary=%v truncated=%v, want false false", isBinary, truncated)
	}
	if total != 12 {
		t.Errorf("text total = %d, want 12", total)
	}
	if string(data) != "hello world\n" {
		t.Errorf("text data = %q, want hello world\\n", data)
	}

	// binary → binary
	_, _, _, isBinary, err = s.ReadFile(binPath, 1024, 0)
	if err != nil {
		t.Fatalf("bin ReadFile: %v", err)
	}
	if !isBinary {
		t.Errorf("bin: isBinary=false, want true")
	}

	// big → truncated
	_, total, truncated, _, err = s.ReadFile(bigPath, 1024, 0)
	if err != nil {
		t.Fatalf("big ReadFile: %v", err)
	}
	if total != 8*1024 {
		t.Errorf("big total = %d, want %d", total, 8*1024)
	}
	if !truncated {
		t.Errorf("big: truncated=false, want true")
	}
}

// TestWorkdirScope_ReadFile_Directory covers the
// production ReadFile path's directory rejection:
// ErrNotRegularFile.

// TestWorkdirScope_ReadFile_Directory covers the
// production ReadFile path's directory rejection:
// ErrNotRegularFile.
func TestWorkdirScope_ReadFile_Directory(t *testing.T) {
	dir := t.TempDir()
	s := &WorkdirScope{Workspace: dir}
	_, _, _, _, err := s.ReadFile(dir, 1024, 0)
	if !errors.Is(err, ErrNotRegularFile) {
		t.Errorf("ReadFile on dir: err = %v, want ErrNotRegularFile", err)
	}
}

// TestWorkdirScope_ReadDir covers the production
// ReadDir path against a real temporary directory.

// TestWorkdirScope_ReadDir covers the production
// ReadDir path against a real temporary directory.
func TestWorkdirScope_ReadDir(t *testing.T) {
	dir := t.TempDir()
	// Two files + a subdirectory
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	s := &WorkdirScope{Workspace: dir}
	entries, total, truncated, err := s.ReadDir(dir, 0)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if truncated {
		t.Errorf("truncated = true, want false")
	}
	if len(entries) != 3 {
		t.Fatalf("entries length = %d, want 3", len(entries))
	}
	// Sorted: a.txt, b.txt, sub.
	want := []string{"a.txt", "b.txt", "sub"}
	for i, w := range want {
		if entries[i].Name != w {
			t.Errorf("entries[%d].Name = %q, want %q", i, entries[i].Name, w)
		}
	}
	if entries[2].Type != "dir" {
		t.Errorf("sub.Type = %v, want dir", entries[2].Type)
	}
	if entries[0].Type != "file" {
		t.Errorf("a.txt.Type = %v, want file", entries[0].Type)
	}
	if entries[0].MTime == "" {
		t.Errorf("a.txt.MTime empty, want non-empty RFC3339")
	}
}
