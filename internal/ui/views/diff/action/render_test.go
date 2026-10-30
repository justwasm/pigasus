package action

import (
	"strings"
	"testing"
)

func TestTabRow(t *testing.T) {
	row := TabRow("git", 0, false, nil)
	if !strings.Contains(row, "Diff (git)") {
		t.Errorf("TabRow missing VCS title: %q", row)
	}
	if !strings.Contains(row, "All Changes") {
		t.Errorf("TabRow missing tab name: %q", row)
	}
}

func TestCounts(t *testing.T) {
	c := Change{Added: 10, Deleted: 5}
	cnt := Counts(c)
	if !strings.Contains(cnt, "+10") || !strings.Contains(cnt, "-5") {
		t.Errorf("Counts mismatch: %q", cnt)
	}
}

func TestAllChangesRow(t *testing.T) {
	c := Change{Path: "main.go", Added: 2}
	row := AllChangesRow(c, false)
	if !strings.Contains(row, "main.go") || !strings.Contains(row, "+2") {
		t.Errorf("AllChangesRow mismatch: %q", row)
	}

	cUntracked := Change{Path: "new.go", Untracked: true}
	uRow := AllChangesRow(cUntracked, true)
	if !strings.Contains(uRow, "[Untracked]") || !strings.Contains(uRow, "> ") {
		t.Errorf("AllChangesRow untracked mismatch: %q", uRow)
	}
}

func TestAllChangesLines(t *testing.T) {
	lines := AllChangesLines(nil, nil, 0, "hint-empty", "hint-nav")
	if len(lines) != 5 || !strings.Contains(lines[2], "No uncommitted changes") {
		t.Errorf("AllChangesLines empty mismatch: %v", lines)
	}
}

func TestAllChangesLinesWindowed(t *testing.T) {
	var tracked []Change
	for i := 1; i <= 20; i++ {
		tracked = append(tracked, Change{Path: strings.Repeat("f", i) + ".go", Added: i})
	}
	// Total 20 files, visible 5, window 0
	lines := AllChangesLinesWindowed(tracked, nil, 2, 0, 5, "empty", "nav")
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "[1-5 of 20 files]") {
		t.Errorf("expected position indicator [1-5 of 20 files], got:\n%s", joined)
	}

	// Scrolled: window 5, visible 5
	linesScrolled := AllChangesLinesWindowed(tracked, nil, 7, 5, 5, "empty", "nav")
	joinedScrolled := strings.Join(linesScrolled, "\n")
	if !strings.Contains(joinedScrolled, "[6-10 of 20 files]") {
		t.Errorf("expected position indicator [6-10 of 20 files], got:\n%s", joinedScrolled)
	}

	// When all fit: no indicator
	linesFit := AllChangesLinesWindowed(tracked, nil, 0, 0, 50, "empty", "nav")
	joinedFit := strings.Join(linesFit, "\n")
	if strings.Contains(joinedFit, "files]") {
		t.Errorf("expected no indicator when all fit, got:\n%s", joinedFit)
	}
}
