package contextpanel

import (
	"strings"
	"testing"
)

// TestColsFor pins the one-cell-per-five-columns rule and the zero-width
// fallback to the historical 220-column shape.
func TestColsFor(t *testing.T) {
	for _, tc := range []struct{ width, want int }{
		{220, 44},
		{200, 40},
		{120, 24},
		{0, 44},
		{-10, 44},
		{3, 1},
	} {
		if got := ColsFor(tc.width); got != tc.want {
			t.Errorf("ColsFor(%d) = %d, want %d", tc.width, got, tc.want)
		}
	}
}

// TestGridRow checks the 44-cell shape and the trailing descriptor.
func TestGridRow(t *testing.T) {
	row := GridRow("hello")
	if n := strings.Count(row, Cell); n != DefaultCols {
		t.Errorf("cells = %d, want %d", n, DefaultCols)
	}
	if !strings.HasSuffix(row, "     hello") {
		t.Errorf("row = %q, want 5-space pad before the descriptor", row)
	}
}

// TestGridRowPadded checks the 6-space pad of the `(0.0%)` row.
func TestGridRowPadded(t *testing.T) {
	row := GridRowPadded("(0.0%)", 6)
	if !strings.HasSuffix(row, "      (0.0%)") {
		t.Errorf("row = %q, want 6-space pad", row)
	}
}

// TestGridRowFilled checks that the fill paints the first len(fill) cells
// and the rest stay empty.
func TestGridRowFilled(t *testing.T) {
	fill := []string{UserGlyph, AgentGlyph}
	row := GridRowFilled("x", 5, 4, fill, 0)
	if !strings.Contains(row, UserGlyph) || !strings.Contains(row, AgentGlyph) {
		t.Errorf("row = %q, want both fill glyphs", row)
	}
	if n := strings.Count(row, Cell); n != 2 {
		t.Errorf("empty cells = %d, want 2", n)
	}
}

// TestGridRowFilledOffset checks that the offset shifts the fill window.
func TestGridRowFilledOffset(t *testing.T) {
	fill := []string{UserGlyph, AgentGlyph, ToolGlyph}
	row := GridRowFilled("x", 0, 2, fill, 1)
	if !strings.Contains(row, AgentGlyph) || !strings.Contains(row, ToolGlyph) {
		t.Errorf("row = %q, want the shifted fill window", row)
	}
	if strings.Contains(row, UserGlyph) {
		t.Errorf("row = %q, want the first fill cell skipped", row)
	}
}

// TestPct pins the percentage formatting.
func TestPct(t *testing.T) {
	if got := Pct(0, MaxContextTokens); got != "(0.0%)" {
		t.Errorf("Pct(0) = %q", got)
	}
	if got := Pct(-5, MaxContextTokens); got != "(0.0%)" {
		t.Errorf("Pct(-5) = %q", got)
	}
	if got := Pct(500_000, MaxContextTokens); got != "(50.0%)" {
		t.Errorf("Pct(500000) = %q", got)
	}
}

// TestTokenLabel pins the raw/k/M thresholds.
func TestTokenLabel(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0k"},
		{1234, "1.2k"},
		{999_949, "999.9k"},
		{999_950, "1.0M"},
		{2_000_000, "2.0M"},
	} {
		if got := TokenLabel(tc.n); got != tc.want {
			t.Errorf("TokenLabel(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// TestStreamingRows pins the in-flight layout: no system categories.
func TestStreamingRows(t *testing.T) {
	rows := StreamingRows("M", 300, 999_700, MaxContextTokens, 100, 150, 50)
	if len(rows) != 7 {
		t.Fatalf("rows = %d, want 7", len(rows))
	}
	if rows[0].Text != "M · 300/1.0M tokens" {
		t.Errorf("header = %q", rows[0].Text)
	}
	if rows[1].Pad != 6 {
		t.Errorf("pct pad = %d, want 6", rows[1].Pad)
	}
	for _, r := range rows {
		if strings.Contains(r.Text, "System prompt") || strings.Contains(r.Text, "Skills") {
			t.Errorf("streaming row leaks a system category: %q", r.Text)
		}
	}
}

// TestLiveRows pins the completed-turn layout: every category present.
func TestLiveRows(t *testing.T) {
	rows := LiveRows("M", 300, 999_700, MaxContextTokens, 100, 150, 50, 10, 20, 30, 40)
	if len(rows) != 11 {
		t.Fatalf("rows = %d, want 11", len(rows))
	}
	joined := ""
	for _, r := range rows {
		joined += r.Text + "\n"
	}
	for _, want := range []string{"User messages:", "Agent responses:", "Tool calls:", "System prompt:", "System tools:", "Skills:", "Subagents:", "Free space:"} {
		if !strings.Contains(joined, want) {
			t.Errorf("live rows missing %q", want)
		}
	}
}

// TestStreamingFill checks that the in-flight layout paints one cell per
// non-empty category (not the 4000-token rule the completed layout uses).
func TestStreamingFill(t *testing.T) {
	if got := StreamingFill(0, 0, 0); len(got) != 0 {
		t.Errorf("empty fill = %v, want none", got)
	}
	got := StreamingFill(4000, 1, 0)
	if len(got) != 2 {
		t.Fatalf("fill = %v, want 2 cells", got)
	}
	if got[0] != UserGlyph || got[1] != AgentGlyph {
		t.Errorf("fill = %v, want the user then agent glyph", got)
	}
	if got := StreamingFill(0, 0, 1); len(got) != 1 || got[0] != ToolGlyph {
		t.Errorf("fill = %v, want one tool glyph", got)
	}
}

// TestLiveFill checks the 4000-token-per-cell rule and the category order.
func TestLiveFill(t *testing.T) {
	// 8000 system-prompt tokens → 2 cells, then 1 user cell.
	got := LiveFill(8000, 1, 0, 0, 0, 0, 0)
	if len(got) != 3 {
		t.Fatalf("fill = %v, want 3 cells", got)
	}
	if got[2] != UserGlyph {
		t.Errorf("fill[2] = %q, want the user glyph", got[2])
	}
	if strings.Contains(got[0], UserGlyph) {
		t.Errorf("fill[0] = %q, want the gray system glyph", got[0])
	}
}
