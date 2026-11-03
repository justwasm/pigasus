package editorkit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestResolveEditor pins the editor resolution and its recovered notice.
func TestResolveEditor(t *testing.T) {
	t.Setenv("EDITOR", "")
	name, args, notice, err := ResolveEditor("")
	want := EditorFallback()
	if err != nil || name != want || len(args) != 0 ||
		notice != "No editor configured; using "+want {
		t.Errorf("ResolveEditor(\"\") = (%q,%v,%q,%v)", name, args, notice, err)
	}
	// Unix falls back to vim; the changelog's edit/notepad fix is Windows-only.
	if runtime.GOOS != "windows" && want != "vim" {
		t.Errorf("EditorFallback() = %q, want vim", want)
	}

	t.Setenv("EDITOR", "nano -w")
	name, args, notice, err = ResolveEditor("auto")
	if err != nil || name != "nano" || len(args) != 1 || args[0] != "-w" || notice != "" {
		t.Errorf("ResolveEditor(auto) = (%q,%v,%q,%v)", name, args, notice, err)
	}

	name, args, notice, err = ResolveEditor("code --wait")
	if err != nil || name != "code" || len(args) != 1 || args[0] != "--wait" || notice != "" {
		t.Errorf("ResolveEditor(code --wait) = (%q,%v,%q,%v)", name, args, notice, err)
	}
}

// TestSplitCommand pins the recovered $EDITOR parsing rule: split on whitespace
// only, never on `=`, so `--alternate-editor=vi` survives as one argument.
// Changelog: "Fixed `$EDITOR` environment variable parsing: resolved issues
// where arguments containing `=` (e.g., `--alternate-editor=vi`) were
// incorrectly split, causing editor launch failures."
func TestSplitCommand(t *testing.T) {
	for _, tc := range []struct {
		in       string
		wantName string
		wantArgs []string
	}{
		{"vim", "vim", nil},
		{"nano -w", "nano", []string{"-w"}},
		{"emacsclient --alternate-editor=vi", "emacsclient", []string{"--alternate-editor=vi"}},
		{"  spaced   out  ", "spaced", []string{"out"}},
		{"", "", nil},
	} {
		name, args := SplitCommand(tc.in)
		if name != tc.wantName {
			t.Errorf("SplitCommand(%q) name = %q, want %q", tc.in, name, tc.wantName)
		}
		if strings.Join(args, "\x00") != strings.Join(tc.wantArgs, "\x00") {
			t.Errorf("SplitCommand(%q) args = %v, want %v", tc.in, args, tc.wantArgs)
		}
	}
}

// TestUnquotePath pins the one-layer quote stripping.
func TestUnquotePath(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain", "plain"},
		{`"quoted"`, "quoted"},
		{"'quoted'", "quoted"},
		{`"unbalanced`, `"unbalanced`},
		{`""`, ""},
		{"  spaced  ", "spaced"},
	} {
		if got := UnquotePath(tc.in); got != tc.want {
			t.Errorf("UnquotePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestPromptScratchFile pins the scratch-file round trip for ctrl+g.
func TestPromptScratchFile(t *testing.T) {
	path, err := PromptScratchFile("original")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(path) }()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original" {
		t.Errorf("scratch file = %q, want %q", data, "original")
	}
}

// TestOpenFeedback covers the argument, missing-file and success paths. The
// command is never run.
func TestOpenFeedback(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", "cat")
	exists := func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	}

	if msg, cmd := OpenFeedback("", "", dir, exists); msg != "Usage: /open <file_path>" || cmd != nil {
		t.Errorf("OpenFeedback(\"\") = (%q,%v)", msg, cmd)
	}

	msg, cmd := OpenFeedback("nope.txt", "", dir, exists)
	if cmd != nil || msg != "File not found: "+filepath.Join(dir, "nope.txt") {
		t.Errorf("OpenFeedback(missing) = (%q,%v)", msg, cmd)
	}

	msg, cmd = OpenFeedback("a.txt", "", dir, exists)
	if cmd == nil || cmd.Args[0] != "cat" || msg != "Opening file: "+file {
		t.Errorf("OpenFeedback(a.txt) = (%q,%v)", msg, cmd)
	}

	// A quoted path with spaces.
	spaced := filepath.Join(dir, "with space.txt")
	if err := os.WriteFile(spaced, []byte("y\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	msg, cmd = OpenFeedback(`"`+spaced+`"`, "", dir, exists)
	if cmd == nil || msg != "Opening file: "+spaced {
		t.Errorf("OpenFeedback(quoted) = (%q,%v)", msg, cmd)
	}

	// An editor that does not exist reports the recovered resolve failure.
	msg, cmd = OpenFeedback("a.txt", "definitely-not-an-editor-xyz", dir, exists)
	if cmd != nil || !strings.HasPrefix(msg, "Failed to resolve editor: ") {
		t.Errorf("OpenFeedback(bad editor) = (%q,%v)", msg, cmd)
	}
}
