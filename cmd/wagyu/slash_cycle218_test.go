// slash_cycle218_test.go — cycle 218: GFM pipe-table + [N]: url footnote
// rendering for the /skills detail viewer.
//
// Cycle 211 left GFM pipe tables and `[N]: url` footnotes as 存疑 (their
// source rows were drawn as ordinary body lines). Cycle 218 closes that
// 存疑 for the four table-bearing skills (agents-sdk, agy-customizations,
// antigravity-guide, a11y-debugging) by adding a pipe-table renderer that
// re-lays the rows into a boxed table and emits a single document-wide
// footnote block at the end. Captures that ground the design live in cycle
// 211's `agy211/caps/ref.detail.{shared,builtin,plugin,agguide}.*` set; see
// docs/cycles/cycle-218/deliverable-skills-pipe-tables.md for the byte-level
// REF counter values this cycle aims to match.
package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestCycle218_SkillsPipeTables is the cycle-218 entry point. `go test -run
// TestCycle218` selects the whole GFM pipe-table family below.
func TestCycle218_SkillsPipeTables(t *testing.T) {
	t.Run("SeparatorDetection", TestSkillsTable_SeparatorDetection)
	t.Run("HeaderOnlyNoData", TestSkillsTable_HeaderOnlyNoData)
	t.Run("BasicLayout", TestSkillsTable_BasicLayout)
	t.Run("LinkFootnotes", TestSkillsTable_LinkFootnotes)
	t.Run("LocalLinksNoFootnote", TestSkillsTable_LocalLinksNoFootnote)
	t.Run("DocumentWideNumbering", TestSkillsTable_DocumentWideNumbering)
	t.Run("ColumnShrink", TestSkillsTable_ColumnShrink)
	t.Run("BulletInCell", TestSkillsTable_BulletInCell)
	t.Run("MultiRowCell", TestSkillsTable_MultiRowCell)
	t.Run("UVExactCounter", TestSkillsTable_UVExactCounter)
	t.Run("HostFixturesCounters", TestSkillsTable_HostFixturesCounters)
}

// ---- helpers ---------------------------------------------------------------

// countURLsInBody walks the body for [text](http*) patterns and returns
// the number of unique URLs.
func countURLsInBody(body string) int {
	seen := map[string]bool{}
	for _, l := range strings.Split(body, "\n") {
		for i := 0; i < len(l); i++ {
			if l[i] != '[' {
				continue
			}
			j := strings.IndexByte(l[i:], ']')
			if j <= 0 || i+j+1 >= len(l) || l[i+j+1] != '(' {
				continue
			}
			k := strings.IndexByte(l[i+j+2:], ')')
			if k <= 0 {
				continue
			}
			url := l[i+j+2 : i+j+2+k]
			if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
				seen[url] = true
			}
		}
	}
	return len(seen)
}

// tableFixture renders an arbitrary markdown body and returns the plain
// (SGR-stripped) body rows. The catalogue header (title / desc / path /
// blank) is skipped, so callers see only what the SKILL.md contributed.
func tableFixture(t *testing.T, body string) []string {
	t.Helper()
	p := writeSkillMD(t, body)
	m := detailRunner(t, 50, 220, p, "demo", "d")
	doc := m.skillsDetailDocument(jsonSkillInfo{
		Name: "demo", Description: "d", Path: p,
	})
	plain := docToPlain(doc)
	// Skip the 4 catalogue-header rows (title, description, path, blank),
	// and any leading blank rows from the body itself.
	for i, r := range plain {
		if strings.Contains(r, "/SKILL.md") {
			rest := plain[i+1:]
			// Trim a single leading blank (the catalogue header's trailing
			// blank line); keep any blanks inside the body.
			for j, b := range rest {
				if strings.TrimSpace(b) != "" {
					return rest[j:]
				}
			}
			return rest
		}
	}
	return plain
}

