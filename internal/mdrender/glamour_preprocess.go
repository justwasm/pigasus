// glamour_preprocess.go — file-link and directory-link URL
// pre-processor for the glamour adapter.
//
// Cycle 1.3.3 contract: a Markdown link whose destination is a
// workspace-relative file path with a raw `#L<n>` anchor is
// normalised so editors that require percent-encoded anchors
// (`%23L<n>`) still resolve the file/line:
//
//	[label](src/foo.go#L12)  →  [label](src/foo.go%23L12)
//
// The legacy mdrender walker hooks into goldmark's *ast.Link node
// and runs render.NormalizeFileLink on the destination before
// emission. glamour v2 takes a raw string, so we have to do the
// rewrite upstream of the parser.
//
// Cycle 252 contract (AGY 1.4.0 changelog line 25 —
// "Fixed links to directories disappearing from agent responses;
// they now show the directory name, for example `pipelines/`."):
// a Markdown link whose destination points at a directory (the URL
// ends with `/`, OR the URL resolves to a directory on disk via
// os.Stat) is rendered as plain text rather than as an OSC 8
// hyperlink. The pre-processor substitutes the entire
// `[label](url)` with the URL itself (with a single trailing `/`)
// so glamour's link style — which would otherwise emit the OSC 8
// frame the user never sees and silently drop the URL — leaves a
// visible directory name on screen:
//
//	[label](pipelines/)      →  pipelines/
//	[Pipeline](src/foo/)     →  src/foo/         (src/foo is a dir on disk)
//
// The dir-link detection runs after the file-link rewrite so the
// normalised URL is what gets classified; the file-link rewrite is
// a no-op for paths without `#L<n>` anchors, so the order is
// observable only for the rare file-with-anchor case (which does
// not double as a directory).
//
// The walker:
//
//  1. is regex-based (cycle-175 path); glamour's AST visitor is
//     not exposed to us, and a regex on the source string is more
//     than fast enough for transcript-sized inputs.
//  2. only touches the destination half of `[label](url)`
//     constructs; the label is left alone in the file-link path.
//  3. leaves `<autolink>` URLs alone (autolinks do not encode
//     `#L<n>` anchors usefully anyway — the surrounding `<…>`
//     already serves as a delimiter).
//  4. is idempotent: a URL that is already percent-encoded passes
//     through (render.NormalizeFileLink is a no-op on its own
//     output).
//  5. round-trips hover markers: a `?h=<n>` query parameter
//     survives verbatim because render.NormalizeFileLink only
//     touches `#L<n>` fragments.
//  6. replaces directory-links with the URL itself (plain text),
//     dropping the markdown wrapper; the OSC 8 hyperlink emission
//     that glamour would otherwise attach is bypassed entirely.
//     The non-directory case is byte-identical to the cycle 1.3.3
//     output, so existing preprocessor tests are unaffected.
package mdrender

import (
	"regexp"

	"github.com/justwasm/pigasus/render"
)

// markdownLinkRe finds every `[label](url)` markdown link with a
// fully bracketed label (no `[` / `]` inside the label, no `\`
// escapes). The label group is non-greedy; the URL group is
// permissive (no whitespace, no `)`).
//
// Autolinks (`<https://…>`) and reference-style links
// (`[label][ref]` with the [ref]: URL elsewhere) are NOT matched
// here. Reference-style links are uncommon in the clone's
// transcripts; the `\`` escaping rule would be a separate regex.
var markdownLinkRe = regexp.MustCompile(`\[([^\[\]\n]+)\]\(([^()\s]+)\)`)

// preprocessFileLinks rewrites every matched markdown link's URL
// through render.NormalizeFileLink so `#L<n>` anchors become
// `%23L<n>` before glamour sees the source.
//
// Cycle 252 extends the walker to also classify directory-links:
// URLs that end with `/` (URL convention) OR resolve to an existing
// directory on disk via os.Stat. A directory-link is replaced with
// the URL itself — single trailing `/`, no markdown wrapper — so
// glamour's link style, which would otherwise emit the OSC 8
// hyperlink frame the user never sees and silently drop the URL,
// leaves a visible directory name on screen. The non-directory
// case is byte-identical to the cycle 1.3.3 output; the dir-link
// branch only fires when DirLinkTarget returns true.
//
// The function takes and returns plain strings; the source is
// never mutated in place. Callers that mutate the source string
// between calls are responsible for sequencing the pre-pass
// before glamour.
//
// Pin in: TestGlamour_PreprocessFileLinks, TestGlamour_DirLinkReplacesOSC8Hyperlink.
func preprocessFileLinks(src string) string {
	if src == "" {
		return src
	}
	return markdownLinkRe.ReplaceAllStringFunc(src, func(match string) string {
		sub := markdownLinkRe.FindStringSubmatch(match)
		if len(sub) != 3 {
			return match
		}
		label, url := sub[1], sub[2]
		normalised := render.NormalizeFileLink(url)
		// Cycle 252: directory-link substitution. When the URL
		// (post file-link normalisation) is a directory-link, the
		// entire `[label](url)` is replaced with the URL itself,
		// so glamour emits plain text instead of an OSC 8 frame
		// the user can't see. The label is intentionally
		// discarded: the changelog example is "they now show
		// the directory name, for example `pipelines/`", and the
		// directory name (with trailing /) is what users expect.
		// RenderDirLink handles the trailing-slash-collapse rule
		// for repeated "/" inputs.
		if dirLabel := RenderDirLink(normalised); dirLabel != "" {
			return dirLabel
		}
		if normalised == url {
			// No anchor rewrite; skip the rebuild to keep the
			// input bytes byte-identical where possible.
			return match
		}
		return "[" + label + "](" + normalised + ")"
	})
}
