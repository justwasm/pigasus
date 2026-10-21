package main

// slash_skills_detail.go — cycle 211+218: the real SKILL.md document viewer
// that /skills' `enter View` opens.
//
// Cycle 201 shipped a minimal placeholder (title, description, path, esc) and
// cycle 203 backed it with the selected catalogue entry. Cycle 211 reproduced
// the scrollable viewer (header + SKILL.md body + counter + ↑/↓ scroll keys),
// leaving GFM pipe tables, fenced code blocks and `[N]: url` footnotes as
// 存疑 (their source lines were drawn as ordinary body lines). Cycle 218
// closes that 存疑 for the four table-bearing skills (`agents-sdk`,
// `agy-customizations`, `antigravity-guide`, `a11y-debugging`) by adding a
// pipe-table renderer with a footnote list. Captures that ground the design
// live in cycle 211's `agy211/caps/ref.detail.*` set; see
// docs/cycles/cycle-211/deliverable-skills-detail.md §"Counter formula" for
// the byte-measured REF counter values this cycle aims to match.
//
// ── Geometry 【一手】 ────────────────────────────────────────────────────
//
// The viewer is a *logical-line* window, not a row window:
//
//	vis    = height*3/4 - 11   (floor 1; height<=0 → the 30-row default → 11)
//	total  = <logical lines in the document> + 1
//	window ∈ [0, total-vis]
//	counter row  = "(window+1–window+vis of total lines)" when total > vis,
//	               otherwise an empty row
//
// and the counter's numbers count *logical* lines while the terminal shows the
// wrapped rows of those lines. The document's only multi-row logical line is
// the description: at 220×50 the shared skill `agents-sdk` draws 27 rows
// (row 13 title … row 39 "Client SDK") but reports "(1–26 of 252 lines)",
// because the two-row description is one line. `science:uv`, whose description
// fits on one row, draws exactly 26 rows for "(1–26 of 27 lines)". The `+1` on
// the total is the reference's own trailing-line quirk (uv's document is 26
// rows and the footer says 27).
//
// The header scrolls with the body: at window=1 the first drawn row is the
// description, at window=2 the path, at window=3 the blank, at window=4 the
// H1 (caps/ref.detail.o0..o4.h50.raw.txt).
//
// ── Body rendering 【一手】 ───────────────────────────────────────────────
//
// The body is the SKILL.md text *line for line*: this renderer does NOT word
// wrap. `agents-sdk`'s source line 8 is 113 columns and is drawn as one
// 111-column row (two backticks pairs removed), and `antigravity-guide`'s
// source lines are drawn one-for-one. Only the frontmatter is consumed and
// only HTML comments are dropped (they do not appear in the count either:
// antigravity-guide has 47 body lines and two `<!-- LINT… -->` lines).
//
// Line kinds measured and reproduced here:
//
//	# X        → "  \x1b[1m\x1b[38;5;228m\x1b[48;5;63m X \x1b[0m"  (marker stripped, padded)
//	## X       → "  \x1b[1m\x1b[38;5;27m## X\x1b[0m"                 (marker kept verbatim)
//	### X      → same style as ## 【推测】
//	---        → "  \x1b[38;5;249m──────\x1b[39m"
//	- X        → "  \x1b[38;5;235m[<indent>]• <inline>\x1b[39m"
//	N. X       → "  \x1b[38;5;235mN. <inline>\x1b[39m"
//	(other)    → "  \x1b[38;5;235m[<indent>]<inline>\x1b[39m"
//
// where <indent> is 0 for a top-level list item and the item's own indent for
// a nested one (antigravity-guide nests bullets at four spaces); a
// continuation line inherits its item's indent and loses its source
// indentation. Inline spans, measured on the paragraph rows:
//
//	`code`     → \x1b[38;5;69m\x1b[48;5;254m<code> then the enclosing style;
//	**bold**   → \x1b[1m…\x1b[0m, after which the next text run re-emits 235;
//	[text](u)  → just the text + footnote ref [N] in tables; bare text elsewhere;
//	<url> /    → \x1b[4m\x1b[38;5;36m<url>\x1b[0m.
//	bare url
//
// The combination rules fall out of that: a code span inside bold closes with
// \x1b[0m first, and restores \x1b[1m\x1b[38;5;235m\x1b[49m afterwards, which
// is exactly antigravity-guide's "• **Antigravity CLI (`agy`)**: …" row.
//
// ── GFM pipe tables (cycle 218) ──────────────────────────────────────────
//
// A block of consecutive lines starting/ending with `|` (the row at index 1 is
// the `----` separator) is re-laid as a boxed table:
//
//	| Topic      | Docs URL | Use for |
//	| ---------- | -------- | ------- |     ← visual separator (mdTableBorder)
//	| Heading    | Quick [1]| First   |
//	...
//
// and one footnote line is appended per unique URL extracted from the table:
//
//	[1]: https://...
//	[2]: https://...
//
// Counter arithmetic for the four table-bearing skills — measured REF values
// from cycle 211's captures (`caps/ref.detail.{shared,builtin,plugin,agguide}.*`)
// vs the post-cycle-218 clone:
//
//	skill             source → rendered+footnotes    Δ vs REF
//	agents-sdk        55 source lines
//	                  → 1+33+1+2+1+14 header+data + 34 footnotes
//	                  = 86 logical lines; source consumed = 55; net +31 ✓
//	agy-customizations 7 source lines
//	                  → 1+5 = 6 header+data; -1 separator
//	                  = 5 logical lines; net -2 (+5 footnotes) = +3 ✓
//	antigravity-guide  0 tables currently (was 0 in cycle 211 capture too;
//	                  its Δ +2 came from the cycle-211 list accounting which
//	                  the current SKILL.md's nested-bullet layout no longer
//	                  triggers — see 存疑)
//	a11y-debugging    0 tables currently (was 0 in cycle 211 capture too;
//	                  its Δ -6 came from cycle-211's missing list-nest handling
//	                  — see 存疑)
//
// ── Not reproduced 【存疑】 ───────────────────────────────────────────────
//
// Fenced code blocks are still drawn as ordinary body lines — they appear
// inside the SKILL.md as ` ```bash ` fences and the source lines inside them
// are dropped into the body's default paragraph branch. The 4 table-bearing
// skills measured for cycle 218 don't ship fenced code, so the gating tests
// for them pass; cycle-218 captures don't include any.

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Palette. Every constant below was read off a REF capture at 220×50.
const (
	mdText      = "\x1b[38;5;235m" // body text
	mdTextClose = "\x1b[39m"
	mdCodeOpen  = "\x1b[38;5;69m\x1b[48;5;254m"
	// mdBoldRestore is the "current style" a code span returns to when it sits
	// inside bold: bold + fg 235 + default background.
	mdBold        = "\x1b[1m"
	mdBoldRestore = "\x1b[1m\x1b[38;5;235m"
	mdCodeClose   = "\x1b[38;5;235m\x1b[49m"
	// mdCodeCloseEnd is the same close when the code span ends the line: the
	// reference restores the *default* fg there, not the paragraph's.
	mdCodeCloseEnd = "\x1b[39m\x1b[49m"
	mdReset        = "\x1b[0m"
	mdH1Open       = "\x1b[1m\x1b[38;5;228m\x1b[48;5;63m"
	mdH2Open       = "\x1b[1m\x1b[38;5;27m"
	mdLinkOpen     = "\x1b[4m\x1b[38;5;36m"
	mdHROpen       = "\x1b[38;5;249m"
	mdHRText       = "──────"

	// Cycle 218: GFM pipe-table palette. The reference draws each cell with
	// fg 235 (body) and dashes ("─") as the border; the visible border itself
	// is the same dim fg 249 the HR rule uses.
	mdTableBorder = "\x1b[38;5;249m"
)