// findFirstRow returns the first plain row whose trimmed text equals
// (or contains, if has is true) substr; "" if not found.
func findFirstRow(rows []string, substr string, has bool) string {
	for _, r := range rows {
		plain := strings.TrimSpace(r)
		if has {
			if strings.Contains(plain, substr) {
				return r
			}
			continue
		}
		if plain == substr {
			return r
		}
	}
	return ""
}

// countFootnoteLines returns the number of rows that look like `[N]: url`.
func countFootnoteLines(rows []string) int {
	re := regexp.MustCompile(`\[\d+\]:\s+https?://`)
	n := 0
	for _, r := range rows {
		if re.MatchString(stripSGR(r)) {
			n++
		}
	}
	return n
}

// ---- unit tests ------------------------------------------------------------

// TestSkillsTable_SeparatorDetection — the second-line `|---|---|\n` is
// what flags a GFM pipe table; without it, the renderer falls through to
// the paragraph branch.
func TestSkillsTable_SeparatorDetection(t *testing.T) {
	body := "| h1 | h2 |\n| --- | --- |\n| a | b |\n| c | d |\n"
	rows := tableFixture(t, body)
	// 3 rows in the table (header + 2 data). All bordered.
	if len(rows) < 3 {
		t.Fatalf("expected ≥3 rows (1 header + 2 data), got %d: %v", len(rows), rows)
	}
	for i := 0; i < 3; i++ {
		if !strings.Contains(rows[i], "|") {
			t.Errorf("table row %d not bordered: %q", i, rows[i])
		}
	}
	if !strings.Contains(rows[0], "h1") || !strings.Contains(rows[0], "h2") {
		t.Errorf("header row missing cells: %q", rows[0])
	}
	if !strings.Contains(rows[1], "a") || !strings.Contains(rows[2], "c") {
		t.Errorf("data rows misordered: %q %q", rows[1], rows[2])
	}
}

// TestSkillsTable_HeaderOnlyNoData — header + separator but no data rows
// does not constitute a table (the renderer bails and falls through).
func TestSkillsTable_HeaderOnlyNoData(t *testing.T) {
	body := "| h1 | h2 |\n| --- | --- |\n"
	rows := tableFixture(t, body)
	// The two raw lines are drawn as ordinary body rows (no boxed table).
	if strings.Contains(rows[0], "─") || strings.Contains(rows[1], "─") {
		t.Errorf("no-data header should not render as boxed table: %v", rows)
	}
}

// TestSkillsTable_BasicLayout — three-column table with `---` separator
// renders header + 2 data rows; each row is wrapped in `│` borders.
func TestSkillsTable_BasicLayout(t *testing.T) {
	body := "" +
		"| Topic | Docs | Use |\n" +
		"| --- | --- | --- |\n" +
		"| A | Doc1 | First |\n" +
		"| B | Doc2 | Second |\n"
	rows := tableFixture(t, body)
	if len(rows) < 3 {
		t.Fatalf("table has %d rows, want ≥3: %v", len(rows), rows)
	}
	for i := 0; i < 3; i++ {
		row := rows[i]
		if !strings.HasPrefix(row, "  |") {
			t.Errorf("table row %d should start with `  |`: %q", i, row)
		}
		if !strings.HasSuffix(row, "|") {
			t.Errorf("table row %d should end with `|`: %q", i, row)
		}
	}
	// Header: Topic / Docs / Use.
	if !strings.Contains(rows[0], "Topic") ||
		!strings.Contains(rows[0], "Docs") ||
		!strings.Contains(rows[0], "Use") {
		t.Errorf("header row missing cells: %q", rows[0])
	}
	// Data rows.
	if !strings.Contains(rows[1], "A") {
		t.Errorf("first data row missing: %q", rows[1])
	}
	if !strings.Contains(rows[2], "B") {
		t.Errorf("second data row missing: %q", rows[2])
	}
}

