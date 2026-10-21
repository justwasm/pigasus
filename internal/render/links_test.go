package render

import (
	"strings"
	"testing"
)

// TestFormatFileLink_WithLine is the canonical changelog line-59 case:
// src/foo.go#L12 must round-trip to src/foo.go%23L12.
func TestFormatFileLink_WithLine(t *testing.T) {
	got := FormatFileLink("src/foo.go", 12)
	if got != "src/foo.go%23L12" {
		t.Errorf("FormatFileLink(src/foo.go, 12) = %q, want %q", got, "src/foo.go%23L12")
	}
}

// TestFormatFileLink_NoLine confirms a zero line emits no fragment.
// Some /codesearch rows reference a file with no specific line; the
// formatter must not invent one.
func TestFormatFileLink_NoLine(t *testing.T) {
	got := FormatFileLink("src/foo.go", 0)
	if got != "src/foo.go" {
		t.Errorf("FormatFileLink(src/foo.go, 0) = %q, want %q", got, "src/foo.go")
	}
	// Negative line is treated the same as zero — defensive against
	// accidental arithmetic wrap-arounds in callers.
	if got := FormatFileLink("src/foo.go", -1); got != "src/foo.go" {
		t.Errorf("FormatFileLink(src/foo.go, -1) = %q, want %q", got, "src/foo.go")
	}
}

// TestFormatFileLink_LineOne pins the smallest legal line. A single-
// digit L<n> case is easy to get wrong if a caller ever hard-codes a
// width or a %02d formatter.
func TestFormatFileLink_LineOne(t *testing.T) {
	got := FormatFileLink("a.go", 1)
	if got != "a.go%23L1" {
		t.Errorf("FormatFileLink(a.go, 1) = %q, want %q", got, "a.go%23L1")
	}
}

// TestFormatFileLink_Line999 pins a three-digit line so the test
// fails loudly if the helper ever introduces a width cap or strips
// trailing digits.
func TestFormatFileLink_Line999(t *testing.T) {
	got := FormatFileLink("a.go", 999)
	if got != "a.go%23L999" {
		t.Errorf("FormatFileLink(a.go, 999) = %q, want %q", got, "a.go%23L999")
	}
}

// TestFormatFileLink_PathWithSpaces confirms the path component is
// NOT percent-encoded. The changelog 1.3.0 fix covers the deeper
// question of *which* paths make it to the formatter (spaces, `#`,
// etc. are normalised at the @mention / /codesearch layer); at this
// layer we trust callers to hand us URL-safe paths and pass them
// through verbatim.
func TestFormatFileLink_PathWithSpaces(t *testing.T) {
	got := FormatFileLink("src/my file.go", 12)
	if got != "src/my file.go%23L12" {
		t.Errorf("FormatFileLink(src/my file.go, 12) = %q, want %q", got, "src/my file.go%23L12")
	}
	// Likewise the literal `#` (no line anchor on the input path).
	if got := FormatFileLink("weird#name.go", 4); got != "weird#name.go%23L4" {
		t.Errorf("FormatFileLink(weird#name.go, 4) = %q, want %q", got, "weird#name.go%23L4")
	}
}

// TestFormatFileLink_MultipleAnchors confirms that a caller can
// build a multi-anchor URL (e.g. for a multi-line selection range
// expressed as two stacked single-line links) by appending — the
// second anchor lands after a `%23` that the first anchor already
// wrote.
func TestFormatFileLink_MultipleAnchors(t *testing.T) {
	got := FormatFileLink("a.go", 12)
	got = FormatFileLink(got, 20)
	if got != "a.go%23L12%23L20" {
		t.Errorf("double anchor = %q, want %q", got, "a.go%23L12%23L20")
	}
}

