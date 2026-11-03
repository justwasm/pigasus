// Package mdrender — dir_link.go: directory-link detection for the
// markdown link rendering path.
//
// Cycle 252: AGY 1.4.0 changelog (`internal/cli/testdata/changelog_reference.txt`
// line 25) notes:
//
//	"Fixed links to directories disappearing from agent responses; they
//	 now show the directory name, for example `pipelines/`."
//
// The reference bug: a markdown link whose destination points at a
// directory (a path that ends with `/`, or a path that resolves to a
// directory on disk) was being dropped by the markdown rendering path
// — glamour's default Link style only emits the link *label* text, the
// URL is part of the OSC 8 frame the user never sees, and AGY's
// pre-pass apparently rejects or rewrites directory URLs in a way that
// makes the link disappear entirely. The fix: detect directory-links
// explicitly and substitute the URL (with trailing `/`) as plain text
// in place of the OSC 8 hyperlink, matching the AGY 1.4.0 contract
// "they now show the directory name, for example `pipelines/`".
//
// The detection is heuristic-but-defensive. A href is treated as a
// directory-link when EITHER:
//
//  1. it ends with one or more `/` (the URL convention for a
//     directory reference); OR
//  2. `os.Stat(href)` resolves to an existing directory on disk — the
//     model wrote `[label](src/foo)` and `src/foo/` happens to be a
//     directory on the working tree, so the link should also be
//     surfaced as the directory name.
//
// The first rule catches the canonical form `[pipelines/](pipelines/)`
// that the changelog quotes verbatim. The second rule catches the
// loose form `[Open modules](src)` when `src/` is in fact a directory,
// which is common for AGY's plan-listing replies.
//
// Two exported entry points:
//
//	DirLinkTarget(href)   (isDir bool, label string)
//	                     — true when href is a directory-link; label
//	                       is the directory name with a single
//	                       trailing "/".
//	RenderDirLink(href)   string
//	                     — the visible replacement text. Equal to
//	                       DirLinkTarget(href).label; empty when
//	                       href is not a directory-link.
//
// os.Stat is wrapped in a deferred recover() — some filesystems
// (notably the odd FUSE / sshfs / network-mount stack the user might
// have when running the CLI on a sandbox) panic on Stat for paths
// that should otherwise fail with an error. We catch the panic and
// return (false, "") so the caller falls back to the regular
// file-link rendering path. This is the cycle's defensive posture
// rather than a fix-the-world rule; the worst case (a panic that
// the recover catches) is "the link renders the way it always did",
// not a crash.
//
// Three test invariants pin the function:
//
//  1. Trimming rule — `pipelines//` → label `pipelines/` (single
//     trailing slash, no doubling). The link's display always has
//     exactly one trailing slash when isDir is true.
//  2. Empty / non-existent href — never panics, returns (false, "").
//  3. Relative path resolution — `os.Stat(href)` resolves against
//     the caller's current working directory, the same way every
//     other Go os.Stat call resolves paths. The test pin:
//     `TestDirLinkTarget_RelativePath` runs from a temp dir so the
//     result is deterministic.
package mdrender

import (
	"os"
	"strings"
)

