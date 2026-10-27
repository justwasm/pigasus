// Package diffaction holds the pure parts of the /diff slash action: the
// version-control detection, the VCS diff runner, the ANSI stripper and the
// "Files:" preamble builder.
//
// The /diff *panel* (the interactive tabbed viewer) is a different
// subsystem — it lives in cmd/wagyu/slash_viewer_diff*.go and its state
// is bound to runMode. This package holds only the one-shot feedback
// path's helpers, which touch no panel state at all.
//
// The split mirrors the reference: the reference detects the workspace's
// VCS, runs the working-copy diff with colour and pagers disabled, and
// strips the escape codes a colour-always configuration still emits.
package diffaction

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/render"
)

// UnifiedDiff computes a unified diff between oldText and newText using udiff.
func UnifiedDiff(oldLabel, newLabel, oldText, newText string) string {
	return udiff.Unified(oldLabel, newLabel, oldText, newText)
}

// StripANSI removes ANSI escape sequences from s. The returned string
// preserves all other bytes, including embedded newlines and surrounding
// whitespace, so a caller's TrimSpace / Split pipeline keeps working.
//
// The stripping is x/ansi's, the same stripper the rest of the tree uses
// for its own rows. It is a full VT parser, so it removes the OSC / DCS
// sequences a colour-always VCS configuration can emit too — the old
// CSI-only regex left them in the body, which is exactly the kind of
// residue the 1.3.4 regression was about.
func StripANSI(s string) string {
	return ansi.Strip(s)
}

// RenderDiffFooter writes the `[/N of /N]  (end of diff)` trailer the
// reference's /diff viewer uses (recovered from 1.3.6 string table)
// and returns `No uncommitted changes` when the body is whitespace-
// only or empty. The VCS path (VCSDiff) and the AGY 1.3.3 line 52
// fallback (the git-HEAD path in cmd/wagyu) both feed through this helper
// so a non-empty diff renders identically regardless of which command
// produced it.
func RenderDiffFooter(body string) string {
	body = strings.TrimRight(body, "\n")
	if strings.TrimSpace(body) == "" {
		return "No uncommitted changes"
	}
	lines := strings.Split(body, "\n")
	var b strings.Builder
	b.WriteString(lines[0])
	for _, l := range lines[1:] {
		b.WriteString("\n")
		b.WriteString(l)
	}
	fmt.Fprintf(&b, "\n  [%d-%d of %d]  (end of diff)", 1, len(lines), len(lines))
	return b.String()
}

// diffPlusFileRe matches the `+++ b/<path>` line that follows each
// `diff --git` block in `git diff` output. The path lives in capture 1;
// the `b/` prefix is what git writes for the working-tree side.
var diffPlusFileRe = regexp.MustCompile(`^\+\+\+ b/(.+?)\s*$`)

// diffHunkRe matches the `@@ -X,Y +A,B @@` hunk header. The first new-
// side line number is capture 1.
var diffHunkRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// appendDiffFileLink formats and appends a link for (path, line) if path is non-empty.
func appendDiffFileLink(links []string, path string, line int) []string {
	if path == "" {
		return links
	}
	label := path
	if line > 0 {
		label = path + ":" + strconv.Itoa(line)
	}
	return append(links, render.MarkdownFileLinkAt(label, path, line, len(links)))
}

// FilesPreamble walks a `git diff` body and emits a comma-separated
// list of clickable file links, one per file touched by the diff,
// pointing at the first changed line. Returns "" when no file path
// could be extracted (e.g. an empty / malformed diff); the /diff
// feedback path then emits no preamble so its tests stay verbatim.
//
// Each link carries a `?h=<index>` hover-card disambiguator
// (render.MarkdownFileLinkAt) so that even when the diff touches the
// same file at the same line twice — or when /diff is run alongside
// /codesearch results that point at the same URL — each link instance
// has a distinct hover card (AGY 1.3.3 changelog line 60). The index is
// the link's position in the emitted list (0-based), which is
// deterministic because git emits `+++ b/path` blocks in the order it
// found them in the working tree.
//
// Implementation note: a `diff --git a/path b/path` block puts the
// new-side path on the immediately-following `+++ b/path` line, and
// the first changed line of any hunk lives on the next `@@ +A,B @@`.
// We walk line-by-line and pick the first hunk header we see after
// each `+++` marker, which matches the way git emits the data.
func FilesPreamble(body string) string {
	var (
		pendingPath string
		pendingLine int
		links       []string
	)
	for _, ln := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(ln, "diff --git "):
			links = appendDiffFileLink(links, pendingPath, pendingLine)
			pendingPath, pendingLine = "", 0
		case diffPlusFileRe.MatchString(ln):
			links = appendDiffFileLink(links, pendingPath, pendingLine)
			pendingPath, pendingLine = "", 0
			if m := diffPlusFileRe.FindStringSubmatch(ln); len(m) >= 2 {
				pendingPath = m[1]
			}
		case diffHunkRe.MatchString(ln):
			if pendingPath != "" && pendingLine == 0 {
				if m := diffHunkRe.FindStringSubmatch(ln); len(m) >= 2 {
					if v, err := strconv.Atoi(m[1]); err == nil && v > 0 {
						pendingLine = v
					}
				}
			}
		}
	}
	links = appendDiffFileLink(links, pendingPath, pendingLine)
	if len(links) == 0 {
		return ""
	}
	return "Files: " + strings.Join(links, ", ")
}
