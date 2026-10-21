// slash_cycle226_test.go — cycle 226 regression tests for the /diff
// follow-up work in TODO §4 of the cycle-207 deliverable:
//
//   - Per Turn: row list when session has turns AND git diff has changes
//     (was a hardcoded "No file changes in conversation" placeholder).
//   - All Changes enter: opens the inline-diff subview (`git diff -- <path>`)
//     for the file under the cursor.
//   - Commit Tree enter: opens the inline-diff subview (`git show <hash>`)
//     for the commit under the cursor.
//   - Esc on the inline subview returns to the list view (no close).
//
// The new tests pin those semantics byte-exactly so a future edit cannot
// silently regress them. The pre-existing cycle-207 tests
// (slash_cycle207_test.go) still pin the unchanged list-view shapes; this
// file complements them.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/backend"
)

// backendMessagef builds a backend.Message with a printf-style body.
// Used in the cycle-226 tests to populate m.session with synthetic
// user-role turns without making the test files read like source.
func backendMessagef(role, format string, args ...any) backend.Message {
	return backend.Message{Role: role, Content: fmt.Sprintf(format, args...)}
}

// initCycle226GitRepo is the shared helper that creates a one-commit
// git repo at dir with a tracked file `f.txt` and the test fixture
// "modified f.txt + added g.txt + untracked h.txt" applied on top.
func initCycle226GitRepo(t *testing.T, dir string) {
	t.Helper()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("f.txt", "old\n")
	write("g.txt", "new\n")
	git("add", "-A")
	git("commit", "-qm", "init")
	// Now dirty the working copy.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("old\nCHANGED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "g.txt"), []byte("new\nCHANGED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write("h.txt", "u\n")
}

// TestCycle226PerTurnEmpty keeps the cycle-207 placeholder when the
// session has no user turns. Cycle 207 had this as the only state; the
// regression test pins it so the cycle-226 row-list path does not
// accidentally suppress the placeholder.
func TestCycle226PerTurnEmpty(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 1
	lines := m.panelDiffLinesVCS()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	if len(plain) != 5 {
		t.Fatalf("Per Turn empty = %d rows, want 5:\n%q", len(plain), plain)
	}
	if plain[2] != "   No file changes in conversation" {
		t.Errorf("Per Turn empty body = %q, want the cycle-207 placeholder", plain[2])
	}
}

// TestCycle226PerTurnRows: when session has user turns AND git diff has
// changes, list one row per turn with a snippet ("Turn N · <file>").
// The cursor (default 0) highlights the first row in the brand blue
// cursor style.
func TestCycle226PerTurnRows(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 1
	// 3 user turns in the session (no assistant replies are required
	// for the count — only user-role messages count as a turn).
	for i := 0; i < 3; i++ {
		m.session = append(m.session, backendMessagef("user", "ask %d", i+1))
	}

	lines := m.panelDiffLinesVCS()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	if !strings.HasPrefix(plain[1], "   3 turn(s) · ") {
		t.Errorf("Per Turn summary row = %q", plain[1])
	}
	if !strings.Contains(plain[1], "3 file(s) changed") {
		t.Errorf("Per Turn summary row does not show file count: %q", plain[1])
	}
	// Rows 3, 4, 5 are the three turns. Each should mention "Turn N · ".
	for i, want := range []string{"Turn 1 · ", "Turn 2 · ", "Turn 3 · "} {
		row := plain[3+i]
		if !strings.Contains(row, want) {
			t.Errorf("Per Turn row %d = %q, want substring %q", i, row, want)
		}
		// Each row carries a file path from the working copy.
		if !strings.Contains(row, ".txt") {
			t.Errorf("Per Turn row %d has no file: %q", i, row)
		}
	}
	// Cursor row 0 carries the brand-blue "> " marker.
	if !strings.Contains(lines[3], cBlueOpen+"> "+cBlueClose) {
		t.Errorf("Per Turn cursor row missing brand-blue marker: %q", lines[3])
	}
}

