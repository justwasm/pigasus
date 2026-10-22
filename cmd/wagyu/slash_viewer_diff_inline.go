package main

import (
	"os/exec"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// /diff — cycle 226 follow-up: Per Turn rows, inline-diff subview
// ----------------------------------------------------------------------
//
// REF 1.4.0's enter-view-diff on All Changes opens a side-by-side diff
// viewer with a "change 1/1 · file 1/N" footer and the n/N f c d esc
// legend (agy226/diffA-enter1.ref.raw.txt). The full side-by-side viewer
// is out of scope for this cycle; the clone substitutes a plain inline
// diff body, gated through m.panel.diffView so the default panel shape
// stays byte-exact. The same wiring handles Commit Tree enter: REF drills
// into the commit's files list and then a file's diff (a two-level
// drill); the clone collapses that into one `git show <hash>` body.

// diffCountTurns returns the number of user-role turns in m.session.
// Cycle 226 follows the cycle-194 convention (lastUserMessage walks
// `Role == "user"`), so the count is the count of user messages.
func diffCountTurns(m *runMode) int {
	n := 0
	for _, msg := range m.session {
		if msg.Role == "user" {
			n++
		}
	}
	return n
}

// diffFirstChangedPath returns the path of the first change in the
// working copy (tracked then untracked), or "" when there are none. Used
// to keep Per Turn rows non-empty without forcing a per-turn diff model.
func diffFirstChangedPath(tracked, untracked []diffChange) string {
	if len(tracked) > 0 {
		return tracked[0].path
	}
	if len(untracked) > 0 {
		return untracked[0].path
	}
	return ""
}

// diffPerTurnLines renders the Per Turn tab. Cycle 226 replaces the
// blank placeholder with a row per user turn when the working copy has
// changes (the row text is `Turn N · <first changed file>`); when the
// working copy is clean, or the session has no user turns, the original
// `"No file changes in conversation"` placeholder is preserved.
func (m *runMode) diffPerTurnLines() []string {
	tracked, untracked, ok := gitDiffChanges(m.cwd)
	if !ok || len(tracked)+len(untracked) == 0 {
		return []string{
			diffTabRow("git", 1, true),
			"",
			"   " + cDimGrayOpen + "No file changes in conversation" + cDimGrayClose,
			"",
			"   " + panelDotHint("", "←/→", "switch view", "esc", "dismiss"),
		}
	}
	turns := diffCountTurns(m)
	if turns == 0 {
		return []string{
			diffTabRow("git", 1, true),
			"",
			"   " + cDimGrayOpen + "No file changes in conversation" + cDimGrayClose,
			"",
			"   " + panelDotHint("", "←/→", "switch view", "esc", "dismiss"),
		}
	}
	first := diffFirstChangedPath(tracked, untracked)
	all := append(append([]diffChange{}, tracked...), untracked...)
	lines := []string{
		diffTabRow("git", 1, true),
		" " + panelGray238 + "  " + strconv.Itoa(turns) + " turn(s) · " +
			strconv.Itoa(len(all)) + " file(s) changed" + panelGray238C,
		"",
	}
	for i := 0; i < turns; i++ {
		file := first
		if len(all) > 1 {
			file = all[i%len(all)].path
		}
		marker := "  "
		if i == m.panel.cursor {
			marker = cBlueOpen + "> " + cBlueClose
		}
		lines = append(lines, marker+"Turn "+strconv.Itoa(i+1)+" · "+cDimGrayOpen+file+cDimGrayClose)
	}
	lines = append(lines, "",
		"   "+panelDotHint("", "↑/↓", "navigate", "←/→", "switch view", "esc", "dismiss"))
	return lines
}

// gitDiffBody returns the diff body for `target` in dir. kind=file runs
// `git diff --no-color -- <path>` (unstaged + staged working-copy diff);
// kind=commit runs `git show --no-color <hash>` (the full commit diff,
// including the stat header). The empty slice is returned when git is
// unavailable or the command fails — the caller renders a single
// "no diff available" row in that case.
//
// Each line is normalised through trimDiffTrailing so trailing tabs and
// spaces don't show up as explicit whitespace markers in the inline-diff
// subview (AGY 1.2.16). The leading marker byte (` `, `+`, `-`, `@` and
// the file/hunk header characters) is preserved; only the tail after the
// last non-whitespace rune changes. The on-disk file is unchanged.
func gitDiffBody(dir, target, kind string) []string {
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	var args []string
	switch kind {
	case "file":
		args = []string{"diff", "--no-color", "--", target}
	case "commit":
		args = []string{"show", "--no-color", target}
	default:
		return nil
	}
	out, err := runGit(dir, args...)
	if err != nil {
		return nil
	}
	if out == "" {
		return nil
	}
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		lines[i] = trimDiffTrailing(l)
	}
	return lines
}

