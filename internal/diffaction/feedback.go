// feedback.go — the one-shot /diff feedback body.
//
// The interactive /diff *panel* is a different subsystem (it lives in
// cmd/wagyu/slash_viewer_diff*.go and its state is bound to runMode).
// This file holds the one-shot path: the transcript row the reference
// echoes when the user types /diff.
//
// The rule itself now lives in internal/diffaction.

package diffaction

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/diffsyntax"
)

// DiffFunc runs the working-copy diff of a detected VCS in a directory.
// It is the seam the feedback path calls; VCSDiff is the production
// implementation.
type DiffFunc func(vcs, dir string) (string, error)

// GitHeadFunc runs `git diff HEAD` in a directory. It is the AGY 1.3.3
// line 52 fallback's seam; RunGit is the production implementation.
type GitHeadFunc func(dir string) (string, error)

// Feedback renders /diff: the workspace's uncommitted changes.
//
// Recovered: `No uncommitted changes` for an empty diff, `no workspace
// directory`, the per-VCS error wrapper `git diff: %w` (and the `hg diff:` /
// `jj diff:` siblings), and the viewer footer `  [%d-%d of %d]  (end of diff)`.
//
// The reference renders the diff in an interactive, syntax-highlighted viewer
// (1.3.5 changelog: "Added syntax highlighting to the /diff file view");
// the clone appends plain lines to the transcript. The *presentation* is therefore
// 【推测】 while the body and the empty-case line are not.
//
// Cycle file-link-percent-encoded-anchor: prepends a "Files:" line listing
// each file touched by the diff as a percent-encoded clickable link
// (`[file:N](file%23LN)`), so the markdown rendering of an /diff echo
// (or any future markdown-rendering integration) lands on the right line
// in editors that only honour percent-encoded anchors (AGY 1.3.3
// changelog line 59). The recovered body and footer stay verbatim.
//
// Cycle separate-links-hover: each emitted link now carries a
// `?h=<index>` hover-card disambiguator (render.MarkdownFileLinkAt), so
// even when the diff references the same file at the same line twice,
// each link instance has a distinct hover card (AGY 1.3.3 changelog
// line 60).
//
// Cycle diff-cjk-cutoff: maxCells caps each rendered line so a runaway diff
// body (minified JS, embedded blobs, long file paths, CJK-heavy content)
// does not overflow the transcript. truncateDiffLine respects grapheme
// cluster boundaries so a CJK character or a ZWJ family-emoji sequence at
// the cutoff edge is never split mid-cluster. Passing maxCells <= 0
// disables truncation entirely and returns the raw diff body.
//
// Cycle diff-color-ui-always: the body is run through StripANSI before the
// emptiness check so a clean repository whose diff command still emits
// colour codes (e.g. colour.ui = always, hg ui.color=always, jj
// ui.color=always) is correctly reported as "No uncommitted changes". The
// displayed body is also stripped so the transcript stays free of stray
// escape bytes.
//
// diffFn is the diff runner; gitHeadFn is the AGY 1.3.3 line 52 fallback
// (see GitDiffHeadFeedback). Both are passed in so the caller keeps its
// own test seams.
func Feedback(workspace string, maxCells int, diffFn DiffFunc, gitHeadFn GitHeadFunc) string {
	if workspace == "" {
		return "no workspace directory"
	}
	vcs := DetectVCS(workspace)
	if vcs == "" {
		// Cycle diff-git-head-fallback (AGY 1.3.3 line 52): when no
		// tracked VCS is detected (workspace is not under git, hg,
		// or jj), REF falls back to `git -C <workspace> diff HEAD`
		// rather than reporting the misleading
		// "No uncommitted changes". The fallback surfaces a clean
		// "Not in a git repository" line when the workspace is
		// outside any repo, and renders the same `[1-N of N]
		// (end of diff)` footer the VCS path uses when the diff
		// body is non-empty. See GitDiffHeadFeedback.
		return GitDiffHeadFeedback(workspace, gitHeadFn)
	}
	out, err := diffFn(vcs, workspace)
	if err != nil {
		return fmt.Sprintf("%s diff: %v", vcs, err)
	}
	body := StripANSI(strings.TrimRight(out, "\n"))
	if strings.TrimSpace(body) == "" {
		return "No uncommitted changes"
	}
	lines := strings.Split(body, "\n")
	preamble := FilesPreamble(body)
	var b strings.Builder
	if preamble != "" {
		b.WriteString(preamble)
		b.WriteString("\n")
	}
	b.WriteString(diffsyntax.TruncateLine(lines[0], maxCells))
	for _, l := range lines[1:] {
		b.WriteString("\n")
		b.WriteString(diffsyntax.TruncateLine(l, maxCells))
	}
	fmt.Fprintf(&b, "\n  [%d-%d of %d]  (end of diff)", 1, len(lines), len(lines))
	return b.String()
}

// GitDiffHeadFeedback is the AGY 1.3.3 line 52 fallback for /diff
// when the workspace is not under git, hg or jj version control. It
// shells out to `git -C <workspace> diff HEAD` (the canonical git
// command for "diff against the last commit") and lets the underlying
// tool's exit code / stderr drive the surfaced string. Expected
// outcomes:
//
//   - workspace IS inside a git repo (e.g. .git file/symlink that
//     DetectVCS missed, submodule with broken work-tree): empty diff
//     body → "No uncommitted changes"; non-empty body → the same
//     `[1-N of N] (end of diff)` footer the VCS path emits.
//   - workspace is NOT inside a git repo: git itself prints
//     `fatal: not a git repository (or any of the parent directories):
//     .git` and exits non-zero, so the runner returns a non-nil error.
//     We surface "Not in a git repository" instead of forwarding the
//     stderr dump and instead of the historical misleading
//     "No uncommitted changes" that hid the lack of a repo.
//
// The fallback uses the same git runner the rest of /diff relies
// on so PAGER=cat / GIT_PAGER=cat / GIT_EDITOR=false are already set;
// no extra hanging on a pager or an invisible editor.
func GitDiffHeadFeedback(workspace string, gitHeadFn GitHeadFunc) string {
	out, err := gitHeadFn(workspace)
	if err != nil {
		return "Not in a git repository"
	}
	return RenderDiffFooter(out)
}
