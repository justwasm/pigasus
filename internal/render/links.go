// Package render formats clickable file links.
//
// AGY 1.3.3 changelog lines 59–60:
//
//	"Fixed file links emitted with raw '#L12' line anchors not opening
//	 at the right line in editors that require percent-encoded anchors
//	 ('%23L12'); the link is normalized to percent-encoding when emitted."
//	"Fixed separate file links pointing to the same URL hovering as one
//	 — each link now has a distinct hover card so users can see both
//	 URLs even if they're identical."
//
// Some editors (JetBrains, VS Code with the OSS hyperlink extension,
// some web-based viewers) refuse to honour a `path#L<n>` anchor when
// the `#` is *not* percent-encoded — they treat the fragment as part
// of the HTTP-style URL they have to parse, and parse `L<n>` as a
// fragment that does not match the editor's own `L<n>` syntax. Other
// editors accept either form. To make the link open at the right line
// in both groups, the CLI emits the URL with `#` percent-encoded as
// `%23`:
//
//	src/foo.go%23L12        → opens src/foo.go at line 12 in either editor
//	src/foo.go#L12          → opens only in the second group
//
// Hover-card identity in most editors / terminals is keyed on the URL
// *string*: two markdown links whose destination strings compare equal
// collapse into a single hover card, so a transcript that mentions the
// same file at the same line twice (e.g. /diff and /codesearch both
// pointing at `src/foo.go%23L12`) only surfaces one hover card even
// though there are two distinct link instances. To split the cards
// without disturbing the file/line anchor, a per-link instance
// `?h=<index>` query parameter is appended:
//
//	src/foo.go%23L12?h=0     → opens src/foo.go at line 12, hover card #0
//	src/foo.go%23L12?h=1     → opens src/foo.go at line 12, hover card #1
//
// Editors ignore the query string when resolving the file path (the
// fragment / path component is what they open), but the URL *string*
// is now unique per link instance, so the hover-card layer treats them
// as distinct. Most terminal emulators (iTerm2, WezTerm, recent
// GNOME Terminal) and most editor hyperlink protocols behave this way.
//
// The path component of the link is *not* percent-encoded. The path is
// already URL-safe under RFC 3986 (unreserved + `/` + sub-delim
// characters); the only character in a workspace-relative file path
// that the URL grammar reserves is `#`, and `#` only matters as the
// fragment separator. Spaces and other unsafe characters are the
// caller's responsibility (AGY 1.3.0 changelog: paths with `#`, `%`,
// or spaces now open the right file across `@` mentions, /codesearch
// and agent-responses).
//
// Three top-level entry points:
//
//	FormatFileLink(path, line)      — emit a brand-new link, no hover
//	                                  disambiguator (legacy form).
//	FormatFileLinkAt(path, line, n) — same, but with `?h=<n>` marker so
//	                                  multiple identical links get
//	                                  distinct hover cards.
//	NormalizeFileLink(s)            — fix up a link that was already
//	                                  written with a raw `#L<n>` anchor
//	                                  (e.g. one surfaced from an upstream
//	                                  tool or from the model's own
//	                                  output). The query-string marker
//	                                  passes through verbatim; the
//	                                  function only touches `#L<n>`
//	                                  fragments.
//
// All helpers are pure: the input is never mutated, and the output uses
// a stable encoding the package's tests pin.
package render

import (
	"regexp"
	"strconv"
	"strings"
)

// lineAnchorRe matches a `#L<digits>` fragment. We only touch the
// fragment portion of a URL — `path#L<digits>` rewrites to
// `path%23L<digits>`. The pattern is anchored so we don't accidentally
// rewrite a `#` that lives inside a path component (which would be a
// pre-existing percent-encoded `%23` and outside our scope).
//
//	A path is matched iff it ends at a `#` followed by `L<digits>`
//	and nothing else; the second branch catches a range anchor
//	`#L<start>-L<end>`.
//
// The regex requires `\d{1,}` (one or more digits) *after* `L`, and
// the digit class excludes `0` so a literal `#L0` (no line number) is
// left alone. Calls are idempotent — `NormalizeFileLink("a%23L12")`
// returns `"a%23L12"` unchanged.
var lineAnchorRe = regexp.MustCompile(`#L([1-9]\d*)(?:-L([1-9]\d*))?`)

