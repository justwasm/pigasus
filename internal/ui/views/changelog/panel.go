// panel.go — the /changelog panel's own rows: the document it scrolls,
// the window over it and the frame under the window.
//
// # Where the facts live
//
// The panel's document has three shapes (see source.go, wrap.go and
// excerpt.go): the loaded reference file, the embedded markdown and the
// excerpt. The window over the document and the counter under it are
// the panel's as well — the host keeps the offset (its panel struct
// carries one window offset that several panels share) and hands the
// clamped window over.

package changelog

import (
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// Document is the /changelog document for a terminal of the given size.
//
// The document's source is the reference's own ladder: the cached copy of
// the published release notes first (see cache.go), then the reference's
// release-notes file when the clone runs from a checkout, and the notes
// embedded in the binary last.
//
// A terminal whose width *and* height are known shows the wrapped
// release notes — or the embedded markdown when the reference's own
// file is missing. Anything else shows the static, unwrapped document:
// the styled source, or the excerpt followed by the remainder of the
// release notes.
func Document(width, height int) []string {
	if md, ok := cachedMarkdown(); ok {
		return markdownDocument(md, width, height)
	}
	if src := Lines(); src != nil {
		if width > 0 && height > 0 {
			return Wrapped(src, width)
		}
		return Styled(src)
	}
	if width > 0 && height > 0 {
		return PanelDoc(width)
	}
	out := append([]string{}, ExcerptRows()...)
	return append(out, BuildRestRows()...)
}

// markdownDocument renders a markdown release-notes document — the cached
// copy or the embedded one — in the panel's two shapes.
//
// The wrapped shape is the markdown rendered to a width; the static shape
// is the unwrapped transcription under the panel's own title and tagline.
func markdownDocument(md string, width, height int) []string {
	if width > 0 && height > 0 {
		return panelDocFrom(md, width, WrapBudget(width))
	}
	out := []string{TitleRow, "", Tagline}
	return append(out, markdownRows(md, 0)...)
}

// VisibleRows is how many document rows the panel shows at a terminal
// height.
//
// A known height gives the reference's own geometry — the panel's title
// and chrome take four rows — and an unknown one falls back to the
// window's fixed height (see WindowRows).
func VisibleRows(height int) int {
	if height > 0 {
		vis := height - 4
		if vis < 1 {
			vis = 1
		}
		return vis
	}
	return WindowRows()
}

// PageRows is how many rows a page key scrolls in the /changelog panel.
//
// A pager scrolls a whole window. The reference's /changelog scrolls one
// row less on the path where the terminal's height is unknown, which is
// the path the unit tests take; a known height scrolls the whole
// window.
//
// The height is the host's fact — the terminal's size is the host's —
// so it arrives as an argument, and the window's own height comes from
// the document the panel scrolls.
func PageRows(spec tuiviewer.Spec, height int) int {
	if height > 0 {
		return spec.Visible
	}
	page := spec.Visible - 1
	if page < 1 {
		page = 1
	}
	return page
}

// PanelLines is the /changelog panel's frame: the counter and the hint
// under the window over the document.
//
// The window itself is the host's — the host's panel struct carries one
// offset that several panels share — so the rows it produced arrive as
// an argument, with the numbers the counter quotes.
func PanelLines(rows []string, start, end, total int) []string {
	lines := append([]string{}, rows...)
	lines = append(lines,
		tuiviewer.Counter(start, end, total),
		"",
		tuilib.PanelDotHint("  ", "↑/↓", "Navigate", "esc", "Cancel"))
	return lines
}
