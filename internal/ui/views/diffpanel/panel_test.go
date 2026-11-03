// panel_test.go — the /diff panel's rows, its tabs and its keys.
//
// The layouts are transcribed from the reference's own screen (REF
// v1.4.0, 220×30). The assertions compare the *plain* (SGR-stripped)
// text, because tmux's capture-pane normalises 24-bit RGB into
// 256-colour indices, so a raw-byte comparison would only be possible
// against the normalised form. The styling is asserted separately, by
// checking the colour spans the capture actually shows.
//
// The /diff panel itself — its rows, its tabs, its inline subview and
// its keys — lives in this package. The host keeps only the wiring:
// the command routing, the exit notice and the Ctrl+O shortcut.

package diffpanel

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
)

// /diff — no-VCS variant with tab row + warning + dot hint.
func TestPanelDiffLayout(t *testing.T) {
	p := newTestPanel(t, "")
	plain := plainLines(p)
	// Tab row: bold "Diff" (active tab) plus three tab names.
	joined := strings.Join(plain, "\n")
	if !strings.Contains(plain[0], "Diff") {
		t.Errorf("missing Diff heading: %q", plain[0])
	}
	if !strings.Contains(plain[0], "All Changes") {
		t.Errorf("missing All Changes tab: %q", plain[0])
	}
	if !strings.Contains(plain[0], "Per Turn") {
		t.Errorf("missing Per Turn tab: %q", plain[0])
	}
	if !strings.Contains(plain[0], "Commit Tree") {
		t.Errorf("missing Commit Tree tab: %q", plain[0])
	}
	// Warning row uses the tool-bullet error colour on the ⚠ glyph.
	if !strings.Contains(joined, "no supported VCS found") {
		t.Errorf("missing VCS warning: %s", joined)
	}
	lines := p.Lines()
	if !strings.Contains(lines[2], toolBulletErrOpen) {
		t.Errorf("warning glyph should be 166: %q", lines[2])
	}
	// Dot hint legends use ←/→ switch view, esc dismiss.
	last := stripSGR(plain[len(plain)-1])
	if !strings.Contains(last, "switch view") || !strings.Contains(last, "dismiss") {
		t.Errorf("legend = %q", last)
	}
}

// /diff's ←/→ switch the active tab. The plain text is identical for all
// three tabs, so the assertion is on which tab carries the brand blue.
func TestPanelDiffTabSwitch(t *testing.T) {
	p := newTestPanel(t, "")
	tabRow := func() string { return p.Lines()[0] }
	if !strings.Contains(tabRow(), blueOpen+"All Changes"+blueClose) {
		t.Fatalf("All Changes should be active at open: %q", tabRow())
	}
	// Two presses move Per Turn then Commit Tree; a third clamps.
	for i, want := range []struct {
		tab  int
		name string
	}{{1, "Per Turn"}, {2, "Commit Tree"}, {2, "Commit Tree"}} {
		if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyRight}); !handled {
			t.Fatalf("right #%d not handled", i)
		}
		if p.tab != want.tab {
			t.Fatalf("right #%d: tab = %d, want %d", i, p.tab, want.tab)
		}
		if !strings.Contains(tabRow(), blueOpen+want.name+blueClose) {
			t.Errorf("tab %d: %q should be active: %q", want.tab, want.name, tabRow())
		}
	}
	for i := 0; i < 5; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyLeft})
	}
	if p.tab != 0 {
		t.Errorf("left should clamp at the first tab, got %d", p.tab)
	}
	if handled, _ := p.HandleKey(tea.Key{Code: 'c', Mod: tea.ModCtrl}); handled {
		t.Error("ctrl+c must fall through to the global binding")
	}
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("esc not handled")
	}
	if !p.closed {
		t.Fatal("esc did not close the diff panel")
	}
}

// The panel owns the "q" key too: the reference's list navigation
// treats it as Esc, so the panel closes on it.
func TestDiff_VimKeybindings(t *testing.T) {
	p := newTestPanel(t, "")
	p.cursor = 0

	handled, _ := p.HandleKey(tea.Key{Code: 'q', Text: "q"})
	if !handled || !p.closed {
		t.Errorf("diff 'q': handled=%v, closed=%v, want the panel closed", handled, p.closed)
	}
}

