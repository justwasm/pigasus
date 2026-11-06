// diff_host_test.go — the host's side of the /diff panel.
//
// The panel itself is tested in internal/ui/views/diffpanel: it owns the
// three tabs, the cursor, the inline diff subview, the per-hunk fold
// state, the keys and the rows.
//
// This file covers what only the host owns:
//
//   - the /diff command routes to the panel,
//   - the panel is rebuilt on every open,
//   - the panel's exit notice is the reference's,
//   - Ctrl+O reaches the panel's fold and never the transcript's
//     expansion.

package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

// initDiffHostRepo creates a git repository with one tracked file that
// carries an unstaged modification, so the /diff panel's All Changes
// tab has a row to open and the inline diff has one hunk.
func initDiffHostRepo(t *testing.T, dir string) {
	t.Helper()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.test",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.test")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	body := "one\ntwo\nthree\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	git("init")
	git("add", ".")
	git("commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\nCHANGED\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestDiffPanelIsRoutedToThePanel — "/diff" opens the /diff panel.
func TestDiffPanelIsRoutedToThePanel(t *testing.T) {
	kind, ok := slashcommands.PanelFor("diff")
	if !ok || kind != panelregistry.Diff {
		t.Fatalf("slashcommands.PanelFor(diff) = %v, %v, want panelregistry.Diff, true", kind, ok)
	}
}

// TestDiffPanelExitNotice — /diff echoes the exit notice. The 1.4.8
// probe lists /diff among the panels that answer "Exited /diff
// command" when they close.
func TestDiffPanelExitNotice(t *testing.T) {
	if !panelregistry.Diff.ExitNotice() {
		t.Error("panelregistry.Diff.ExitNotice() = false, want true")
	}
}

// TestDiffPanelIsRebuiltOnOpen — the panel is rebuilt every time it
// opens, so a stale tab or a stale cursor is never shown.
func TestDiffPanelIsRebuiltOnOpen(t *testing.T) {
	m := &runMode{}
	if !m.openSlashPanel("diff") {
		t.Fatal("openSlashPanel(diff) = false, want true")
	}
	if m.overlays.panel.df == nil {
		t.Fatal("the /diff panel was not built on open")
	}
	m.diffPanel().SetTab(2)
	m.diffPanel().SetCursor(3)

	if !m.openSlashPanel("diff") {
		t.Fatal("re-opening the /diff panel failed")
	}
	if got := m.diffPanel().Tab(); got != 0 {
		t.Errorf("the reopened panel's tab = %d, want 0", got)
	}
	if got := m.diffPanel().Cursor(); got != 0 {
		t.Errorf("the reopened panel's cursor = %d, want 0", got)
	}
}

// TestDiffPanelCtrlOFoldsTheHunk — Ctrl+O in the /diff panel reaches
// the panel's fold and leaves the transcript's expansion alone.
//
// The host intercepts Ctrl+O before the panel sees it (the panel's own
// handler has the same binding, but the host's global handler runs
// first), so the routing is the host's own fact.
func TestDiffPanelCtrlOFoldsTheHunk(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initDiffHostRepo(t, dir)

	m := &runMode{}
	m.session.cwd = dir
	if !m.openSlashPanel("diff") {
		t.Fatal("openSlashPanel(diff) = false, want true")
	}
	// Enter opens the inline diff subview.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter was not handled by the /diff panel")
	}
	if got := m.diffPanel().View(); got != 1 {
		t.Fatalf("the panel's view = %d, want 1 (file inline)", got)
	}

	// Put the inline cursor on the hunk header: the body's first row
	// is the "diff --git" file header and the hunk header follows it.
	row := diffHunkHeaderRow(t, m)
	m.diffPanel().SetInlineRow(row)

	m.viewport.expanded = false
	_, cmd := m.Update(ctrlKey('o'))
	if cmd != nil {
		t.Errorf("ctrl+o in the /diff panel returned cmd %v, want nil", cmd)
	}
	if m.viewport.expanded {
		t.Error("ctrl+o in the /diff panel mutated m.viewport.expanded")
	}
	folded := m.diffPanel().Folded()
	if len(folded) != 1 || !folded[0] {
		t.Errorf("ctrl+o in the /diff panel left the fold state %v, want hunk 0 folded", folded)
	}
}

// diffHunkHeaderRow returns the inline body's row index of the first
// hunk header.
//
// The inline subview renders the title and a blank row above the body,
// so the body's own row index is the rendered index minus the body's
// first row — the "diff --git" file header.
func diffHunkHeaderRow(t *testing.T, m *runMode) int {
	t.Helper()
	lines := m.diffPanel().Lines()
	bodyStart := -1
	hunk := -1
	for i, l := range lines {
		plain := strings.TrimLeft(termcolor.StripSGR(l), " ")
		plain = strings.TrimPrefix(plain, "> ")
		plain = strings.TrimLeft(plain, " ")
		switch {
		case bodyStart < 0 && strings.HasPrefix(plain, "diff --git"):
			bodyStart = i
		case hunk < 0 && strings.HasPrefix(plain, "@@"):
			hunk = i
		}
	}
	if bodyStart < 0 {
		t.Fatalf("the inline diff has no file header: %q", lines)
	}
	if hunk < 0 {
		t.Fatalf("the inline diff has no hunk header: %q", lines)
	}
	return hunk - bodyStart
}
