package filesystem

// Guard for the 1.5.18 file-URI fix at the tool layer.
//
// 【一手】 agy 1.5.18 changelog:
//
//	Fixed file permission rules and approval prompts not applying to files the
//	agent reads or edits through `file:///` URIs; those paths now resolve to
//	the real file before permissions are checked.
//
// The permission half is pinned in internal/core/fileuri and
// internal/core/policy/permissions. This file pins the other half: the tool
// has to *open* the file the URI names. Measured on the 1.5.17 clone,
// read_file("file:///…/secret.txt") answered
//
//	open …/ws/file:/…/secret.txt: no such file or directory
//
// — the URI was joined onto the workspace as if it were a relative name, so
// the permission engine and the tool disagreed about which file the call was
// about. Resolving the URI in ReadPath makes both sides name the same file.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileOpensTheFileAFileURINames(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	target := filepath.Join(dir, "outside.txt")
	if err := os.WriteFile(target, []byte("URICONTENT\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := &ReadFile{Workspace: ws}
	out, err := r.Run(context.Background(), json.RawMessage(`{"path":"file://`+target+`"}`))
	if err != nil {
		t.Fatalf("read_file(file://…) must open the URI's file, got error: %v", err)
	}
	body, _ := out.(string)
	if !strings.Contains(body, "URICONTENT") {
		t.Fatalf("read_file(file://…) did not read the URI's file; output = %q", body)
	}
}

func TestViewFileOpensTheFileAFileURINames(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	target := filepath.Join(dir, "outside.txt")
	if err := os.WriteFile(target, []byte("URICONTENT\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	v := &ViewFile{Workspace: ws}
	out, err := v.Run(context.Background(), json.RawMessage(`{"path":"file://`+target+`"}`))
	if err != nil {
		t.Fatalf("view_file(file://…) must open the URI's file, got error: %v", err)
	}
	body, _ := out.(string)
	if !strings.Contains(body, "URICONTENT") {
		t.Fatalf("view_file(file://…) did not read the URI's file; output = %q", body)
	}
}

// TestFileURIDoesNotEscapeTheWorkspaceWriteGate is the containment half: a
// URI must not become a way to write outside the workspace. Absolute paths
// are gated by the same rule whether they are spelled plainly or as a URI,
// so a URI naming a file outside the roots is rejected exactly as the plain
// path is.
func TestFileURIDoesNotEscapeTheWorkspaceWriteGate(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	outside := filepath.Join(dir, "escape.txt")

	w := &WriteFile{Workspace: ws}
	for _, p := range []string{outside, "file://" + outside} {
		if _, err := w.Run(context.Background(), json.RawMessage(`{"path":"`+p+`","content":"x"}`)); err == nil {
			t.Errorf("write_file(%q) outside the workspace was allowed; the URI spelling must not be laxer than the path spelling", p)
		}
	}
	if _, err := os.Stat(outside); err == nil {
		t.Errorf("write_file created %s outside the workspace", outside)
	}
}
