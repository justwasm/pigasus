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

	// Estimated marks a count the host could not measure and filled from a
	// built-in figure. The row shows it with a leading "~" so a figure that
	// never changes cannot be mistaken for a measurement.
	Estimated bool
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

// bucketDesc is one bucket's fixed facts — everything about a row that is not
// a number.
//
// The count and the "is this an estimate" flag are not here: they are the
// breakdown's own state, so the descriptor answers "what is this row" and the
// Breakdown answers "how much". Splitting them that way is what lets the
// descriptor be a static table instead of a method that rebuilds thirteen
// literals on every call.
type bucketDesc struct {
	// label is the row's name, e.g. "User messages".
	label string
	// marker is the glyph the row carries.
	marker string
	// cell is the glyph the category paints in the usage grid.
	cell string
	// always keeps the row on screen even at zero tokens.
	always bool
	// lead puts the category's cells at the head of the grid's fill.
	lead bool
	// pick returns the breakdown's value for this bucket. Used instead of a
	// field name so the table can stay a package-level value.
	pick func(Breakdown) (tokens int, estimated bool)
}

// bucketDescs is every bucket, in the reference's row order.
//
// The order is the documentation's: the three conversation rows, then the
// system prompt, the system tools and the system overhead, then the
// customization rows (rules, skills, subagents, workflows and plugins), then
// the MCP tools, then the system messages. This slice IS the order —
// Categories() does not sort.
//
// Adding a bucket is one row here and one field on Breakdown. Before it was
// one row here, one field on Breakdown, a line in panel.go's mirror of the
// same fields, and a hand-written literal in aCategories() that repeated the
// two glyph names for every system row.
var bucketDescs = []bucketDesc{
	{label: "User messages", marker: UserRowMarker, cell: UserGlyph, always: true,
		pick: func(b Breakdown) (int, bool) { return b.User, false }},
	{label: "Agent responses", marker: AgentGlyph, cell: AgentGlyph, always: true,
		pick: func(b Breakdown) (int, bool) { return b.Agent, false }},
	{label: "Tool calls", marker: ToolRowMarker, cell: ToolGlyph, always: true,
		pick: func(b Breakdown) (int, bool) { return b.Tool, false }},

	{label: "System prompt", marker: SystemGlyph, cell: SystemCellGlyph, lead: true,
		pick: func(b Breakdown) (int, bool) { return b.SystemPrompt, b.SystemPromptEstimated }},
	{label: "System tools", marker: SystemGlyph, cell: SystemCellGlyph,
		pick: func(b Breakdown) (int, bool) { return b.SystemTools, b.SystemToolsEstimated }},
	{label: "System overhead", marker: SystemGlyph, cell: SystemCellGlyph,
		pick: func(b Breakdown) (int, bool) { return b.SystemOverhead, false }},

	{label: "Rules", marker: SystemGlyph, cell: SystemCellGlyph,
		pick: func(b Breakdown) (int, bool) { return b.Rules, b.RulesEstimated }},
	{label: "Skills", marker: SystemGlyph, cell: SystemCellGlyph,
		pick: func(b Breakdown) (int, bool) { return b.Skills, b.SkillsEstimated }},
	{label: "Subagents", marker: SystemGlyph, cell: SystemCellGlyph,
		pick: func(b Breakdown) (int, bool) { return b.Subagents, b.SubagentsEstimated }},
	{label: "Workflows", marker: SystemGlyph, cell: SystemCellGlyph,
		pick: func(b Breakdown) (int, bool) { return b.Workflows, false }},
	{label: "Plugins", marker: SystemGlyph, cell: SystemCellGlyph,
		pick: func(b Breakdown) (int, bool) { return b.Plugins, false }},
	{label: "MCP tools", marker: SystemGlyph, cell: SystemCellGlyph,
		pick: func(b Breakdown) (int, bool) { return b.MCPTools, false }},
	{label: "System messages", marker: SystemCellGlyph, cell: SystemCellGlyph,
		pick: func(b Breakdown) (int, bool) { return b.SystemMessages, b.SystemMessagesEstimated }},
}

// allCategories returns every category the breakdown knows, in the
// reference's row order — including the ones that consume no tokens and
// are therefore not rows.
func (b Breakdown) allCategories() []Category {
	out := make([]Category, len(bucketDescs))
	for i, d := range bucketDescs {
		tokens, estimated := d.pick(b)
		out[i] = Category{
			Label:     d.label,
			Marker:    d.marker,
			Cell:      d.cell,
			Tokens:    tokens,
			Always:    d.always,
			Lead:      d.lead,
			Estimated: estimated,
		}
	}
	return out
}