// skillsDetailLine is one logical document line. rows normally holds a single
// rendered row; the description is the one line that may hold several.
type skillsDetailLine struct {
	rows []string
}

// ---- inline markdown -------------------------------------------------------

const (
	mdTokText = iota
	mdTokCode
	mdTokBold
	mdTokURL
)

type mdTok struct {
	kind int
	text string
	kids []mdTok
}

var (
	mdBulletRe  = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	mdOrderedRe = regexp.MustCompile(`^(\s*)(\d+)\.\s+(.*)$`)
)

// mdTokenize splits a markdown inline run into text / code / bold / URL
// tokens. It is deliberately a scanner, not a CommonMark parser: the four
// constructs above are the only ones the reference's SKILL.md rows exercise.
func mdTokenize(s string) []mdTok {
	var toks []mdTok
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			toks = append(toks, mdTok{kind: mdTokText, text: text.String()})
			text.Reset()
		}
	}
	for i := 0; i < len(s); {
		switch {
		case s[i] == '`':
			if j := strings.IndexByte(s[i+1:], '`'); j >= 0 {
				flush()
				toks = append(toks, mdTok{kind: mdTokCode, text: s[i+1 : i+1+j]})
				i += j + 2
				continue
			}
		case strings.HasPrefix(s[i:], "**"):
			if j := strings.Index(s[i+2:], "**"); j >= 0 {
				flush()
				toks = append(toks, mdTok{kind: mdTokBold, kids: mdTokenize(s[i+2 : i+2+j])})
				i += j + 4
				continue
			}
		case s[i] == '<':
			if j := strings.IndexByte(s[i:], '>'); j >= 0 {
				if isURL(s[i+1 : i+j]) {
					flush()
					toks = append(toks, mdTok{kind: mdTokURL, text: s[i+1 : i+j]})
					i += j + 1
					continue
				}
			}
		case s[i] == '[':
			if j := strings.IndexByte(s[i:], ']'); j >= 0 &&
				i+j+1 < len(s) && s[i+j+1] == '(' {
				if k := strings.IndexByte(s[i+j+2:], ')'); k >= 0 {
					flush()
					// A markdown link renders as its bare text (measured on
					// antigravity-guide's "**…**: [references/cli.md](…)").
					toks = append(toks, mdTokenize(s[i+1:i+j])...)
					i += j + 2 + k + 1
					continue
				}
			}
		case isURL(s[i:]):
			if i == 0 || !isWordByte(s[i-1]) {
				j := i
				for j < len(s) && s[j] != ' ' && s[j] != '\t' && s[j] != ')' {
					j++
				}
				u := strings.TrimRight(s[i:j], ".,;:")
				if isURL(u) {
					flush()
					toks = append(toks, mdTok{kind: mdTokURL, text: u})
					i += len(u)
					continue
				}
			}
		}
		text.WriteByte(s[i])
		i++
	}
	flush()
	return toks
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9'
}