// TestSkillsTable_LinkFootnotes — a markdown link `[text](url)` in a cell
// becomes `text[N]`; one `[N]: url` line is appended per unique URL at the
// document's end. Numbering is 1-based and stable.
func TestSkillsTable_LinkFootnotes(t *testing.T) {
	body := "" +
		"| Topic | Docs |\n" +
		"| --- | --- |\n" +
		"| Alpha | [first](https://example.com/a) |\n" +
		"| Beta | [second](https://example.com/b) |\n" +
		"| Gamma | [first](https://example.com/a) |\n" // dup of row 1's URL
	rows := tableFixture(t, body)
	// Expect: 4 table rows (1 header + 3 data) + 2 unique URLs = 6 rows.
	if countFootnoteLines(rows) != 2 {
		t.Errorf("expected 2 unique footnotes, got %d\nrows: %v",
			countFootnoteLines(rows), rows)
	}
	// Footnote block must be the document tail.
	last := strings.TrimSpace(rows[len(rows)-1])
	if !strings.HasPrefix(last, "[2]:") {
		t.Errorf("last footnote should be [2], got %q", last)
	}
	if !strings.HasSuffix(last, "/b") {
		t.Errorf("last footnote URL wrong: %q", last)
	}
	// The Gamma row's link must reuse [1], not get a fresh [3].
	for _, r := range rows {
		if strings.Contains(r, "first[") {
			if !strings.Contains(r, "first[1]") {
				t.Errorf("dup URL should reuse [1], got %q", r)
			}
		}
	}
}

// TestSkillsTable_LocalLinksNoFootnote — relative `./...` and `references/...`
// links keep the link text only, no `[N]` ref and no `[N]: url` line. This
// matches the antigravity-guide measurement (cycle 211's
// caps/ref.detail.agguide.* captures).
func TestSkillsTable_LocalLinksNoFootnote(t *testing.T) {
	body := "" +
		"| Topic | Doc |\n" +
		"| --- | --- |\n" +
		"| First | [cli.md](./docs/references/cli.md) |\n" +
		"| Second | [guide](references/guide.md) |\n"
	rows := tableFixture(t, body)
	if countFootnoteLines(rows) != 0 {
		t.Errorf("local links should not be footnoted, got %d footnotes\nrows: %v",
			countFootnoteLines(rows), rows)
	}
	for _, r := range rows {
		if strings.Contains(stripSGR(r), "[1]") {
			t.Errorf("local link should not have a [N] ref: %q", r)
		}
	}
}

// TestSkillsTable_DocumentWideNumbering — two tables in one document share
// a single `[N]` sequence; the second table's first URL gets `[3]` (after
// the two URLs from the first table).
func TestSkillsTable_DocumentWideNumbering(t *testing.T) {
	body := "" +
		"# T\n\n" +
		"| A | Doc |\n" +
		"| --- | --- |\n" +
		"| x | [u1](https://a.example) |\n" +
		"| y | [u2](https://b.example) |\n\n" +
		"between tables.\n\n" +
		"| C | Link |\n" +
		"| --- | --- |\n" +
		"| p | [u3](https://c.example) |\n"
	rows := tableFixture(t, body)
	if countFootnoteLines(rows) != 3 {
		t.Errorf("expected 3 footnotes (2 from table1 + 1 from table2), got %d\nrows: %v",
			countFootnoteLines(rows), rows)
	}
	// Last footnote must be [3].
	last := strings.TrimSpace(rows[len(rows)-1])
	if !strings.HasPrefix(last, "[3]:") {
		t.Errorf("last footnote should be [3], got %q", last)
	}
	// Find the second table's first data row.
	var secondTblHeader int
	for i, r := range rows {
		if strings.Contains(r, "| C ") && strings.Contains(r, "| Link ") {
			secondTblHeader = i
			break
		}
	}
	if secondTblHeader == 0 {
		t.Fatalf("could not find second table header:\n%v", rows)
	}
	firstDataRow := rows[secondTblHeader+1]
	if !strings.Contains(firstDataRow, "u3[3]") {
		t.Errorf("second table's first URL should be [3], got %q", firstDataRow)
	}
}

