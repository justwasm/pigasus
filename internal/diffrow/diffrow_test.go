// diffrow_test.go — the reference's diff row model.
//
// The literals pinned here are the ones recovered from agy 1.4.8's
// binary: the gutter's format string, the three body format strings,
// the three summary forms and the truncation marker.
package diffrow

import (
	"fmt"
	"strings"
	"testing"
)

func TestGutterIsTheReferenceFormat(t *testing.T) {
	// 【反编译】 `render.codeActionDetail.func2` builds the gutter with
	// fmt.Sprintf("%*d", numWidth, num) — the literal is the three
	// bytes "%*d" at 0x1033bf730 in agy 1.4.8. There is no trailing
	// space: the marker column carries the separation.
	l := Layout{NumWidth: 4}
	if got, want := l.Gutter(3), fmt.Sprintf("%*d", 4, 3); got != want {
		t.Fatalf("Gutter(3) = %q, want %q", got, want)
	}
	if got, want := l.Gutter(3), "   3"; got != want {
		t.Fatalf("Gutter(3) = %q, want %q", got, want)
	}
	if got, want := (Layout{NumWidth: 5}).Gutter(1234), " 1234"; got != want {
		t.Fatalf("Gutter(1234) = %q, want %q", got, want)
	}
}

func TestMarkerColumnIsFourWide(t *testing.T) {
	// 【反编译】 the three body format strings: "+  %s" (0x1033d1ad8),
	// "-  %s" (0x1033d1add) and "    %s" (0x1033df8e9). The added and
	// removed bodies carry one leading space more than the context
	// body, so the text starts in the same column in all three.
	if got, want := Marker(Add), " +  "; got != want {
		t.Fatalf("Marker(Add) = %q, want %q", got, want)
	}
	if got, want := Marker(Remove), " -  "; got != want {
		t.Fatalf("Marker(Remove) = %q, want %q", got, want)
	}
	if got, want := Marker(Context), "    "; got != want {
		t.Fatalf("Marker(Context) = %q, want %q", got, want)
	}
	for _, k := range []Kind{Add, Remove, Context} {
		if got := len([]rune(Marker(k))); got != 4 {
			t.Fatalf("Marker(%q) is %d columns wide, want 4", k, got)
		}
	}
}

func TestSummaryForms(t *testing.T) {
	// 【反编译】 "Added %d line%s" (0x1034b43b4), "Removed %d line%s"
	// (0x1034dcd2d), and the "+%d" (0x1033bf754) / "-%d" (0x1033bf757)
	// pair joined by " / " (0x1033bf19c) and closed by " lines"
	// (0x1033df8e3).
	for _, tc := range []struct {
		added, removed int
		want           string
	}{
		{1, 0, "Added 1 line"},
		{3, 0, "Added 3 lines"},
		{0, 1, "Removed 1 line"},
		{0, 4, "Removed 4 lines"},
		{1, 1, "+1 / -1 lines"},
		{2, 5, "+2 / -5 lines"},
	} {
		if got := Summary(tc.added, tc.removed); got != tc.want {
			t.Errorf("Summary(%d,%d) = %q, want %q", tc.added, tc.removed, got, tc.want)
		}
	}
}

func TestMoreMarker(t *testing.T) {
	// 【反编译】 "%*s    … and %d more lines" (0x10358a97d).
	if got, want := More(0, 3), "    … and 3 more lines"; got != want {
		t.Fatalf("More(0,3) = %q, want %q", got, want)
	}
	if got, want := More(4, 1), "        … and 1 more lines"; got != want {
		t.Fatalf("More(4,1) = %q, want %q", got, want)
	}
}

func TestNewLayoutWidths(t *testing.T) {
	// 【反编译】 the gutter width is max(4, digits) and the content
	// width max(1, width-numWidth-2) — so a row is one column
	// narrower than the width it was given.
	l := NewLayout(80, 12)
	if l.NumWidth != 4 {
		t.Fatalf("NumWidth = %d, want 4", l.NumWidth)
	}
	if l.ContentWidth != 80-4-2 {
		t.Fatalf("ContentWidth = %d, want %d", l.ContentWidth, 80-4-2)
	}
	if got := NewLayout(80, 12345).NumWidth; got != 5 {
		t.Fatalf("NumWidth = %d, want 5", got)
	}
	if got := NewLayout(3, 1).ContentWidth; got != 1 {
		t.Fatalf("ContentWidth = %d, want 1", got)
	}
}

func TestRenderRowShape(t *testing.T) {
	l := Layout{NumWidth: 4, ContentWidth: 12}
	s := Style{}

	// The context row: the number in the gutter, the marker column and
	// the text — and no padding, because the reference pads only the
	// added and removed bodies.
	got := Render(l, s, Row{Kind: Context, Num: 3, Text: "alpha"})
	if want := "   3    alpha"; got != want {
		t.Fatalf("context row = %q, want %q", got, want)
	}

	// The added row: the marker column's "+" and the text, padded to the
	// content width.
	got = Render(l, s, Row{Kind: Add, Num: 4, Text: "beta"})
	if want := "   4 +  beta     "; got != want {
		t.Fatalf("added row = %q, want %q", got, want)
	}
	// The row is the gutter, a space, the marker column and the body
	// padded to the content width.
	if n := len([]rune(got)); n != 4+1+12 {
		t.Fatalf("added row is %d columns wide, want %d", n, 4+1+12)
	}

	got = Render(l, s, Row{Kind: Remove, Num: 4, Text: "beta"})
	if want := "   4 -  beta     "; got != want {
		t.Fatalf("removed row = %q, want %q", got, want)
	}
}

func TestRenderStyledMeasuresThePlainText(t *testing.T) {
	l := Layout{NumWidth: 4, ContentWidth: 12}
	s := Style{}
	styled := "\x1b[38;2;1;2;3mbeta\x1b[39m"
	got := RenderStyled(l, s, Add, 4, "beta", styled)
	if !strings.Contains(got, styled) {
		t.Fatalf("styled row lost its styling: %q", got)
	}
	// The padding counts the visible characters, not the escapes.
	if n := len([]rune(stripANSI(got))); n != 4+1+12 {
		t.Fatalf("styled row is %d columns wide, want %d", n, 4+1+12)
	}
}

// stripANSI removes the escape sequences from a rendered row.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func TestFileViewStyleCarriesTheTint(t *testing.T) {
	// 【strings】 the `/diff` file view paints "a background tint on
	// added and removed lines"
	// (internal/cli/testdata/changelog_reference.txt:127).
	if !strings.Contains(FileViewStyle().AddBg, "\x1b[48;2;") {
		t.Fatal("the file view style carries no tint")
	}
	if !strings.Contains(FileViewStyle().RemoveBg, "\x1b[48;2;") {
		t.Fatal("the file view style carries no tint")
	}
	// The transcript's diff carries the stripe, not the tint.
	if strings.Contains(TranscriptStyle().AddBg, "\x1b[48;2;25;50;25m") {
		t.Fatal("the transcript style carries the file view's tint")
	}
}
