package modelpicker

import (
	"testing"
)

func TestSearchMatch(t *testing.T) {
	cases := []struct {
		label     string
		query     string
		wantOK    bool
		wantFirst int
	}{
		{"Claude Sonnet 4.6 (Thinking)", "clauson", true, 0},
		{"Claude Sonnet 4.6 (Thinking)", "cl-opus", false, 0},
		{"Gemini 3.8 Flash", "gemini", true, 0},
		{"Gemini 3.8 Flash", "flash", true, 11},
		{"GPT-OSS 120B (Medium)", "gpt", true, 0},
		{"Gemini 3.8 Flash", "zzz", false, 0},
	}
	for _, c := range cases {
		_, first, ok := SearchMatch(c.label, c.query)
		if ok != c.wantOK {
			t.Errorf("SearchMatch(%q, %q) ok = %v, want %v", c.label, c.query, ok, c.wantOK)
			continue
		}
		if ok && first != c.wantFirst {
			t.Errorf("SearchMatch(%q, %q) first = %d, want %d", c.label, c.query, first, c.wantFirst)
		}
	}
}

func TestSearchMatchIsCaseInsensitive(t *testing.T) {
	if _, _, ok := SearchMatch("Claude Sonnet 4.6", "CLAUDE"); !ok {
		t.Error("SearchMatch is case-sensitive, want case-insensitive")
	}
}

func TestSearchMatchEmptyQuery(t *testing.T) {
	boundary, first, ok := SearchMatch("anything", "")
	if !ok {
		t.Error("SearchMatch with an empty query returned ok=false, want true")
	}
	if boundary != 0 || first != 0 {
		t.Errorf("SearchMatch with an empty query = (%d, %d), want (0, 0)", boundary, first)
	}
}

func TestRowsFilteredRelevanceOrder(t *testing.T) {
	// The query "s" puts "Claude Sonnet 4.6 (Thinking)" first: its "s"
	// starts a word.
	rows := RowsFiltered("s")
	if len(rows) == 0 {
		t.Fatal("RowsFiltered(\"s\") returned no rows")
	}
	if rows[0].Display != "Claude Sonnet 4.6 (Thinking)" {
		t.Errorf("the first row = %q, want %q", rows[0].Display, "Claude Sonnet 4.6 (Thinking)")
	}
}

func TestRowsFilteredDimSortsLast(t *testing.T) {
	// The query "4" puts "Gemini 4 Argon" last although its label is the
	// shortest (it is the dim, unselectable row).
	rows := RowsFiltered("4")
	if len(rows) == 0 {
		t.Fatal("RowsFiltered(\"4\") returned no rows")
	}
	last := rows[len(rows)-1]
	if last.Display != "Gemini 4 Argon" {
		t.Errorf("the last row = %q, want %q", last.Display, "Gemini 4 Argon")
	}
	if !last.Dim {
		t.Error("the last row is not dim, want the unselectable Argon row")
	}
}

func TestRowsFilteredEmptyQueryIsUnchanged(t *testing.T) {
	rows := RowsFiltered("")
	if len(rows) != len(RowsView()) {
		t.Fatalf("RowsFiltered(\"\") returned %d rows, want %d", len(rows), len(RowsView()))
	}
}

func TestRowsFilteredNoMatch(t *testing.T) {
	if rows := RowsFiltered("zzzzz"); len(rows) != 0 {
		t.Errorf("RowsFiltered returned %d rows, want none", len(rows))
	}
}

func TestCatalogueID(t *testing.T) {
	gemini := Row{Display: "Gemini 3.8 Flash", Slug: "gemini-3.8-flash"}
	cases := []struct {
		cursor int
		want   string
	}{
		{0, "gemini-3.8-flash-low"},
		{1, "gemini-3.8-flash-medium"},
		{2, "gemini-3.8-flash-high"},
	}
	for _, c := range cases {
		if got := gemini.CatalogueID(c.cursor); got != c.want {
			t.Errorf("CatalogueID(%d) = %q, want %q", c.cursor, got, c.want)
		}
	}
	// The other rows are fixed IDs.
	claude := Row{Display: "Claude Sonnet 4.6 (Thinking)", Slug: "claude-sonnet-4-6"}
	if got := claude.CatalogueID(2); got != "claude-sonnet-4-6" {
		t.Errorf("CatalogueID(2) = %q, want the fixed ID", got)
	}
}

func TestEffortSlugForCursor(t *testing.T) {
	cases := []struct {
		base   string
		cursor int
		want   string
	}{
		{"gemini-3.8-flash", 0, "low"},
		{"gemini-3.8-flash", 1, "medium"},
		{"gemini-3.8-flash", 2, "high"},
		{"claude-sonnet-4-6", 0, "medium"},
		{"claude-sonnet-4-6", 2, "medium"},
	}
	for _, c := range cases {
		if got := EffortSlugForCursor(c.base, c.cursor); got != c.want {
			t.Errorf("EffortSlugForCursor(%q, %d) = %q, want %q", c.base, c.cursor, got, c.want)
		}
	}
}

func TestResolveCatalogueID(t *testing.T) {
	if got := ResolveCatalogueID("gemini-3.8-flash-high"); got != "gemini-3.8-flash-high" {
		t.Errorf("ResolveCatalogueID = %q, want the catalogue id", got)
	}
	if got := ResolveCatalogueID("  gemini-3.8-flash-high  "); got != "gemini-3.8-flash-high" {
		t.Errorf("ResolveCatalogueID = %q, want it trimmed", got)
	}
	if got := ResolveCatalogueID("no such model"); got != "" {
		t.Errorf("ResolveCatalogueID = %q, want it empty", got)
	}
}

func TestBaseSlug(t *testing.T) {
	if got := BaseSlug("Gemini 3.8 Flash (High)"); got != "gemini-3.8-flash" {
		t.Errorf("BaseSlug = %q, want %q", got, "gemini-3.8-flash")
	}
	if got := BaseSlug("Claude Sonnet 4.6 (Thinking)"); got != "claude-sonnet-4-6" {
		t.Errorf("BaseSlug = %q, want %q", got, "claude-sonnet-4-6")
	}
	if got := BaseSlug("unknown"); got != "unknown" {
		t.Errorf("BaseSlug = %q, want it unchanged", got)
	}
}

func TestRowPad(t *testing.T) {
	if got := RowPad("Gemini 3.8 Flash"); got != 30-2-len("Gemini 3.8 Flash") {
		t.Errorf("RowPad = %d, want the column-30 pad", got)
	}
	// A label that already fills the column is not padded at all.
	if got := RowPad("Claude Opus 4.6 (Thinking) and more"); got != 0 {
		t.Errorf("RowPad = %d, want 0 for an over-long label", got)
	}
	// Rune-based, so a multi-byte label pads correctly.
	if got := RowPad("モデル"); got != 30-2-3 {
		t.Errorf("RowPad = %d, want the rune count", got)
	}
}

func TestStripLeadingSpaces(t *testing.T) {
	if got := StripLeadingSpaces("    abc", 4); got != "abc" {
		t.Errorf("StripLeadingSpaces = %q, want %q", got, "abc")
	}
	if got := StripLeadingSpaces("  abc", 4); got != "abc" {
		t.Errorf("StripLeadingSpaces = %q, want %q", got, "abc")
	}
	if got := StripLeadingSpaces("abc", 4); got != "abc" {
		t.Errorf("StripLeadingSpaces = %q, want %q", got, "abc")
	}
}
