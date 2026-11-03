package rewindpanel

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/session/rewind"
)

// TestFirstRow pins the snippet rule: first non-empty line, trimmed,
// capped at max runes with an ellipsis.
func TestFirstRow(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"empty", "", ""},
		{"blank-first-line", "\n\nreal", "real"},
		{"trims", "   padded   ", "padd…"},
		{"first-line-only", "one\ntwo", "one"},
		{"under-cap", "short", "short"},
		{"at-cap", "12345", "12345"},
		{"over-cap", "123456", "1234…"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstRow(tc.in, 5); got != tc.want {
				t.Errorf("firstRow(%q, 5) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestStepHeight pins the visible-row budget: the cycle-189 default of 17
// when the height is unknown, and height-6 once the terminal reports one.
func TestStepHeight(t *testing.T) {
	for _, tc := range []struct {
		h    int
		want int
	}{
		{0, 17},
		{-5, 17},
		{23, 17},
		{24, 18},
		{6, 1},
		{3, 1},
	} {
		if got := stepHeight(tc.h); got != tc.want {
			t.Errorf("stepHeight(%d) = %d, want %d", tc.h, got, tc.want)
		}
	}
}

// TestRenderStepRows pins the row shape: the cursor row is blue and
// prefixed with "> ", the newest turn carries a "(current)" annotation.
func TestRenderStepRows(t *testing.T) {
	steps := []rewind.Step{{Content: "newest"}, {Content: "older"}}
	rows := renderStepRows(steps, 1, "<B>", "</B>", "<G>", "</G>")
	if len(rows) != 2 {
		t.Fatalf("renderStepRows = %d rows, want 2", len(rows))
	}
	if !strings.Contains(rows[0], "<G>  (current)</G>") {
		t.Errorf("row 0 = %q, want the (current) annotation", rows[0])
	}
	if !strings.HasPrefix(rows[1], "<B>> ") {
		t.Errorf("row 1 = %q, want the blue cursor prefix", rows[1])
	}
	if !strings.Contains(rows[1], "Turn 2") {
		t.Errorf("row 1 = %q, want the Turn 2 label", rows[1])
	}
}

// TestRenderStepRowsSnippet pins the row's label: the step's own text,
// cut to the row's budget.
func TestRenderStepRowsSnippet(t *testing.T) {
	steps := []rewind.Step{{Content: "\n\n  the first line  \nand the second"}}
	rows := renderStepRows(steps, 0, "<B>", "</B>", "<G>", "</G>")
	if !strings.Contains(rows[0], "Turn 1  the first line") {
		t.Errorf("row 0 = %q, want the turn's first line", rows[0])
	}
	long := []rewind.Step{{Content: "0123456789012345678901234567890123456789"}}
	rows = renderStepRows(long, 0, "<B>", "</B>", "<G>", "</G>")
	if !strings.Contains(rows[0], "01234567890123456789012345678…") {
		t.Errorf("row 0 = %q, want the capped snippet", rows[0])
	}
}

// TestStepCounter pins the footer counter's shape.
func TestStepCounter(t *testing.T) {
	got := stepCounter(1, 5, 20, "<D>", "</D>")
	want := "<D>  (1–5 of 20 turns)</D>"
	if got != want {
		t.Errorf("stepCounter = %q, want %q", got, want)
	}
}
