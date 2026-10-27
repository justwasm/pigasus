// Package diffaction inline.go — git diff body execution and inline diff styling.
package diffaction

import (
	"os/exec"
	"strings"

	"github.com/justwasm/pigasus/diffsyntax"
	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// FirstChangedPath returns the path of the first change in the working copy.
func FirstChangedPath(tracked, untracked []string) string {
	if len(tracked) > 0 {
		return tracked[0]
	}
	if len(untracked) > 0 {
		return untracked[0]
	}
	return ""
}

// GitDiffBody returns the diff body for `target` in dir. kind=file runs
// `git diff --no-color -- <path>` (unstaged + staged working-copy diff);
// kind=commit runs `git show --no-color <hash>` (the full commit diff,
// including the stat header). The empty slice is returned when git is
// unavailable or the command fails — the caller renders a single
// "no diff available" row in that case.
//
// Each line is normalised through TrimTrailing so trailing tabs and
// spaces don't show up as explicit whitespace markers in the inline-diff
// subview (AGY 1.2.16). The leading marker byte (` `, `+`, `-`, `@` and
// the file/hunk header characters) is preserved; only the tail after the
// last non-whitespace rune changes. The on-disk file is unchanged.
func GitDiffBody(dir, target, kind string) []string {
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
	out, err := RunGit(dir, args...)
	if err != nil || out == "" {
		return nil
	}
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		lines[i] = diffsyntax.TrimTrailing(l)
	}
	return lines
}

// InlineStyle applies REF-style +/- colouring to one diff line. The
// `/` lines (file headers) and `@@` lines (hunk headers) pass through
// plain; `+` lines get the green low-effort colour, `-` lines get the
// red-orange tool-bullet colour. The leading byte of each line is the
// only thing that drives the colour, matching the captured per-line
// pattern REF's side-by-side viewer paints (agy226/diffA-enter1.raw.txt
// rows 4-5).
func InlineStyle(line string) string {
	if line == "" {
		return line
	}
	switch line[0] {
	case '+':
		return palette.ToolAddOpen + line + "\x1b[39m"
	case '-':
		return palette.ToolBulletErrOpen + line + "\x1b[39m"
	default:
		return line
	}
}

// InlineLegend returns the keyboard hint row the inline subview
// shows. REF's diff viewer advertises `n/N next/prev diff · ←/→ cycle
// file · f full file · c comment · d delete comment · esc back`; the
// clone implements the f-binding (cycle diff-folding, AGY 1.3.5,
// hunk fold) and the esc-back binding, and skips the bindings the
// reference's side-by-side viewer supports that the plain inline view
// does not (n/N, ←/→, c, d).
func InlineLegend() string {
	return "   " + tuilib.PanelDotHint("", "f", "fold hunk", "esc", "back")
}