// TestCycle226PerTurnNoChanges: turns exist but the working copy is
// clean. The cycle-207 placeholder is preserved (a turn list would be
// misleading without changes to show).
func TestCycle226PerTurnNoChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	// Clean repo: one commit, no further mutations.
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "-A"}, {"commit", "-qm", "init"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = os.Environ()
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 1
	m.session = append(m.session, backendMessagef("user", "ask 1"))
	lines := m.panelDiffLinesVCS()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	if plain[2] != "   No file changes in conversation" {
		t.Errorf("Per Turn no-changes body = %q, want the cycle-207 placeholder", plain[2])
	}
}

// TestCycle226AllChangesEnter: pressing Enter on All Changes opens the
// inline-diff subview (`git diff -- <path>`) for the file under the
// cursor. The subview title carries the path; + lines are styled green
// and - lines red-orange; Esc returns to the list (does NOT close the
// panel).
func TestCycle226AllChangesEnter(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 0
	// Cursor on f.txt (the first tracked file, sorted alphabetically).
	m.panel.cursor = 0
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.diffView != 1 {
		t.Fatalf("diffView = %d, want 1 (file inline)", m.panel.diffView)
	}
	if m.panel.diffViewPath != "f.txt" {
		t.Errorf("diffViewPath = %q, want f.txt", m.panel.diffViewPath)
	}
	lines := m.panelDiffLinesVCS()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	// Title row carries the path.
	if plain[0] != "f.txt" {
		t.Errorf("inline title = %q, want f.txt", plain[0])
	}
	// The body should mention a real + line (the "+CHANGED" addition).
	foundPlus := false
	for _, l := range plain {
		if strings.Contains(l, "+CHANGED") {
			foundPlus = true
			break
		}
	}
	if !foundPlus {
		t.Errorf("inline body missing +CHANGED: %q", plain)
	}
	// Esc returns to the list; the panel stays open.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("Esc not handled")
	}
	if m.panel.diffView != 0 {
		t.Errorf("after Esc diffView = %d, want 0", m.panel.diffView)
	}
	if m.panel.kind != panelDiff {
		t.Errorf("after Esc panel.kind = %d, want panelDiff", m.panel.kind)
	}
}

// TestCycle226CommitTreeEnter: pressing Enter on a commit row opens
// the inline-diff subview (`git show <hash>`) for that commit. The
// subview title is `commit <hash>`; the body contains the commit
// metadata + diff.
func TestCycle226CommitTreeEnter(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 2
	// Move cursor to the first commit (cursor==0 = working-copy row;
	// cursor==1 = first commit row).
	m.panel.cursor = 1
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.diffView != 2 {
		t.Fatalf("diffView = %d, want 2 (commit inline)", m.panel.diffView)
	}
	if m.panel.diffViewPath == "" {
		t.Fatal("diffViewPath empty")
	}
	// The diffViewPath should be the commit hash (git show's short
	// hash is 7 chars). Fetch the real hash for comparison.
	logOut, err := exec.Command("git", "-C", dir, "log", "--pretty=%h").Output()
	if err != nil {
		t.Fatal(err)
	}
	wantHash := strings.SplitN(strings.TrimRight(string(logOut), "\n"), "\n", 2)[0]
	if m.panel.diffViewPath != wantHash {
		t.Errorf("diffViewPath = %q, want commit hash %q", m.panel.diffViewPath, wantHash)
	}
	lines := m.panelDiffLinesVCS()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	if !strings.HasPrefix(plain[0], "commit ") {
		t.Errorf("inline title = %q, want \"commit <hash>\"", plain[0])
	}
	// Body should mention the commit message ("init").
	foundInit := false
	for _, l := range plain {
		if strings.Contains(l, "init") {
			foundInit = true
			break
		}
	}
	if !foundInit {
		t.Errorf("inline body missing commit subject: %q", plain)
	}
}

// TestCycle226WorkingCopyEnter: cursor==0 on Commit Tree is the
// working-copy row. Enter there is a no-op (working copy has no
// per-commit hash to drill into).
func TestCycle226WorkingCopyEnter(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 2
	m.panel.cursor = 0 // working-copy row
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.diffView != 0 {
		t.Errorf("Enter on working-copy row set diffView = %d, want 0", m.panel.diffView)
	}
}