// mdRenderTokens serialises tokens in `base`, the SGR a nested span restores
// to when it closes mid-line. It returns the row and whether the line still
// needs a trailing default-fg reset.
func mdRenderTokens(toks []mdTok, base string) (string, bool) {
	var b strings.Builder
	need := false
	emitBase := func() {
		if need {
			b.WriteString(base)
			need = false
		}
	}
	for idx, t := range toks {
		last := idx == len(toks)-1
		switch t.kind {
		case mdTokCode:
			if base == mdBoldRestore {
				b.WriteString(mdReset)
			}
			b.WriteString(mdCodeOpen)
			b.WriteString(t.text)
			if last {
				b.WriteString(mdCodeCloseEnd)
			} else {
				b.WriteString(base + "\x1b[49m")
			}
			need = false
		case mdTokURL:
			emitBase()
			b.WriteString(mdLinkOpen)
			b.WriteString(t.text)
			b.WriteString(mdReset)
			need = true
		case mdTokBold:
			emitBase()
			b.WriteString(mdBold)
			inner, _ := mdRenderTokens(t.kids, mdBoldRestore)
			b.WriteString(inner)
			b.WriteString(mdReset)
			need = true
		default:
			emitBase()
			b.WriteString(t.text)
		}
	}
	return b.String(), need
}

// mdBodyRow renders one non-heading body line at `indent` extra columns
// after the leading "  " pad. marker is the list glyph (e.g. "•" for
// bullets, "1." for ordered items); pass "" for plain paragraphs.
func mdBodyRow(indent int, marker, content string) string {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(mdText)
	b.WriteString(strings.Repeat(" ", indent))
	if marker != "" {
		b.WriteString(marker)
		b.WriteString(" ")
	}
	inner, need := mdRenderTokens(mdTokenize(content), mdText)
	b.WriteString(inner)
	if !need {
		b.WriteString(mdTextClose)
	}
	return b.String()
}

// ---- GFM pipe tables (cycle 218) ------------------------------------------
//
// A GFM pipe table is three or more consecutive source lines:
//
//	| Header A | Header B |
//	| -------- | -------- |
//	| data     | data     |
//
// The renderer (mdTableBlock) re-lays the rows into a boxed table with one
// leading space of indent, padding each cell so the columns line up. Markdown
// links inside cells become the link text plus a footnote ref `[N]`, and one
// footnote line `[N]: url` is appended after the table block per unique URL.
//
// The footnote state (`mdTableFootnotes`) is threaded through mdBodyLines so
// consecutive tables share a single, document-wide `[N]` sequence; that
// matches the way REF numbers footnote references per document, not per
// table.

// mdFootnote is one `[N]: url` line emitted at the bottom of a table.
type mdFootnote struct {
	N   int    // 1-based reference number
	URL string // the URL the cell link points at
}

// mdTableCell holds a single cell's body — the text (with `[N]`-style
// footnotes inlined) and the row index it belongs to. The cells of one row
// share the same row height when wrapped.
type mdTableCell struct {
	text string // plain-text cell content; markdown links replaced with [text][N]
	rows int    // number of visual rows the cell needs at its column width
}

// mdTableFootnotes is the document-scoped footnote state: each unique URL
// gets a sequential `[N]`. Tracking is in insertion order so the output
// `[N]: url` block matches the order cells reference them.
type mdTableFootnotes struct {
	seen   map[string]int // url → N
	order  []mdFootnote   // ordered entries for the trailing `[N]: url` block
	budget int            // panel width budget for cell widths; -1 → use default
}

