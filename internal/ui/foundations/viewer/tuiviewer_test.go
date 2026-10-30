package viewer

import (
	"strings"
	"testing"
	"time"
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

func TestQuotaCountdown(t *testing.T) {
	now := time.Now()
	if got := QuotaCountdown(now.Add(3665*time.Second + 500*time.Millisecond)); got != "1h 1m" {
		t.Fatalf("expected '1h 1m', got %q", got)
	}
	if got := QuotaCountdown(now.Add(125*time.Second + 500*time.Millisecond)); got != "2m" {
		t.Fatalf("expected '2m', got %q", got)
	}
	if got := QuotaCountdown(now.Add(35*time.Second + 500*time.Millisecond)); got != "35s" {
		t.Fatalf("expected '35s', got %q", got)
	}
	if got := QuotaCountdown(now.Add(-10 * time.Second)); got != "0s" {
		t.Fatalf("expected '0s', got %q", got)
	}
}

func TestChangelogExcerpt(t *testing.T) {
	rows := ChangelogExcerptRows()
	if len(rows) < 3 || !strings.Contains(rows[0], "Pigasus CLI Changelog") {
		t.Fatalf("unexpected excerpt rows: %v", rows)
	}
}

func TestDiffFirstChangedPath(t *testing.T) {
	if got := DiffFirstChangedPath([]string{"a.go"}, []string{"b.go"}); got != "a.go" {
		t.Fatalf("expected a.go, got %q", got)
	}
	if got := DiffFirstChangedPath(nil, []string{"b.go"}); got != "b.go" {
		t.Fatalf("expected b.go, got %q", got)
	}
	if got := DiffFirstChangedPath(nil, nil); got != "" {
		t.Fatalf("expected empty string, got %q", got)
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

