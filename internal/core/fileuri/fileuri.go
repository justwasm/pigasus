// Package fileuri resolves a `file://` URI to the path it names.
//
// 【一手】 agy 1.5.18 changelog: "Fixed file permission rules and approval
// prompts not applying to files the agent reads or edits through `file:///`
// URIs; those paths now resolve to the real file before permissions are
// checked."
//
// Two layers need the same answer, for the same reason: the permission
// engine matches rules against paths, and the file tools open paths — and
// a model can name one file two ways, `/etc/passwd` and
// `file:///etc/passwd`. If either layer resolved the URI differently from
// the other, the call would be judged against one file and act on another,
// so the resolution lives here rather than in either caller.
//
// The package is tier 1 and imports only the standard library: it is a
// fact about strings, not about the filesystem, and the permission engine
// (tier 1) cannot reach anything above itself.
package fileuri

import (
	"net/url"
	"strings"
)

// Path turns a `file://` URI into the path it names, and returns anything
// else unchanged.
//
// Only the shapes a file URI actually takes are rewritten:
//
//	file:///abs/path        →  /abs/path
//	file://localhost/abs/x  →  /abs/x
//	file://host/share/x     →  //host/share/x   (a UNC path; kept spelled so
//	                                              the credential-leak gate
//	                                              can still reject it)
//	file:/abs/path          →  /abs/path
//	file:///C:/x            →  /C:/x            (the drive strip is the
//	                                              caller's normalisation)
//
// Percent escapes are decoded, because that is how a URI spells a space
// (`file:///tmp/my%20notes.md`) while a rule on disk, and a path on disk,
// are written with the real character. The decode is a path decode, not a
// query decode: `+` stays a literal plus.
//
// The function is purely syntactic — it never touches the filesystem.
// Permissions are decided before anything is opened, so resolving symlinks
// here would answer a question about a *different* file than the one the
// rule names, and would answer it differently on a machine where the file
// does not exist yet.
func Path(p string) string {
	if !isFileURIPrefix(p) {
		return p
	}
	rest := p[len("file:"):]
	// An authority only exists in the "file://" form; "file:/abs" has none,
	// and its remainder already starts at the root.
	if strings.HasPrefix(rest, "//") {
		rest = rest[2:]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			authority, tail := rest[:i], rest[i:]
			switch strings.ToLower(authority) {
			case "", "localhost":
				rest = tail
			default:
				// A named host: the URI names a remote share, so keep it
				// spelled as a UNC path. It must not become a local path
				// that a rule about a local file would match.
				rest = "//" + authority + tail
			}
		} else {
			// "file://" with no path at all.
			rest = ""
		}
	}
	if decoded, err := url.PathUnescape(rest); err == nil {
		rest = decoded
	}
	if rest == "" {
		return "/"
	}
	return rest
}

// isFileURIPrefix reports whether p spells a file URI rather than a path
// that happens to start with the same letters.
//
// The test is the scheme plus a slash: `file://...` and `file:/...` are
// URIs, while a relative name like "file:notes.txt" is not — treating it as
// one would silently drop the "file:" the rule was written with.
func isFileURIPrefix(p string) bool {
	if len(p) < len("file:/") || !strings.EqualFold(p[:len("file:")], "file:") {
		return false
	}
	return p[len("file:")] == '/'
}
