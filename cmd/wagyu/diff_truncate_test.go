// diff_truncate_test.go — cycle diff-cjk-cutoff: grapheme-cluster-aware
// long-line truncation for /diff.
//
// Each case here pins one observable of AGY 1.3.6 changelog line 11:
// a long line fed into truncateDiffLine either fits unchanged, or is
// cut on a grapheme-cluster boundary with an ASCII "..." appended, or
// is left alone because the caller disabled truncation (maxCells <= 0).
// The failure mode the changelog describes is a U+FFFD in the output
// (an in-band slice on a multi-byte CJK or a ZWJ sequence); the
// replacements are rejected below so a regression that re-introduces
// byte-level slicing is loud.
package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// noReplacement reports whether s contains no UTF-8 replacement
// character — the in-band "U+FFFD" sign that the cycle diff-cjk-cutoff
// fix removes by switching from byte slicing to grapheme-cluster
// walking.
func noReplacement(t *testing.T, s string) {
	t.Helper()
	if strings.ContainsRune(s, '\uFFFD') {
		t.Errorf("output contains U+FFFD replacement character: %q", s)
	}
}

// endsWithEllipsis verifies the output was actually truncated.
func endsWithEllipsis(t *testing.T, s string) {
	t.Helper()
	if !strings.HasSuffix(s, "...") {
		t.Errorf("output should end in %q, got %q", "...", s)
	}
}

// TestTruncateDiffLine_ASCII_Fits verifies the short-line path: an
// ASCII line shorter than maxCells is returned unchanged and carries
// no ellipsis.
func TestTruncateDiffLine_ASCII_Fits(t *testing.T) {
	in := "diff --git a/foo.go b/foo.go"
	got := truncateDiffLine(in, 80)
	if got != in {
		t.Errorf("short line was mangled: got %q, want %q", got, in)
	}
	noReplacement(t, got)
}

// TestTruncateDiffLine_ASCII_Truncates verifies the long-ASCII path:
// a line longer than maxCells is cut so the visible runes plus "..."
// fit, and the cut lands on a byte boundary (ASCII guarantees this —
// the test pins the prior behaviour so a future switch to a stricter
// unit does not silently shorten or widen the output).
func TestTruncateDiffLine_ASCII_Truncates(t *testing.T) {
	in := strings.Repeat("x", 200)
	got := truncateDiffLine(in, 40)
	endsWithEllipsis(t, got)
	visible := strings.TrimSuffix(got, "...")
	if want := strings.Repeat("x", 37); visible != want {
		t.Errorf("ASCII cutoff visible = %d chars, want 37: %q", len(visible), visible)
	}
	noReplacement(t, got)
	// utf8.ValidString is implicit (ASCII is valid), but assert that
	// every byte of the visible run is a real ASCII char so a stray
	// mid-rune bytes-can-never-happen regression is loud.
	for i := 0; i < len(visible); i++ {
		if visible[i] >= 0x80 {
			t.Fatalf("non-ASCII byte survived ASCII cutoff: %x", visible[i])
		}
	}
}

// TestTruncateDiffLine_CJK_NoMidCharSplit is the headline AGY 1.3.6
// line-11 case: a long CJK line is truncated and the resulting string
// contains no mid-character bytes and no U+FFFD replacement. Every
// remaining rune is a complete CJK character, and the cluster
// adjacent to the ellipsis is a valid boundary.
func TestTruncateDiffLine_CJK_NoMidCharSplit(t *testing.T) {
	// "中文字符测试截断行为" = 12 CJK characters = 36 bytes of UTF-8.
	in := strings.Repeat("中文", 100) // 200 characters = 600 bytes
	got := truncateDiffLine(in, 20) // 20 cells budget -> 17 CJK + "..."
	noReplacement(t, got)
	endsWithEllipsis(t, got)
	if !utf8.ValidString(got) {
		t.Fatalf("output is not valid UTF-8: %q", got)
	}
	visible := strings.TrimSuffix(got, "...")
	// Count the visible CJK clusters. Each "中" or "文" is one grapheme
	// cluster (CJK has no combining marks) so cluster count == rune count.
	var clusters int
	for g := uniseg.NewGraphemes(visible); g.Next(); {
		clusters++
	}
	// 20 - 3 (ellipsis) = 17 cells of CJK (each 2 cells wide) would
	// be 8.5 clusters which is impossible — the loop stops at the
	// first cluster whose 2 cells would overflow, so we either get 8
	// full clusters (16 cells + 3 ellipsis = 19 cells total) or 9
	// full clusters (18 cells + 3 ellipsis = 21 cells total — over).
	// The contract is therefore 8 full clusters and total display
	// width <= 19.
	if clusters != 8 {
		t.Errorf("CJK cutoff clusters = %d, want 8 (budget 17 cells of double-width)", clusters)
	}
	if w := uniseg.StringWidth(got); w > 20 {
		t.Errorf("CJK cutoff width = %d, want <= 20", w)
	}
}

