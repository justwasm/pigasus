// panel_render.go — the /diff panel's rows.
//
// The frames are the reference's own: the tab row, the count row, a
// blank, the rows, a blank and the legend. The three tabs are All
// Changes (the working copy's file list), Per Turn (one row per user
// turn) and Commit Tree (the working-copy row plus the commits).

package diffpanel

import (
	"fmt"
	"os"
	"strconv"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
	"github.com/justwasm/pigasus/palette"
)

// Lines renders the /diff panel.
//
// The panel shows the inline diff subview when one is open; otherwise
// it shows the active tab's list — the working copy's file list in a
// git workspace, the reference's "no supported VCS found" warning
// anywhere else.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	if p.view != 0 {
		return p.inlineLines()
	}
	if p.cwd() != "" && diffaction.DetectVCS(p.cwd()) == "git" {
		return p.vcsLines()
	}
	return p.noVCSLines()
}

// vcsLines renders /diff in a git workspace: the All Changes list, the
// Per Turn list or the Commit Tree.
//
// Per Turn has no captured per-turn model (the clone does not track
// per-turn changes), and the Commit Tree is only ever captured with a
// single commit, so both carry a 【推测】 marker in the cycle report.
//
// Cycle 226: when the user has pressed enter on a working-copy file row
// (tab 0) or a commit row (tab 2), the view is replaced by the inline
// diff body (inlineLines); Esc returns to the list.
func (p *Panel) vcsLines() []string {
	if p == nil {
		return nil
	}
	if p.view != 0 {
		return p.inlineLines()
	}
	tab := p.tab
	if tab < 0 || tab >= len(diffaction.Views) {
		tab = 0
	}
	switch tab {
	case 1: // Per Turn
		return p.perTurnLines()
	case 2: // Commit Tree
		return p.commitTreeLines()
	default:
		tracked, untracked, _ := p.changes()
		total := len(tracked) + len(untracked)
		vis := visibleRowsFor(p.height())
		if vis > 0 && total > vis {
			p.adjustWindow(p.cursor, vis, total)
			return p.allChangesLinesWindowed(tracked, untracked, p.cursor, p.window, vis)
		}
		return p.allChangesLines(tracked, untracked, p.cursor)
	}
}

// tabRow is the /diff header.
func tabRow(vcs string, active int, leading bool) string {
	return diffaction.TabRow(vcs, active, leading, diffaction.Views)
}

// emptyHint is the legend the empty states show: there is nothing to
// navigate.
func emptyHint() string {
	return tuilib.PanelDotHint("", "←/→", "switch view", "esc", "dismiss")
}

// navHint is the legend the All Changes list shows.
func navHint() string {
	return tuilib.PanelDotHint("", "←/→", "switch view", "↑/↓/j/k", "navigate", "enter", "view diff", "esc", "dismiss")
}

// allChangesLines renders the tab's file list, or the empty state.
func (p *Panel) allChangesLines(tracked, untracked []Change, cursor int) []string {
	return diffaction.AllChangesLines(tracked, untracked, cursor, emptyHint(), navHint())
}

// allChangesLinesWindowed renders the tab's file list with the visible
// window and the reference's position indicator.
func (p *Panel) allChangesLinesWindowed(tracked, untracked []Change, cursor, window, visible int) []string {
	return diffaction.AllChangesLinesWindowed(tracked, untracked, cursor, window, visible, emptyHint(), navHint())
}

// noVCSLines renders the /diff panel in a directory that is not a
// repository: the tab row without a VCS name, the warning row and the
// legend.
func (p *Panel) noVCSLines() []string {
	cwd, _ := os.Getwd()
	if cwd == "" {
		cwd = "."
	}
	tab := p.tab
	if tab < 0 || tab >= len(diffaction.Views) {
		tab = 0
	}
	var row []byte
	row = append(row, " \x1b[1mDiff\x1b[0m"...)
	for i, name := range diffaction.Views {
		row = append(row, "  "...)
		if i == tab {
			row = append(row, blueOpen+name+blueClose...)
		} else {
			row = append(row, name...)
		}
	}
	return []string{
		string(row),
		"",
		"   " + toolBulletErrOpen + "⚠" + "\x1b[39m" +
			" no supported VCS found in " + cwd,
		"",
		tuilib.PanelDotHint("  ", "←/→", "switch view", "esc", "dismiss"),
	}
}

// perTurnLines renders the Per Turn tab.
//
// Cycle 226 replaces the blank placeholder with a row per user turn
// when the working copy has changes (the row text is `Turn N · <first
// changed file>`); when the working copy is clean, or the session has
// no user turns, the original "No file changes in conversation"
// placeholder is preserved.
func (p *Panel) perTurnLines() []string {
	tracked, untracked, ok := p.changes()
	if !ok || len(tracked)+len(untracked) == 0 {
		return p.perTurnEmpty()
	}
	turns := p.turns()
	if turns == 0 {
		return p.perTurnEmpty()
	}
	first := firstChangedPath(tracked, untracked)
	all := append(append([]Change{}, tracked...), untracked...)

	vis := visibleRowsFor(p.height())
	start := 0
	end := turns
	indicator := ""
	if vis > 0 && turns > vis {
		p.adjustWindow(p.cursor, vis, turns)
		start = p.window
		end = start + vis
		if end > turns {
			end = turns
		}
		indicator = fmt.Sprintf("  [%d-%d of %d rows]", start+1, end, turns)
	}

	lines := []string{
		tabRow("git", 1, true),
		" " + gray238 + "  " + strconv.Itoa(turns) + " turn(s) · " +
			strconv.Itoa(len(all)) + " file(s) changed" + palette.DimGrayOpen + indicator + palette.DimGrayClose + gray238Close,
		"",
	}
	for i := start; i < end; i++ {
		file := first
		if len(all) > 1 {
			file = all[i%len(all)].Path
		}
		marker := "  "
		if i == p.cursor {
			marker = blueOpen + "> " + blueClose
		}
		lines = append(lines, marker+"Turn "+strconv.Itoa(i+1)+" · "+dimGrayOpen+file+dimGrayClose)
	}
	lines = append(lines, "",
		"   "+tuilib.PanelDotHint("", "↑/↓", "navigate", "←/→", "switch view", "esc", "dismiss"))
	return lines
}

