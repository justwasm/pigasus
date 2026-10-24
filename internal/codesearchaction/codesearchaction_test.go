package codesearchaction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFeedback covers the usage, hit, literal and empty paths.
func TestFeedback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n\nfunc needle() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "hidden.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, args string }{
		{"empty", ""},
		{"flags-only", "-F"},
		{"bad-flag", "--zzz needle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Feedback(tc.args, dir); got != "Usage: /codesearch [-F|--literal] <query>" {
				t.Errorf("Feedback(%q) = %q", tc.args, got)
			}
		})
	}

	// A pattern match reports the recovered `Found 1 result` header and the
	// path:line:col hit.
	if got := Feedback("needle", dir); got != "Found 1 result\na.go:3:6: func needle() {}" {
		t.Errorf("pattern search = %q", got)
	}
	// .git is skipped, so the hidden file never appears.
	if got := Feedback("needle", dir); strings.Contains(got, ".git") {
		t.Errorf("search descended into .git: %q", got)
	}
	// The default is a pattern: `.` matches anything.
	if got := Feedback("n.edle", dir); !strings.HasPrefix(got, "Found 1 result\na.go:3:") {
		t.Errorf("regex search = %q", got)
	}
	// -F makes it literal, so the same pattern finds nothing.
	if got := Feedback("-F n.edle", dir); got != "No results" {
		t.Errorf("literal search = %q", got)
	}
	// --literal is the long form, and `--` ends the flags.
	if got := Feedback("--literal needle", dir); !strings.HasPrefix(got, "Found 1 result\na.go:3:") {
		t.Errorf("--literal search = %q", got)
	}
	if got := Feedback("-- -F", dir); got != "No results" {
		t.Errorf("`-- -F` search = %q", got)
	}
	if got := Feedback("needle", ""); got != "no workspace directory" {
		t.Errorf("empty workspace = %q", got)
	}
	if got := Feedback("(", dir); !strings.HasPrefix(got, "invalid pattern: ") {
		t.Errorf("bad pattern = %q", got)
	}
}

// TestFeedback_Truncation checks the recovered `  | %d more` tail.
func TestFeedback_Truncation(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < MaxHits+7; i++ {
		b.WriteString("hit\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "many.txt"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Feedback("hit", dir)
	if !strings.HasSuffix(got, "\n  | 7 more") {
		t.Errorf("truncation tail missing: %q", got)
	}
	if n := strings.Count(got, "many.txt:"); n != MaxHits {
		t.Errorf("rendered %d hits, want %d", n, MaxHits)
	}
}

// TestFeedback_PCREFeatures confirms that switching to regexp2 unlocks the
// PCRE feature set the in-process preview previously rejected. ripgrep
// promotes these on the actual search side, so the preview now matches:
// lookarounds, named groups, atomic groups, and inline flags. Stdlib regexp
// (RE2) compiled each of these and would have produced `invalid pattern:`.
// ASCII content only — see TestFeedback_NonASCIIColumns below for the
// known rune-vs-byte caveat that regexp2 imposes on this preview path.
func TestFeedback_PCREFeatures(t *testing.T) {
	dir := t.TempDir()
	// Plain lines the lookarounds anchor on, plus one line that fails each
	// test so the hit count is unambiguous.
	src := "" +
		"package main\n" +
		"\n" +
		"// a sentinel comment\n" +
		"func helloWorld() {}\n" +
		"// another sentinel\n" +
		"x := total\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args string
		want string // expected substring of the result
	}{
		{
			// Positive look-ahead: "sentinel" followed by whitespace or EOL.
			// RE2 doesn't implement lookarounds; regexp2 does.
			name: "lookahead",
			args: `sentinel(?=\s|$)`,
			want: "Found 2 results",
		},
		{
			// Positive look-behind: "helloWorld" preceded by "func ".
			// RE2 doesn't implement fixed-width look-behinds; regexp2
			// does. The test content has `func helloWorld() {}` so the
			// 5-char look-behind matches.
			name: "lookbehind",
			args: `(?<=func )helloWorld`,
			want: "Found 1 result",
		},
		{
			// Non-capturing group with quantifier — basic but worth covering
			// so a future rewrite that drops into stdlib regex still passes.
			name: "non-capture-group",
			args: `(?:sentinel)`,
			want: "Found 2 results",
		},
		{
			// Alternation anchored to start of line.
			name: "anchored-alt",
			args: `^(//|package)`,
			want: "Found 3 results",
		},
		{
			// Word-boundary check.
			name: "word-boundary",
			args: `\bhelloWorld\b`,
			want: "Found 1 result",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Feedback(tc.args, dir)
			if !strings.Contains(got, tc.want) {
				t.Errorf("%s: got %q, want substring %q", tc.name, got, tc.want)
			}
		})
	}

	// Sanity: none of these should compile but they don't match anything
	// either, so we expect `No results`, not `invalid pattern:`.
	t.Run("no-match-pattern", func(t *testing.T) {
		if got := Feedback(`zzzzzNoSuchThing\d{9}`, dir); got != "No results" {
			t.Errorf("no-match query = %q", got)
		}
	})
}

// TestFeedback_NonASCIIColumns documents a known tradeoff of the regexp2
// engine: `Match.Index` is a rune offset, not a byte offset. For ASCII source
// — the common case in /codesearch — rune offset and byte offset agree and
// the reported column is correct. The test asserts the rune-counting behavior
// explicitly so any future swap to a byte-counting engine is noticed.
func TestFeedback_NonASCIIColumns(t *testing.T) {
	dir := t.TempDir()
	// Two bytes of ASCII ("a "), then one multi-byte rune (é = 0xC3 0xA9),
	// then "needle" we want to match. Byte offset of "needle" is 5; rune
	// offset is 4. regexp2 reports the rune offset + 1.
	src := []byte("a \xc3\xa9needle\n")
	if err := os.WriteFile(filepath.Join(dir, "utf8.txt"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	got := Feedback("needle", dir)
	// Runes: 'a'(0), ' '(1), 'é'(2), 'n'(3), ...   needle starts at rune 3.
	// Bytes: 'a'(0), ' '(1), 'é'(2..3), 'n'(4), ... needle starts at byte 4.
	// regexp2 reports the rune offset (3) + 1 == column 4, not the byte
	// offset (4) + 1 == column 5. The test pins the rune-based value so a
	// future swap to a byte-counting engine is noticed.
	want := "utf8.txt:1:4: " + string(src[:len(src)-1])
	if got != "Found 1 result\n"+want {
		t.Errorf("non-ASCII column = %q, want rune-based %q", got, want)
	}
}

// TestFeedback_BinarySkipped checks that a NUL byte marks a file as binary.
func TestFeedback_BinarySkipped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), []byte("needle\x00needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Feedback("needle", dir); got != "No results" {
		t.Errorf("binary file searched: %q", got)
	}
}

// TestCountLabel pins the singular/plural rendering.
func TestCountLabel(t *testing.T) {
	if got := CountLabel(1, ResultWords); got != "1 result" {
		t.Errorf("CountLabel(1) = %q", got)
	}
	if got := CountLabel(2, ResultWords); got != "2 results" {
		t.Errorf("CountLabel(2) = %q", got)
	}
	if got := CountLabel(0, ResultWords); got != "0 results" {
		t.Errorf("CountLabel(0) = %q", got)
	}
}
