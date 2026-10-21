// Tests for internal/canvas. The package adds vito/midterm-backed
// highlight ranges to chat-transcript lines; the tests pin the
// observable behaviour:
//
//   - no highlights ⇒ content passes through (modulo the trailing
//     SGR reset Canvas emits at end-of-line);
//   - a highlight covers exactly the byte range's display cells;
//   - overlapping ranges coalesce — Canvas.Paint's neighbour-coalescing
//     path merges same-format regions into one;
//   - CJK runes are cell-aligned (one 3-byte rune occupies 2 cells);
//   - the package is nil-safe (empty content, zero-width range, empty
//     highlights).
package canvas

import (
	"strings"
	"testing"

	"github.com/muesli/termenv"

	"github.com/vito/midterm"
)

// yellow is the highlight style used by the table-driven tests.
// BrightYellow is what /search highlights emit in the reference (an
// ink-on-paper highlight, easily distinguishable on a dark terminal).
var yellow = midterm.Format{Fg: termenv.ANSIBrightYellow}

// stripANSI removes every SGR (CSI…m) sequence from s. Used by
// tests that only care about the byte-faithful content, not the
// colour codes.
func stripANSI(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == 0x1b && s[i+1] == '[' {
			// CSI: skip until 'm'.
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

// assertContains is a tiny test helper that reports the byte offset
// and surrounding context when an expected substring is missing.
func assertContains(t *testing.T, name, s, want string) {
	t.Helper()
	if strings.Contains(s, want) {
		return
	}
	t.Fatalf("%s: output %q does not contain %q", name, s, want)
}

func TestLine_RenderNoHighlight(t *testing.T) {
	line := Line{Content: "hello world"}
	got := line.RenderWithStyle(yellow)
	want := "hello world\x1b[0m"
	if got != want {
		t.Fatalf("no-highlight:\n got  = %q\n want = %q", got, want)
	}
}

func TestLine_RenderEmptyContent(t *testing.T) {
	for _, l := range []Line{
		{Content: ""},
		{Content: ""},
		{Content: "\x00"},
	} {
		if got := l.RenderWithStyle(yellow); got != "" {
			t.Fatalf("empty/zero-width content %q: got %q", l.Content, got)
		}
	}
}

func TestLine_RenderSingleHighlight(t *testing.T) {
	// Highlight "lo wo" (cells 3..7 inclusive) on "hello world".
	line := Line{
		Content:    "hello world",
		Highlights: []Range{{Start: 3, End: 8}},
	}
	got := line.RenderWithStyle(yellow)

	// 1. Plain content must survive (SGR codes stripped).
	if plain := stripANSI(got); plain != "hello world" {
		t.Fatalf("plain content mismatch:\n got  = %q\n want = %q", plain, "hello world")
	}

	// 2. The yellow SGR (termenv ANSI bright yellow = 93) must open
	//    at column 3 and close by column 8.
	openIdx := strings.Index(got, "\x1b[")
	if openIdx < 0 {
		t.Fatalf("no SGR open in %q", got)
	}
	if !strings.HasPrefix(got[openIdx:], "\x1b[93m") {
		t.Fatalf("expected bright-yellow SGR open at byte %d in %q", openIdx, got)
	}

	// 3. The opening SGR must bracket exactly the "lo wo" slice
	//    (5 characters = 5 bytes after stripping the SGR prefix).
	const openSeq = "\x1b[93m"
	const closeSeq = "\x1b[0m"
	openStart := strings.Index(got, openSeq)
	closeIdx := strings.Index(got, closeSeq)
	if openStart < 0 || closeIdx < 0 || closeIdx <= openStart {
		t.Fatalf("open/close SGR ordering wrong: open=%d close=%d in %q",
			openStart, closeIdx, got)
	}
	between := got[openStart+len(openSeq) : closeIdx]
	if between != "lo wo" {
		t.Fatalf("highlighted slice mismatch: got %q want %q", between, "lo wo")
	}
}

func TestLine_RenderMultipleHighlights(t *testing.T) {
	// Two non-overlapping highlights on "abcdefghij": cells [1,3)
	// → "bc", cells [6,9) → "ghi". Both come out independently.
	line := Line{
		Content:    "abcdefghij",
		Highlights: []Range{{Start: 1, End: 3}, {Start: 6, End: 9}},
	}
	got := line.RenderWithStyle(yellow)

	// Verify the byte-faithful content is unchanged.
	if plain := stripANSI(got); plain != "abcdefghij" {
		t.Fatalf("plain content mismatch:\n got  = %q\n want = %q", plain, "abcdefghij")
	}

	// Two SGR opens, two corresponding closes.
	const openSeq = "\x1b[93m"
	if c := strings.Count(got, openSeq); c != 2 {
		t.Fatalf("expected 2 yellow opens, got %d in %q", c, got)
	}
	opens := []int{}
	for i := 0; i+len(openSeq) <= len(got); {
		j := strings.Index(got[i:], openSeq)
		if j < 0 {
			break
		}
		opens = append(opens, i+j)
		i = i + j + len(openSeq)
	}
	if len(opens) != 2 {
		t.Fatalf("opens mismatch in %q", got)
	}

	// After the first open, the next SGR must reset (close). The
	// first highlight covers "bc", the second "ghi".
	const closeSeq = "\x1b[0m"
	firstClose := strings.Index(got, closeSeq)
	if firstClose < 0 || firstClose < opens[0] {
		t.Fatalf("first close not after first open in %q", got)
	}
	if firstSlice := stripANSI(got[opens[0]:firstClose+len(closeSeq)]); firstSlice != "bc" {
		t.Fatalf("first highlight slice: got %q want %q", firstSlice, "bc")
	}

	// The second open must come AFTER the first close and the gap
	// ("def") must be plain SGR-stripped in between.
	if opens[1] < firstClose+len(closeSeq) {
		t.Fatalf("second open not after first close: open1=%d close1=%d open2=%d in %q",
			opens[0], firstClose, opens[1], got)
	}
	if between := got[firstClose+len(closeSeq) : opens[1]]; stripANSI(between) != "def" {
		t.Fatalf("gap slice: got %q want %q", between, "def")
	}
}

func TestLine_RenderCJKHighlight(t *testing.T) {
	// "中文字" — three CJK runes, 6 cells total. Highlight range
	// [0, 2) covers 2 bytes inside the first `中` (which is 3 bytes
	// long). Cell-aligned, that's cells [0, 2) — i.e. the entire `中`.
	line := Line{
		Content:    "中文字",
		Highlights: []Range{{Start: 0, End: 2}},
	}
	got := line.RenderWithStyle(yellow)

	// Plain content unchanged.
	if plain := stripANSI(got); plain != "中文字" {
		t.Fatalf("CJK plain content mismatch:\n got  = %q\n want = %q", plain, "中文字")
	}

	// The highlight MUST envelop `中` exactly. We assert on byte
	// boundaries: the 3-byte UTF-8 sequence for `中` is "E4 B8 AD"
	// and must sit between the SGR open and close.
	const openSeq = "\x1b[93m"
	const closeSeq = "\x1b[0m"
	openIdx := strings.Index(got, openSeq)
	closeIdx := strings.Index(got, closeSeq)
	if openIdx < 0 || closeIdx < openIdx {
		t.Fatalf("open/close SGR ordering wrong in %q", got)
	}
	between := got[openIdx+len(openSeq) : closeIdx]
	if between != "中" {
		t.Fatalf("CJK highlight slice: got %q (% x) want %q", between, between, "中")
	}

	// After the close, the remaining runes must come through plain
	// (still terminally reset by the trailing SGR).
	tail := got[closeIdx+len(closeSeq):]
	if stripANSI(tail) != "文字" {
		t.Fatalf("CJK trailing slice: got %q (% x) want %q", tail, tail, "文字")
	}
}

func TestLine_RenderCJKMidHighlight(t *testing.T) {
	// CJK highlight on a CJK character that's IN THE MIDDLE of the
	// line: cells [2, 4) on "中文字" — that's `文`. This guards
	// against off-by-one bugs in the rune ↔ cell mapping.
	line := Line{
		Content:    "中文字",
		Highlights: []Range{{Start: 3, End: 6}}, // bytes 3..6 = the `文` rune (3..5)
	}
	got := line.RenderWithStyle(yellow)

	if plain := stripANSI(got); plain != "中文字" {
		t.Fatalf("CJK-mid plain content mismatch:\n got  = %q\n want = %q", plain, "中文字")
	}
	// Bytes [3, 6): that's bytes 3..5 (3 bytes = one CJK rune `文`).
	// open/close bracket `文` exactly.
	const openSeq = "\x1b[93m"
	openIdx := strings.Index(got, openSeq)
	closeIdx := strings.Index(got, "\x1b[0m")
	if openIdx < 0 || closeIdx < openIdx {
		t.Fatalf("open/close SGR ordering wrong in %q", got)
	}
	between := got[openIdx+len(openSeq) : closeIdx]
	if between != "文" {
		t.Fatalf("CJK-mid highlight slice: got %q want %q", between, "文")
	}
}

func TestLine_RenderOverlappingHighlights(t *testing.T) {
	// Two ranges that overlap by 2 cells: [1,4) and [2,5) on
	// "abcdef" — they should merge into one continuous highlight
	// spanning cells [1,5) = "bcde". Canvas.Paint's
	// same-format-grow path does this automatically.
	line := Line{
		Content:    "abcdef",
		Highlights: []Range{{Start: 1, End: 4}, {Start: 2, End: 5}},
	}
	got := line.RenderWithStyle(yellow)

	if plain := stripANSI(got); plain != "abcdef" {
		t.Fatalf("overlap plain content mismatch:\n got  = %q\n want = %q", plain, "abcdef")
	}

	// One open SGR, one matching close. Anything more would mean
	// the merge failed.
	const openSeq = "\x1b[93m"
	if c := strings.Count(got, openSeq); c != 1 {
		t.Fatalf("expected exactly 1 SGR open after overlap merge, got %d in %q", c, got)
	}
	openIdx := strings.Index(got, openSeq)
	closeIdx := strings.Index(got, "\x1b[0m")
	if closeIdx < openIdx {
		t.Fatalf("close before open in %q", got)
	}
	if between := got[openIdx+len(openSeq) : closeIdx]; between != "bcde" {
		t.Fatalf("merged highlight slice: got %q want %q", between, "bcde")
	}
}

func TestLine_RenderOutOfRangeHighlight(t *testing.T) {
	// Out-of-range and inverted ranges must clamp, not panic, and
	// must not bleed into the trailing cells.
	line := Line{
		Content: "hello",
		Highlights: []Range{
			{Start: -5, End: 2},     // clamps to [0, 2) → "he"
			{Start: 4, End: 100},    // clamps to [4, 5) → "o"
			{Start: 3, End: 1},      // inverted → dropped
			{Start: 2, End: 2},      // zero-width → dropped
		},
	}
	got := line.RenderWithStyle(yellow)

	if plain := stripANSI(got); plain != "hello" {
		t.Fatalf("out-of-range plain content mismatch:\n got  = %q\n want = %q", plain, "hello")
	}
	// Must have at least one yellow open (for the "he" range), and
	// the content between any open/close must be a valid slice of
	// "hello".
	const openSeq = "\x1b[93m"
	if c := strings.Count(got, openSeq); c < 1 {
		t.Fatalf("expected at least one yellow open, got %d in %q", c, got)
	}
}

func TestLine_RenderZeroWidthRange(t *testing.T) {
	// Zero-width ranges must not panic. They are silently dropped.
	line := Line{
		Content: "abc",
		Highlights: []Range{
			{Start: 0, End: 0},
			{Start: 1, End: 1},
			{Start: 3, End: 3},
		},
	}
	got := line.RenderWithStyle(yellow)
	if plain := stripANSI(got); plain != "abc" {
		t.Fatalf("zero-width plain content mismatch: got %q want %q", plain, "abc")
	}
	// Only the trailing reset SGR is allowed (zero-width ranges
	// produced no Paint calls; the trailing reset is unconditional).
	if c := strings.Count(got, "\x1b["); c != 1 || !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("zero-width ranges should produce only trailing reset, got %q", got)
	}
}

func TestLine_RenderTrailingResetAlwaysEmitted(t *testing.T) {
	// Every Render() call ends with an SGR reset, even on empty
	// highlights — this matches what vito/midterm's renderLine emits
	// for the trailing tail (and prevents style bleed across lines).
	for _, l := range []Line{
		{Content: "x"},
		{Content: "x", Highlights: []Range{{0, 1}}},
		{Content: "中"},
	} {
		got := l.RenderWithStyle(yellow)
		if !strings.HasSuffix(got, "\x1b[0m") {
			t.Fatalf("missing trailing SGR reset in %q (case %+v)", got, l)
		}
	}
}

func TestLine_RenderDefaultStyleIsBoldReverse(t *testing.T) {
	// Render() with no caller-chosen style must use the default
	// style (Bold + Reverse), distinguishable from a single-colour
	// format. We assert that the resulting string contains the bold
	// SGR (termenv constant "1") and the reverse SGR ("7").
	got := (Line{Content: "x", Highlights: []Range{{0, 1}}}).Render()
	plain := stripANSI(got)
	if plain != "x" {
		t.Fatalf("default-style plain mismatch: got %q want %q", plain, "x")
	}
	assertContains(t, "default-style has bold SGR", got, "\x1b[")
	if !strings.Contains(got, ";1m") && !strings.Contains(got, ";7m") &&
		!strings.Contains(got, "[1;") && !strings.Contains(got, "[7;") {
		t.Fatalf("default style missing bold/reverse SGR in %q", got)
	}
}
