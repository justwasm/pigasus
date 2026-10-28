package cliapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

func TestDiffPositionIndicators_AllChanges(t *testing.T) {
	var tracked []diffChange
	for i := 1; i <= 20; i++ {
		tracked = append(tracked, diffChange{Path: "file" + strings.Repeat("x", i) + ".go", Added: i})
	}
	// Height 15 -> visible = 15 - 6 = 9
	lines := diffAllChangesLinesWindowed(tracked, nil, 0, 0, 9)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "[1-9 of 20 files]") {
		t.Fatalf("expected indicator [1-9 of 20 files], got:\n%s", joined)
	}

	// Scrolled
	linesScrolled := diffAllChangesLinesWindowed(tracked, nil, 10, 5, 9)
	joinedScrolled := strings.Join(linesScrolled, "\n")
	if !strings.Contains(joinedScrolled, "[6-14 of 20 files]") {
		t.Fatalf("expected indicator [6-14 of 20 files], got:\n%s", joinedScrolled)
	}
}

func TestDiffPositionIndicators_PerTurn(t *testing.T) {
	m := &runMode{
		panel: slashPanel{
			kind:   panelDiff,
			tab:    1,
			cursor: 0,
		},
		height: 15, // visible = 9
	}
	// Add 15 user turns
	for i := 1; i <= 15; i++ {
		m.session = append(m.session, backend.Message{Role: "user", Content: "hello"})
	}
	// Mock cwd with changes
	dir := t.TempDir()
	m.cwd = dir
	_, _ = runGit(dir, "init")
	_, _ = runGit(dir, "config", "user.name", "Test")
	_, _ = runGit(dir, "config", "user.email", "test@test.com")
	_, _ = runGit(dir, "commit", "--allow-empty", "-m", "init")

	// Create uncommitted file
	_ = os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644)

	lines := m.diffPerTurnLines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "[1-9 of 15 rows]") {
		t.Fatalf("expected indicator [1-9 of 15 rows], got:\n%s", joined)
	}
}

func TestDiffPositionIndicators_CommitTree(t *testing.T) {
	dir := t.TempDir()
	m := &runMode{
		cwd: dir,
		panel: slashPanel{
			kind:   panelDiff,
			tab:    2,
			cursor: 0,
		},
		height: 14, // visible = 14 - 6 = 8
	}
	_, _ = runGit(dir, "init")
	_, _ = runGit(dir, "config", "user.name", "Test")
	_, _ = runGit(dir, "config", "user.email", "test@test.com")
	for i := 1; i <= 12; i++ {
		_, _ = runGit(dir, "commit", "--allow-empty", "-m", "commit "+string(rune('a'+i)))
	}

	lines := m.diffCommitTreeLines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "[1-8 of 12 commits]") {
		t.Fatalf("expected indicator [1-8 of 12 commits], got:\n%s", joined)
	}
}
