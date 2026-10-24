package diffaction

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
