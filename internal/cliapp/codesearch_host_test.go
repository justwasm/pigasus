// codesearch_host_test.go — the host's side of the /codesearch file
// viewer.
//
// The panel owns the view; the host owns the disk and the process
// (invariant 163): it reads the file the panel asks for and it starts
// the editor the panel asks for. These tests pin the two answers the
// host gives.

package cliapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCodesearchReadFile: the host reads the file the panel names,
// relative to the search root, and answers with its lines.
func TestCodesearchReadFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"),
		[]byte("line1\nline2 needle here\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &runMode{}
	m.cwd = root
	lines, message, ok := m.codesearchReadFile("a.txt")
	if !ok {
		t.Fatalf("read failed: %s", message)
	}
	if message != "" {
		t.Errorf("message = %q, want empty", message)
	}
	want := []string{"line1", "line2 needle here", "line3"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i+1, lines[i], want[i])
		}
	}
}

// TestCodesearchReadFileMissing: a file that is not there answers with
// the reference's own sentence.
func TestCodesearchReadFileMissing(t *testing.T) {
	root := t.TempDir()
	m := &runMode{}
	m.cwd = root
	_, message, ok := m.codesearchReadFile("nope.txt")
	if ok {
		t.Fatal("a missing file must not be readable")
	}
	want := "File not found: " + filepath.Join(root, "nope.txt")
	if message != want {
		t.Errorf("message = %q, want %q", message, want)
	}
}

// TestCodesearchReadFileNotRegular: a directory is not a file, and the
// reference says so in its own words.
func TestCodesearchReadFileNotRegular(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &runMode{}
	m.cwd = root
	_, message, ok := m.codesearchReadFile("sub")
	if ok {
		t.Fatal("a directory must not be readable")
	}
	if message != "Cannot view a pipe, socket, or device; only regular files can be viewed." {
		t.Errorf("message = %q", message)
	}
}

// TestEditorLineArgs: the clone's own line convention. The reference's
// convention is not recovered, so this is the clone's table.
func TestEditorLineArgs(t *testing.T) {
	cases := []struct {
		editor string
		path   string
		line   int
		want   []string
	}{
		{"vim", "/w/a.txt", 12, []string{"+12", "/w/a.txt"}},
		{"nvim", "/w/a.txt", 12, []string{"+12", "/w/a.txt"}},
		{"code", "/w/a.txt", 12, []string{"-g", "/w/a.txt:12"}},
		{"codium", "/w/a.txt", 12, []string{"-g", "/w/a.txt:12"}},
		{"subl", "/w/a.txt", 12, []string{"/w/a.txt:12"}},
		{"emacs", "/w/a.txt", 12, []string{"/w/a.txt"}},
		{"/usr/local/bin/vim", "/w/a.txt", 12, []string{"+12", "/w/a.txt"}},
	}
	for _, c := range cases {
		got := editorLineArgs(c.editor, c.path, c.line)
		if len(got) != len(c.want) {
			t.Errorf("editorLineArgs(%q) = %q, want %q", c.editor, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("editorLineArgs(%q) = %q, want %q", c.editor, got, c.want)
				break
			}
		}
	}
}

// TestSplitFileLines: the trailing newline is not a line of its own and
// a CRLF's carriage return is not part of the line's text.
func TestSplitFileLines(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a\nb\n", []string{"a", "b"}},
		{"a\nb", []string{"a", "b"}},
		{"a\r\nb\r\n", []string{"a", "b"}},
		{"\n", nil},
		{"", nil},
	}
	for _, c := range cases {
		got := splitFileLines([]byte(c.in))
		if len(got) != len(c.want) {
			t.Errorf("splitFileLines(%q) = %q, want %q", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitFileLines(%q) = %q, want %q", c.in, got, c.want)
				break
			}
		}
	}
}

// TestCodesearchPath: the path the panel carries is relative to the
// search root.
func TestCodesearchPath(t *testing.T) {
	m := &runMode{}
	m.cwd = "/work"
	if got := m.codesearchPath("a/b.txt"); got != filepath.Join("/work", "a/b.txt") {
		t.Errorf("codesearchPath = %q", got)
	}
	if got := m.codesearchPath("/abs/b.txt"); got != "/abs/b.txt" {
		t.Errorf("an absolute path must be kept: %q", got)
	}
}

// TestCodesearchPipeMessage: the reference's sentence is byte for byte.
func TestCodesearchPipeMessage(t *testing.T) {
	want := "Cannot view a pipe, socket, or device; only regular files can be viewed."
	if codesearchPipeMessage != want {
		t.Errorf("codesearchPipeMessage = %q, want %q", codesearchPipeMessage, want)
	}
	if !strings.HasSuffix(codesearchPipeMessage, ".") {
		t.Error("the reference's sentence ends with a full stop")
	}
}