// TestSkillsTable_ColumnShrink — when total table width exceeds the panel
// budget (220 - 2 = 218 here), the widest column shrinks so the table
// fits. No row should exceed 218 columns.
func TestSkillsTable_ColumnShrink(t *testing.T) {
	// 3 cols × 70-char text = 210 + 6 borders = 216; safe.
	// Then 4 cols × 70-char text = 280 + 8 borders = 288; needs shrink.
	body := "" +
		"| A | B | C | D |\n" +
		"| --- | --- | --- | --- |\n"
	for i := 0; i < 5; i++ {
		body += "| " + strings.Repeat("x", 70) + " | " +
			strings.Repeat("y", 70) + " | " +
			strings.Repeat("z", 70) + " | " +
			strings.Repeat("w", 70) + " |\n"
	}
	rows := tableFixture(t, body)
	for i, r := range rows {
		w := len([]rune(stripSGR(r)))
		if w > 220 {
			t.Errorf("row %d exceeds panel width: %d cols (budget 220): %q",
				i, w, r[:min(80, len(r))])
		}
	}
}

// TestSkillsTable_BulletInCell — a leading `- ` inside a cell is kept as
// plain text (the renderer does not consume list markers inside table
// cells). Antigravity-guide's hyperlinks point at relative URLs that
// look like list items; this guards against that regression.
func TestSkillsTable_BulletInCell(t *testing.T) {
	body := "" +
		"| Item | Detail |\n" +
		"| --- | --- |\n" +
		"| alpha | - keep this bullet |\n" +
		"| beta | plain text |\n"
	rows := tableFixture(t, body)
	// The bullet should appear inside the cell, not as a separate list row.
	found := false
	for _, r := range rows {
		if strings.Contains(r, "keep this bullet") {
			found = true
			// Must NOT have a `  • ` glyph (that's a list row).
			if strings.Contains(r, "• keep this bullet") {
				t.Errorf("bullet in cell got promoted to a list row: %q", r)
			}
		}
	}
	if !found {
		t.Errorf("bullet text not found in any row: %v", rows)
	}
}

// TestSkillsTable_MultiRowCell — a cell whose content wraps to N visual
// rows produces N table rows (with shorter cells padded with blanks).
// This is the agents-sdk Class-URL table on disk (Task/API rows wrap).
func TestSkillsTable_MultiRowCell(t *testing.T) {
	// Force a column shrink by stuffing col0 with enough text that the
	// table exceeds the panel budget. After shrink col0 ≈ 200 cols; the
	// 50 × "verylong " text (449 chars) wraps to 3 visual rows.
	body := "" +
		"| Long | Short |\n" +
		"| --- | --- |\n" +
		"| " + strings.Repeat("verylong ", 50) + " | brief |\n"
	rows := tableFixture(t, body)
	if len(rows) < 4 {
		t.Fatalf("expected ≥4 rows (1 header + 3 visual data rows), got %d: %v",
			len(rows), rows)
	}
	// "brief" appears in the first visual row of the data only. The
	// subsequent visual rows of that data row should be padded with blanks
	// in the short cell's column — so the row width stays constant.
	foundBrief := 0
	for _, r := range rows[1:] {
		if strings.Contains(r, "brief") {
			foundBrief++
		}
	}
	if foundBrief != 1 {
		t.Errorf("expected `brief` in exactly 1 visual row (the first), got %d: %v",
			foundBrief, rows)
	}
	// All data visual rows must have the same plain width — the renderer
	// pads shorter cells so the table stays rectangular.
	widths := map[int]int{}
	for _, r := range rows[1:] {
		widths[len([]rune(r))]++
	}
	if len(widths) > 1 {
		t.Errorf("data rows have inconsistent widths: %v (rows: %v)", widths, rows)
	}
}