// diffAllChangesLines reproduces REF's All Changes list byte for byte
// (modulo the colour constants): the fixture is repo3, i.e. the data behind
// diff3.ref.raw.txt.
func TestCycle207DiffAllChanges(t *testing.T) {
	tracked := []Change{
		{Path: "del.txt", Deleted: 2},
		{Path: "mod.txt", Added: 2, Deleted: 1},
	}
	untracked := []Change{
		{Path: "sub/deep.txt", Added: 1, Untracked: true},
		{Path: "untracked.txt", Added: 3, Untracked: true},
	}
	lines := diffaction.AllChangesLines(tracked, untracked, 0, emptyHint(), navHint())

	wantPlain := []string{
		"Diff (git)  All Changes  Per Turn  Commit Tree",
		"  4 file(s) changed",
		"",
		"> del.txt  -2",
		"  mod.txt  +2 -1",
		"",
		"  ? [Untracked] sub/deep.txt  +1",
		"  ? [Untracked] untracked.txt  +3",
		"",
		"  ←/→ switch view · ↑/↓/j/k navigate · enter view diff · esc dismiss",
	}
	if len(lines) != len(wantPlain) {
		t.Fatalf("lines = %d, want %d:\n%q", len(lines), len(wantPlain), lines)
	}
	for i, want := range wantPlain {
		if got := stripSGR(lines[i]); got != want {
			t.Errorf("line %d: got %q, want %q", i, got, want)
		}
	}
	// Exact SGR on the cursor row, the untracked badge and the counts.
	if !strings.HasPrefix(lines[0], "\x1b[1mDiff (git)\x1b[0m  "+blueOpen+"All Changes"+blueClose) {
		t.Errorf("title row = %q", lines[0])
	}
	if lines[3] != blueOpen+"> del.txt"+blueClose+"  "+toolBulletErrOpen+"-2\x1b[39m" {
		t.Errorf("cursor row = %q", lines[3])
	}
	if !strings.Contains(lines[6], "\x1b[38;5;172m? [Untracked] \x1b[39m") {
		t.Errorf("untracked badge = %q", lines[6])
	}
	// Moving the cursor to the first untracked row re-colours it (diff3_dd).
	cursorLine := diffaction.AllChangesLines(tracked, untracked, 2, emptyHint(), navHint())
	if !strings.Contains(cursorLine[6], blueOpen+"> "+"\x1b[38;5;172m? [Untracked] "+blueOpen+"sub/deep.txt"+blueClose) {
		t.Errorf("untracked cursor row = %q", cursorLine[6])
	}
}

// The clean All Changes / Per Turn bodies are the indented empty states with
// the short legend.
func TestCycle207DiffEmptyStates(t *testing.T) {
	clean := diffaction.AllChangesLines(nil, nil, 0, emptyHint(), navHint())
	wantClean := []string{
		" Diff (git)  All Changes  Per Turn  Commit Tree",
		"",
		"   No uncommitted changes",
		"",
		"   ←/→ switch view · esc dismiss",
	}
	if len(clean) != len(wantClean) {
		t.Fatalf("clean = %d lines, want %d: %q", len(clean), len(wantClean), clean)
	}
	for i, want := range wantClean {
		if got := stripSGR(clean[i]); got != want {
			t.Errorf("clean line %d: got %q, want %q", i, got, want)
		}
	}
	// The clean title carries the one-space margin REF shows.
	if !strings.HasPrefix(clean[0], " \x1b[1mDiff (git)\x1b[0m") {
		t.Errorf("clean title = %q", clean[0])
	}
	// Per Turn goes through the panel's git view.
	p := newTestPanel(t, "/tmp/nowhere-cycle207")
	p.tab = 1
	pt := p.vcsLines()
	if got := stripSGR(pt[2]); got != "   No file changes in conversation" {
		t.Errorf("per-turn empty = %q", got)
	}
}