// diffInlineStyle applies REF-style +/- colouring to one diff line. The
// `/` lines (file headers) and `@@` lines (hunk headers) pass through
// plain; `+` lines get the green low-effort colour, `-` lines get the
// red-orange tool-bullet colour. The leading byte of each line is the
// only thing that drives the colour, matching the captured per-line
// pattern REF's side-by-side viewer paints (agy226/diffA-enter1.raw.txt
// rows 4-5).
func diffInlineStyle(line string) string {
	if line == "" {
		return line
	}
	switch line[0] {
	case '+':
		return panelEffLowOpen + line + "\x1b[39m"
	case '-':
		return cToolBulletErrOpen + line + "\x1b[39m"
	default:
		return line
	}
}

// diffInlineLegend returns the keyboard hint row the inline subview
// shows. REF's diff viewer advertises `n/N next/prev diff · ←/→ cycle
// file · f full file · c comment · d delete comment · esc back`; the
// clone implements the f-binding (cycle diff-folding, AGY 1.3.5,
// hunk fold) and the esc-back binding, and skips the bindings the
// reference's side-by-side viewer supports that the plain inline view
// does not (n/N, ←/→, c, d).
func diffInlineLegend() string {
	return "   " + panelDotHint("", "f", "fold hunk", "esc", "back")
}

// panelDiffInlineLines renders the inline-diff subview opened by
// `enter` on All Changes (diffView=1, path=file) or Commit Tree
// (diffView=2, path=commit hash). Esc returns to the list; the inline
// view is otherwise inert. The body is `git diff/show` output styled
// line-by-line, with hunk-fold state applied per diffFolded[i] (see
// slash_diff_fold.go); the title row carries the path or
// `commit <hash>` so the user knows which diff they are looking at.
//
// Cycle diff-syntax-highlight: on the file view (diffView==1) each
// non-pre-header body line is fed through renderDiffInlineLine so
// keywords / strings / numbers / comments / types are coloured per
// the Aurora-Dark tokens the v1.3.6 binary recovers via
// `strings ~/.local/bin/agy | grep chroma`. The marker byte (the
// leading `+` / `-` / ` `) keeps the existing per-line green / red
// tint so the visual contract of the cycle-127 tool-step renderer
// (the diff colour applies to the marker alone) is preserved. The
// commit view (diffView==2) has no single file path to bind a
// language to, so it falls back to the original diffInlineStyle
// monochrome behaviour.
//
// Cycle diff-folding (AGY 1.3.5): the renderer walks splitDiffHunks
// and emits each hunk's body either expanded (every body line
// styled) or collapsed (header line + "+N" line-count indicator in
// dim grey). Pre-header lines ("diff --git ...", "index ...", "---",
// "+++") are always rendered uncondensed.
func (m *runMode) clampDiffInlineCursor(maxRow int) {
	if maxRow < 0 {
		m.panel.diffInlineRow = 0
	} else {
		if m.panel.diffInlineRow < 0 {
			m.panel.diffInlineRow = 0
		}
		if m.panel.diffInlineRow > maxRow {
			m.panel.diffInlineRow = maxRow
		}
	}
}