// FormatFileLink returns a clickable link to path at line. When line
// is 0 or negative the result is path verbatim (no fragment is
// emitted for "no specific line"). When path already ends with a
// fragment anchor (because the caller handed in an absolute path
// that was already partially built), the new anchor is appended in
// the percent-encoded form so the result is still parseable by both
// editor groups:
//
//	FormatFileLink("src/foo.go", 12)  → "src/foo.go%23L12"
//	FormatFileLink("src/foo.go", 0)   → "src/foo.go"
//	FormatFileLink("src/foo.go#L9", 12) → "src/foo.go%23L9%23L12"
//
// The path is *not* percent-encoded: the function trusts callers to
// hand in URL-safe paths (workspace-relative or absolute). The AGY
// 1.3.0 changelog covers paths with spaces and `#`; this function is
// the post-1.3.0 layer that assumes that work has already been done.
func FormatFileLink(path string, line int) string {
	if line <= 0 {
		return path
	}
	anchor := "%23L" + strconv.Itoa(line)
	if path == "" {
		return anchor
	}
	if strings.HasSuffix(path, "%23") {
		// already percent-encoded anchor separator; just append L<line>
		return path + "L" + strconv.Itoa(line)
	}
	return path + anchor
}

// FormatFileLinkRange returns a clickable link to path spanning the
// inclusive range [start, end]. When end <= start the function
// degrades to FormatFileLink(path, start). When start <= 0 the
// function degrades to FormatFileLink(path, end) so the caller still
// gets a single-line anchor for the only meaningful boundary.
//
//	FormatFileLinkRange("a.go", 10, 20)  → "a.go%23L10-L20"
//	FormatFileLinkRange("a.go", 5, 5)    → "a.go%23L5"
//	FormatFileLinkRange("a.go", 0, 0)    → "a.go"
//	FormatFileLinkRange("a.go", 7, 3)    → "a.go%23L7"
func FormatFileLinkRange(path string, start, end int) string {
	switch {
	case start <= 0 && end <= 0:
		return path
	case start <= 0:
		return FormatFileLink(path, end)
	case end <= start:
		return FormatFileLink(path, start)
	default:
		return path + "%23L" + strconv.Itoa(start) + "-L" + strconv.Itoa(end)
	}
}

// NormalizeFileLink rewrites any `#L<digits>` (or `#L<start>-L<end>`)
// fragment in s to the percent-encoded `%23L<digits>` form. Calls
// are idempotent: a URL that is already percent-encoded passes
// through unchanged because the regex looks for an unescaped `#`.
//
//	NormalizeFileLink("a.go#L12")             → "a.go%23L12"
//	NormalizeFileLink("a.go%23L12")           → "a.go%23L12"
//	NormalizeFileLink("a.go#L10-L20")         → "a.go%23L10-L20"
//	NormalizeFileLink("a.go#L1#L2")           → "a.go%23L1%23L2"
//	NormalizeFileLink("https://example.com")  → "https://example.com"
//	NormalizeFileLink("https://example.com#x") → "https://example.com#x"
//
// Only `#L<digits>` and `#L<digits>-L<digits>` are rewritten. A bare
// `#fragment` without the `L<digits>` prefix is left alone so the
// function does not break arbitrary URL fragments (e.g. `#section` in
// an HTTP doc link).
//
// Implementation note: the rewrite uses a function callback so we
// can omit the trailing `-L<end>` half when the input was a single-
// line `#L<n>` (the optional group is empty). A fixed-string
// replacement would always emit the `-L` separator and produce
// `a.go%23L12-L` for `#L12`, which is malformed.
func NormalizeFileLink(s string) string {
	if s == "" || !strings.Contains(s, "#") {
		return s
	}
	return lineAnchorRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := lineAnchorRe.FindStringSubmatch(m)
		if len(sub) < 2 || sub[1] == "" {
			return m
		}
		out := "%23L" + sub[1]
		if len(sub) >= 3 && sub[2] != "" {
			out += "-L" + sub[2]
		}
		return out
	})
}

// MarkdownFileLink renders a clickable markdown link `[label](url)`
// where the URL is the percent-encoded file link for (path, line).
//
//	MarkdownFileLink("see foo.go:12", "src/foo.go", 12)
//	  → "[see foo.go:12](src/foo.go%23L12)"
//
// When line <= 0 the link URL has no fragment, so terminals that
// resolve only the URL (not the label) still navigate to the file
// without a line hint.
//
// MarkdownFileLink is the *legacy* no-marker form. Callers that want
// distinct hover cards per link instance should use MarkdownFileLinkAt
// (AGY 1.3.3 changelog line 60). The legacy form is kept for callers
// that intentionally want the URL to be byte-identical across link
// instances (e.g. regression tests pinning a specific URL string).
func MarkdownFileLink(label, path string, line int) string {
	return "[" + label + "](" + FormatFileLink(path, line) + ")"
}