// DirLinkTarget reports whether href points at a directory. A href is
// treated as a directory-link when EITHER:
//
//   - the href ends with one or more `/` (URL convention); OR
//   - `os.Stat(href)` resolves to a directory on disk.
//
// URLs with a recognised scheme prefix (http://, https://, ftp://,
// file://, ssh://, ws://, wss://) are NEVER classified as directory
// links, even when they end with `/`. The AGY 1.4.0 changelog row is
// about workspace file links ("they now show the directory name, for
// example `pipelines/`"), and a web URL ending in `/` is just URL
// convention — it might be a directory listing, an HTTP path
// component, or a route. Treating those as dir-links would emit
// `https://example.com/foo/` as plain text in place of an OSC 8
// hyperlink, which the user never asked for. The scheme-prefix gate
// keeps the rule scoped to local file paths.
//
// On a directory-link, label is the directory's basename with exactly
// one trailing `/` added (or the trimmed href + `/` when the input
// already ended with `/`s that got collapsed). On a non-directory-link,
// isDir is false and label is the empty string.
//
// Empty href → (false, ""). The function never returns a populated
// label with isDir == false.
//
// os.Stat panics on certain weird filesystem mounts (sandbox FUSE,
// sshfs hangs, network-mount disconnects). A deferred recover() in
// statIsDir catches those and returns false so the caller falls back
// to the regular file-link rendering path.
//
// Examples:
//
//	DirLinkTarget("pipelines/")              → true,  "pipelines/"
//	DirLinkTarget("pipelines//")             → true,  "pipelines/"   (collapses repeated slashes)
//	DirLinkTarget("/tmp")                    → true,  "/tmp/"        (os.Stat resolves /tmp as a dir)
//	DirLinkTarget("/etc/passwd")             → false, ""             (regular file)
//	DirLinkTarget("/nonexistent")            → false, ""             (Stat returns ENOENT)
//	DirLinkTarget("")                        → false, ""
//	DirLinkTarget("https://example.com/foo/")→ false, ""             (scheme prefix — not a local dir)
//	DirLinkTarget("file:///etc/passwd/")     → false, ""             (scheme prefix — not a local dir)
func DirLinkTarget(href string) (isDir bool, label string) {
	if href == "" {
		return false, ""
	}
	// Scheme-prefix gate: only local paths can be directory-links.
	// Recognise the common URI schemes; everything else is treated
	// as a local path. The match is on a literal "scheme://"
	// prefix because that's the unambiguous separator — "mailto:"
	// or "javascript:" is a different shape and never ends in "/"
	// anyway, so they're not gated here (the trailing-slash rule
	// below will skip them).
	if hasSchemePrefix(href) {
		return false, ""
	}
	// URL-convention rule: trailing "/" → directory-link. Trim
	// repeated slashes before re-adding exactly one, so
	// "pipelines//" → "pipelines/" (no doubling) and
	// "pipelines///" → "pipelines/" (no tripling).
	trimmed := strings.TrimRight(href, "/")
	if trimmed != href {
		// href had at least one trailing slash. The label is the
		// trimmed href + exactly one "/". Empty after trim
		// (e.g. href = "/") → still a directory-link (root), but
		// label collapses to "/".
		if trimmed == "" {
			return true, "/"
		}
		return true, trimmed + "/"
	}
	// Stat-based rule: resolves a directory on disk even when the
	// URL convention is absent. os.Stat is relative to the caller's
	// cwd, which is the documented Go convention; this is what
	// `ls src` resolves against from the shell.
	if statIsDir(href) {
		return true, href + "/"
	}
	return false, ""
}

// hasSchemePrefix reports whether s starts with `scheme://` for any
// scheme in the closed allow-list. The function is deliberately
// conservative — schemes not in the list (e.g. `mailto:`, `tel:`,
// `data:`) fall through to the local-path rules, which is the safe
// direction (a non-local URL that happens not to be in the list
// still gets the trailing-slash treatment if it ends in `/`).
//
// Schemes covered:
//
//	http, https — the web; trailing `/` is URL convention, not a
//	              filesystem directory reference.
//	file        — `file:///path/` should render as an OSC 8 link,
//	              not as a directory name; the AGY clone does not
//	              open file:// URLs from chat transcripts.
//	ftp, ssh    — remote shells; treated the same as https.
//	ws, wss     — WebSocket; same reasoning.
//
// The match is case-sensitive (the IETF URI spec says scheme is
// case-insensitive, but lowercase ASCII is what every URL parser in
// Go produces; uppercase schemes are vanishingly rare in practice
// and the strict lowercase match keeps the rule pin-clean).
func hasSchemePrefix(s string) bool {
	prefixes := []string{"http://", "https://", "file://", "ftp://", "ssh://", "ws://", "wss://"}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// RenderDirLink returns the visible replacement text for a
// directory-link href — i.e. the directory name with a single trailing
// `/`. The function is a thin wrapper around DirLinkTarget that drops
// the bool, because every caller wants "the text to print" or "an
// empty string means render normally".
//
//	RenderDirLink("pipelines/")   → "pipelines/"
//	RenderDirLink("pipelines//")  → "pipelines/"
//	RenderDirLink("/tmp")         → "/tmp/"   (when /tmp is a directory)
//	RenderDirLink("/etc/passwd")  → ""        (regular file → caller keeps the OSC 8 path)
//	RenderDirLink("")             → ""
func RenderDirLink(href string) string {
	_, label := DirLinkTarget(href)
	return label
}

// statIsDir reports whether path resolves to a directory on disk via
// os.Stat. Stat errors are reported as not-a-directory; panic is
// recovered and treated the same way (rare on weird FUSE / sshfs
// mounts where the Stat call itself panics instead of returning an
// error).
//
// Path resolution is Go's standard "relative paths resolve against
// the current working directory" rule; nothing custom.
func statIsDir(path string) bool {
	defer func() {
		// Recover from any panic os.Stat might raise. The cycle
		// pin: a panic here must NOT crash the TUI — fall back
		// to "not a directory" so the regular file-link render
		// path runs. The recovered value is intentionally
		// discarded.
		_ = recover()
	}()
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
