package mdrender

import (
	"strings"
	"testing"
)

// TestCycle269_TrimTrailingNewline pins the cycle-269 fix: the ANSI
// variant trims the lone trailing newline glamour appends, so
// splitting the output on "\n" produces the row count the user
// expects — content rows + the inter-content blank separators they
// wrote in markdown — and not N+1 with a phantom empty row at the
// end.
//
// Pre-cycle-269 the asymmetry was hidden in streaming because
// LineANSI (stream_glamour.go:190) already did TrimRight "\n" on
// per-chunk glamour renders; the final-render path ANSI → split
// had no such trim, so a reply that streamed as "Para A\n\nPara B"
// (3 rows after split) re-rendered at final turn-done as 4 rows
// ([A, blank, B, ""]). After this fix both paths produce the same
// row count.
func TestCycle269_TrimTrailingNewline(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"single-paragraph", "single paragraph of text", 1},
		{"two-paragraphs", "first paragraph\n\nsecond paragraph", 3},
		{"three-paragraphs", "Para 1\n\nPara 2\n\nPara 3", 5},
		{"heading-then-para", "# Heading\n\npara after heading", 3},
	}
	for _, w := range []int{80, 120, 250} {
		for _, c := range cases {
			out, err := ANSI(c.in, w)
			if err != nil {
				t.Fatalf("width=%d %s: ANSI err: %v", w, c.name, err)
			}
			// Mimic tui_run.go:3511: split on "\n" to get
			// per-row transcript entries.
			var got int
			if out == "" {
				got = 0
			} else {
				got = len(strings.Split(out, "\n"))
			}
			if got != c.want {
				t.Errorf("width=%d %s: rows=%d, want %d  (bytes=%d)",
					w, c.name, got, c.want, len(out))
			}
			// Guard against regression: out must not end in "\n".
			if strings.HasSuffix(out, "\n") {
				t.Errorf("width=%d %s: trailing \\n still present", w, c.name)
			}
		}
	}
}

// TestCycle269_AnsiNoPhantomTrailingRow pins that the final-render
// ANSI path does not introduce a phantom empty trailing row. The
// streaming path renders one LineANSI call per chunk and never had
// this issue (stream_glamour.go:190 already trims); pre-cycle-269
// the final-render ANSI did not trim and split-on-newline produced
// one trailing empty element. Splitting ANSI(reply, w) for any
// reply that ends without a blank line must produce exactly N
// content rows, not N+1.
func TestCycle269_AnsiNoPhantomTrailingRow(t *testing.T) {
	for _, w := range []int{80, 120, 250} {
		SetCurrentWidth(w)
		cases := []struct {
			in   string
			want int
		}{
			{"", 0},
			{"single paragraph", 1},
			{"first\n\nsecond", 3},
			{"first\n\n\nsecond", 3}, // glamour collapses multiple blanks
		}
		for _, c := range cases {
			out, err := ANSI(c.in, w)
			if err != nil {
				t.Fatalf("width=%d in=%q: ANSI err: %v", w, c.in, err)
			}
			var rows []string
			if out != "" {
				rows = strings.Split(out, "\n")
			}
			if len(rows) != c.want {
				t.Errorf("width=%d in=%q: rows=%d, want %d  (last=%q bytes=%d)",
					w, c.in, len(rows), c.want, lastOr(rows, "(none)"), len(out))
			}
			// No trailing empty row.
			if n := len(rows); n > 0 && rows[n-1] == "" {
				t.Errorf("width=%d in=%q: trailing empty row present", w, c.in)
			}
		}
	}
}

func lastOr(rows []string, fallback string) string {
	if len(rows) == 0 {
		return fallback
	}
	return rows[len(rows)-1]
}