package atfiles

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// TestParse covers the AGY 1.3.6 line-12 fix explicitly: the
// dropdown must appear when typing "@<char>" at the END of any
// line, not just on the first line. Each table entry is a buffer
// that ends in the active mention token — the parser must report
// (prefix, start, end, true) on every one of them.
func TestParse(t *testing.T) {
	cases := []struct {
		name       string
		buf        string
		wantPrefix string
		wantStart  int
		wantEnd    int
		wantOK     bool
	}{
		// AGY 1.3.6 bug — exercise it explicitly: a freshly-typed
		// "@" after a newline in a multi-line prompt.
		{"empty", "", "", 0, 0, false},
		{"bare @", "@", "", 0, 1, true},
		{"single char at start", "@a", "a", 0, 2, true},
		{"path-like mid-line", "look at @src/", "src/", 8, 13, true},
		{"after newline (the fix)", "look at this\n@src", "src", 13, 17, true},
		{"trailing slash after newline", "look at this\n@src/", "src/", 13, 18, true},
		{"dotted name", "@main.go", "main.go", 0, 8, true},
		{"nested path", "@cmd/wagyu/main.go", "cmd/wagyu/main.go", 0, 18, true},
		{"mid-text without trailing space", "abc @foo", "foo", 4, 8, true},
		{"whitespace after token kills it", "@foo ", "", 0, 0, false},
		{"newline after token kills it", "@foo\n", "", 0, 0, false},
		{"second @ in active token wins", "@a/@b", "a/@b", 0, 5, true},
		{"mid-word @ disqualified", "obj@foo", "", 0, 0, false},
		{"active token is just @", "abc @", "", 4, 5, true},
		// Comma is a sentence break, the @ starts a new token at
		// run-position 0, so the trailing run is just "@foo" and
		// the parser accepts the mention.
		{"mention after punctuation", "hello,@foo", "foo", 6, 10, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, ok := Parse(tc.buf)
			if ok != tc.wantOK {
				t.Fatalf("Parse(%q) ok=%v want %v", tc.buf, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if m.Prefix != tc.wantPrefix || m.Start != tc.wantStart || m.End != tc.wantEnd {
				t.Fatalf("Parse(%q) = (%q,%d,%d), want (%q,%d,%d)",
					tc.buf, m.Prefix, m.Start, m.End,
					tc.wantPrefix, tc.wantStart, tc.wantEnd)
			}
		})
	}
}

// TestParse_AfterNewlineBehavesLikeEndOfLine is the AGY 1.3.6
// line-12 regression guard. The prefix produced for
// "look at this\n@src" must match the prefix for "look at @src"
// when both end on the same character — the "drop-down shows just
// like at the end of a line" promise.
func TestParse_AfterNewlineBehavesLikeEndOfLine(t *testing.T) {
	a, okA := Parse("look at @src")
	b, okB := Parse("look at this\n@src")
	if !okA || !okB {
		t.Fatalf("both should be active: A=%v B=%v", okA, okB)
	}
	if a.Prefix != b.Prefix {
		t.Fatalf("prefix mismatch after newline: A=%q B=%q", a.Prefix, b.Prefix)
	}
}

// TestFindEntries uses a t.TempDir full of known filenames so we
// can assert the three 1.4.3 picker modes: recursive workspace
// listing, directory browsing, and fuzzy query matching.
func TestFindEntries(t *testing.T) {
	root := t.TempDir()
	files := []string{
		"main.go",
		"main_test.go",
		"cmd/wagyu/main.go",
		"cmd/wagyu/main_test.go",
		"internal/atfiles/atfiles.go",
		"README.md",
		"LICENSE",
	}
	for _, f := range files {
		full := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(""), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	t.Run("empty prefix includes directories and nested files", func(t *testing.T) {
		got, err := FindEntries(root, "")
		if err != nil {
			t.Fatalf("FindEntries: %v", err)
		}
		var rels []string
		for _, e := range got {
			rels = append(rels, e.Rel)
		}
		sort.Strings(rels)
		want := []string{
			"LICENSE",
			"README.md",
			"cmd/",
			"cmd/wagyu/",
			"cmd/wagyu/main.go",
			"cmd/wagyu/main_test.go",
			"internal/",
			"internal/atfiles/",
			"internal/atfiles/atfiles.go",
			"main.go",
			"main_test.go",
		}
		if !reflect.DeepEqual(rels, want) {
			t.Errorf("FindEntries(root, \"\") =\n  %v\nwant\n  %v", rels, want)
		}
	})
	t.Run("query is recursive and name-only", func(t *testing.T) {
		got, err := FindEntries(root, "main")
		if err != nil {
			t.Fatalf("FindEntries: %v", err)
		}
		var rels []string
		for _, e := range got {
			rels = append(rels, e.Rel)
		}
		want := []string{
			"main.go",
			"main_test.go",
			"cmd/wagyu/main.go",
			"cmd/wagyu/main_test.go",
		}
		if !reflect.DeepEqual(rels, want) {
			t.Errorf("FindEntries(root, \"main\") =\n  %v\nwant\n  %v", rels, want)
		}
	})
	t.Run("directory browse yields direct children only", func(t *testing.T) {
		got, err := FindEntries(root, "cmd/wagyu/")
		if err != nil {
			t.Fatalf("FindEntries: %v", err)
		}
		var rels []string
		for _, e := range got {
			rels = append(rels, e.Rel)
		}
		want := []string{
			"cmd/wagyu/main.go",
			"cmd/wagyu/main_test.go",
		}
		if !reflect.DeepEqual(rels, want) {
			t.Errorf("FindEntries(root, \"cmd/wagyu/\") =\n  %v\nwant\n  %v", rels, want)
		}
	})
	t.Run("no matches", func(t *testing.T) {
		got, err := FindEntries(root, "nonexistent-prefix-zzz")
		if err != nil {
			t.Fatalf("FindEntries: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("FindEntries(root, \"nonexistent-prefix-zzz\") = %v, want empty", got)
		}
	})
	t.Run("skips git and runtime state", func(t *testing.T) {
		_ = os.MkdirAll(filepath.Join(root, ".git"), 0o755)
		_ = os.MkdirAll(filepath.Join(root, ".crush"), 0o755)
		_ = os.WriteFile(filepath.Join(root, ".git", "config"), []byte(""), 0o644)
		_ = os.WriteFile(filepath.Join(root, ".crush", "state.db"), []byte(""), 0o644)
		got, err := FindEntries(root, "config")
		if err != nil {
			t.Fatalf("FindEntries: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("FindEntries(root, \"config\") = %v, want empty", got)
		}
	})
}