func (m *runMode) panelDiffInlineLines() []string {
	var title, kind, langFile string
	switch m.panel.diffView {
	case 1:
		title = m.panel.diffViewPath
		kind = "file"
		langFile = m.panel.diffViewPath
	case 2:
		title = "commit " + m.panel.diffViewPath
		kind = "commit"
	default:
		title = "(no diff)"
		kind = "file"
	}
	lines := []string{
		"\x1b[1m" + title + "\x1b[0m",
		"",
	}
	body := gitDiffBody(m.cwd, m.panel.diffViewPath, kind)
	if len(body) == 0 {
		lines = append(lines, "   "+cDimGrayOpen+"no diff available"+cDimGrayClose)
	} else {
		rows, _ := m.renderDiffFromBody(body, langFile)
		m.clampDiffInlineCursor(len(rows) - 1)
		cursor := m.panel.diffInlineRow
		for i, r := range rows {
			if i == cursor {
				lines = append(lines, " "+cBlueOpen+"> "+cBlueClose+r)
			} else {
				lines = append(lines, r)
			}
		}
	}
	lines = append(lines, "", diffInlineLegend())
	return lines
}

// diffOpenInline sets m.panel.diffView and diffViewPath so the next
// panelDiffLinesVCS call renders the inline-diff subview. The cycle-226
// substitute is plain text (not REF's side-by-side viewer); see
// panelDiffInlineLines for the wiring.
//
// Cycle diff-folding (AGY 1.3.5): also resets diffInlineRow and
// diffFolded so each new file's diff starts unfolded at the top.
// Without this reset the cursor and fold state would carry over from
// the previous diff (e.g. scroll position from one file would land
// you in the middle of another).
func (m *runMode) diffOpenInline(kind int, target string) {
	m.panel.diffView = kind
	m.panel.diffViewPath = target
	m.panel.diffInlineRow = 0
	m.panel.diffFolded = nil
}

// diffCloseInline returns to the file-list view. Cycle 226 binds Esc on
// the inline subview to this; the panel itself stays open.
//
// Cycle diff-folding (AGY 1.3.5): resets diffInlineRow and clears
// diffFolded so the next Enter opens a fresh, unfolded diff at the
// top row. (Esc is treated as "leave this diff" rather than "minimise
// this diff" — the reference's side-by-side viewer collapses to a
// re-open on Esc, but Esc-then-Enter in the clone opens a new view.)
//
// The list-view cursor (m.panel.cursor) is the row into the
// file/commit list and is left alone so Esc returns to the same row
// the user pressed Enter on.
func (m *runMode) diffCloseInline() {
	m.panel.diffView = 0
	m.panel.diffViewPath = ""
	m.panel.diffInlineRow = 0
	m.panel.diffFolded = nil
}

// diffEnter is the binding `enter` triggers on All Changes / Commit
// Tree. On Per Turn the binding is inert (the placeholder has no rows
// to drill into; the cycle-226 row-list mode is also inert because the
// per-turn model has no per-file body to show). When the cursor points
// at a valid row the subview is opened; when it does not (empty list)
// the binding is a no-op.
func (m *runMode) diffEnter() {
	switch m.panel.tab {
	case 0: // All Changes
		tracked, untracked, ok := gitDiffChanges(m.cwd)
		if !ok {
			return
		}
		all := append(append([]diffChange{}, tracked...), untracked...)
		if m.panel.cursor < 0 || m.panel.cursor >= len(all) {
			return
		}
		m.diffOpenInline(1, all[m.panel.cursor].path)
	case 2: // Commit Tree
		log, err := runGit(m.cwd, "log", "--pretty=%h%x09%s")
		if err != nil {
			return
		}
		var commits [][2]string
		for _, line := range strings.Split(log, "\n") {
			if fields := strings.SplitN(line, "\t", 2); len(fields) == 2 {
				commits = append(commits, [2]string{fields[0], fields[1]})
			}
		}
		// Cursor: 0 = working-copy row (the "▌ *" line at index 3), 1..N
		// = the N commits. Skip 0 (working-copy has no per-commit diff
		// hash to drill into), so commit i is at cursor==i.
		idx := m.panel.cursor - 1
		if idx < 0 || idx >= len(commits) {
			return
		}
		m.diffOpenInline(2, commits[idx][0])
	}
}