// TestFormatFileLinkRange covers the four quadrants of (start, end):
// real range, equal bounds, zero bounds, and inverted bounds.
func TestFormatFileLinkRange(t *testing.T) {
	cases := []struct {
		name       string
		path, want string
		start, end int
	}{
		{"range", "a.go", "a.go%23L10-L20", 10, 20},
		{"equal", "a.go", "a.go%23L5", 5, 5},
		{"zero", "a.go", "a.go", 0, 0},
		{"only start", "a.go", "a.go%23L7", 7, 3},
		{"only end", "a.go", "a.go%23L20", 0, 20},
		{"both zero", "a.go", "a.go", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatFileLinkRange(tc.path, tc.start, tc.end); got != tc.want {
				t.Errorf("FormatFileLinkRange(%q,%d,%d) = %q, want %q",
					tc.path, tc.start, tc.end, got, tc.want)
			}
		})
	}
}

// TestNormalizeFileLink_Single is the inverse of FormatFileLink:
// given an unencoded `#L<n>`, rewrite to `%23L<n>`.
func TestNormalizeFileLink_Single(t *testing.T) {
	got := NormalizeFileLink("a.go#L12")
	if got != "a.go%23L12" {
		t.Errorf("NormalizeFileLink(a.go#L12) = %q, want %q", got, "a.go%23L12")
	}
}

// TestNormalizeFileLink_Idempotent confirms the helper is safe to
// call on already-normalised URLs. Without idempotence the mdrender
// integration would double-encode.
func TestNormalizeFileLink_Idempotent(t *testing.T) {
	for _, s := range []string{
		"a.go%23L12",
		"src/foo.go%23L12",
		"a.go%23L10-L20",
	} {
		if got := NormalizeFileLink(s); got != s {
			t.Errorf("NormalizeFileLink(%q) = %q, want unchanged", s, got)
		}
	}
}

// TestNormalizeFileLink_Range covers `#L<start>-L<end>`.
func TestNormalizeFileLink_Range(t *testing.T) {
	got := NormalizeFileLink("a.go#L10-L20")
	if got != "a.go%23L10-L20" {
		t.Errorf("NormalizeFileLink(a.go#L10-L20) = %q, want %q", got, "a.go%23L10-L20")
	}
}

// TestNormalizeFileLink_MultipleAnchors fires when a single URL
// stacks two anchors (the "view both line 12 and line 20" case).
// Both get rewritten; no double-encoding.
func TestNormalizeFileLink_MultipleAnchors(t *testing.T) {
	got := NormalizeFileLink("a.go#L12#L20")
	if got != "a.go%23L12%23L20" {
		t.Errorf("NormalizeFileLink(a.go#L12#L20) = %q, want %q", got, "a.go%23L12%23L20")
	}
}

// TestNormalizeFileLink_LeavesHTTPAlone ensures we don't touch
// arbitrary URL fragments. An HTTP link to a docs page's `#section`
// anchor must round-trip unchanged.
func TestNormalizeFileLink_LeavesHTTPAlone(t *testing.T) {
	cases := []string{
		"https://example.com",
		"https://example.com/page",
		"https://example.com/page#section",
		"https://example.com/page?q=hello#world",
	}
	for _, s := range cases {
		if got := NormalizeFileLink(s); got != s {
			t.Errorf("NormalizeFileLink(%q) = %q, want unchanged", s, got)
		}
	}
}

// TestNormalizeFileLink_EmptyAndNoHash short-circuits cheaply: an
// empty string and a string with no `#` at all must return in O(1).
func TestNormalizeFileLink_EmptyAndNoHash(t *testing.T) {
	for _, s := range []string{"", "a.go", "src/foo bar.go", "https://example.com/x"} {
		if got := NormalizeFileLink(s); got != s {
			t.Errorf("NormalizeFileLink(%q) = %q, want unchanged", s, got)
		}
	}
}

// TestMarkdownFileLink verifies the wrapper produces a well-formed
// markdown link with the URL already in the percent-encoded form.
func TestMarkdownFileLink(t *testing.T) {
	got := MarkdownFileLink("see foo.go:12", "src/foo.go", 12)
	want := "[see foo.go:12](src/foo.go%23L12)"
	if got != want {
		t.Errorf("MarkdownFileLink = %q, want %q", got, want)
	}
	// With line == 0 the URL has no fragment.
	if got := MarkdownFileLink("open foo.go", "src/foo.go", 0); got != "[open foo.go](src/foo.go)" {
		t.Errorf("MarkdownFileLink(no line) = %q", got)
	}
}

