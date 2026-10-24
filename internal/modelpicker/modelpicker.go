// Package modelpicker holds the pure rules behind the /model picker: the
// row model, the search's subsequence match and relevance order, and the
// catalogue-id mapping.
//
// The TUI front-end (cmd/wagyu/slash_panel_model.go) owns the cursor, the
// search box and the effort slider's interaction. Everything here is a
// *decision* — which rows match, in what order, what catalogue id a row
// stands for — so it can be tested without a running TUI.
package modelpicker

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/justwasm/pigasus/internal/cli"
)

// Row is one picker row the reference renders between the Search field
// and the effort slider. Display is the label-column text the reference
// renders: the Gemini row drops the "(High)" tier (the tier lives on the
// effort slider instead), the dim "Gemini 4 Argon" row is drawn in gray
// 241 (unavailable on this account). Slug is the catalogue ID prefix;
// the effort suffix is appended from the slider.
type Row struct {
	Display string // label column, verbatim from REF
	Slug    string // catalogue base ID (no effort suffix)
	Dim     bool   // row is drawn in dim gray 241
	Note    string // optional trailing description (custom registry models)
}

// Rows is the static, always-present portion of the /model picker.
// Order is the live 1.4.3 list captured at 120x40: Gemini 3.8 Flash,
// Claude Sonnet 4.6, Claude Opus 4.6, GPT-OSS 120B, Gemini 4 Argon.
// Cycle model-catalog-claude-55 adds Claude Opus 5.5 / Claude Sonnet 5.5
// as a conditional suffix via Rows55(); the picker iterates the
// concatenation (see RowsView).
//
// The models the user configured are NOT here: they are not the
// reference's, they are not static, and their labels belong to the user's
// own configuration. ConfiguredRows adds them.
var Rows = []Row{
	{"Gemini 3.8 Flash", "gemini-3.8-flash", false, ""},
	{"Claude Sonnet 4.6 (Thinking)", "claude-sonnet-4-6", false, ""},
	{"Claude Opus 4.6 (Thinking)", "claude-opus-4-6-thinking", false, ""},
	{"GPT-OSS 120B (Medium)", "gpt-oss-120b-medium", false, ""},
	{"Gemini 4 Argon", "", true, ""}, // slug unknown (not in `agy models`); cannot be selected
}

// Rows55 returns the conditional Claude Opus 5.5 / Claude Sonnet
// 5.5 picker rows. Empty unless cli.Claude55Available() is true —
// the AGY 1.3.6 changelog line 1 contract ("Claude models now appear
// in /model only if your Google Cloud project can use them").
//
// Returning a fresh slice on each call lets tests toggle the env
// vars between renders without restarting the binary, and avoids
// mutating the caller's backing array.
func Rows55() []Row {
	if !cli.Claude55Available() {
		return nil
	}
	return []Row{
		{"Claude Opus 5.5", "claude-opus-5-5", false, ""},
		{"Claude Sonnet 5.5", "claude-sonnet-5-5", false, ""},
	}
}

// ConfiguredRows returns the picker rows for the models the user
// configured.
//
// The reference's rows are static; these are not. A model is in the picker
// because the user's provider configuration lists it, and the label is
// whatever the configuration calls it. The rows are appended after the
// reference's own, so the cursor indices of the reference rows stay stable
// no matter what the user configured.
func ConfiguredRows() []Row {
	reference := make(map[string]bool, 8)
	for _, m := range cli.ReferenceModels() {
		reference[m.ID] = true
	}
	var out []Row
	for _, m := range cli.ModelCatalogue() {
		if reference[m.ID] {
			continue
		}
		out = append(out, Row{
			Display: m.Label,
			Slug:    m.ID,
			Dim:     false,
			Note:    "configured model",
		})
	}
	return out
}

// RowsView returns the full picker view: Rows + any conditional 5.5
// entries + the models the user configured. The picker always renders
// Rows first so cursor indices 0–4 are stable across ADC state changes;
// the 5.5 rows are appended at the end when present, and the configured
// rows follow them.
func RowsView() []Row {
	extra := Rows55()
	configured := ConfiguredRows()
	if len(extra) == 0 && len(configured) == 0 {
		return Rows
	}
	out := make([]Row, 0, len(Rows)+len(extra)+len(configured))
	out = append(out, Rows...)
	out = append(out, extra...)
	out = append(out, configured...)
	return out
}