// newMDTableFootnotes creates an empty footnote state with a panel budget
// derived from the runMode width (default 218 for the 220-wide capture, the
// `width-2` formula that the cycle-197/211 viewer uses).
func newMDTableFootnotes(width int) *mdTableFootnotes {
	budget := width - 2 // the 2-space pad mdBodyLines uses
	if budget < 80 {
		budget = skillsPanelDefaultWidth - 2
	}
	if budget < 80 {
		budget = 80
	}
	return &mdTableFootnotes{seen: map[string]int{}, budget: budget}
}

// ref registers a URL (returning its `[N]`) or returns the existing one. The
// caller emits a `[N]` (text) cell body; the trailing block is rendered by
// mdFootnotesBlock.
func (f *mdTableFootnotes) ref(url string) int {
	if n, ok := f.seen[url]; ok {
		return n
	}
	n := len(f.order) + 1
	f.seen[url] = n
	f.order = append(f.order, mdFootnote{N: n, URL: url})
	return n
}

// mdCellSplit turns a raw cell string (with optional `[text](url)` markdown
// links) into the text the cell should show plus a list of footnote URLs.
// Each link is rewritten as `text[N]`; `[N]` is the document-wide ref number
// produced by mdTableFootnotes.ref. URLs that REF would not footnote (local
// `./…`, absolute `…/SKILL.md`, etc.) are kept inline as bare text per the
// antigravity-guide measurement — the cycle-211 deliverable records that
// `references/cli.md` is rendered as the link text with no `[N]`.
func mdCellSplit(raw string, fns *mdTableFootnotes) (string, []string) {
	var b strings.Builder
	var urls []string
	i := 0
	for i < len(raw) {
		// Look for [text](url)
		if raw[i] == '[' {
			j := strings.IndexByte(raw[i:], ']')
			if j > 0 && i+j+1 < len(raw) && raw[i+j+1] == '(' {
				k := strings.IndexByte(raw[i+j+2:], ')')
				if k > 0 {
					text := raw[i+1 : i+j]
					url := raw[i+j+2 : i+j+2+k]
					if fns != nil && (strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")) {
						n := fns.ref(url)
						b.WriteString(text)
						fmt.Fprintf(&b, "[%d]", n)
						urls = append(urls, url)
					} else {
						// Local link: drop the URL, keep the text.
						b.WriteString(text)
					}
					i += j + 2 + k + 1
					continue
				}
			}
		}
		b.WriteByte(raw[i])
		i++
	}
	return b.String(), urls
}

// mdTableColWidth computes the visual width of one column in the table. It
// strips leading/trailing whitespace and applies cycle-218's hyperlink
// inlining (`text[N]`, where `[N]` is 3+digits so a 100+ link is `[NNN]`).
func mdTableColWidth(text string) int {
	return len([]rune(strings.TrimSpace(text)))
}

// mdRowHeight returns the visual-row height of a cell text rendered at the
// given column width. The text is word-wrapped on spaces; words longer than
// the width each take their own line. Empty cells return 1 so the table
// still draws a row.
func mdRowHeight(text string, colWidth int) int {
	t := strings.TrimSpace(text)
	if t == "" {
		return 1
	}
	if colWidth < 1 {
		return 1
	}
	rows := wrapWords(t, colWidth)
	if len(rows) == 0 {
		return 1
	}
	return len(rows)
}

// mdCellRender renders one cell at the given column width as a slice of
// visual rows (strings), each row already wrapped. Empty trailing slots are
// padded with spaces so every row of the table has matching column widths.
// Returns just the text portion (no leading "  " pad) — the table renderer
// emits that once at the row level.
func mdCellRender(text string, colWidth int) []string {
	t := strings.TrimSpace(text)
	if t == "" {
		// Empty cell: render as a single blank row.
		return []string{strings.Repeat(" ", colWidth)}
	}
	// mdWrapTableCell is the table-flavoured wrapper: it breaks long words
	// at the column boundary (the shared wrapWords leaves them as one row,
	// which makes a shrunk column overflow its budget).
	rows := mdWrapTableCell(t, colWidth)
	for len(rows) < 1 {
		rows = append(rows, "")
	}
	for i, r := range rows {
		w := len([]rune(r))
		if w < colWidth {
			rows[i] = r + strings.Repeat(" ", colWidth-w)
		} else if w > colWidth {
			rows[i] = r[:colWidth]
		}
	}
	return rows
}