// TestHasLineAnchor discriminates between URLs that look like file
// links and URLs that just happen to contain `#`. The matcher only
// fires on `#L<digits>`.
func TestHasLineAnchor(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"a.go#L12", true},
		{"a.go%23L12", true}, // already-encoded form still detectable
		{"a.go#L10-L20", true},
		{"a.go#L0", false}, // not L<digits>
		{"https://example.com#section", false},
		{"https://example.com", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.s, func(t *testing.T) {
			if got := HasLineAnchor(tc.s); got != tc.want {
				t.Errorf("HasLineAnchor(%q) = %v, want %v", tc.s, got, tc.want)
			}
		})
	}
}

// TestNormalizeFileLink_LongLine ensures we don't truncate long
// line numbers. A test file with thousands of lines is realistic
// in the codebase (cycle reports are well past 2000 lines).
func TestNormalizeFileLink_LongLine(t *testing.T) {
	s := "doc.go#L12345"
	if got := NormalizeFileLink(s); got != "doc.go%23L12345" {
		t.Errorf("NormalizeFileLink(%q) = %q", s, got)
	}
}

// TestPathPreservation makes sure the helper never percent-encodes
// the path component itself. The changelog explicitly says only the
// `#` separator gets rewritten.
func TestPathPreservation(t *testing.T) {
	paths := []string{
		"a.go",
		"src/foo.go",
		"src/sub/dir/file.go",
		"my file.go", // spaces — left alone, the AGY 1.3.0 layer is upstream
		"weird#name.go",
	}
	for _, p := range paths {
		got := FormatFileLink(p, 7)
		want := p + "%23L7"
		if got != want {
			t.Errorf("FormatFileLink(%q, 7) = %q, want %q", p, got, want)
		}
		// Same path through NormalizeFileLink when already encoded.
		if NormalizeFileLink(got) != got {
			t.Errorf("NormalizeFileLink(%q) != %q (not idempotent)", got, got)
		}
	}
}

// TestAllPercentEncoded_HasNoRawHash is a defensive guard: every
// output of FormatFileLink must contain "%23" if line > 0, and
// none of them must contain a raw `#`. We rely on this invariant in
// the mdrender integration — if a future refactor reintroduces a
// raw `#`, this test fires before the integration regresses.
func TestAllPercentEncoded_HasNoRawHash(t *testing.T) {
	for _, line := range []int{1, 5, 12, 999, 12345} {
		got := FormatFileLink("src/foo.go", line)
		if !strings.Contains(got, "%23L") {
			t.Errorf("FormatFileLink(src/foo.go, %d) = %q missing %%23L", line, got)
		}
		if strings.Contains(got, "#") {
			t.Errorf("FormatFileLink(src/foo.go, %d) = %q still contains raw #", line, got)
		}
	}
}

// TestFormatFileLinkAt_BasicMarker is the canonical AGY 1.3.3 changelog
// line-60 case: a file link at index 0 has a `?h=0` query suffix that
// makes the URL string distinct from any other URL the editor might
// have seen. The path / fragment portion of the URL still uses the
// line-59 percent-encoded anchor form so the link is functional in
// both editor groups.
func TestFormatFileLinkAt_BasicMarker(t *testing.T) {
	got := FormatFileLinkAt("src/foo.go", 12, 0)
	want := "src/foo.go%23L12?h=0"
	if got != want {
		t.Errorf("FormatFileLinkAt(src/foo.go, 12, 0) = %q, want %q", got, want)
	}
}