// FileLinkHoverParam is the query-parameter name appended to file
// links to disambiguate hover cards. The constant is exported so tests
// (and future call sites that need to detect / strip a hover marker)
// can reference a single symbol rather than the literal `"h"`. See
// FormatFileLinkAt for the full rationale.
const FileLinkHoverParam = "h"

// FormatFileLinkAt returns a clickable link to path at line with a
// unique per-instance hover-card disambiguator.
//
// The marker is a query parameter (`?<FileLinkHoverParam>=<index>`)
// appended after the file/line anchor. Most editors parse the path /
// fragment portion and ignore the query string when opening the file
// (so the link still navigates to the right line), but the URL
// *string* is now unique per link instance — which is what hover-card
// implementations key on. AGY 1.3.3 changelog line 60:
//
//	"Fixed separate file links pointing to the same URL hovering as
//	 one — each link now has a distinct hover card so users can see
//	 both URLs even if they're identical."
//
//	FormatFileLinkAt("src/foo.go", 12, 0) → "src/foo.go%23L12?h=0"
//	FormatFileLinkAt("src/foo.go", 12, 3) → "src/foo.go%23L12?h=3"
//	FormatFileLinkAt("src/foo.go", 0, 0)   → "src/foo.go?h=0"
//	FormatFileLinkAt("src/foo.go", 12, -1) → "src/foo.go%23L12"
//
// A negative index is treated as "no marker" and falls back to the
// bare FormatFileLink output. This is the escape hatch for callers
// that genuinely want two links to collapse into a single hover card
// (none today, but the symmetry with MarkdownFileLink is intentional).
//
// When the underlying URL already contains a query string (e.g. the
// caller handed in a path that is itself a URL — rare, but possible
// for an absolute `https://example.com/x?y=1` reference) the marker is
// appended with `&` rather than `?`:
//
//	FormatFileLinkAt("https://example.com/x?y=1", 0, 0)
//	  → "https://example.com/x?y=1&h=0"
//
// The path / fragment portion of the URL is unchanged: the function
// composes on top of FormatFileLink, so all the AGY 1.3.3 line-59
// guarantees (no raw `#`, percent-encoded anchors, no path mutation)
// still hold.
func FormatFileLinkAt(path string, line, index int) string {
	base := FormatFileLink(path, line)
	if index < 0 {
		return base
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + FileLinkHoverParam + "=" + strconv.Itoa(index)
}

// MarkdownFileLinkAt is the markdown-link wrapper for FormatFileLinkAt.
//
//	MdFileLinkAt("foo.go:12", "src/foo.go", 12, 0)
//	  → "[foo.go:12](src/foo.go%23L12?h=0)"
//
//	MarkdownFileLinkAt("foo.go:12", "src/foo.go", 12, 1)
//	  → "[foo.go:12](src/foo.go%23L12?h=1)"
//
// The label is unchanged; the destination URL carries the marker. Two
// MarkdownFileLinkAt calls with the same (path, line) but different
// indices produce two URLs that differ only in the `?h=<n>` suffix,
// which is exactly what the changelog wants: distinct hover cards
// even when the file/line anchor is identical.
func MarkdownFileLinkAt(label, path string, line, index int) string {
	return "[" + label + "](" + FormatFileLinkAt(path, line, index) + ")"
}

// HasLineAnchor reports whether s contains a `#L<digits>` (or
// `#L<start>-L<end>`) fragment, percent-encoded or not. Callers use
// it to decide whether NormalizeFileLink is worth invoking; the
// function itself short-circuits when the URL has no `#` and no
// `%23L`.
func HasLineAnchor(s string) bool {
	if s == "" {
		return false
	}
	if strings.Contains(s, "#L") && lineAnchorRe.MatchString(s) {
		return true
	}
	return encodedLineAnchorRe.MatchString(s)
}

// encodedLineAnchorRe matches the percent-encoded form `%23L<digits>`
// (and `%23L<start>-L<end>`). Mirrors lineAnchorRe for the encoded
// shape so HasLineAnchor can answer "is this a file-link fragment?"
// without the caller having to know which encoding is in use.
var encodedLineAnchorRe = regexp.MustCompile(`%23L([1-9]\d*)(?:-L([1-9]\d*))?`)
