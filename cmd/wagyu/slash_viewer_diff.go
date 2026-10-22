package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
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
type diffChange struct {
	path      string
	added     int
	deleted   int
	untracked bool
}

// gitDiffChanges returns the tracked then untracked changes of the git
// working copy rooted (anywhere) at dir. Tracked counts come from
// `git diff --numstat` plus `git diff --cached --numstat`; untracked files
// are counted directly, the way `git diff --numstat` reports a new file.
// It returns ok=false when git is unavailable or dir is not a repo.
func parseGitStatusPorcelain(dir, status string) (map[string]diffChange, []diffChange) {
	known := map[string]diffChange{}
	var untracked []diffChange
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 4 {
			continue
		}
		code, path := line[:2], line[3:]
		if code == "??" {
			ut := diffChange{path: path, added: countFileLines(dir, path), untracked: true}
			untracked = append(untracked, ut)
			continue
		}
		if !strings.ContainsAny(code, "MADRCUT") {
			continue
		}
		known[path] = diffChange{path: path}
	}
	return known, untracked
}

func applyGitNumstat(dir string, known map[string]diffChange, args []string) {
	numstat, err := runGit(dir, args...)
	if err != nil {
		return
	}
	for _, line := range strings.Split(numstat, "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		c, exists := known[fields[2]]
		if !exists {
			continue
		}
		c.added += diffAtoi(fields[0])
		c.deleted += diffAtoi(fields[1])
		known[fields[2]] = c
	}
}

func gitDiffChanges(dir string) (tracked, untracked []diffChange, ok bool) {
	status, err := runGit(dir, "status", "--porcelain", "-uall")
	if err != nil {
		return nil, nil, false
	}
	known, untracked := parseGitStatusPorcelain(dir, status)
	if len(known) == 0 {
		sortChanges(untracked)
		return nil, untracked, true
	}
	applyGitNumstat(dir, known, []string{"diff", "--numstat"})
	applyGitNumstat(dir, known, []string{"diff", "--cached", "--numstat"})
	for _, c := range known {
		tracked = append(tracked, c)
	}
	sortChanges(tracked)
	sortChanges(untracked)
	return tracked, untracked, true
}

// runGitEnvKVs is the slice of KEY=VALUE pairs runGit always injects on top
// of os.Environ() before exec'ing git. Mirrors internal/tools.AgentShellEnvKVs
// in shape (a package-level slice + a small helper that prepends the parent
// environment) so unit tests can pin the contract without invoking git.
//
// Two groups:
//
//   - Pager disabling: PAGER=cat, GIT_PAGER=cat, JJ_PAGER=cat. AGY 1.3.3
//     changelog lines 50-51: "Fixed jj (Jujutsu VCS) command output being
//     broken by the `less` pager — JJ_PAGER=cat is now set alongside
//     GIT_PAGER=cat so commands like `jj log` show full output instead of
//     paging through interactive less." `cat` writes the file to stdout
//     unchanged. Even though runGit only invokes `git` today, JJ_PAGER=cat
//     travels alongside GIT_PAGER=cat so the two settings stay in lockstep
//     with the agent-shell-env trio in internal/tools/command.go and the
//     vcsDiff env in slash_actions.go — a future caller that pipes git
//     output into a jj-aware step inherits the same pager contract.
//   - Editor disabling: GIT_EDITOR=false. AGY 1.3.6 changelog line 7: a
//     future caller invoking `git commit` without `-m` cannot hang the
//     TUI on a hidden vim. HGEDITOR / JJ_EDITOR are omitted because runGit
//     only ever execs git; vcsDiff (slash_actions.go) is the multi-VCS path
//     and sets the full trio.
//   - Locale pinning: LC_ALL=C. Keeps `git status` / `git diff` parseable
//     across locales (a German LANG= setting turns "On branch main" into
//     something ungreppable).
var runGitEnvKVs = []string{
	"PAGER=cat",
	"GIT_PAGER=cat",
	"JJ_PAGER=cat",
	"GIT_EDITOR=false",
	"LC_ALL=C",
}

// runGitEnv returns os.Environ() with runGitEnvKVs appended. The KVs are
// appended *after* the parent environment so on POSIX systems where
// duplicate env entries take the last value, our overrides win over
// anything the user's shell set (e.g. a GIT_PAGER=less from their rcfile).
func runGitEnv() []string {
	return append(os.Environ(), runGitEnvKVs...)
}

// runGit runs a git subcommand in dir and returns its trimmed stdout.
// PAGER=cat / GIT_PAGER=cat / JJ_PAGER=cat disable any pager so a long
// `git log` lands on stdout. GIT_EDITOR=false (1.3.6 line 7) keeps a
// future `git commit` without `-m` from wedging the TUI on a hidden vim.
// LC_ALL=C keeps diffs and status parseable across locales.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = runGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// countFileLines counts the lines git would report for a brand-new file:
// newline count, plus one when the file does not end in a newline, and zero
// for an empty file.
func countFileLines(dir, path string) int {
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil || len(data) == 0 {
		return 0
	}
	n := strings.Count(string(data), "\n")
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}

func diffAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func sortChanges(cs []diffChange) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].path < cs[j].path })
}

// diffCounts renders a change's " +N -M" suffix (each part omitted when
// zero), matching ref_usage… style: +N in the effort-low green and -M in
// the error red-orange. The leading space is added by the row builder.
func diffCounts(c diffChange) string {
	var parts []string
	if c.added > 0 {
		parts = append(parts, panelEffLowOpen+"+"+strconv.Itoa(c.added)+"\x1b[39m")
	}
	if c.deleted > 0 {
		parts = append(parts, cToolBulletErrOpen+"-"+strconv.Itoa(c.deleted)+"\x1b[39m")
	}
	return strings.Join(parts, " ")
}

// diffTabRow is the /diff header. The active tab is brand blue, the others
// are plain; `leading` writes the one-space left margin REF shows. The
// margin is present in every captured view except All Changes with changes
// to list, which is flush-left: diff3/diff3_dd/diff3_jk/diffA/diffB (no
// margin) versus diffD/diffE/diffclean2 (one space). 【一手】
func diffTabRow(vcs string, active int, leading bool) string {
	var b strings.Builder
	if leading {
		b.WriteString(" ")
	}
	b.WriteString("\x1b[1mDiff (" + vcs + ")\x1b[0m")
	for i, name := range diffViews {
		b.WriteString("  ")
		if i == active {
			b.WriteString(cBlueOpen + name + cBlueClose)
		} else {
			b.WriteString(name)
		}
	}
	return b.String()
}

// diffAllChangesRow renders one change row. The cursor row carries the
// "> " marker in brand blue; an untracked row carries the orange
// "[Untracked]" badge.
func diffAllChangesRow(c diffChange, cursor bool) string {
	var b strings.Builder
	switch {
	case cursor && c.untracked:
		b.WriteString(cBlueOpen + "> " + "\x1b[38;5;172m? [Untracked] " + cBlueOpen + c.path + cBlueClose)
	case cursor:
		b.WriteString(cBlueOpen + "> " + c.path + cBlueClose)
	case c.untracked:
		b.WriteString("  " + "\x1b[38;5;172m? [Untracked] " + "\x1b[39m" + c.path)
	default:
		b.WriteString("  " + c.path)
	}
	if counts := diffCounts(c); counts != "" {
		b.WriteString("  ")
		b.WriteString(counts)
	}
	return b.String()
}

// diffAllChangesLines renders the tab's file list, or the empty state.
// cursor indexes the flattened (tracked + untracked) list.
func diffAllChangesLines(tracked, untracked []diffChange, cursor int) []string {
	total := len(tracked) + len(untracked)
	if total == 0 {
		return []string{
			diffTabRow("git", 0, true),
			"",
			"   " + cDimGrayOpen + "No uncommitted changes" + cDimGrayClose,
			"",
			"   " + panelDotHint("", "←/→", "switch view", "esc", "dismiss"),
		}
	}
	if cursor < 0 || cursor >= total {
		cursor = 0
	}
	lines := []string{
		diffTabRow("git", 0, false),
		panelGray238 + "  " + strconv.Itoa(total) + " file(s) changed" + panelGray238C,
		"",
	}
	for i, c := range tracked {
		lines = append(lines, diffAllChangesRow(c, cursor == i))
	}
	if len(tracked) > 0 && len(untracked) > 0 {
		lines = append(lines, "")
	}
	for i, c := range untracked {
		lines = append(lines, diffAllChangesRow(c, cursor == len(tracked)+i))
	}
	lines = append(lines, "",
		"  "+panelDotHint("", "←/→", "switch view", "↑/↓/j/k", "navigate", "enter", "view diff", "esc", "dismiss"))
	return lines
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

// diffViews are the three tabs of the /diff viewer, in REF order
// (ppref.diff.raw.txt row 1).
var diffViews = []string{"All Changes", "Per Turn", "Commit Tree"}


func (m *runMode) handleDiffCodeKey(key tea.Key) (bool, tea.Cmd, bool) {
	switch key.Code {
	case tea.KeyLeft:
		if m.panel.diffView == 0 && m.panel.tab > 0 {
			m.panel.tab--
			m.panel.cursor = 0
		}
		return true, nil, true
	case tea.KeyRight:
		if m.panel.diffView == 0 && m.panel.tab < len(diffViews)-1 {
			m.panel.tab++
			m.panel.cursor = 0
		}
		return true, nil, true
	case tea.KeyUp:
		if m.panel.diffView != 0 {
			m.moveDiffInlineCursor(-1)
		} else {
			m.diffMoveCursor(-1)
		}
		return true, nil, true
	case tea.KeyDown:
		if m.panel.diffView != 0 {
			m.moveDiffInlineCursor(1)
		} else {
			m.diffMoveCursor(1)
		}
		return true, nil, true
	case tea.KeyEnter:
		if m.panel.diffView == 0 {
			m.diffEnter()
		}
		return true, nil, true
	case tea.KeyEsc:
		if m.panel.diffView != 0 {
			m.diffCloseInline()
			return true, nil, true
		}
		return true, m.closePanel(), true
	}
	return false, nil, false
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