// TestFormatFileLinkAt_TwoSameURLDistinct pins the changelog line-60
// invariant the gap is about: two links pointing at the same file and
// line must produce *different* URL strings, so editors / terminals
// keying hover-card state on the URL string treat them as separate
// cards. The two URLs differ only in the `?h=<n>` suffix; the file
// path / anchor is byte-identical.
func TestFormatFileLinkAt_TwoSameURLDistinct(t *testing.T) {
	a := FormatFileLinkAt("src/foo.go", 12, 0)
	b := FormatFileLinkAt("src/foo.go", 12, 1)
	if a == b {
		t.Fatalf("two links at same URL collapsed: a == b == %q", a)
	}
	// The path / anchor must be identical (the link is still
	// functional — both still open src/foo.go at line 12).
	if !strings.HasPrefix(a, "src/foo.go%23L12?") {
		t.Errorf("a lost path/anchor: %q", a)
	}
	if !strings.HasPrefix(b, "src/foo.go%23L12?") {
		t.Errorf("b lost path/anchor: %q", b)
	}
	// ... and the markers themselves are distinct integers.
	if !strings.HasSuffix(a, "?h=0") {
		t.Errorf("a missing ?h=0 marker: %q", a)
	}
	if !strings.HasSuffix(b, "?h=1") {
		t.Errorf("b missing ?h=1 marker: %q", b)
	}
}

// TestFormatFileLinkAt_URLFunctional pins the property the marker
// must not break: the resulting URL still has the canonical
// `path%23L<n>` anchor that the AGY 1.3.3 changelog line 59 fix
// guarantees, so the link still opens the right file at the right
// line in editors that honour the percent-encoded form. The marker is
// an additive suffix, not a replacement.
func TestFormatFileLinkAt_URLFunctional(t *testing.T) {
	cases := []struct {
		path      string
		line      int
		idx       int
		wantPre   string // prefix before '?h='
		wantParam string // marker suffix
	}{
		{"src/foo.go", 12, 0, "src/foo.go%23L12", "?h=0"},
		{"src/foo.go", 1, 5, "src/foo.go%23L1", "?h=5"},
		{"src/foo.go", 0, 2, "src/foo.go", "?h=2"},
		{"src/sub/dir/file.go", 999, 7, "src/sub/dir/file.go%23L999", "?h=7"},
	}
	for _, tc := range cases {
		got := FormatFileLinkAt(tc.path, tc.line, tc.idx)
		if !strings.HasPrefix(got, tc.wantPre) {
			t.Errorf("FormatFileLinkAt(%q,%d,%d) = %q, want prefix %q",
				tc.path, tc.line, tc.idx, got, tc.wantPre)
		}
		if !strings.HasSuffix(got, tc.wantParam) {
			t.Errorf("FormatFileLinkAt(%q,%d,%d) = %q, want suffix %q",
				tc.path, tc.line, tc.idx, got, tc.wantParam)
		}
		// No raw `#` survives — the line-59 invariant must hold
		// even after the marker is added.
		if strings.Contains(got, "#") {
			t.Errorf("FormatFileLinkAt(%q,%d,%d) = %q contains raw #", tc.path, tc.line, tc.idx, got)
		}
	}
}

// TestFormatFileLinkAt_NegativeNoMarker is the explicit opt-out: a
// caller can pass index = -1 to suppress the marker. This is the
// escape hatch for tests that pin a byte-exact URL and for any future
// caller that genuinely wants two links to share a hover card. The
// negative form falls back to FormatFileLink verbatim — same return
// value, byte-for-byte.
func TestFormatFileLinkAt_NegativeNoMarker(t *testing.T) {
	legacy := FormatFileLink("src/foo.go", 12)
	for _, idx := range []int{-1, -100, -2147483648} {
		got := FormatFileLinkAt("src/foo.go", 12, idx)
		if got != legacy {
			t.Errorf("FormatFileLinkAt(_, %d) = %q, want legacy %q", idx, got, legacy)
		}
		if strings.Contains(got, "?h=") {
			t.Errorf("FormatFileLinkAt(_, %d) leaked marker: %q", idx, got)
		}
	}
}

