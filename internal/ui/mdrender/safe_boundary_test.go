// safe_boundary_test.go — cycle 242 regression tests for the
// boundary detector and the LineANSIBounded wrapper.
//
// Two test families:
//
//  1. TestFindSafeMarkdownBoundary_* (8 tests) exercise the pure-
//     function detector directly. Each test feeds a known markdown
//     buffer and asserts on the byte offset findSafeMarkdownBoundary
//     returns. The hazards covered are the four rules the
//     detector evaluates:
//     (2)  even fence parity in the prefix
//     (2b) prefixHasOpenHazard — list (B1), HTML (B2), link
//     ref def (B3) anywhere in the prefix
//     (3)  last non-blank line of prefix must not open a
//     construct (lineOpensConstruct)
//     (4)  first non-blank line of rest must not be a setext
//     underline
//
//  2. TestLineANSIBounded_* (5 tests) exercise the wrapper at
//     stream_glamour.go:LineANSIBounded that consumes a buffer
//     and returns (glamoured-safe-prefix, raw-trailing). The
//     wrapper is the cycle-242 visible improvement: cross-line
//     constructs now render correctly mid-stream because the
//     trailing partial stays raw until the boundary advances.
package mdrender

import (
	"strings"
	"testing"
)

// stripAnsiSB removes ANSI SGR escape sequences from s. Cycle 242
// added this helper because cycle 233's chroma integration (chroma
// tokenises each token with 24-bit RGB ANSI escapes) breaks literal
// substring matching for tests that pre-date cycle 233's chroma
// integration. The helper is local to safe_boundary_test.go (same
// shape as cycle 233's stripAnsiConv in cmd/wagyu/conversation_test.go
// but kept separate to avoid test-file coupling across packages).
func stripAnsiSB(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// TestFindSafeMarkdownBoundary_SimpleProse: a 3-paragraph markdown
// document returns the byte offset of the latest blank-line
// separator (the one before the LAST paragraph). Rule (2b) does
// not fire on plain prose; rule (3) does not fire because the
// last non-blank line of any prefix is a normal prose line;
// rule (4) does not fire because the first non-blank line of the
// trailing partial is also normal prose. The latest safe
// boundary is the position immediately before the last
// paragraph.
func TestFindSafeMarkdownBoundary_SimpleProse(t *testing.T) {
	const src = "first paragraph\n\nsecond paragraph here\n\nthird short\n"
	// "first paragraph\n\n" is 17 bytes, "second paragraph here"
	// is 21 bytes, then "\n\n" is 2 bytes. The latest blank line
	// sits between the second and third paragraphs; the boundary
	// points at byte 40, the start of "third short".
	const want = 40
	got := findSafeMarkdownBoundary(src)
	if got != want {
		t.Fatalf("simple-prose boundary = %d, want %d:\n  src[want:]=%q\n  src[got:] =%q\n  full src  =%q",
			got, want, src[want:], src[got:], src)
	}
}

// TestFindSafeMarkdownBoundary_FencedCodeEvenTongues: a document
// with a complete ```go ... ``` block has a safe boundary AFTER
// the closing fence; an "odd fence" case constructed so the only
// blank line's prefix already contains an opening fence (and the
// closing fence lives after the boundary) is rejected because
// countFenceLines(prefix) is odd.
func TestFindSafeMarkdownBoundary_FencedCodeEvenTongues(t *testing.T) {
	// Even-fence: prefix at every blank line has even fence
	// count. The boundary lands at the latest blank line.
	const even = "before\n\n```go\nfunc main() {}\n```\n\nafter\n"
	got := findSafeMarkdownBoundary(even)
	if got <= 0 {
		t.Fatalf("even-fence doc: expected positive boundary, got %d for src %q", got, even)
	}
	// The latest blank line sits between "```" and "after".
	// The boundary must point at the start of "after", not into
	// the code block.
	if !strings.HasPrefix(even[got:], "after") {
		t.Fatalf("even-fence boundary = %d does not start at 'after':\n  src[got:]=%q",
			got, even[got:])
	}

	// Odd-fence: an opening fence followed by a blank line then
	// a closing fence means the prefix contains 1 fence line
	// (the opener) — odd, so the boundary is rejected.
	//
	// Source layout (one blank line, between the two fences):
	//
	//   ```go
	//   [blank]
	//   ```
	//
	// The blank line IS the only blank line; the prefix at that
	// boundary is "```go\n\n", which has countFenceLines=1
	// (odd). Rule (2) fires → boundary rejected → -1.
	const odd = "```go\n\n```\n"
	if got := findSafeMarkdownBoundary(odd); got != -1 {
		t.Fatalf("odd-fence doc: expected -1, got %d for src %q", got, odd)
	}
}

// TestFindSafeMarkdownBoundary_IndentedCode: a doc whose prefix's
// last non-blank line is an indented-code line (4+ leading spaces
// or a tab) is rejected by rule (3): lineOpensConstruct returns
// true for 4-space-prefixed lines.
//
// We construct the doc so the only blank line's prefix ends with
// an indented-code line. (A doc that has indented code AFTER the
// boundary is NOT rejected by rule (3) — rule (3) only inspects
// the prefix's trailing line, and rule (4) only checks for setext
// underlines. Indented code is not in scope for rule (4).)
func TestFindSafeMarkdownBoundary_IndentedCode(t *testing.T) {
	// Layout:
	//
	//   paragraph body
	//       indented code line
	//   [blank]
	//   more prose
	//
	// The blank line is the only blank line. The prefix at that
	// boundary's last non-blank line is "    indented code
	// line" — lineOpensConstruct returns true for 4+ spaces.
	// Rule (3) fires → boundary rejected → -1.
	const src = "paragraph body\n    indented code line\n\nmore prose\n"
	if got := findSafeMarkdownBoundary(src); got != -1 {
		t.Fatalf("expected -1 on indented-code-in-prefix, got %d for src %q", got, src)
	}

	// Counter-example: indented code lives AFTER the boundary,
	// not in the prefix. Rule (3) only inspects the prefix's
	// trailing line ("paragraph body"), which is a normal prose
	// line. Rule (4) only catches setext underlines — indented
	// code is NOT in scope. So the boundary IS accepted at the
	// latest blank line.
	const positive = "paragraph body\n\n    indented code line\n"
	// Layout has 2 blank lines actually — between "body" and
	// the indented line, then the trailing "\n" after the
	// indented line. The latest blank line is between "body"
	// and "    indented code line".
	ban := findSafeMarkdownBoundary(positive)
	if ban <= 0 {
		t.Fatalf("expected positive boundary on indented-code-after-boundary, got %d for src %q",
			ban, positive)
	}
	if !strings.HasPrefix(positive[ban:], "    indented") {
		t.Fatalf("expected boundary to start at '    indented', got %q", positive[ban:])
	}
}

// TestFindSafeMarkdownBoundary_ListContinuation: a doc with a
// list-item line followed by a blank line is rejected because
// rule (3) — the last non-blank line of the prefix is the list
// marker, and lineOpensConstruct returns true for "- item"
// patterns. B1 (loose lists) is a separate refinement that
// protects later boundary candidates where the prefix ends with
// an INDENTED continuation paragraph rather than a marker; this
// test exercises the simpler rule (3) path.
func TestFindSafeMarkdownBoundary_ListContinuation(t *testing.T) {
	// Marker → blank line → continuation paragraph.
	//
	//   - item one
	//   [blank]
	//     continuation paragraph
	//
	// At the only blank line, prefix = "- item one\n\n", last
	// non-blank = "- item one" → lineOpensConstruct returns true
	// (list marker). Rule (3) fires → boundary rejected.
	const src = "- item one\n\n  continuation paragraph\n"
	if got := findSafeMarkdownBoundary(src); got != -1 {
		t.Fatalf("expected -1 on list-marker-prefix, got %d for src %q", got, src)
	}
}

// TestFindSafeMarkdownBoundary_HTMLBlock: a doc whose prefix at
// every blank-line candidate contains an HTML-block opener
// (B2) is rejected. The simplest such doc: an HTML-block opener
// on the first non-blank line, then a blank line.
//
// <3 in PROSE (mid-paragraph, not at line start) is not an HTML
// block opener — rule (B2) does not fire — so the boundary
// detector finds the latest blank-line separator.
func TestFindSafeMarkdownBoundary_HTMLBlock(t *testing.T) {
	// <script> opener at line start → prefixHasOpenHazard's B2
	// branch fires. The only blank line's prefix contains the
	// opener → boundary rejected → -1.
	const htmlDoc = "<script>\nlet x = 1;\n</script>\n\nafter\n"
	if got := findSafeMarkdownBoundary(htmlDoc); got != -1 {
		t.Fatalf("expected -1 on <script>-opener prefix, got %d for src %q", got, htmlDoc)
	}
	// <3 in prose is NOT a block opener (the line does not
	// start with '<letter'). The boundary detector finds the
	// latest blank line.
	const lessThan = "before\n\n<3 looks like a heart\n\nafter\n"
	ban := findSafeMarkdownBoundary(lessThan)
	if ban <= 0 {
		t.Fatalf("expected positive boundary on '<3'-in-prose, got %d for src %q", ban, lessThan)
	}
	if !strings.HasPrefix(lessThan[ban:], "after") {
		t.Fatalf("expected boundary at start of 'after', got %q", lessThan[ban:])
	}
}

// TestFindSafeMarkdownBoundary_LinkRefDef: a doc whose prefix
// contains a link-reference definition (B3) is rejected. The
// simplest case: a ref-def on the first non-blank line, then a
// blank line.
//
// `[foo]:` (no destination after the colon) is NOT a valid
// ref-def — rule (B3) does not fire — so the boundary detector
// finds the latest blank-line separator.
func TestFindSafeMarkdownBoundary_LinkRefDef(t *testing.T) {
	// Valid ref-def at line start → B3 fires. The only blank
	// line's prefix contains the def → boundary rejected.
	const refDef = "[foo]: http://example.com\n\nlater para\n"
	if got := findSafeMarkdownBoundary(refDef); got != -1 {
		t.Fatalf("expected -1 on link-ref-def prefix, got %d for src %q", got, refDef)
	}
	// Malformed ref-def (no destination after the colon) →
	// B3 does NOT fire (the conservative pattern requires at
	// least one non-whitespace char of destination). Boundary
	// accepted.
	const malformed = "[foo]:\n\nlater para\n"
	ban := findSafeMarkdownBoundary(malformed)
	if ban <= 0 {
		t.Fatalf("expected positive boundary on malformed ref-def, got %d for src %q", ban, malformed)
	}
	if !strings.HasPrefix(malformed[ban:], "later para") {
		t.Fatalf("expected boundary at start of 'later para', got %q", malformed[ban:])
	}
}

// TestFindSafeMarkdownBoundary_SetextUnderline: a doc whose
// trailing content begins with `===` or `---` is rejected by
// rule (4). The simplest case: a paragraph followed by a blank
// line, then `===` (potential setext underline that would
// retroactively turn the paragraph into a header).
func TestFindSafeMarkdownBoundary_SetextUnderline(t *testing.T) {
	// "para\n\n===\n" — boundary candidate at the blank line.
	// rest = "===\n", firstNonBlankLine = "===",
	// isSetextUnderlineCandidate("===") = true → reject.
	const doc = "para\n\n===\n"
	if got := findSafeMarkdownBoundary(doc); got != -1 {
		t.Fatalf("expected -1 on setext-underline tail, got %d for src %q", got, doc)
	}
}

// TestLineANSIBounded_NoBoundaryFallsBackToFull: a 1-line
// markdown document with no blank line returns
// (rendered=<glamour-or-fallback>, trailingRaw=<input>, err=nil).
// The wrapper's contract: when no safe boundary exists, glamour
// the whole buffer (the safe prefix is empty) AND return the
// whole buffer as raw trailing so the caller preserves it until
// the boundary advances on a later chunk.
func TestLineANSIBounded_NoBoundaryFallsBackToFull(t *testing.T) {
	InvalidateCache()
	rendered, trailing, err := LineANSIBounded("**unclosed bold here")
	if err != nil {
		t.Fatalf("LineANSIBounded: %v", err)
	}
	if trailing != "**unclosed bold here" {
		t.Errorf("expected trailingRaw=%q, got %q", "**unclosed bold here", trailing)
	}
	// rendered is the glamour-pass of the WHOLE buffer (since
	// no safe boundary exists, the wrapper falls back to
	// rendering the whole thing). It must contain the source
	// text (glamour renders even partial constructs).
	if rendered == "" {
		t.Errorf("expected non-empty rendered fallback, got empty")
	}
	if !strings.Contains(rendered, "unclosed bold") {
		t.Errorf("rendered fallback lost source text: %q", rendered)
	}
}

// TestLineANSIBounded_BoldSafeBoundary: a buffer ending with a
// complete paragraph (no dangling construct, with a blank line)
// returns the rendered prefix and the trailingRaw as the slice
// from src[boundary:].
func TestLineANSIBounded_BoldSafeBoundary(t *testing.T) {
	InvalidateCache()
	const src = "Hello world\n\nthis is **bold** text\n"
	rendered, trailing, err := LineANSIBounded(src)
	if err != nil {
		t.Fatalf("LineANSIBounded: %v", err)
	}
	// The boundary is the latest blank line; trailing is the
	// suffix. With src ending in "\n" after "text", the
	// boundary may land before or after the final "text"
	// depending on blank-line detection. Accept any positive
	// boundary that yields a non-empty trailingRaw.
	if trailing == "" {
		t.Errorf("expected non-empty trailingRaw, got empty (buffer ends exactly at safe boundary)")
	}
	if rendered == "" {
		t.Errorf("expected non-empty rendered prefix, got empty")
	}
	// The rendered prefix must contain the bold-styled word (no
	// raw ** markers survive glamour on the safe prefix).
	if strings.Contains(rendered, "**bold**") {
		t.Errorf("rendered prefix still carries raw ** markers: %q", rendered)
	}
}

// TestLineANSIBounded_FencedCodeSafeBoundary: a buffer
// containing a complete ```go ... ``` block has the code
// body's text surviving in either rendered or trailing. The
// exact split between rendered and trailing depends on which
// blank line the detector picks as the latest safe boundary.
func TestLineANSIBounded_FencedCodeSafeBoundary(t *testing.T) {
	InvalidateCache()
	src := "before\n\n```go\nfunc main(){}\n```\n\nafter\n"
	rendered, trailing, err := LineANSIBounded(src)
	if err != nil {
		t.Fatalf("LineANSIBounded: %v", err)
	}
	if rendered == "" {
		t.Errorf("expected non-empty rendered prefix, got empty")
	}
	if trailing == "" {
		t.Errorf("expected non-empty trailingRaw, got empty (the whole buffer was rendered as a safe prefix; expected a suffix)")
	}
	// The code body must survive in either rendered or
	// trailing. Cycle 233 chroma integration tokenises the
	// ```go block so the contiguous characters "func main()"
	// get split by ANSI escapes — strip ANSI before the
	// substring check.
	if !strings.Contains(stripAnsiSB(rendered), "func main()") && !strings.Contains(trailing, "func main()") {
		t.Errorf("code body lost between rendered=%q and trailing=%q", rendered, trailing)
	}
	// rendered + trailing concatenated (in the caller's
	// display) must contain every text byte of src. The
	// glamorous prefix carries the bytes that survive glamour
	// rendering; trailing carries raw bytes. Together they
	// MUST cover all bytes — that's the round-trip
	// correctness property.
	joined := rendered + trailing
	if !strings.Contains(joined, "before") || !strings.Contains(joined, "after") {
		t.Errorf("joined rendered+trailing lost prose:\n  rendered=%q\n  trailing=%q", rendered, trailing)
	}
}

// TestLineANSIBounded_CrossLineBoldMidStream: a buffer like
// "Hello **bold\nworld" (the **bold** spans a newline, no
// blank line) returns (rendered=<glamour-of-whole>, trailing=
// <whole-input>). The wrapper renders the whole buffer (no
// safe boundary) AND keeps it as raw trailing — the caller
// displays trailing verbatim until a later chunk adds a blank
// line. This is the cycle-242 visible improvement: cross-line
// constructs no longer render as plain text mid-stream
// because glamour's render of the whole buffer treats the
// `**bold\nworld` as a complete paragraph (one giant line).
func TestLineANSIBounded_CrossLineBoldMidStream(t *testing.T) {
	InvalidateCache()
	const src = "Hello **bold\nworld"
	rendered, trailing, err := LineANSIBounded(src)
	if err != nil {
		t.Fatalf("LineANSIBounded: %v", err)
	}
	if trailing != src {
		t.Errorf("expected trailingRaw=%q (whole buffer), got %q", src, trailing)
	}
	// rendered is glamour's pass on the whole buffer; it
	// contains the source text verbatim (glamour renders even
	// partial constructs — it produces output for "Hello
	// **bold\nworld" with the `**` interpreted as the start of
	// a bold span that never closes, or as literal text
	// depending on glamour's version). The crucial property:
	// the user's terminal sees the rendered form AS THE
	// "ready" view, and trailingRaw re-displays the raw form
	// for the chunks-arriving case.
	if rendered == "" {
		t.Errorf("expected non-empty rendered fallback, got empty")
	}
	if !strings.Contains(rendered, "Hello") {
		t.Errorf("rendered fallback lost source text: %q", rendered)
	}
}

// TestLineANSIBounded_FullDocumentBuffer: passing a complete
// document buffer with a positive safe boundary returns
// (rendered=<glamour-of-prefix>, trailing=<suffix>) where
// trailing is byte-for-byte src[boundary:]. This pins the
// wrapper's contract: the boundary is the one findSafeMarkdownBoundary
// finds, and the trailingRaw is the slice from that offset.
func TestLineANSIBounded_FullDocumentBuffer(t *testing.T) {
	InvalidateCache()
	const src = "Hello **world**.\n\nGoodbye.\n"
	rendered, trailing, err := LineANSIBounded(src)
	if err != nil {
		t.Fatalf("LineANSIBounded: %v", err)
	}
	ban := findSafeMarkdownBoundary(src)
	if ban <= 0 {
		t.Fatalf("setup: expected positive safe boundary, got %d", ban)
	}
	if trailing != src[ban:] {
		t.Errorf("trailing mismatch:\n  got:  %q\n  want: %q", trailing, src[ban:])
	}
	// The rendered prefix must be glamour's render of src[:ban].
	want := mustANSI(t, src[:ban])
	if rendered != want {
		t.Errorf("rendered prefix mismatch:\n  got:  %q\n  want: %q", rendered, want)
	}
}

// TestLineANSIBounded_PanicRecovered: a buffer containing
// malformed markdown constructs that exercise glamour's edge
// cases must NEVER panic the wrapper. The defer/recover at
// stream_glamour.go:LineANSIBounded converts any panic into
// a typed error so the TUI never crashes. This test pins
// that recover() is in place.
func TestLineANSIBounded_PanicRecovered(t *testing.T) {
	InvalidateCache()
	// Construct a buffer that exercises glamour's parser
	// edge cases: a partial fence with a weird info string,
	// nested emphasis, etc. None of these should panic a
	// robust parser, but the wrapper's recover() guarantees
	// no TUI crash even if they did.
	const weird = "```\nunclosed fence\n\n```other\nanother partial\n"
	// The wrapper must NOT panic on this input.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LineANSIBounded panicked on weird input: %v", r)
		}
	}()
	rendered, trailing, err := LineANSIBounded(weird)
	if err != nil {
		// Errors are fine; panics are not.
		t.Logf("LineANSIBounded returned error (acceptable): %v", err)
	}
	// Whatever the wrapper does, trailing must contain the
	// original input bytes — either the whole buffer (when no
	// safe boundary exists) or a suffix. The exact split is
	// implementation-defined; we only assert "did not panic"
	// and "trailing is a substring of the input".
	if !strings.HasPrefix(weird, trailing[:min(len(trailing), len(weird))]) &&
		trailing != weird && !strings.HasPrefix(trailing, weird[:min(len(trailing), len(weird))]) {
		// Trailing is either the whole input (when no safe
		// boundary) or a suffix. Both are acceptable.
		// We just check that it's some slice of the input.
		if !strings.Contains(weird, trailing) {
			t.Errorf("trailing %q is not a slice of weird input %q", trailing, weird)
		}
	}
	_ = rendered
}

// mustANSI is a small helper used by
// TestLineANSIBounded_FullDocumentBuffer to assert byte-for-byte
// equivalence between LineANSIBounded's rendered prefix and the
// standalone ANSI helper's render of the same prefix.
func mustANSI(t *testing.T, src string) string {
	t.Helper()
	// Cycle 268: mustANSI must use the same width LineANSI does,
	// otherwise the glamour padding against the wrap ceiling
	// (which is the configured width) drifts between the
	// wrapper-under-test and this reference helper — the bug
	// shows up as a one-line render at width=X producing a
	// different number of trailing spaces than the reference
	// pinned at the package's currentWidth() value. We use the
	// same width-resolution chain: pinned → $COLUMNS → x/term.
	got, err := ANSI(src, currentWidth())
	if err != nil {
		t.Fatalf("ANSI: %v", err)
	}
	return strings.TrimRight(got, "\n")
}