// RowsFiltered returns the picker rows that match the search query,
// most relevant first. An empty query returns the rows unchanged.
//
// The reference matches the *display label* only: typing "cl-opus" — a
// subsequence of the catalogue id, not of the label — matches nothing
// (agy 1.4.5, live). The match is a case-insensitive *subsequence*, not
// a substring: "clauson" matches "Claude Sonnet 4.6 (Thinking)".
//
// The order is the reference's relevance order, read off live captures:
//
//   - a row the user cannot select (dim) sorts last;
//   - then the rows whose match lands on a word boundary ("Claude
//     Sonnet…" wins the query "c" over a mid-word match);
//   - then the shorter label;
//   - then the earlier first match.
//
// Each key was measured, not guessed: the query "s" puts "Claude Sonnet
// 4.6 (Thinking)" first (its "s" starts a word) and orders the rest by
// label length, and the query "4" puts "Gemini 4 Argon" last although its
// label is the shortest (it is the dim, unselectable row).
func RowsFiltered(query string) []Row {
	rows := RowsView()
	if query == "" {
		return rows
	}
	type hit struct {
		row      Row
		boundary int
		length   int
		first    int
	}
	var hits []hit
	for _, r := range rows {
		boundary, first, ok := SearchMatch(r.Display, query)
		if !ok {
			continue
		}
		hits = append(hits, hit{
			row:      r,
			boundary: boundary,
			length:   utf8.RuneCountInString(r.Display),
			first:    first,
		})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.row.Dim != b.row.Dim {
			return !a.row.Dim // a selectable row outranks a dim one
		}
		if a.boundary != b.boundary {
			return a.boundary > b.boundary
		}
		if a.length != b.length {
			return a.length < b.length
		}
		return a.first < b.first
	})
	out := make([]Row, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.row)
	}
	return out
}

// SearchMatch reports whether the label contains the query as a
// case-insensitive subsequence. It also returns the number of matched
// characters that land on a word boundary (the first character of the
// label, or one preceded by a space) and the position of the first match
// — the two relevance signals RowsFiltered sorts on.
func SearchMatch(label, query string) (boundary, first int, ok bool) {
	lr := []rune(strings.ToLower(label))
	qr := []rune(strings.ToLower(query))
	if len(qr) == 0 {
		return 0, 0, true
	}
	pos := 0
	first = -1
	for _, qc := range qr {
		found := -1
		for i := pos; i < len(lr); i++ {
			if lr[i] == qc {
				found = i
				break
			}
		}
		if found < 0 {
			return 0, 0, false
		}
		if first < 0 {
			first = found
		}
		if found == 0 || lr[found-1] == ' ' {
			boundary++
		}
		pos = found + 1
	}
	return boundary, first, true
}

// CatalogueID returns the full catalogue ID a picker row represents at
// the given effort cursor. Only the three-tier Gemini encodes the
// cursor; the other rows are fixed IDs.
func (r Row) CatalogueID(effortCursor int) string {
	if r.Slug == "gemini-3.8-flash" {
		return r.Slug + "-" + EffortSlugForCursor(r.Slug, effortCursor)
	}
	return r.Slug
}

// EffortSlugForCursor maps (base, cursor) to the effort slug that
// belongs in the catalogue ID. For Gemini (low/medium/high) the cursor
// directly indexes; for Claude/GPT-OSS the cursor is forced to "medium".
func EffortSlugForCursor(base string, cursor int) string {
	switch base {
	case "gemini-3.8-flash":
		switch cursor {
		case 0:
			return "low"
		case 2:
			return "high"
		default:
			return "medium"
		}
	default:
		return "medium"
	}
}

// RowPad is the number of cells a picker row is padded to (2 = the
// `  `/`> ` gutter). Rune-based, so a row with a multi-byte label pads
// correctly.
//
// A label that already reaches column 30 is not padded at all: the
// reference pads the *cursor* row to column 30 (agy 1.4.5, live:
// "> Claude Opus 4.6 (Thinking)  " — two spaces) and leaves a row that
// fills the column alone ("…(Thinking)" with nothing after it).
func RowPad(display string) int {
	pad := 30 - 2 - utf8.RuneCountInString(display)
	if pad < 0 {
		pad = 0
	}
	return pad
}