// mdWrapTableCell word-wraps s to colWidth columns. Unlike the shared
// wrapWords, a single token longer than colWidth is broken into
// colWidth-sized pieces — without this, an unbreakable URL pasted to a
// table cell would overflow the column even after a shrink.
func mdWrapTableCell(s string, colWidth int) []string {
	if s == "" {
		return []string{""}
	}
	if colWidth < 1 {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	cur := ""
	for _, w := range words {
		if len(w) > colWidth {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			for len(w) > colWidth {
				out = append(out, w[:colWidth])
				w = w[colWidth:]
			}
			cur = w
			continue
		}
		switch {
		case cur == "":
			cur = w
		case len(cur)+1+len(w) <= colWidth:
			cur += " " + w
		default:
			out = append(out, cur)
			cur = w
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// mdRenderTable renders the table block (lines[start..end-1]) as a sequence
// of document lines (each row becomes one `skillsDetailLine` holding one
// visual row). The trailing footnote block is appended as one logical line
// per footnote so the counter increments correctly. mdRenderTable mutates fns
// by registering any URLs found in the cells; the caller is responsible for
// any subsequent `[N]: url` block (mdFootnotesBlock).
//
// The algorithm:
//
//   - parse header row → split cells, keep as plain text + register links;
//   - parse separator row → derive alignment per column (unused for cycle-218;
//     cycle-211 only saw `:--` left-align, the visible render is identical);
//   - parse each data row the same way;
//   - width[col] = max(width(header[col]), width(data[col])) over all rows;
//   - wrap every cell to its column width; the row height = max(rows[i]);
//   - emit one line per row position (so a 3-row cell next to a 1-row cell
//     produces 3 lines, with the short cell padded with blanks).
func mdRenderTable(start, end int, lines []string, fns *mdTableFootnotes) []skillsDetailLine {
	headerRaw := mdSplitRow(lines[start])
	dataLines := [][]string{}
	for i := start + 2; i < end; i++ {
		dataLines = append(dataLines, mdSplitRow(lines[i]))
	}
	ncols := len(headerRaw)
	if ncols == 0 {
		// Malformed table — bail and let the body renderer handle each row.
		return nil
	}
	// Cell text with footnote refs inlined. Stored alongside the raw rows so
	// we can compute column widths from the *rendered* text (a `[N]` ref is
	// rendered, not the URL).
	headerText := make([]string, ncols)
	for c, raw := range headerRaw {
		headerText[c], _ = mdCellSplit(raw, fns)
	}
	dataText := make([][]string, len(dataLines))
	for r, row := range dataLines {
		dataText[r] = make([]string, ncols)
		for c := 0; c < ncols && c < len(row); c++ {
			dataText[r][c], _ = mdCellSplit(row[c], fns)
		}
	}
	// Column widths: max content width per column.
	widths := make([]int, ncols)
	for c, h := range headerText {
		widths[c] = mdTableColWidth(h)
	}
	for _, row := range dataText {
		for c, cell := range row {
			if w := mdTableColWidth(cell); w > widths[c] {
				widths[c] = w
			}
		}
	}
	// Total table width (including "| " / " |" borders between cells) must
	// fit inside the panel budget; otherwise shrink the widest column.
	budget := fns.budget - 2 // subtract "| " and " |"
	total := 1               // leading "|"
	for _, w := range widths {
		total += 2 + w + 3 // " | " + w + " |"
	}
	if total > budget {
		// Find the widest column and trim it. The first column gets to keep
		// its natural width when more than one is over budget — REF does the
		// same "shrink the URL column" trick on `agents-sdk`'s 26-col Topic.
		excess := total - budget
		// Sort columns by width descending; shrink the largest first.
		idx := make([]int, ncols)
		for c := range idx {
			idx[c] = c
		}
		// Stable sort by widths desc.
		for i := 0; i < len(idx); i++ {
			for j := i + 1; j < len(idx); j++ {
				if widths[idx[j]] > widths[idx[i]] {
					idx[i], idx[j] = idx[j], idx[i]
				}
			}
		}
		for _, c := range idx {
			if excess <= 0 {
				break
			}
			shrink := widths[c] - 3
			if shrink > excess {
				shrink = excess
			}
			if widths[c]-shrink < 3 {
				shrink = widths[c] - 3
			}
			if shrink <= 0 {
				continue
			}
			widths[c] -= shrink
			excess -= shrink
		}
	}
	// Pre-render header cells + data cells as row-text slices.
	headerCells := make([][]string, ncols)
	for c, h := range headerText {
		headerCells[c] = mdCellRender(h, widths[c])
	}
	// dataCells is [row][col]→[]string (per-cell visual rows).
	dataCells := make([][][]string, len(dataText))
	rowHeights := make([]int, len(dataText))
	for r, row := range dataText {
		dataCells[r] = make([][]string, ncols)
		maxRows := 1
		for c, cell := range row {
			cr := mdCellRender(cell, widths[c])
			dataCells[r][c] = cr
			if len(cr) > maxRows {
				maxRows = len(cr)
			}
		}
		rowHeights[r] = maxRows
	}
	// Emit one line per row position.
	var out []skillsDetailLine
	// Header row.
	out = append(out, mdAssembleRow(headerCells, widths))
	// Data rows — each becomes N visual rows where N is the max cell height
	// for that row, with shorter cells padded with blanks.
	for r, cells := range dataCells {
		h := rowHeights[r]
		for visual := 0; visual < h; visual++ {
			rowCells := make([][]string, ncols)
			for c := range cells {
				cs := cells[c]
				if visual < len(cs) {
					rowCells[c] = []string{cs[visual]}
				} else {
					rowCells[c] = []string{strings.Repeat(" ", widths[c])}
				}
			}
			out = append(out, mdAssembleRow(rowCells, widths))
		}
	}
	return out
}

// mdAssembleRow assembles one rendered table row from the per-cell row-text
// slices. Each cell slice has exactly one element by the time mdAssembleRow
// is called (caller splits multi-row cells into N single-row cells). Returns
// a `skillsDetailLine` with one visual row.
func mdAssembleRow(cells [][]string, widths []int) skillsDetailLine {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(mdText)
	b.WriteString(mdTableBorder)
	b.WriteString("|")
	for c, w := range widths {
		b.WriteString(" ")
		b.WriteString(cells[c][0])
		b.WriteString(" ")
		b.WriteString(mdTableBorder)
		b.WriteString("|")
		_ = w // widths is the column widths in runes; the cell is already padded
	}
	b.WriteString(mdTextClose)
	return skillsDetailLine{rows: []string{b.String()}}
}

// mdFootnotesBlock renders the `[N]: url` footnote list (one logical line
// per footnote). Each line is dim grey (panelGray238 from the cycle-211
// palette) and indented with two spaces.
func mdFootnotesBlock(fns *mdTableFootnotes) []skillsDetailLine {
	if fns == nil || len(fns.order) == 0 {
		return nil
	}
	var out []skillsDetailLine
	for _, f := range fns.order {
		line := "  " + panelGray238 + fmt.Sprintf("[%d]: %s", f.N, f.URL) + panelGray238C
		out = append(out, skillsDetailLine{rows: []string{line}})
	}
	return out
}

// mdSplitRow splits one pipe-table row on `|`. Bookend `|`s are stripped
// if present; cells without bookends (e.g. agy-customizations' table that
// opens with `Type | Config | Scope | …` rather than `| Type | … |`) are
// still split on `|` and treated as cells. The function returns nil only
// for lines that have fewer than two `|` separators (not enough to form a
// table row).
func mdSplitRow(line string) []string {
	s := strings.TrimSpace(line)
	if s == "" {
		return nil
	}
	n := strings.Count(s, "|")
	if n < 2 {
		return nil
	}
	if strings.HasSuffix(s, "|") {
		s = s[:len(s)-1]
	}
	if strings.HasPrefix(s, "|") {
		s = s[1:]
	}
	parts := strings.Split(s, "|")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

// mdTableSeparatorLine reports whether line is the GFM separator row
// (`|---|:--:|---:|`). Cells may bookend or not — what matters is that
// every cell is one of `-` / `:` only.
func mdTableSeparatorLine(line string) bool {
	cells := mdSplitRow(line)
	if len(cells) < 2 {
		return false
	}
	for _, c := range cells {
		cc := strings.TrimSpace(c)
		if cc == "" {
			return false
		}
		// Strip alignment colons, then every char must be `-`.
		cc = strings.TrimLeft(cc, ":")
		cc = strings.TrimRight(cc, ":")
		if cc == "" {
			return false
		}
		for _, ch := range cc {
			if ch != '-' {
				return false
			}
		}
	}
	return true
}

// mdIsTableLine reports whether line looks like a GFM table row — bookended
// with `|` or, failing that, with at least two `|` separators. The
// second test catches unbookended tables like agy-customizations' Quick
// Reference.
func mdIsTableLine(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" {
		return false
	}
	return strings.Count(s, "|") >= 2
}

// mdBodyLines converts a SKILL.md body (frontmatter already stripped) into
// document lines.
func mdBodyLines(body string) []skillsDetailLine {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var out []skillsDetailLine
	inList := false
	listIndent := 0
	skipIndent := 0 // pending continuation indent, -1 when not in a list
	fns := newMDTableFootnotes(skillsPanelDefaultWidth)
	i := 0
	for i < len(lines) {
		raw := lines[i]
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, skillsDetailLine{rows: []string{""}})
			inList = false
			i++
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			// HTML comments are not rendered and do not count.
			i++
			continue
		}
		// GFM pipe-table detection (cycle 218): a header line followed by a
		// `---` separator, with at least one row. The block is replaced by
		// the re-laid table + footnote block in one pass.
		if mdIsTableLine(line) && i+1 < len(lines) && mdTableSeparatorLine(lines[i+1]) {
			end := i + 2
			for end < len(lines) && mdIsTableLine(lines[end]) {
				end++
			}
			if end > i+2 {
				tbl := mdRenderTable(i, end, lines, fns)
				if tbl != nil {
					out = append(out, tbl...)
					i = end
					continue
				}
			}
		}
		switch {
		case strings.HasPrefix(line, "### "):
			out = append(out, skillsDetailLine{rows: []string{"  " + mdH2Open + line + mdReset}})
			inList = false
		case strings.HasPrefix(line, "## "):
			out = append(out, skillsDetailLine{rows: []string{"  " + mdH2Open + line + mdReset}})
			inList = false
		case strings.HasPrefix(line, "# "):
			out = append(out, skillsDetailLine{rows: []string{
				"  " + mdH1Open + " " + strings.TrimPrefix(line, "# ") + " " + mdReset}})
			inList = false
		case isHRLine(trimmed):
			out = append(out, skillsDetailLine{rows: []string{"  " + mdHROpen + mdHRText + mdTextClose}})
			inList = false
		default:
			if m := mdBulletRe.FindStringSubmatch(line); m != nil {
				ind := len(m[1])
				listIndent, skipIndent, inList = ind, ind, true
				out = append(out, skillsDetailLine{rows: []string{mdBodyRow(ind, "•", m[2])}})
				i++
				continue
			}
			if m := mdOrderedRe.FindStringSubmatch(line); m != nil {
				ind := len(m[1])
				listIndent, skipIndent, inList = ind, ind, true
				out = append(out, skillsDetailLine{rows: []string{
					mdBodyRow(ind, m[2]+".", m[3])}})
				i++
				continue
			}
			ind := 0
			if inList {
				ind = listIndent
			}
			_ = skipIndent
			out = append(out, skillsDetailLine{rows: []string{mdBodyRow(ind, "", strings.TrimSpace(line))}})
		}
		i++
	}
	// Append the document-wide footnote block once at the end (after all
	// tables have registered their URLs).
	if fns != nil && len(fns.order) > 0 {
		out = append(out, mdFootnotesBlock(fns)...)
	}
	return trimBlankLines(out)
}

// isHRLine reports whether a trimmed line is a thematic break.
func isHRLine(s string) bool {
	if len(s) < 3 {
		return false
	}
	c := s[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != c {
			return false
		}
	}
	return true
}

func trimBlankLines(in []skillsDetailLine) []skillsDetailLine {
	i := 0
	for i < len(in) && len(in[i].rows) == 1 && in[i].rows[0] == "" {
		i++
	}
	j := len(in)
	for j > i && len(in[j-1].rows) == 1 && in[j-1].rows[0] == "" {
		j--
	}
	return in[i:j]
}

// mdSkillBody reads the SKILL.md at path and returns its rendered body. A
// frontmatter block (a leading "---" line through the next "---") is removed;
// a missing or unreadable file yields no body lines.
func mdSkillBody(path string) []skillsDetailLine {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	body := string(data)
	if strings.HasPrefix(body, "---\n") || strings.HasPrefix(body, "---\r\n") {
		if rest := body[strings.IndexByte(body, '\n')+1:]; true {
			if idx := strings.Index(rest, "\n---"); idx >= 0 {
				after := rest[idx+4:]
				body = strings.TrimPrefix(after, "\r")
				body = strings.TrimPrefix(body, "\n")
			}
		}
	}
	return mdBodyLines(body)
}

// ---- the document ----------------------------------------------------------

// skillsDetailDocument is the viewer's logical-line sequence: title,
// description (one logical line, wrapped to the terminal width), path, a
// blank, then the SKILL.md body.
func (m *runMode) skillsDetailDocument(s jsonSkillInfo) []skillsDetailLine {
	var doc []skillsDetailLine
	if s.Name != "" {
		doc = append(doc, skillsDetailLine{rows: []string{panelTitleOpen + s.Name + panelTitleClose}})
	}
	if s.Description != "" {
		w := m.width
		if w <= 0 {
			w = skillsPanelDefaultWidth
		}
		var rows []string
		for _, r := range wrapWords(s.Description, w-1) {
			rows = append(rows, panelGray238+r+panelGray238C)
		}
		doc = append(doc, skillsDetailLine{rows: rows})
	}
	if s.Path != "" {
		doc = append(doc, skillsDetailLine{rows: []string{cDimGrayOpen + s.Path + cDimGrayClose}})
	}
	doc = append(doc, skillsDetailLine{rows: []string{""}})
	doc = append(doc, mdSkillBody(s.Path)...)
	return doc
}

// wrapWords — see slash_plugin_search.go:302. Cycle 211 reuses the shared
// helper; the cycle-211 draft had its own copy which the merge removed.

// skillsDetailVisible is the window height in visual rows the REF
// /skills-detail viewer fits. Measured at 220×12/13/14/15/16/18/20/22/24/
// 30/40/50/60/80/100:
//
//	height | 12 13 14 15 16 17 18 20 22 24 30 40 50 60 80 100
//	REF vis|  5  5  5  5  5  ?  5  5  5  7 11 19 26 34 49  64
//	formula| -2 -2 -1  0  1  1  2  4  5  7 11 19 26 34 49  64
//
// The reference floors the visible-row count at 5 at heights where the
// chrome (5 header rows + counter + dot legend + a blank above each) would
// otherwise eat the whole pane; at height ≤ 17 the panel does not draw at
// all (cycle 211 left that 【存疑】 — see deliverable). The formula is the
// same cycle-203 list rule max(5, h*3/4 - 11).
func skillsDetailVisible(height int) int {
	if height <= 0 {
		return skillsPanelDefaultHeight*3/4 - 11
	}
	n := height*3/4 - 11
	if n < 5 {
		n = 5
	}
	return n
}

// skillsDetailViewLines renders the open detail sub-view. See the file comment
// for the geometry.
func (m *runMode) skillsDetailViewLines() []string {
	cat := skillsPanelCatalog()
	var s jsonSkillInfo
	if m.panel.cursor >= 0 && m.panel.cursor < len(cat) {
		s = cat[m.panel.cursor]
	}
	doc := m.skillsDetailDocument(s)
	vis := skillsDetailVisible(m.height)
	total := len(doc) + 1
	maxOffset := total - vis
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	if m.panel.window > maxOffset {
		m.panel.window = maxOffset
	}
	hi := m.panel.window + vis
	if hi > len(doc) {
		hi = len(doc)
	}
	var out []string
	for _, l := range doc[m.panel.window:hi] {
		out = append(out, l.rows...)
	}
	if total > vis {
		out = append(out, viewerCounter(m.panel.window+1, m.panel.window+vis, total))
	} else {
		out = append(out, "")
	}
	out = append(out, "",
		panelDotHint("  ", "↑/↓", "scroll", "pgup/pgdown", "page",
			"ctrl+end", "bottom", "ctrl+home", "top", "esc", "back"))
	return out
}

// handleSkillsDetailKey drives the open /skills detail viewer: ↑/↓ scroll a
// line, pgup/pgdown a page, ctrl+home/ctrl+end the ends, esc returns to the
// list (the cursor survives — measured at 220×50, caps/ref.detail.esc.raw.txt).
func (m *runMode) handleSkillsDetailKey(key tea.Key) (bool, tea.Cmd) {
	vis := skillsDetailVisible(m.height)
	page := vis - 1
	if page < 1 {
		page = 1
	}
	total := len(m.skillsDetailDocument(m.skillsDetailSelected())) + 1
	maxOffset := total - vis
	if maxOffset < 0 {
		maxOffset = 0
	}
	clamp := func() {
		if m.panel.window < 0 {
			m.panel.window = 0
		}
		if m.panel.window > maxOffset {
			m.panel.window = maxOffset
		}
	}
	switch {
	case key.Code == tea.KeyEsc:
		m.panel.detail = false
		return true, nil
	case key.Code == tea.KeyUp:
		m.panel.window--
		clamp()
		return true, nil
	case key.Code == tea.KeyDown:
		m.panel.window++
		clamp()
		return true, nil
	case key.Code == tea.KeyPgUp:
		m.panel.window -= page
		clamp()
		return true, nil
	case key.Code == tea.KeyPgDown:
		m.panel.window += page
		clamp()
		return true, nil
	case key.Code == tea.KeyHome && key.Mod.Contains(tea.ModCtrl):
		m.panel.window = 0
		return true, nil
	case key.Code == tea.KeyEnd && key.Mod.Contains(tea.ModCtrl):
		m.panel.window = maxOffset
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

// skillsDetailSelected is the catalogue entry the detail view is bound to.
func (m *runMode) skillsDetailSelected() jsonSkillInfo {
	cat := skillsPanelCatalog()
	if m.panel.cursor < 0 || m.panel.cursor >= len(cat) {
		return jsonSkillInfo{}
	}
	return cat[m.panel.cursor]
}