// perTurnEmpty is the Per Turn placeholder: the tab row, the empty
// notice and the legend.
func (p *Panel) perTurnEmpty() []string {
	return []string{
		tabRow("git", 1, true),
		"",
		"   " + dimGrayOpen + "No file changes in conversation" + dimGrayClose,
		"",
		"   " + tuilib.PanelDotHint("", "←/→", "switch view", "esc", "dismiss"),
	}
}

// commitTreeLines renders the Commit Tree tab.
//
// Only the single-commit shape was captured (diffE/diff_right2), so the
// cycle-207 implementation extrapolated a "|" tree connector before
// every commit. The multi-commit probe (agy226/diff50-CT.ref.raw.txt)
// shows the connector is only emitted **once**, between the working-copy
// row and the first commit: subsequent commits are stacked directly.
// Cycle 226 corrects the connector logic and the cursor mapping
// (cycle-207 had the row cursor inert on Commit Tree; cycle 226
// unifies it with the other tabs).
func (p *Panel) commitTreeLines() []string {
	commits := p.commits()

	vis := visibleRowsFor(p.height())
	start := 0
	end := len(commits)
	indicator := ""
	if vis > 0 && len(commits) > vis {
		commitCursor := p.cursor - 1
		if commitCursor < 0 {
			commitCursor = 0
		}
		p.adjustWindow(commitCursor, vis, len(commits))
		start = p.window
		end = start + vis
		if end > len(commits) {
			end = len(commits)
		}
		indicator = fmt.Sprintf("  [%d-%d of %d commits]", start+1, end, len(commits))
	}

	lines := []string{
		tabRow("git", 2, true),
		" " + gray238 + "  " + strconv.Itoa(len(commits)) + " commit(s) available" + palette.DimGrayOpen + indicator + palette.DimGrayClose + gray238Close,
		"",
		" \x1b[38;5;69m\x1b[48;5;255m▌\x1b[39m \x1b[1m*\x1b[0m\x1b[48;5;255m  \x1b[1m\x1b[38;5;172mWorking Copy (Uncommitted Changes)\x1b[0m\x1b[48;5;255m",
	}
	for i := start; i < end; i++ {
		c := commits[i]
		if i == 0 {
			lines = append(lines, "\x1b[49m   |")
		}
		marker := "   * "
		if i+1 == p.cursor {
			marker = " " + blueOpen + "> *" + blueClose + " "
		}
		lines = append(lines, marker+blueOpen+c[0]+blueClose+" "+gray238+c[1]+gray238Close)
	}
	lines = append(lines, "",
		"   "+tuilib.PanelDotHint("", "↑/↓", "navigate", "enter", "view diff", "←/→", "switch view", "esc", "dismiss"))
	return lines
}

// inlineLines renders the inline-diff subview opened by `enter` on All
// Changes (view=1, path=file) or Commit Tree (view=2, path=commit
// hash). Esc returns to the list; the inline view is otherwise inert.
// The body is `git diff/show` output styled line-by-line, with the
// per-hunk fold state applied (see panel_fold.go); the title row
// carries the path or `commit <hash>` so the user knows which diff
// they are looking at.
//
// Cycle diff-syntax-highlight: on the file view (view==1) each
// non-pre-header body line is fed through the row styler so keywords /
// strings / numbers / comments / types are coloured per the Aurora-Dark
// tokens the v1.3.6 binary recovers via `strings ~/.local/bin/agy |
// grep chroma`. The marker byte (the leading `+` / `-` / ` `) keeps the
// existing per-line green / red tint so the visual contract of the
// cycle-127 tool-step renderer (the diff colour applies to the marker
// alone) is preserved. The commit view (view==2) has no single file
// path to bind a language to, so it falls back to the monochrome
// behaviour.
//
// Cycle diff-folding (AGY 1.3.5): the renderer walks the hunks and
// emits each hunk's body either expanded (every body line styled) or
// collapsed (header line + "+N" line-count indicator in dim grey).
// Pre-header lines ("diff --git ...", "index ...", "---", "+++") are
// always rendered uncondensed.
func (p *Panel) inlineLines() []string {
	var title, kind, langFile string
	switch p.view {
	case 1:
		title = p.viewPath
		kind = "file"
		langFile = p.viewPath
	case 2:
		title = "commit " + p.viewPath
		kind = "commit"
	default:
		title = "(no diff)"
		kind = "file"
	}
	lines := []string{
		"\x1b[1m" + title + "\x1b[0m",
		"",
	}
	body := p.body(p.viewPath, kind)
	if len(body) == 0 {
		lines = append(lines, "   "+dimGrayOpen+"no diff available"+dimGrayClose)
	} else {
		rows, _ := p.renderDiffFromBody(body, langFile)
		p.clampInlineCursor(len(rows) - 1)
		cursor := p.inlineRow
		for i, r := range rows {
			if i == cursor {
				lines = append(lines, " "+blueOpen+"> "+blueClose+r)
			} else {
				lines = append(lines, r)
			}
		}
	}
	lines = append(lines, "", diffaction.InlineLegend())
	return lines
}
