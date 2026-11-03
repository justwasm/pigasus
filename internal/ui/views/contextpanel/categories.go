// categories.go — the /context breakdown's categories.
//
// The row set, the row order, the markers and the visibility rule come
// from the reference's own documentation
// (official-docs/cli/commands/context.md:32-42):
//
//   - the **User messages**, **Agent responses** and **Tool calls** rows
//     are always shown; the remaining rows appear only when that
//     category is consuming tokens;
//   - `◉` marks a conversation row, `⛁` the system prompt, the system
//     tools, the system overhead and the customization rows, `□` the
//     free space and `⊠` the checkpoint buffer.
//
// The row labels are the reference's own strings: they are in the 1.4.11
// binary's string table — the full dump is retired, and the surviving
// pruned index of that table, out/local-macos-1.4.11/strings-index.txt,
// carries the format string the rows are rendered from — `%s %s: %s
// tokens (%.1f%%)`, whose two leading verbs are the row's marker and its
// label — but not the labels themselves.
//
// The dump that table was taken from is ASCII-filtered, so it carries no
// glyph: the markers below are the documentation's, and the colours are
// the clone's own.

package contextpanel

// Category is one row of the /context token breakdown.
//
// A category that consumes no tokens is not a row: the reference shows
// a category's row only while that category is consuming tokens. The
// three conversation rows are the exception — they are always shown.
type Category struct {
	// Label is the row's name, e.g. "User messages".
	Label string

	// Marker is the glyph the row carries.
	Marker string

	// Cell is the glyph the category paints in the usage grid.
	Cell string

	// Tokens is the category's token count.
	Tokens int

	// Always keeps the row on screen even when the count is zero.
	Always bool

	// Lead puts the category's cells at the head of the grid's fill
	// sequence, before every other category.
	//
	// The reference's own capture puts the system prompt's cells first
	// (cycle 224); the categories that follow keep their row order.
	Lead bool
}

// Categories returns the breakdown's visible categories, in the
// reference's row order.
//
// The order is the documentation's: the three conversation rows, then
// the system prompt, the system tools and the system overhead, then the
// customization rows (rules, skills, subagents, workflows and plugins),
// then the MCP tools, then the system messages.
func (b Breakdown) Categories() []Category {
	cats := b.allCategories()
	out := make([]Category, 0, len(cats))
	for _, c := range cats {
		if c.Always || c.Tokens > 0 {
			out = append(out, c)
		}
	}
	return out
}

// allCategories returns every category the breakdown knows, in the
// reference's row order — including the ones that consume no tokens and
// are therefore not rows.
func (b Breakdown) allCategories() []Category {
	return []Category{
		{Label: "User messages", Marker: UserRowMarker, Cell: UserGlyph, Tokens: b.User, Always: true},
		{Label: "Agent responses", Marker: AgentGlyph, Cell: AgentGlyph, Tokens: b.Agent, Always: true},
		{Label: "Tool calls", Marker: ToolRowMarker, Cell: ToolGlyph, Tokens: b.Tool, Always: true},
		{Label: "System prompt", Marker: SystemGlyph, Cell: SystemCellGlyph, Tokens: b.SystemPrompt, Lead: true},
		{Label: "System tools", Marker: SystemGlyph, Cell: SystemCellGlyph, Tokens: b.SystemTools},
		{Label: "System overhead", Marker: SystemGlyph, Cell: SystemCellGlyph, Tokens: b.SystemOverhead},
		{Label: "Rules", Marker: SystemGlyph, Cell: SystemCellGlyph, Tokens: b.Rules},
		{Label: "Skills", Marker: SystemGlyph, Cell: SystemCellGlyph, Tokens: b.Skills},
		{Label: "Subagents", Marker: SystemGlyph, Cell: SystemCellGlyph, Tokens: b.Subagents},
		{Label: "Workflows", Marker: SystemGlyph, Cell: SystemCellGlyph, Tokens: b.Workflows},
		{Label: "Plugins", Marker: SystemGlyph, Cell: SystemCellGlyph, Tokens: b.Plugins},
		{Label: "MCP tools", Marker: SystemGlyph, Cell: SystemCellGlyph, Tokens: b.MCPTools},
		{Label: "System messages", Marker: SystemCellGlyph, Cell: SystemCellGlyph, Tokens: b.SystemMessages},
	}
}