// moveCursor clamps over the flattened tracked+untracked list.
func TestCycle207DiffMoveCursor(t *testing.T) {
	// Non-git cwd: inert.
	p := newTestPanel(t, "/tmp/nowhere-cycle207")
	p.moveCursor(1)
	if p.cursor != 0 {
		t.Errorf("cursor moved in a non-repo: %d", p.cursor)
	}
	// Per Turn tab: inert.
	p.tab = 1
	p.moveCursor(1)
	if p.cursor != 0 {
		t.Errorf("cursor moved on Per Turn: %d", p.cursor)
	}
}

// The change list parses a real repo the way REF's list needs it:
// tracked files with numstat counts, untracked files with their line
// counts.
func TestCycle207DiffGitChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
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
	git(t, dir, "init", "-q")
	write("del.txt", "a\nb\n")
	write("mod.txt", "line1\nline2\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")

	if err := os.Remove(filepath.Join(dir, "del.txt")); err != nil {
		t.Fatal(err)
	}
	write("mod.txt", "line1\nCHANGED\n")
	write("sub/deep.txt", "d\n")
	write("untracked.txt", "u1\nu2\nu3\n")

	tracked, untracked, ok := diffaction.GitChanges(dir)
	if !ok {
		t.Fatal("GitChanges reported not-a-repo")
	}
	if len(tracked) != 2 || len(untracked) != 2 {
		t.Fatalf("tracked=%v untracked=%v", tracked, untracked)
	}
	if tracked[0].Path != "del.txt" || tracked[0].Deleted != 2 || tracked[0].Added != 0 {
		t.Errorf("tracked[0] = %+v, want del.txt -2", tracked[0])
	}
	if tracked[1].Path != "mod.txt" || tracked[1].Added != 1 || tracked[1].Deleted != 1 {
		t.Errorf("tracked[1] = %+v, want mod.txt +1 -1", tracked[1])
	}
	if untracked[0].Path != "sub/deep.txt" || untracked[0].Added != 1 || !untracked[0].Untracked {
		t.Errorf("untracked[0] = %+v, want sub/deep.txt +1", untracked[0])
	}
	if untracked[1].Path != "untracked.txt" || untracked[1].Added != 3 {
		t.Errorf("untracked[1] = %+v, want untracked.txt +3", untracked[1])
	}
}

// A non-repo cwd keeps the no-VCS fallback; the commit tree renders the
// single-commit shape captured for REF.
func TestCycle207DiffCommitTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")

	p := newTestPanel(t, dir)
	p.tab = 2
	lines := p.vcsLines()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	if plain[1] != "   1 commit(s) available" {
		t.Errorf("commit count row = %q", plain[1])
	}
	if plain[3] != " ▌ *  Working Copy (Uncommitted Changes)" {
		t.Errorf("working-copy row = %q", plain[3])
	}
	if !strings.HasPrefix(plain[5], "   * ") || !strings.HasSuffix(plain[5], " init") {
		t.Errorf("commit row = %q", plain[5])
	}
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

	p := newTestPanel(t, dir)
	p.tab = 1
	lines := p.vcsLines()
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

