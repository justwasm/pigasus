package cliapp

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/diffaction"
	"github.com/justwasm/pigasus/internal/tuilib"
)

// /diff — REF 1.4.0 git working-copy view
// ----------------------------------------------------------------------
//
// REF 1.4.0 detects the workspace's VCS. In a git tree /diff's All Changes
// tab shows a header row, a "N file(s) changed" count, the tracked files
// (path-sorted, each with its +added/-deleted counts), a blank, the
// untracked files and a legend. The other two tabs are placeholders
// ("No file changes in conversation" / a commit list). Recovered from the
// diff3/diff50/diffA/diffD/diffE captures against three git fixtures.

// diffChange is one working-copy file with its line counts.
//
// The model itself now lives in internal/diffaction; the alias keeps the
// clone's own vocabulary (and its tests').
type diffChange = diffaction.Change

// parseGitStatusPorcelain reads `git status --porcelain -uall` output and
// returns the tracked files (keyed by path, with no counts yet) and the
// untracked files (with their counts already measured).
//
// The rule itself now lives in internal/diffaction.
func parseGitStatusPorcelain(dir, status string) (map[string]diffChange, []diffChange) {
	return diffaction.ParseGitStatusPorcelain(dir, status)
}

// applyGitNumstat runs `git <args...> --numstat` in dir and folds the
// reported added/deleted counts into the known map.
//
// The rule itself now lives in internal/diffaction.
func applyGitNumstat(dir string, known map[string]diffChange, args []string) {
	diffaction.ApplyGitNumstat(dir, known, args)
}

// gitDiffChanges returns the tracked then untracked changes of the git
// working copy rooted (anywhere) at dir. Tracked counts come from
// `git diff --numstat` plus `git diff --cached --numstat`; untracked files
// are counted directly, the way `git diff --numstat` reports a new file.
// It returns ok=false when git is unavailable or dir is not a repo.
//
// The rule itself now lives in internal/diffaction.
func gitDiffChanges(dir string) (tracked, untracked []diffChange, ok bool) {
	return diffaction.GitChanges(dir)
}

// runGitEnvKVs is the slice of KEY=VALUE pairs runGit always injects on top
// of os.Environ() before exec'ing git.
//
// The rule itself now lives in internal/diffaction.
var runGitEnvKVs = diffaction.RunGitEnvKVs

// runGitEnv returns os.Environ() with runGitEnvKVs appended. The KVs are
// appended *after* the parent environment so on POSIX systems where
// duplicate env entries take the last value, our overrides win over
// anything the user's shell set (e.g. a GIT_PAGER=less from their rcfile).
//
// The rule itself now lives in internal/diffaction.
func runGitEnv() []string {
	return diffaction.RunGitEnv()
}

// runGit runs a git subcommand in dir and returns its trimmed stdout.
// PAGER=cat / GIT_PAGER=cat / JJ_PAGER=cat disable any pager so a long
// `git log` lands on stdout. GIT_EDITOR=false (1.3.6 line 7) keeps a
// future `git commit` without `-m` from wedging the TUI on a hidden vim.
// LC_ALL=C keeps diffs and status parseable across locales.
//
// The rule itself now lives in internal/diffaction.
func runGit(dir string, args ...string) (string, error) {
	return diffaction.RunGit(dir, args...)
}

// diffCounts renders a change's " +N -M" suffix.
func diffCounts(c diffChange) string {
	return diffaction.Counts(c)
}

// diffTabRow is the /diff header.
func diffTabRow(vcs string, active int, leading bool) string {
	return diffaction.TabRow(vcs, active, leading, diffViews)
}

// diffAllChangesRow renders one change row.
func diffAllChangesRow(c diffChange, cursor bool) string {
	return diffaction.AllChangesRow(c, cursor)
}

// diffAllChangesLines renders the tab's file list, or the empty state.
func diffAllChangesLines(tracked, untracked []diffChange, cursor int) []string {
	emptyHint := panelDotHint("", "←/→", "switch view", "esc", "dismiss")
	navHint := panelDotHint("", "←/→", "switch view", "↑/↓/j/k", "navigate", "enter", "view diff", "esc", "dismiss")
	return diffaction.AllChangesLines(tracked, untracked, cursor, emptyHint, navHint)
}

// panelDiffLinesVCS renders /diff in a git workspace: the All Changes list,
// the Per Turn placeholder or the Commit Tree. Per Turn has no captured
// per-turn model (the clone does not track per-turn changes), and the
// Commit Tree is only ever captured with a single commit, so both carry a
// 【推测】 marker in the cycle report.
//
// Cycle 226: when the user has pressed enter on a working-copy file row
// (tab 0) or a commit row (tab 2), m.panel.diffView is non-zero and the
// view is replaced by the inline diff body (panelDiffInlineLines); Esc
// returns to the list. Per Turn (tab 1) keeps the placeholder.
func (m *runMode) panelDiffLinesVCS() []string {
	if m.panel.diffView != 0 {
		return m.panelDiffInlineLines()
	}
	tab := m.panel.tab
	if tab < 0 || tab >= len(diffViews) {
		tab = 0
	}
	switch tab {
	case 1: // Per Turn
		return m.diffPerTurnLines()
	case 2: // Commit Tree
		return m.diffCommitTreeLines()
	default:
		tracked, untracked, _ := gitDiffChanges(m.cwd)
		return diffAllChangesLines(tracked, untracked, m.panel.cursor)
	}
}