// TestCycle226DiffCursorCommitTree: cycle-207 had the row cursor inert
// on Commit Tree; cycle 226 unifies the cursor model. Down moves
// cursor from 0 (working copy) to 1 (first commit), and over the cap
// it clamps.
func TestCycle226DiffCursorCommitTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 2
	m.panel.cursor = 0
	m.diffMoveCursor(1)
	if m.panel.cursor != 1 {
		t.Errorf("after Down cursor = %d, want 1", m.panel.cursor)
	}
	// Single-commit repo: cap is 1 (cursor==0 working-copy, cursor==1 commit).
	m.diffMoveCursor(1)
	if m.panel.cursor != 1 {
		t.Errorf("after Down at cap cursor = %d, want 1 (clamped)", m.panel.cursor)
	}
	m.diffMoveCursor(-1)
	if m.panel.cursor != 0 {
		t.Errorf("after Up cursor = %d, want 0", m.panel.cursor)
	}
}

// TestCycle226DiffCursorPerTurn: Per Turn row list supports cursor
// navigation when session has turns AND working copy has changes.
func TestCycle226DiffCursorPerTurn(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 1
	for i := 0; i < 5; i++ {
		m.session = append(m.session, backendMessagef("user", "ask %d", i+1))
	}
	m.panel.cursor = 0
	m.diffMoveCursor(1)
	if m.panel.cursor != 1 {
		t.Errorf("Per Turn after Down cursor = %d, want 1", m.panel.cursor)
	}
	m.diffMoveCursor(-1)
	if m.panel.cursor != 0 {
		t.Errorf("Per Turn after Up cursor = %d, want 0", m.panel.cursor)
	}
	// cap: 4 (cursor indexes 0..4 over 5 turns).
	for i := 0; i < 10; i++ {
		m.diffMoveCursor(1)
	}
	if m.panel.cursor != 4 {
		t.Errorf("Per Turn clamped cursor = %d, want 4", m.panel.cursor)
	}
}

// TestCycle226CommitTreeMultiCommitShape: the cycle-207 connector
// logic was a "|" before every commit, but REF only puts the
// connector between working-copy and the first commit; subsequent
// commits stack directly. The cycle-226 fix must match.
func TestCycle226CommitTreeMultiCommitShape(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "first")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\ny\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-qm", "second")

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 2
	lines := m.panelDiffLinesVCS()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	// Layout (cycle-226 correction):
	//   0: title
	//   1: 2 commit(s) available
	//   2: (blank)
	//   3: ▌ *  Working Copy ...
	//   4:   |                    <- the connector, ONLY here
	//   5:   * <hash1> first
	//   6:   * <hash2> second     <- no "|" above this row
	//   7: (blank)
	//   8: legend
	if plain[4] != "   |" {
		t.Errorf("row 4 = %q, want connector \"   |\"", plain[4])
	}
	if plain[5] == "   |" || !strings.HasPrefix(plain[5], "   * ") {
		t.Errorf("row 5 = %q, want first commit row, no extra connector", plain[5])
	}
	if plain[6] == "   |" || !strings.HasPrefix(plain[6], "   * ") {
		t.Errorf("row 6 = %q, want second commit row, no extra connector", plain[6])
	}
}

// TestCycle226PanelKeyDefaults: ensure the cycle-207 key actions are
// preserved when diffView == 0 (the list view). Tab switch resets the
// cursor, esc closes the panel.
func TestCycle226PanelKeyDefaults(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 0
	m.panel.cursor = 2
	// Right arrow advances the tab; cycle-226 resets the cursor to 0.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("Right not handled")
	}
	if m.panel.tab != 1 {
		t.Errorf("after Right tab = %d, want 1", m.panel.tab)
	}
	if m.panel.cursor != 0 {
		t.Errorf("after Right cursor = %d, want 0 (reset)", m.panel.cursor)
	}
	// Esc closes the panel (cycle-207 behaviour, preserved). closePanel
	// returns a nil Cmd; the contract is `handled == true` + state
	// cleared.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Error("Esc on list view not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("after Esc panel.kind = %d, want panelNone", m.panel.kind)
	}
}
