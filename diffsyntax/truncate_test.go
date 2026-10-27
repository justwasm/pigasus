package diffsyntax

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

func noReplacement(t *testing.T, s string) {
	t.Helper()
	if strings.ContainsRune(s, '\uFFFD') {
		t.Errorf("output contains U+FFFD replacement character: %q", s)
	}
}

func endsWithEllipsis(t *testing.T, s string) {
	t.Helper()
	if !strings.HasSuffix(s, "...") {
		t.Errorf("output should end in %q, got %q", "...", s)
	}
}

func TestTruncateDiffLine_ASCII_Fits(t *testing.T) {
	in := "diff --git a/foo.go b/foo.go"
	got := TruncateLine(in, 80)
	if got != in {
		t.Errorf("short line was mangled: got %q, want %q", got, in)
	}
	noReplacement(t, got)
}

func TestTruncateDiffLine_ASCII_Truncates(t *testing.T) {
	in := strings.Repeat("x", 200)
	got := TruncateLine(in, 40)
	endsWithEllipsis(t, got)
	visible := strings.TrimSuffix(got, "...")
	if want := strings.Repeat("x", 37); visible != want {
		t.Errorf("ASCII cutoff visible = %d chars, want 37: %q", len(visible), visible)
	}
	noReplacement(t, got)
	for i := 0; i < len(visible); i++ {
		if visible[i] >= 0x80 {
			t.Fatalf("non-ASCII byte survived ASCII cutoff: %x", visible[i])
		}
	}
}

func TestTruncateDiffLine_CJK_NoMidCharSplit(t *testing.T) {
	in := strings.Repeat("中文", 100)
	got := TruncateLine(in, 20)
	noReplacement(t, got)
	endsWithEllipsis(t, got)
	if !utf8.ValidString(got) {
		t.Fatalf("output is not valid UTF-8: %q", got)
	}
	visible := strings.TrimSuffix(got, "...")
	var clusters int
	for g := uniseg.NewGraphemes(visible); g.Next(); {
		clusters++
	}
	if clusters != 8 {
		t.Errorf("CJK cutoff clusters = %d, want 8 (budget 17 cells of double-width)", clusters)
	}
	if w := uniseg.StringWidth(got); w > 20 {
		t.Errorf("CJK cutoff width = %d, want <= 20", w)
	}
}

func TestTruncateDiffLine_EmojiZWJ_NoSplit(t *testing.T) {
	single := "\U0001F468\u200D\U0001F469\u200D\U0001F467"
	if got, want := uniseg.GraphemeClusterCount(single), 1; got != want {
		t.Fatalf("input is %d clusters, want 1", got)
	}
	in := strings.Repeat(single, 5)
	for _, maxCells := range []int{1, 2, 3, 4, 5, 7, 12, 50} {
		got := TruncateLine(in, maxCells)
		noReplacement(t, got)
		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8 at maxCells=%d: %q", maxCells, got)
		}
		if uniseg.StringWidth(in) > maxCells && !strings.HasSuffix(got, "...") {
			t.Errorf("maxCells=%d should truncate, got %q", maxCells, got)
		}
		visible := got
		if strings.HasSuffix(got, "...") {
			visible = strings.TrimSuffix(got, "...")
		}
		if visible != "" && !strings.HasPrefix(in, visible) {
			t.Errorf("maxCells=%d: visible %q is not a prefix of input (mid-cluster split)", maxCells, visible)
		}
	}
}

func TestTruncateDiffLine_MixedASCIIAndCJK(t *testing.T) {
	in := strings.Repeat("abc中def文ghi中jkl文mno", 5)
	got := TruncateLine(in, 30)
	noReplacement(t, got)
	endsWithEllipsis(t, got)
	if !utf8.ValidString(got) {
		t.Fatalf("output is not valid UTF-8: %q", got)
	}
	if w := uniseg.StringWidth(got); w > 30 {
		t.Errorf("mixed cutoff width = %d, want <= 30", w)
	}
	if !strings.HasPrefix(got, "abc") && !strings.HasPrefix(got, "...") {
		t.Errorf("mixed cutoff lost the ASCII prefix: %q", got[:min(20, len(got))])
	}
}

func TestTruncateDiffLine_ShortIsUnchanged(t *testing.T) {
	in := "已修改: src/main.go"
	got := TruncateLine(in, 200)
	if got != in {
		t.Errorf("short CJK line was altered: got %q, want %q", got, in)
	}
	noReplacement(t, got)
	if strings.Contains(got, "...") {
		t.Errorf("short CJK line picked up a spurious ellipsis: %q", got)
	}
}

func TestTruncateDiffLine_MultipleCutoffs(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"ascii", strings.Repeat("abcdef", 100)},
		{"cjk", strings.Repeat("漢字混在文字列", 50)},
		{"emoji_zwj", strings.Repeat("\U0001F468\u200D\U0001F469\u200D\U0001F467", 30)},
		{"mixed", strings.Repeat("a漢b字c文d字e漢f", 30)},
		{"regional_flag", strings.Repeat("🇯🇵", 30)},
	}
	widths := []int{1, 2, 3, 4, 7, 8, 15, 16, 31, 32, 50, 100, 200, 500, 1000}
	for _, c := range cases {
		for _, w := range widths {
			t.Run(c.name+"/w="+strconv.Itoa(w), func(t *testing.T) {
				got := TruncateLine(c.line, w)
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

func TestTruncateDiffLine_ZeroWidthDisables(t *testing.T) {
	in := strings.Repeat("中文字符", 100)
	got := TruncateLine(in, 0)
	if got != in {
		t.Errorf("maxCells=0 should be pass-through; got %d bytes, want %d", len(got), len(in))
	}
}