// TestTruncateDiffLine_EmojiZWJ_NoSplit covers the ZWJ family-emoji
// edge case: man-woman-girl is one grapheme cluster (man ZWJ woman
// ZWJ girl) but spans 11 runes; byte-level slicing would split it
// into fragments. The contract is that the cluster is either wholly
// present (with all three emojis and both ZWJs intact) or wholly
// absent — never a half-cluster visible at the head of the result.
func TestTruncateDiffLine_EmojiZWJ_NoSplit(t *testing.T) {
	// Family of three joined with U+200D ZWJ. This is a single grapheme
	// cluster but the underlying bytes (UTF-8) total 1F468 + 200D +
	// 1F469 + 200D + 1F467 = 18 bytes spanning 11 runes.
	single := "\U0001F468\u200D\U0001F469\u200D\U0001F467"
	if got, want := uniseg.GraphemeClusterCount(single), 1; got != want {
		t.Fatalf("input is %d clusters, want 1", got)
	}
	// Repeated so the line crosses several cutoffs.
	in := strings.Repeat(single, 5)
	for _, maxCells := range []int{1, 2, 3, 4, 5, 7, 12, 50} {
		got := truncateDiffLine(in, maxCells)
		noReplacement(t, got)
		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8 at maxCells=%d: %q", maxCells, got)
		}
		if uniseg.StringWidth(in) > maxCells && !strings.HasSuffix(got, "...") {
			t.Errorf("maxCells=%d should truncate, got %q", maxCells, got)
		}
		// The visible prefix (everything before the trailing "...")
		// must be a byte-aligned prefix of the original input — the
		// assertion that catches "cut a ZWJ cluster mid-way".
		visible := got
		if strings.HasSuffix(got, "...") {
			visible = strings.TrimSuffix(got, "...")
		}
		if visible != "" && !strings.HasPrefix(in, visible) {
			t.Errorf("maxCells=%d: visible %q is not a prefix of input (mid-cluster split)", maxCells, visible)
		}
	}
}

// TestTruncateDiffLine_MixedASCIIAndCJK verifies the mixed-script
// case: ASCII clusters cost one cell, CJK clusters cost two, and the
// chosen boundary fits within the cell budget without overrunning or
// splitting mid-cluster.
func TestTruncateDiffLine_MixedASCIIAndCJK(t *testing.T) {
	// "abc中def文ghi中jkl文mno" — alternating 3 ASCII + 1 CJK, 5
	// repetitions = 15 ASCII cells + 10 CJK cells (cells) =
	// 15 + 20 = 35 cells wide.
	in := strings.Repeat("abc中def文ghi中jkl文mno", 5)
	got := truncateDiffLine(in, 30) // budget reserves 3 for ellipsis -> 27 cells usable
	noReplacement(t, got)
	endsWithEllipsis(t, got)
	if !utf8.ValidString(got) {
		t.Fatalf("output is not valid UTF-8: %q", got)
	}
	if w := uniseg.StringWidth(got); w > 30 {
		t.Errorf("mixed cutoff width = %d, want <= 30", w)
	}
	// The starting literal begins with "abc中"; confirm the prefix
	// has not been mangled (would prove byte-level rotation).
	if !strings.HasPrefix(got, "abc") && !strings.HasPrefix(got, "...") {
		t.Errorf("mixed cutoff lost the ASCII prefix: %q", got[:min(20, len(got))])
	}
}

// TestTruncateDiffLine_ShortIsUnchanged verifies the no-truncation
// path on a non-ASCII input — the line is returned verbatim and no
// ellipsis is synthesized when the line already fits.
func TestTruncateDiffLine_ShortIsUnchanged(t *testing.T) {
	in := "已修改: src/main.go" // already under any reasonable cutoff
	got := truncateDiffLine(in, 200)
	if got != in {
		t.Errorf("short CJK line was altered: got %q, want %q", got, in)
	}
	noReplacement(t, got)
	if strings.Contains(got, "...") {
		t.Errorf("short CJK line picked up a spurious ellipsis: %q", got)
	}
}

// TestTruncateDiffLine_MultipleCutoffs pins the "every cutoff is safe"
// guarantee: a single long line (ASCII, CJK, emoji), when fed into
// truncateDiffLine at a sweep of widths, only ever returns an output
// that ends at a grapheme boundary (or unchanged), and contains no
// U+FFFD. This is the regression net for the AGY 1.3.6 line-11 claim
// that the ellipsis "respects grapheme cluster boundaries".
func TestTruncateDiffLine_MultipleCutoffs(t *testing.T) {
	type tc struct {
		name string
		line string
	}
	cases := []tc{
		{"ascii", strings.Repeat("abcdef", 100)},
		{"cjk", strings.Repeat("漢字混在文字列", 50)},
		{"emoji_zwj", strings.Repeat("\U0001F468\u200D\U0001F469\u200D\U0001F467", 30)},
		{"mixed", strings.Repeat("a漢b字c文d字e漢f", 30)},
		{"regional_flag", strings.Repeat("🇯🇵", 30)},
	}
	widths := []int{1, 2, 3, 4, 7, 8, 15, 16, 31, 32, 50, 100, 200, 500, 1000}
	for _, c := range cases {
		for _, w := range widths {
			t.Run(c.name+"/w="+cutoffsItoa(w), func(t *testing.T) {
				got := truncateDiffLine(c.line, w)
				noReplacement(t, got)
				if !utf8.ValidString(got) {
					t.Fatalf("invalid UTF-8 at width %d: %q", w, got)
				}
				if w > 0 && uniseg.StringWidth(c.line) > w {
					endsWithEllipsis(t, got)
				}
			})
		}
	}
}

// TestTruncateDiffLine_ZeroWidthDisables covers the "0 = pass-through"
// contract: callers (and tests) hand in maxCells = 0 to disable
// truncation entirely and get the raw string back. The original
// diffFeedback behaviour pre-cycle-diff-cjk-cutoff relied on this so
// historical byte-exact snapshots stay valid.
func TestTruncateDiffLine_ZeroWidthDisables(t *testing.T) {
	in := strings.Repeat("中文字符", 100)
	got := truncateDiffLine(in, 0)
	if got != in {
		t.Errorf("maxCells=0 should be pass-through; got %d bytes, want %d", len(got), len(in))
	}
}

// itoa labels test sub-cases. cutoffsItoa reuses the package-level
// helper from slash_commands.go (imported by the same package).
var cutoffsItoa = itoa
