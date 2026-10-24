package codesearch

import (
	"regexp"
	"strings"
	"testing"
)

func TestStatusRow(t *testing.T) {
	// Running with 0 hits
	row, done := StatusRow(StateRunning, 0, 0, 0)
	if !done || !strings.Contains(row, "Searching") {
		t.Errorf("running 0 hits: row=%q, done=%v", row, done)
	}

	// Running with hits
	row, done = StatusRow(StateRunning, 5, 2, 0)
	if done || !strings.Contains(row, "Found 5 matches in 2 files") {
		t.Errorf("running with hits: row=%q, done=%v", row, done)
	}

	// Done with 0 hits
	row, done = StatusRow(StateDone, 0, 0, 0)
	if !done || !strings.Contains(row, "No results") {
		t.Errorf("done 0 hits: row=%q, done=%v", row, done)
	}

	// Done with hits
	row, done = StatusRow(StateDone, 3, 1, 0)
	if done || !strings.Contains(row, "Found 3 matches in 1 file") {
		t.Errorf("done with hits: row=%q, done=%v", row, done)
	}

	// Cancelled with hits
	row, done = StatusRow(StateCancelled, 3, 1, 0)
	if done || !strings.Contains(row, "Cancelled after 3 matches in 1 file") {
		t.Errorf("cancelled: row=%q", row)
	}
}

func TestListBudget(t *testing.T) {
	if got := ListBudget(0); got != 0 {
		t.Errorf("ListBudget(0) = %d, want 0", got)
	}
	if got := ListBudget(30); got <= 0 {
		t.Errorf("ListBudget(30) = %d, want > 0", got)
	}
}

func TestHeaderAndRuleRow(t *testing.T) {
	hdr := HeaderRow("main.go")
	if !strings.Contains(hdr, "main.go") {
		t.Errorf("HeaderRow missing filename: %q", hdr)
	}

	rule := RuleRow(40)
	if !strings.Contains(rule, "─") {
		t.Errorf("RuleRow missing dashes: %q", rule)
	}
}

func TestAppendFileHitsAndBlockRows(t *testing.T) {
	content := "first line\nsecond match line\nthird line\n"
	re := regexp.MustCompile("match")
	hits := AppendFileHits(nil, "sample.txt", content, re, 10)
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	h := hits[0]
	if h.Line != 2 || !h.HasPrev || !h.HasNext {
		t.Fatalf("unexpected hit structure: %+v", h)
	}

	blocks := BlockRows(h, 1, 80)
	if len(blocks) != 3 {
		t.Fatalf("expected 3 context lines, got %d", len(blocks))
	}
	if !strings.Contains(blocks[1], "> L2: ") {
		t.Errorf("cursor line missing '> L2: ': %q", blocks[1])
	}
}

func TestWindowStartAndFollow(t *testing.T) {
	if got := WindowStart(5, 2, 10); got != 2 {
		t.Errorf("WindowStart = %d, want cursor 2", got)
	}
	if got := WindowStart(0, 0, 0); got != 0 {
		t.Errorf("WindowStart empty = %d, want 0", got)
	}

	hits := []Hit{
		{File: "a.go", Line: 1, Text: "a"},
		{File: "b.go", Line: 2, Text: "b"},
	}
	w := Follow(0, 1, 24, 80, hits)
	if w != 0 {
		t.Errorf("Follow = %d, want 0", w)
	}
}