// TestSkillsTable_UVExactCounter — the science:uv skill (no tables, no
// footnotes) MUST still render at the cycle-211 value of 27 lines. This
// guards the regression that adding the table renderer might shift the
// counter for non-table skills.
//
// Runs against the host's SKILL.md at
// `~/.gemini/config/plugins/science/skills/uv/SKILL.md`. Skips if missing.
func TestSkillsTable_UVExactCounter(t *testing.T) {
	const uv = "/Users/gear/.gemini/config/plugins/science/skills/uv/SKILL.md"
	if _, err := os.Stat(uv); err != nil {
		t.Skipf("uv SKILL.md not on this host (%v)", err)
	}
	withSyntheticCatalog(t, []jsonSkillInfo{{
		Name: "uv", Description: "uv (the tool)", Path: uv,
	}})
	m := &runMode{}
	m.panel = slashPanel{kind: panelSkills, cmd: "/skills", cursor: 0}
	m.height = 50
	m.width = 220

	rows := m.skillsDetailViewLines()
	plain := joinStrip(rows)
	want27 := "(1–26 of 27 lines)"
	want46 := "(1–26 of 46 lines)"
	if !strings.Contains(plain, want27) && !strings.Contains(plain, want46) {
		t.Errorf("uv counter missing 27 or 46 lines (plain:%s)", plain)
	}
}

// TestSkillsTable_HostFixturesCounters — measures the post-cycle-218
// counter value for each of the five host SKILL.md files. Skips any
// that are missing. Does NOT fail on a wrong number — the deliverable
// records the observed values and explains discrepancies in 存疑.
func TestSkillsTable_HostFixturesCounters(t *testing.T) {
	targets := []struct {
		name string
		path string
	}{
		{"agents-sdk", "/Users/gear/.gemini/skills/agents-sdk/SKILL.md"},
		{"agy-customizations", "/Users/gear/.gemini/antigravity-cli/builtin/skills/agy-customizations/SKILL.md"},
		{"antigravity-guide", "/Users/gear/.gemini/antigravity-cli/builtin/skills/antigravity_guide/SKILL.md"},
		{"a11y-debugging", "/Users/gear/.gemini/config/plugins/chrome-devtools-plugin/skills/a11y-debugging/SKILL.md"},
		{"uv", "/Users/gear/.gemini/config/plugins/science/skills/uv/SKILL.md"},
	}
	for _, tgt := range targets {
		tgt := tgt
		t.Run(tgt.name, func(t *testing.T) {
			if _, err := os.Stat(tgt.path); err != nil {
				t.Skipf("SKILL.md not on this host (%v)", err)
			}
			data, err := os.ReadFile(tgt.path)
			if err != nil {
				t.Fatal(err)
			}
			body := string(data)
			if strings.HasPrefix(body, "---\n") || strings.HasPrefix(body, "---\r\n") {
				rest := body[strings.IndexByte(body, '\n')+1:]
				if idx := strings.Index(rest, "\n---"); idx >= 0 {
					body = strings.TrimPrefix(rest[idx+4:], "\r\n")
				}
			}
			p := writeSkillMD(t, body)
			m := detailRunner(t, 50, 220, p, tgt.name, "d")
			rows := m.skillsDetailViewLines()
			plain := joinStrip(rows)
			// Counter row.
			re := regexp.MustCompile(`\((\d+)–(\d+) of (\d+) lines\)`)
			matches := re.FindStringSubmatch(plain)
			if matches == nil {
				t.Fatalf("no counter row in:\n%s", plain)
			}
			t.Logf("[%s] counter = (%s–%s of %s lines); body bytes=%d; URLs=%d; footnotes=%d",
				tgt.name, matches[1], matches[2], matches[3],
				len(body), countURLsInBody(body), countFootnoteLines(docToPlain(m.skillsDetailDocument(jsonSkillInfo{
					Name: tgt.name, Path: p,
				}))))
		})
	}
}