// TestFormatFileLinkAt_DistinctFilesDistinctMarkers covers the case
// where two links point at *different* files (or different lines, or
// both). The marker is appended per link instance, so the URLs differ
// in both the path / anchor AND the `?h=<n>` suffix. The point of the
// test is to confirm that two links with already-distinct paths /
// anchors do not accidentally collapse — i.e. the marker does not
// erase existing path / anchor differences.
func TestFormatFileLinkAt_DistinctFilesDistinctMarkers(t *testing.T) {
	a := FormatFileLinkAt("src/foo.go", 12, 0)
	b := FormatFileLinkAt("src/bar.go", 12, 0)
	c := FormatFileLinkAt("src/foo.go", 99, 0)
	d := FormatFileLinkAt("src/foo.go", 12, 1)

	urls := []string{a, b, c, d}
	for i, x := range urls {
		for j, y := range urls {
			if i == j {
				continue
			}
			if x == y {
				t.Errorf("urls %d and %d collapsed: %q", i, j, x)
			}
		}
	}
	// Every URL in this test must carry a marker (idx >= 0).
	for i, x := range urls {
		if !strings.Contains(x, "?h=") {
			t.Errorf("urls[%d] = %q missing marker", i, x)
		}
	}
}

// TestFormatFileLinkAt_ExistingQuery uses `&` not `?` when the
// underlying URL already carries a query string. This keeps the
// resulting URL a well-formed query string (`?y=1&h=0`, not
// `?y=1?h=0` which most URL parsers reject).
func TestFormatFileLinkAt_ExistingQuery(t *testing.T) {
	got := FormatFileLinkAt("https://example.com/x?y=1", 0, 0)
	want := "https://example.com/x?y=1&h=0"
	if got != want {
		t.Errorf("FormatFileLinkAt(url with ?) = %q, want %q", got, want)
	}
}

// TestMarkdownFileLinkAt_RoundTrip pins the markdown wrapper for the
// changelog line-60 case: the URL string emitted by
// MarkdownFileLinkAt carries the `?h=<n>` marker verbatim, so two
// such calls with the same (path, line) and different indices produce
// two distinct URL strings inside the `[…](…)` syntax. This is the
// layer the /diff preamble calls into.
func TestMarkdownFileLinkAt_RoundTrip(t *testing.T) {
	a := MarkdownFileLinkAt("see foo.go:12", "src/foo.go", 12, 0)
	want := "[see foo.go:12](src/foo.go%23L12?h=0)"
	if a != want {
		t.Errorf("MarkdownFileLinkAt(idx 0) = %q, want %q", a, want)
	}
	b := MarkdownFileLinkAt("see foo.go:12", "src/foo.go", 12, 1)
	if b == a {
		t.Fatalf("MarkdownFileLinkAt same-url different-idx collapsed: %q", a)
	}
	if !strings.Contains(b, "?h=1") {
		t.Errorf("MarkdownFileLinkAt idx 1 missing marker: %q", b)
	}
	// The label is unchanged across calls — only the URL marker
	// distinguishes the two links, which is exactly the changelog's
	// "users can see both URLs even if they're identical" property.
	if !strings.Contains(a, "see foo.go:12") || !strings.Contains(b, "see foo.go:12") {
		t.Errorf("labels lost: a=%q b=%q", a, b)
	}
}

// TestFileLinkHoverParam pins the exported marker name. The constant
// exists so tests and future callers can refer to it without
// duplicating the literal `"h"`. A regression that silently renamed
// the param would change every emitted URL on disk — this test fires
// before that reaches a transcript viewer.
func TestFileLinkHoverParam(t *testing.T) {
	if FileLinkHoverParam != "h" {
		t.Errorf("FileLinkHoverParam = %q, want %q", FileLinkHoverParam, "h")
	}
	got := FormatFileLinkAt("a.go", 1, 0)
	want := "a.go%23L1?" + FileLinkHoverParam + "=0"
	if got != want {
		t.Errorf("FormatFileLinkAt uses %q: got %q, want %q", FileLinkHoverParam, got, want)
	}
}
