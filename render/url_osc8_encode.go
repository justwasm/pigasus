// url_osc8_encode.go — minimum-viable URI-segment encoder for the OSC 8
// hyperlink emitter at render.go:Hyperlink.
//
// AGY 1.4.1 fix (recovered from the binary strings table and the
// changelog pin at internal/cli/testdata/changelog_reference.txt:13):
//
//	"Fixed clickable links in agent responses whose URL contains `;`
//	 opening a URL cut off at the first `;` in terminals and
//	 multiplexers that split link sequences on semicolons."
//
// The OSC 8 escape is
//
//	ESC ] 8 ; ; <URI> ESC \ <body> ESC ] 8 ; ; ESC \
//
// where <URI> is, per the OSC 8 spec, RFC 3986 percent-encoded. Some
// terminals and multiplexers (notably some tmux builds, the GNU screen
// fork, and a handful of web-based terminal emulators) treat the bare
// `;` character as an OSC parameter separator and split the sequence
// at the first `;` they see inside the URI segment — so a URL like
// `https://example.com/foo?a=1;b=2` (perfectly valid in HTTP) opens as
// `https://example.com/foo?a=1` and the query string is silently
// dropped.
//
// The fix is to percent-encode the URI segment before writing it into
// the OSC 8 frame. The cycle ships the minimum viable encoding: only
// `;` (the OSC parameter separator that triggers the bug) and `\\`
// (the ST escape character — `\\` would close the OSC frame early and
// leak the body text). All other characters are left verbatim:
//
//   - RFC 3986 *allows* `?`, `&`, `=`, `#`, `/`, `:`, `@` and
//     sub-delims in their respective URI components without
//     percent-encoding, and the rendered hyperlink needs to navigate
//     to the URL the caller asked for. Re-encoding them would change
//     what the link opens.
//   - Spaces, percent signs, non-ASCII characters etc. are the
//     caller's responsibility — the existing call sites
//     (render.FormatFileLink, render.FormatFileLinkAt,
//     render.NormalizeFileLink) already build RFC-3986-clean URLs.
//     The encoder is a defence-in-depth layer for raw URLs that
//     bypass those helpers (e.g. URLs surfaced directly from the
//     model's output).
//
// A full RFC 3986 percent-encoding pass — every reserved / unreserved /
// percent-triplet — would be a follow-up cycle. It would also be
// opinionated about which component (path / query / fragment) gets
// which treatment, and the strings table cannot fully reveal which
// characters AGY 1.4.1 chose to encode. This cycle is deliberately
// scoped to the two characters the bug report names.

package render

import "strings"

// osc8URIReplacer is the single-pass replacement that encodes the two
// OSC-frame-significant characters. strings.NewReplacer is a single
// allocation and a fixed table; it's the cheapest stdlib tool for the
// "replace a small, fixed set of literal characters" job. A for-loop
// builder would also work, but the replacer reads cleaner and ships
// in the same package without an extra import.
var osc8URIReplacer = strings.NewReplacer(
	";", "%3B",
	"\\", "%5C",
)

// urlForOSC8 returns rawURL with every `;` replaced by `%3B` and every
// `\\` replaced by `%5C`, leaving all other characters verbatim.
//
// The function is the minimum viable AGY 1.4.1 fix: terminals and
// multiplexers that split OSC 8 sequences on `;` would otherwise
// truncate the URI segment at the first unencoded semicolon, and a
// literal `\\` would close the OSC frame before the ST terminator and
// leak the body text outside the hyperlink. See the file-level
// comment for the full rationale and the explicit non-goals (no
// full RFC 3986 pass; spaces / non-ASCII / percent-triplet handling
// stays the caller's responsibility).
//
// urlForOSC8 is a pure function. It does no I/O, no allocation beyond
// the result string, and never mutates rawURL.
//
//	urlForOSC8("https://example.com/foo?a=1;b=2") → "https://example.com/foo?a=1%3Bb=2"
//	urlForOSC8("https://example.com/\\backslash")  → "https://example.com/%5Cbackslash"
//	urlForOSC8("https://example.com/no-special")  → "https://example.com/no-special" (byte-identical)
func urlForOSC8(rawURL string) string {
	return osc8URIReplacer.Replace(rawURL)
}
