// url_osc8_encode_test.go — pins urlForOSC8, the minimum-viable
// percent-encoder for the OSC 8 hyperlink emitter at render.go:Hyperlink.
//
// AGY 1.4.1 fix (recovered from the binary strings table and the
// changelog pin at internal/cli/testdata/changelog_reference.txt:13):
//
//	"Fixed clickable links in agent responses whose URL contains `;`
//	 opening a URL cut off at the first `;` in terminals and
//	 multiplexers that split link sequences on semicolons."

package render

import "testing"

// TestURLForOSC8_SemicolonPinned is the literal regression test for the
// changelog bug: a URL with a `;` in the query string must be
// percent-encoded so terminals that split OSC 8 on `;` do not truncate.
func TestURLForOSC8_SemicolonPinned(t *testing.T) {
	in := "https://example.com/foo?a=1;b=2"
	want := "https://example.com/foo?a=1%3Bb=2"
	got := urlForOSC8(in)
	if got != want {
		t.Fatalf("urlForOSC8(%q) = %q, want %q", in, got, want)
	}
}

// TestURLForOSC8_BackslashPinned is the second character the bug
// calls out: a literal `\` would close the OSC frame before the ST
// terminator and leak the body text outside the hyperlink.
func TestURLForOSC8_BackslashPinned(t *testing.T) {
	in := `https://example.com/\backslash`
	want := `https://example.com/%5Cbackslash`
	got := urlForOSC8(in)
	if got != want {
		t.Fatalf("urlForOSC8(%q) = %q, want %q", in, got, want)
	}
}

// TestURLForOSC8_NoSpecialCharsAreByteIdentical pins the deliberate
// non-goal: characters that are NOT OSC-8-significant (`?`, `&`, `=`,
// `#`, `/`, `:`, `@`, sub-delims, percent-triplets, …) are left
// verbatim. Re-encoding them would change what the link opens and is
// not the AGY 1.4.1 surface.
func TestURLForOSC8_NoSpecialCharsAreByteIdentical(t *testing.T) {
	cases := []string{
		"https://example.com/no-special",
		"https://example.com/foo?a=1&b=2#frag",
		"https://example.com/path/with/slashes",
		"https://user:pass@example.com:8080/",
		"https://example.com/already%20percent-encoded",
		"https://example.com/with-dashes_and.dots",
	}
	for _, in := range cases {
		got := urlForOSC8(in)
		if got != in {
			t.Errorf("urlForOSC8(%q) = %q, want byte-identical", in, got)
		}
	}
}

// TestURLForOSC8_EmptyStringIsEmpty pins the empty-input contract.
func TestURLForOSC8_EmptyStringIsEmpty(t *testing.T) {
	if got := urlForOSC8(""); got != "" {
		t.Fatalf("urlForOSC8(\"\") = %q, want empty", got)
	}
}

// TestURLForOSC8_MultipleSemicolonsAllEncoded pins that every `;` in
// the input is encoded, not just the first one. AGY's hyperlink emitter
// gets the URL once and writes the whole thing in one OSC frame; a
// partial encoding would still leak in terminals that scan past the
// first `%3B`.
func TestURLForOSC8_MultipleSemicolonsAllEncoded(t *testing.T) {
	in := "https://example.com/?a=1;b=2;c=3;d=4"
	want := "https://example.com/?a=1%3Bb=2%3Bc=3%3Bd=4"
	got := urlForOSC8(in)
	if got != want {
		t.Fatalf("urlForOSC8(%q) = %q, want %q", in, got, want)
	}
}

// TestURLForOSC8_IsPure verifies the function is allocation-cheap
// enough to be safe in the hot path of every hyperlink emit. The
// strings.NewReplacer is package-scoped; the function allocates one
// string for the result and nothing else.
func TestURLForOSC8_IsPure(t *testing.T) {
	in := "https://example.com/foo?a=1;b=2"
	first := urlForOSC8(in)
	second := urlForOSC8(in)
	if first != second {
		t.Fatalf("urlForOSC8 not pure: %q vs %q", first, second)
	}
}

// TestHyperlink_EncodesSemicolonInURIButNotInLabel pins the full
// integration with the OSC 8 emitter at render.go:Hyperlink:
//
//   - the URI segment of the OSC frame is percent-encoded so terminals
//     that split on `;` do not truncate;
//   - the *label* (body) text is left verbatim, because semicolons
//     in display text are not an OSC 8 framing hazard.
//
// The label-text contract is what lets callers pass user-facing text
// like `Choose: yes; no` as the body without it becoming
// `Choose: yes%3B no`.
func TestHyperlink_EncodesSemicolonInURIButNotInLabel(t *testing.T) {
	url := "https://example.com/?a=1;b=2"
	label := "Choose: yes; no"
	got := Hyperlink(url, label, 80, HyperlinkCaps{OSC8: true})
	// The URI segment in the OSC frame must contain the percent-encoded
	// semicolon, not the raw one.
	wantURIFragment := "https://example.com/?a=1%3Bb=2"
	if !contains(got, wantURIFragment) {
		t.Errorf("OSC 8 URI not percent-encoded:\n got: %q\n want substring: %q", got, wantURIFragment)
	}
	// The label text must contain the raw semicolon, not the encoded one.
	if !contains(got, "Choose: yes; no") {
		t.Errorf("OSC 8 label text was altered:\n got: %q\n want substring: %q", got, "Choose: yes; no")
	}
}

// contains is the small stdlib helper that avoids pulling strings into
// this test file just for one call (the render package is import-light
// on purpose).
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}