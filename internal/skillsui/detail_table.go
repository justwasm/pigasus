package skillsui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/textwrap"
)

// MDFootnote is one [N]: url line emitted at the bottom of a table.
type MDFootnote struct {
	N   int
	URL string
}

// MDTableCell holds a single cell's body.
type MDTableCell struct {
	Text string
	Rows int
}

// MDTableFootnotes is the document-scoped footnote state.
type MDTableFootnotes struct {
	Seen   map[string]int
	Order  []MDFootnote
	Budget int
}

// NewMDTableFootnotes creates an empty footnote state with a panel budget.
func NewMDTableFootnotes(width int) *MDTableFootnotes {
	budget := width - 2
	if budget < 80 {
		budget = SkillsPanelDefaultWidth - 2
	}
	if budget < 80 {
		budget = 80
	}
	return &MDTableFootnotes{Seen: map[string]int{}, Budget: budget}
}

func (f *MDTableFootnotes) ref(url string) int {
	if n, ok := f.Seen[url]; ok {
		return n
	}
	n := len(f.Order) + 1
	f.Seen[url] = n
	f.Order = append(f.Order, MDFootnote{N: n, URL: url})
	return n
}

// MDCellSplit turns a raw cell string into text plus footnote URLs.
func MDCellSplit(raw string, fns *MDTableFootnotes) (string, []string) {
	var b strings.Builder
	var urls []string
	i := 0
	for i < len(raw) {
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

// mdTableColWidth is the natural width of one column: the width of its
// widest cell, in terminal cells.
//
// The cells are counted the way the wrap counts them — in terminal
// cells, not in runes or bytes. A CJK character is two cells wide, so a
// column of ideographs is twice as wide as its rune count says.
func mdTableColWidth(text string) int {
	return ansi.StringWidth(strings.TrimSpace(text))
}

func mdCellRender(text string, colWidth int) []string {
	t := strings.TrimSpace(text)
	if t == "" {
		return []string{strings.Repeat(" ", colWidth)}
	}
	rows := MDWrapTableCell(t, colWidth)
	for len(rows) < 1 {
		rows = append(rows, "")
	}
	for i, r := range rows {
		w := ansi.StringWidth(r)
		if w < colWidth {
			rows[i] = r + strings.Repeat(" ", colWidth-w)
		} else if w > colWidth {
			rows[i] = ansi.Truncate(r, colWidth, "")
		}
	}
	return rows
}

// MDWrapTableCell word-wraps s to colWidth columns.
//
// The wrap is the shared one (internal/textwrap): the rows break on
// word boundaries and the budget is counted in terminal cells, not in
// bytes. A token that is wider than the column is broken at the
// column's boundary — a shrunk column whose data is one long URL would
// overflow the column otherwise (cycle 218's finding for the GFM pipe
// tables).
func MDWrapTableCell(s string, colWidth int) []string {
	return textwrap.Break(s, colWidth)
}

func mdComputeColWidths(headerText []string, dataText [][]string, ncols int) []int {
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
	return widths
}

func mdFitColWidths(widths []int, budget int) {
	total := 1
	for _, w := range widths {
		total += 2 + w + 3
	}
	if total <= budget {
		return
	}
	excess := total - budget
	idx := make([]int, len(widths))
	for c := range idx {
		idx[c] = c
	}
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

func mdParseDataRows(start, end int, lines []string, ncols int, fns *MDTableFootnotes) [][]string {
	var dataText [][]string
	for i := start + 2; i < end; i++ {
		row := MDSplitRow(lines[i])
		rendered := make([]string, ncols)
		for c := 0; c < ncols && c < len(row); c++ {
			rendered[c], _ = MDCellSplit(row[c], fns)
		}
		dataText = append(dataText, rendered)
	}
	return dataText
}

func mdAssembleDataLines(dataText [][]string, widths []int, ncols int) []DetailLine {
	var out []DetailLine
	for _, row := range dataText {
		maxRows := 1
		cellRows := make([][]string, ncols)
		for c, cell := range row {
			cellRows[c] = mdCellRender(cell, widths[c])
			if len(cellRows[c]) > maxRows {
				maxRows = len(cellRows[c])
			}
		}
		for visual := 0; visual < maxRows; visual++ {
			rowCells := make([][]string, ncols)
			for c := range cellRows {
				cs := cellRows[c]
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

// MDRenderTable renders the table block as document lines.
func MDRenderTable(start, end int, lines []string, fns *MDTableFootnotes) []DetailLine {
	headerRaw := MDSplitRow(lines[start])
	ncols := len(headerRaw)
	if ncols == 0 {
		return nil
	}
	headerText := make([]string, ncols)
	for c, raw := range headerRaw {
		headerText[c], _ = MDCellSplit(raw, fns)
	}
	dataText := mdParseDataRows(start, end, lines, ncols, fns)
	widths := mdComputeColWidths(headerText, dataText, ncols)
	mdFitColWidths(widths, fns.Budget-2)

	headerCells := make([][]string, ncols)
	for c, h := range headerText {
		headerCells[c] = mdCellRender(h, widths[c])
	}
	out := []DetailLine{mdAssembleRow(headerCells, widths)}
	return append(out, mdAssembleDataLines(dataText, widths, ncols)...)
}

func mdAssembleRow(cells [][]string, widths []int) DetailLine {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(MDText)
	b.WriteString(MDTableBorder)
	b.WriteString("|")
	for c := range widths {
		b.WriteString(" ")
		b.WriteString(cells[c][0])
		b.WriteString(" ")
		b.WriteString(MDTableBorder)
		b.WriteString("|")
	}
	b.WriteString(MDTextClose)
	return DetailLine{Rows: []string{b.String()}}
}

// MDFootnotesBlock renders the [N]: url footnote list.
func MDFootnotesBlock(fns *MDTableFootnotes) []DetailLine {
	if fns == nil || len(fns.Order) == 0 {
		return nil
	}
	var out []DetailLine
	for _, f := range fns.Order {
		line := "  " + PanelGray238 + fmt.Sprintf("[%d]: %s", f.N, f.URL) + PanelGray238C
		out = append(out, DetailLine{Rows: []string{line}})
	}
	return out
}

// MDSplitRow splits one pipe-table row on |.
func MDSplitRow(line string) []string {
	s := strings.TrimSpace(line)
	if s == "" || strings.Count(s, "|") < 2 {
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

// MDTableSeparatorLine reports whether line is the GFM separator row.
func MDTableSeparatorLine(line string) bool {
	cells := MDSplitRow(line)
	if len(cells) < 2 {
		return false
	}
	for _, c := range cells {
		cc := strings.TrimSpace(c)
		if cc == "" {
			return false
		}
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

// MDIsTableLine reports whether line looks like a GFM table row.
func MDIsTableLine(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" {
		return false
	}
	return strings.Count(s, "|") >= 2
}
