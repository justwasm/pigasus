package fileuri

// Tests for the 1.5.18 file-URI resolution.
//
// 【一手】 agy 1.5.18 changelog:
//
//	Fixed file permission rules and approval prompts not applying to files the
//	agent reads or edits through `file:///` URIs; those paths now resolve to
//	the real file before permissions are checked.
//
// The resolution is the precondition for the permissions fix: the engine
// matches rules against paths, and before this the URI spelling normalised
// to the literal "file:/etc/passwd" — measured on the 1.5.17 clone, where
// `deny(read_file(/etc/passwd))` answered Deny for "/etc/passwd" and Allow
// for "file:///etc/passwd".

import "testing"

func TestPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"absolute", "file:///etc/passwd", "/etc/passwd"},
		{"single slash", "file:/etc/passwd", "/etc/passwd"},
		{"explicit localhost", "file://localhost/etc/passwd", "/etc/passwd"},
		{"localhost is case-insensitive", "file://LocalHost/tmp/a.txt", "/tmp/a.txt"},
		{"empty authority with a doubled slash", "file:////server/share/x", "//server/share/x"},
		{"named host stays a UNC path", "file://server/share/x", "//server/share/x"},
		{"percent twenty decodes", "file:///tmp/my%20notes.md", "/tmp/my notes.md"},
		{"plus is literal", "file:///tmp/a+b.md", "/tmp/a+b.md"},
		{"drive letter is left to the caller", "file:///C:/Users/me/x.txt", "/C:/Users/me/x.txt"},
		{"file with no path", "file://", "/"},
		{"plain absolute path", "/etc/passwd", "/etc/passwd"},
		{"plain relative path", "src/main.go", "src/main.go"},
		{"colon but no slash", "file:notes.txt", "file:notes.txt"},
		{"a word that only starts alike", "files/x", "files/x"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Path(c.in); got != c.want {
				t.Fatalf("Path(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestPathLeavesNonFileSchemesAlone pins that the resolver is not a general
// URL rewriter: only the file scheme is its business.
func TestPathLeavesNonFileSchemesAlone(t *testing.T) {
	for _, p := range []string{
		"https://example.com/a/b",
		"ssh://host/path",
		"ws://host/socket",
		"data:text/plain;base64,AA==",
	} {
		if got := Path(p); got != p {
			t.Errorf("Path(%q) = %q, want it unchanged", p, got)
		}
	}
}