// TestCycle226PerTurnRows: when the session has user turns AND git diff
// has changes, list one row per turn with a snippet ("Turn N · <file>").
// The cursor (default 0) highlights the first row in the brand blue
// cursor style.
func TestCycle226PerTurnRows(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	p := newTestPanel(t, dir)
	p.tab = 1
	// 3 user turns in the session (no assistant replies are required
	// for the count — only user-role messages count as a turn).
	p.turns = 3

	lines := p.vcsLines()
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
	if !strings.Contains(lines[3], blueOpen+"> "+blueClose) {
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
	git(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")

	p := newTestPanel(t, dir)
	p.tab = 1
	p.turns = 1
	lines := p.vcsLines()
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

	p := newTestPanel(t, dir)
	p.tab = 0
	// Cursor on f.txt (the first tracked file, sorted alphabetically).
	p.cursor = 0
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if p.view != 1 {
		t.Fatalf("view = %d, want 1 (file inline)", p.view)
	}
	if p.viewPath != "f.txt" {
		t.Errorf("viewPath = %q, want f.txt", p.viewPath)
	}
	lines := p.vcsLines()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	// Title row carries the path.
	if plain[0] != "f.txt" {
		t.Errorf("inline title = %q, want f.txt", plain[0])
	}
	// The body should mention a real + line (the CHANGED addition).
	// The reference's row model carries the sign in its marker column.
	foundPlus := false
	for _, l := range plain {
		if strings.Contains(l, " +  CHANGED") {
			foundPlus = true
			break
		}
	}
	if !foundPlus {
		t.Errorf("inline body missing +CHANGED: %q", plain)
	}
	// Esc returns to the list; the panel stays open.
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("Esc not handled")
	}
	if p.view != 0 {
		t.Errorf("after Esc view = %d, want 0", p.view)
	}
	if p.closed {
		t.Error("after Esc the panel must stay open")
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

	p := newTestPanel(t, dir)
	p.tab = 2
	// Move cursor to the first commit (cursor==0 = working-copy row;
	// cursor==1 = first commit row).
	p.cursor = 1
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if p.view != 2 {
		t.Fatalf("view = %d, want 2 (commit inline)", p.view)
	}
	if p.viewPath == "" {
		t.Fatal("viewPath empty")
	}
	// The viewPath should be the commit hash (git show's short
	// hash is 7 chars). Fetch the real hash for comparison.
	logOut, err := exec.Command("git", "-C", dir, "log", "--pretty=%h").Output()
	if err != nil {
		t.Fatal(err)
	}
	wantHash := strings.SplitN(strings.TrimRight(string(logOut), "\n"), "\n", 2)[0]
	if p.viewPath != wantHash {
		t.Errorf("viewPath = %q, want commit hash %q", p.viewPath, wantHash)
	}
	lines := p.vcsLines()
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

	p := newTestPanel(t, dir)
	p.tab = 2
	p.cursor = 0 // working-copy row
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if p.view != 0 {
		t.Errorf("Enter on working-copy row set view = %d, want 0", p.view)
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

	p := newTestPanel(t, dir)
	p.tab = 2
	p.cursor = 0
	p.moveCursor(1)
	if p.cursor != 1 {
		t.Errorf("after Down cursor = %d, want 1", p.cursor)
	}
	// Single-commit repo: cap is 1 (cursor==0 working-copy, cursor==1 commit).
	p.moveCursor(1)
	if p.cursor != 1 {
		t.Errorf("after Down at cap cursor = %d, want 1 (clamped)", p.cursor)
	}
	p.moveCursor(-1)
	if p.cursor != 0 {
		t.Errorf("after Up cursor = %d, want 0", p.cursor)
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

	p := newTestPanel(t, dir)
	p.tab = 1
	p.turns = 5
	p.cursor = 0
	p.moveCursor(1)
	if p.cursor != 1 {
		t.Errorf("Per Turn after Down cursor = %d, want 1", p.cursor)
	}
	p.moveCursor(-1)
	if p.cursor != 0 {
		t.Errorf("Per Turn after Up cursor = %d, want 0", p.cursor)
	}
	// cap: 4 (cursor indexes 0..4 over 5 turns).
	for i := 0; i < 10; i++ {
		p.moveCursor(1)
	}
	if p.cursor != 4 {
		t.Errorf("Per Turn clamped cursor = %d, want 4", p.cursor)
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
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "first")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\ny\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "second")

	p := newTestPanel(t, dir)
	p.tab = 2
	lines := p.vcsLines()
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
// preserved when view == 0 (the list view). Tab switch resets the
// cursor, esc closes the panel.
func TestCycle226PanelKeyDefaults(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	p := newTestPanel(t, dir)
	p.tab = 0
	p.cursor = 2
	// Right arrow advances the tab; cycle-226 resets the cursor to 0.
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("Right not handled")
	}
	if p.tab != 1 {
		t.Errorf("after Right tab = %d, want 1", p.tab)
	}
	if p.cursor != 0 {
		t.Errorf("after Right cursor = %d, want 0 (reset)", p.cursor)
	}
	// Esc closes the panel (cycle-207 behaviour, preserved). The
	// contract is `handled == true` + the way out taken.
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Error("Esc on list view not handled")
	}
	if !p.closed {
		t.Error("after Esc the panel must be closed")
	}
}

// TestDiffTrimEndToEndRealRepo is the integration test: a tracked file
// with trailing tabs is edited to swap trailing-tab content for clean
// content, and the inline-diff subview's body is asserted to contain no
// trailing-tab characters in its rendered form.
func TestDiffTrimEndToEndRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "init", "-q")
	write("tab.txt", "line without tabs\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	if err := os.WriteFile(filepath.Join(dir, "tab.txt"),
		[]byte("line without tabs\nline WITH TRAILING TAB\t\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := newTestPanel(t, dir)
	p.tab = 0
	p.cursor = 0
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if p.view != 1 || p.viewPath != "tab.txt" {
		t.Fatalf("view=%d path=%q, want enter-on-file inline view",
			p.view, p.viewPath)
	}
	lines := p.inlineLines()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	// The reference's row model carries the sign in its four-wide marker
	// column, in front of the text.
	const want = " +  line WITH TRAILING TAB"
	found := false
	for _, l := range plain {
		if strings.Contains(l, want) {
			found = true
			if strings.HasSuffix(l, "\t") || strings.HasSuffix(strings.TrimPrefix(l, "  "), " \t") {
				t.Errorf("rendered diff line still ends in whitespace marker: %q", l)
			}
		}
	}
	if !found {
		t.Errorf("no +line rendered in the trimmed body, lines=%q", plain)
	}
}

// ---- the position indicators ---------------------------------------------

func TestDiffPositionIndicators_AllChanges(t *testing.T) {
	var tracked []Change
	for i := 1; i <= 20; i++ {
		tracked = append(tracked, Change{Path: "file" + strings.Repeat("x", i) + ".go", Added: i})
	}
	// Height 15 -> visible = 15 - 6 = 9
	lines := diffaction.AllChangesLinesWindowed(tracked, nil, 0, 0, 9, emptyHint(), navHint())
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "[1-9 of 20 files]") {
		t.Fatalf("expected indicator [1-9 of 20 files], got:\n%s", joined)
	}

	// Scrolled
	linesScrolled := diffaction.AllChangesLinesWindowed(tracked, nil, 10, 5, 9, emptyHint(), navHint())
	joinedScrolled := strings.Join(linesScrolled, "\n")
	if !strings.Contains(joinedScrolled, "[6-14 of 20 files]") {
		t.Fatalf("expected indicator [6-14 of 20 files], got:\n%s", joinedScrolled)
	}
}

func TestDiffPositionIndicators_PerTurn(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.name", "Test")
	git(t, dir, "config", "user.email", "test@test.com")
	git(t, dir, "commit", "--allow-empty", "-m", "init")

	// Create uncommitted file
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := newTestPanel(t, dir)
	p.height = 15 // visible = 9
	p.tab = 1
	p.turns = 15

	lines := p.perTurnLines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "[1-9 of 15 rows]") {
		t.Fatalf("expected indicator [1-9 of 15 rows], got:\n%s", joined)
	}
}

func TestDiffPositionIndicators_CommitTree(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.name", "Test")
	git(t, dir, "config", "user.email", "test@test.com")
	for i := 1; i <= 12; i++ {
		git(t, dir, "commit", "--allow-empty", "-m", "commit "+string(rune('a'+i)))
	}

	p := newTestPanel(t, dir)
	p.height = 14 // visible = 14 - 6 = 8
	p.tab = 2
	p.cursor = 0

	lines := p.commitTreeLines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "[1-8 of 12 commits]") {
		t.Fatalf("expected indicator [1-8 of 12 commits], got:\n%s", joined)
	}
}