// diffCommitTreeLines renders the Commit Tree tab. Only the single-commit
// shape was captured (diffE/diff_right2), so the cycle-207 implementation
// extrapolated a "|" tree connector before every commit. The
// multi-commit probe (agy226/diff50-CT.ref.raw.txt) shows the connector
// is only emitted **once**, between the working-copy row and the first
// commit: subsequent commits are stacked directly. Cycle 226 corrects
// the connector logic and the cursor mapping (cycle-207 had the row
// cursor inert on Commit Tree; cycle 226 unifies it with the other
// tabs).

// diffMoveCursor moves /diff's row cursor over the active tab's row
// list. Cycle 226 extends the cycle-207 cursor model (All Changes
// only) to Per Turn and Commit Tree, both of which now carry row lists
// the user can navigate. The inline subview (m.panel.diffView != 0)
// owns its own scroll, so the cursor is inert there.
//
// Cursor mapping per tab:
//   - All Changes (0): cursor is the index into tracked+untracked.
//   - Per Turn (1):    cursor is the index into the turns list.
//   - Commit Tree (2): cursor is the index into working-copy (0) plus
//     the N commits (1..N).
//
// Cycle 207 retained the no-VCS short-circuit; this still holds.

// ----------------------------------------------------------------------

func (m *runMode) panelDiffLines() []string {
	if m.cwd != "" && detectVCS(m.cwd) == "git" {
		return m.panelDiffLinesVCS()
	}
	cwd, _ := os.Getwd()
	if cwd == "" {
		cwd = "."
	}
	tab := m.panel.tab
	if tab < 0 || tab >= len(diffViews) {
		tab = 0
	}
	var row strings.Builder
	row.WriteString(" \x1b[1mDiff\x1b[0m")
	for i, name := range diffViews {
		row.WriteString("  ")
		if i == tab {
			row.WriteString(cBlueOpen + name + cBlueClose)
		} else {
			row.WriteString(name)
		}
	}
	return []string{
		row.String(),
		"",
		"   " + cToolBulletErrOpen + "\u26a0" + "\x1b[39m" +
			" no supported VCS found in " + cwd,
		"",
		panelDotHint("  ", "←/→", "switch view", "esc", "dismiss"),
	}
}

// diffViews are the three tabs of the /diff viewer, in REF order.
var diffViews = diffaction.Views

func (m *runMode) handleDiffCodeKey(key tea.Key) (bool, tea.Cmd, bool) {
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavLeft:
		if m.panel.diffView == 0 && m.panel.tab > 0 {
			m.panel.tab--
			m.panel.cursor = 0
		}
		return true, nil, true
	case tuilib.NavRight:
		if m.panel.diffView == 0 && m.panel.tab < len(diffViews)-1 {
			m.panel.tab++
			m.panel.cursor = 0
		}
		return true, nil, true
	case tuilib.NavUp:
		m.diffNavStep(-1)
		return true, nil, true
	case tuilib.NavDown:
		m.diffNavStep(1)
		return true, nil, true
	case tuilib.NavTop:
		m.diffNavStep(-999999)
		return true, nil, true
	case tuilib.NavBottom:
		m.diffNavStep(999999)
		return true, nil, true
	case tuilib.NavSelect:
		if m.panel.diffView == 0 {
			m.diffEnter()
		}
		return true, nil, true
	case tuilib.NavCancel:
		if m.panel.diffView != 0 {
			m.diffCloseInline()
			return true, nil, true
		}
		return true, m.closePanel(), true
	}
	return false, nil, false
}

func (m *runMode) diffNavStep(delta int) {
	if m.panel.diffView != 0 {
		m.moveDiffInlineCursor(delta)
	} else {
		m.diffMoveCursor(delta)
	}
}

func (m *runMode) handleDiffPanelKey(key tea.Key) (bool, tea.Cmd) {
	if handled, cmd, ok := m.handleDiffCodeKey(key); ok {
		return handled, cmd
	}
	if d := diffTextKey(key); d != 0 {
		if m.panel.diffView != 0 {
			m.moveDiffInlineCursor(d)
		} else {
			m.diffMoveCursor(d)
		}
		return true, nil
	}
	if key.Mod == 0 && key.Code == 'f' {
		if m.panel.diffView != 0 {
			m.toggleDiffHunkFoldAtCursor()
		}
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

// handleFeedbackTextKey edits the /feedback stage-2 text form: printable
// keys append, backspace deletes, enter (or ctrl+s) submits and esc
// returns to the category picker with the draft discarded.
//
// Cycle 221: openagy's Enter submits. REF's stage-2 form is a textarea
// whose legend advertises ctrl+s (fin_fb_stage2.raw.txt), so the
// reference binds Enter to a newline and ctrl+s to submit; but openagy
// has no upload endpoint and the user can only ever see the local-only
// echo, so a single Enter-submit binding matches the panel's UX. ctrl+s
// is kept as a parallel submit key so muscle memory from REF still
// works. See docs/cycles/cycle-221/.