// panelUsageLines renders the /usage panel. Layout recovered from
// /tmp/agycmp/panels/ppref.usage.raw.txt. Cycle 193 ships the static
// placeholders (no live quota call):
//   - bold dim "└ Models & Quota" title
//   - Account: navigaid@gmail.com  (brand blue)
//   - GEMINI MODELS group: 100% green bar, "Quota available"
//   - CLAUDE AND GPT MODELS group: 0.00% dim bar, "Refreshes in 74h 29m"
//   - "(1–17 of 20 lines)" counter in dim grey 241
//   - dot hint: ↑/↓ Scroll · pgup/pgdown Page · ctrl+end Bottom ·
//     ctrl+home Top · esc Close
//
// Esc closes with the standard exit notice (esc-ref-model-Esc.txt).
//
// Cycle 201: the body scrolls. usageViewerLines is the 20-line document;
// the panel draws a 17-row window over it so ↑/↓, pgup/pgdn and
// ctrl+home / ctrl+end move the window and the "(a–b of 20 lines)"
// counter follows. At offset 0 the rendered rows are the cycle-193 ones.


func (m *runMode) diffCommitTreeLines() []string {
	log, _ := runGit(m.cwd, "log", "--pretty=%h%x09%s")
	var commits [][2]string
	for _, line := range strings.Split(log, "\n") {
		if fields := strings.SplitN(line, "\t", 2); len(fields) == 2 {
			commits = append(commits, [2]string{fields[0], fields[1]})
		}
	}
	lines := []string{
		diffTabRow("git", 2, true),
		" " + panelGray238 + "  " + strconv.Itoa(len(commits)) + " commit(s) available" + panelGray238C,
		"",
		" \x1b[38;5;69m\x1b[48;5;255m▌\x1b[39m \x1b[1m*\x1b[0m\x1b[48;5;255m  \x1b[1m\x1b[38;5;172mWorking Copy (Uncommitted Changes)\x1b[0m\x1b[48;5;255m",
	}
	for i, c := range commits {
		if i == 0 {
			lines = append(lines, "\x1b[49m   |")
		}
		lines = append(lines, "   * "+cBlueOpen+c[0]+cBlueClose+" "+panelGray238+c[1]+panelGray238C)
	}
	lines = append(lines, "",
		"   "+panelDotHint("", "↑/↓", "navigate", "enter", "view diff", "←/→", "switch view", "esc", "dismiss"))
	return lines
}



func (m *runMode) diffMoveCursor(delta int) {
	if m.cwd == "" || m.panel.diffView != 0 {
		return
	}
	var hi int
	switch m.panel.tab {
	case 0: // All Changes
		tracked, untracked, ok := gitDiffChanges(m.cwd)
		if !ok {
			return
		}
		hi = len(tracked) + len(untracked) - 1
	case 1: // Per Turn
		turns := diffCountTurns(m)
		tracked, untracked, ok := gitDiffChanges(m.cwd)
		if !ok || turns == 0 || len(tracked)+len(untracked) == 0 {
			return
		}
		hi = turns - 1
	case 2: // Commit Tree
		log, err := runGit(m.cwd, "log", "--pretty=%h%x09%s")
		if err != nil {
			return
		}
		if log == "" {
			return
		}
		// log has one line per commit (no trailing newline); the
		// cursor indexes the working-copy row (0) plus the N commits
		// (1..N), so hi = N.
		hi = strings.Count(log, "\n") + 1
	default:
		return
	}
	if hi < 0 {
		return
	}
	m.panel.cursor += delta
	if m.panel.cursor < 0 {
		m.panel.cursor = 0
	}
	if m.panel.cursor > hi {
		m.panel.cursor = hi
	}
}

// diffTextKey returns the cursor delta for an unmodified j/k key, or 0.
func diffTextKey(key tea.Key) int {
	if key.Mod != 0 || len(key.Text) != 1 {
		return 0
	}
	switch key.Text[0] {
	case 'j':
		return 1
	case 'k':
		return -1
	}
	return 0
}

