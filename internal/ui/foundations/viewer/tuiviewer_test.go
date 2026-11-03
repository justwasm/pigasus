package viewer

import (
	"strings"
	"testing"
)

func TestWindow(t *testing.T) {
	spec := Spec{
		Lines:   []string{"line1", "line2", "line3", "line4", "line5"},
		Visible: 3,
		Total:   5,
	}
	rows, start, end, offset := Window(spec, 0)
	if len(rows) != 3 || rows[0] != "line1" || rows[2] != "line3" {
		t.Fatalf("unexpected rows: %v", rows)
	}
	if start != 1 || end != 3 || offset != 0 {
		t.Fatalf("unexpected start/end/offset: %d, %d, %d", start, end, offset)
	}

	// Test scrolling past end
	rows, start, end, offset = Window(spec, 10)
	if len(rows) != 3 || rows[0] != "line3" || rows[2] != "line5" {
		t.Fatalf("unexpected scrolled rows: %v", rows)
	}
	if start != 3 || end != 5 || offset != 2 {
		t.Fatalf("unexpected start/end/offset: %d, %d, %d", start, end, offset)
	}
}

func TestCounter(t *testing.T) {
	got := Counter(1, 10, 100)
	if !strings.Contains(got, "(1–10 of 100 lines)") {
		t.Fatalf("unexpected counter format: %q", got)
	}
}

func TestMaxOffset(t *testing.T) {
	spec := Spec{
		Lines:   []string{"1", "2", "3", "4", "5"},
		Visible: 3,
	}
	if got := MaxOffset(spec); got != 2 {
		t.Errorf("MaxOffset = %d, want 2", got)
	}
	specSmall := Spec{
		Lines:   []string{"1"},
		Visible: 5,
	}
	if got := MaxOffset(specSmall); got != 0 {
		t.Errorf("MaxOffset small = %d, want 0", got)
	}
}
