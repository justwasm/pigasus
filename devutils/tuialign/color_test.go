package main

import (
	"testing"
)

func TestParseStyledLine(t *testing.T) {
	line := " \x1b[1m\x1b[38;2;66;133;244mAntigravity CLI\x1b[0m \x1b[38;5;240m(dim)\x1b[39m"
	cells := ParseStyledLine(line)

	if len(cells) == 0 {
		t.Fatalf("expected cells, got 0")
	}

	// First cell is space with default style
	if cells[0].Rune != ' ' || cells[0].Style.Fg.Type != ColorDefault {
		t.Errorf("cell 0 style mismatch: %+v", cells[0])
	}

	// 'A' should have Bold=true and RGB(66, 133, 244)
	aCell := cells[1]
	if aCell.Rune != 'A' {
		t.Errorf("expected 'A', got %c", aCell.Rune)
	}
	if !aCell.Style.Bold {
		t.Errorf("expected bold")
	}
	if aCell.Style.Fg.Hex() != "#4285F4" {
		t.Errorf("expected fg #4285F4, got %s", aCell.Style.Fg.Hex())
	}

	// Check 256 color
	dimCell := cells[len(cells)-3] // inside '(dim)'
	if dimCell.Style.Fg.Type != Color256 {
		t.Errorf("expected 256 color, got %v", dimCell.Style.Fg.Type)
	}
}
