package contextpanel

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/compaction"
)

// docCategoryOrder is the reference's row order, taken from its own
// documentation (official-docs/cli/commands/context.md:32-42).
//
// The order is the documentation's own: the three conversation rows,
// then the system prompt, the system tools and the system overhead,
// then the customization rows (rules, skills, subagents, workflows and
// plugins), then the MCP tools, then the system messages.
var docCategoryOrder = []string{
	"User messages",
	"Agent responses",
	"Tool calls",
	"System prompt",
	"System tools",
	"System overhead",
	"Rules",
	"Skills",
	"Subagents",
	"Workflows",
	"Plugins",
	"MCP tools",
	"System messages",
}

// TestCategoriesRowSetAndOrder pins the category set and the row order.
//
// The labels are the reference's own strings: they are in the 1.4.11
// binary's string table — the full dump is retired, and the surviving
// pruned index of that table, out/local-macos-1.4.11/strings-index.txt,
// carries the format string the rows are rendered from — `%s %s: %s
// tokens (%.1f%%)` — but not the labels themselves.
func TestCategoriesRowSetAndOrder(t *testing.T) {
	b := Breakdown{
		User: 1, Agent: 1, Tool: 1,
		SystemPrompt: 1, SystemTools: 1, SystemOverhead: 1,
		Rules: 1, Skills: 1, Subagents: 1,
		Workflows: 1, Plugins: 1, MCPTools: 1,
		SystemMessages: 1,
	}
	cats := b.allCategories()
	if len(cats) != len(docCategoryOrder) {
		t.Fatalf("categories = %d, want %d", len(cats), len(docCategoryOrder))
	}
	for i, want := range docCategoryOrder {
		if cats[i].Label != want {
			t.Errorf("category %d = %q, want %q", i, cats[i].Label, want)
		}
	}
}

// TestCategoriesMarkers pins the marker of every row.
//
// The documentation names the markers: `◉` for the conversation rows,
// `⛁` for the system prompt, the system tools, the system overhead and
// the customization rows, and `⊠` for the checkpoint buffer.
func TestCategoriesMarkers(t *testing.T) {
	b := Breakdown{
		User: 1, Agent: 1, Tool: 1,
		SystemPrompt: 1, SystemTools: 1, SystemOverhead: 1,
		Rules: 1, Skills: 1, Subagents: 1,
		Workflows: 1, Plugins: 1, MCPTools: 1,
		SystemMessages: 1,
	}
	want := map[string]string{
		"User messages":   UserRowMarker,
		"Agent responses": AgentGlyph,
		"Tool calls":      ToolRowMarker,
		"System prompt":   SystemGlyph,
		"System tools":    SystemGlyph,
		"System overhead": SystemGlyph,
		"Rules":           SystemGlyph,
		"Skills":          SystemGlyph,
		"Subagents":       SystemGlyph,
		"Workflows":       SystemGlyph,
		"Plugins":         SystemGlyph,
		"MCP tools":       SystemGlyph,
		"System messages": SystemCellGlyph,
	}
	for _, c := range b.allCategories() {
		if got := want[c.Label]; got != c.Marker {
			t.Errorf("category %q marker = %q, want %q", c.Label, c.Marker, got)
		}
	}
	if !strings.Contains(CheckpointGlyph, "⊠") {
		t.Errorf("the checkpoint marker = %q, want the ⊠", CheckpointGlyph)
	}
}

// TestCategoriesHideZeroRows pins the visibility rule: the three
// conversation rows are always shown, and a category that consumes no
// tokens is not a row.
func TestCategoriesHideZeroRows(t *testing.T) {
	b := Breakdown{User: 10, Agent: 20, Tool: 30, SystemPrompt: 40}
	cats := b.Categories()
	if len(cats) != 4 {
		t.Fatalf("visible categories = %d, want 4", len(cats))
	}
	want := []string{"User messages", "Agent responses", "Tool calls", "System prompt"}
	for i, w := range want {
		if cats[i].Label != w {
			t.Errorf("visible category %d = %q, want %q", i, cats[i].Label, w)
		}
	}

	// The three conversation rows stay on screen even when they are zero.
	empty := Breakdown{}.Categories()
	if len(empty) != 3 {
		t.Fatalf("empty categories = %d, want the 3 conversation rows", len(empty))
	}
	for _, c := range empty {
		if !c.Always {
			t.Errorf("category %q is hidden, want it always shown", c.Label)
		}
	}
}

// TestLiveRowsBytes pins the completed-turn layout's rows, byte for byte.
//
// The row shape is the reference's own format string
// (`%s %s: %s tokens (%.1f%%)\n`): the marker, the label, the token
// count and the percentage.
func TestLiveRowsBytes(t *testing.T) {
	b := Breakdown{
		User: 1000, Agent: 400, Tool: 100,
		SystemPrompt: 200, SystemTools: 50, SystemOverhead: 20,
		Rules: 30, Skills: 10, Subagents: 5,
		Workflows: 4, Plugins: 3, MCPTools: 2,
		SystemMessages: 1,
	}
	used := b.TotalUsed()
	free := MaxContextTokens - used
	rows := LiveRows("M", used, free, MaxContextTokens, b.Categories())

	want := []string{
		"M · 1.8k/1.0M tokens",
		"(0.2%)",
		"Token usage by category",
		"◉ User messages: 1.0k tokens (0.1%)",
		"◉ Agent responses: 400 tokens (0.0%)",
		"◉ Tool calls: 100 tokens (0.0%)",
		"⛁ System prompt: 200 tokens (0.0%)",
		"⛁ System tools: 50 tokens (0.0%)",
		"⛁ System overhead: 20 tokens (0.0%)",
		"⛁ Rules: 30 tokens (0.0%)",
		"⛁ Skills: 10 tokens (0.0%)",
		"⛁ Subagents: 5 tokens (0.0%)",
		"⛁ Workflows: 4 tokens (0.0%)",
		"⛁ Plugins: 3 tokens (0.0%)",
		"⛁ MCP tools: 2 tokens (0.0%)",
		"◉ System messages: 1 tokens (0.0%)",
		"□ Free space: 1.0M (99.8%)",
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d", len(rows), len(want))
	}
	for i, w := range want {
		if got := stripSGR(rows[i].Text); got != w {
			t.Errorf("row %d = %q, want %q", i, got, w)
		}
	}
}

// TestCheckpointRowBytes pins the checkpoint-buffer row.
//
// The reference's own format string is
// `%s Checkpoint buffer: %s tokens (not counted in usage)` — the row
// carries no percentage, because the checkpoint is not counted in the
// usage.
func TestCheckpointRowBytes(t *testing.T) {
	got := CheckpointRow(1234)
	want := CheckpointGlyph + " Checkpoint buffer: 1.2k tokens (not counted in usage)"
	if got != want {
		t.Errorf("checkpoint row = %q, want %q", got, want)
	}
}

// TestSystemMessageTokens pins the system-messages count: the session's
// system-generated messages are counted, and the conversation's own
// messages are not.
func TestSystemMessageTokens(t *testing.T) {
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	msgs := []backend.Message{
		{Role: "user", Content: "hello", WireContent: backend.WrapUserContent("hello", "", false, now)},
		{Role: "assistant", Content: "hi"},
		{Role: "system", Content: "a background task finished", WireContent: backend.WrapUserContent("a background task finished", "", false, now)},
	}
	want := backend.UserContentTokenCount(msgs[2].WireContent)
	if got := SystemMessageTokens(msgs); got != want {
		t.Errorf("SystemMessageTokens = %d, want %d", got, want)
	}
	if got := SystemMessageTokens(msgs[:2]); got != 0 {
		t.Errorf("SystemMessageTokens without a system message = %d, want 0", got)
	}
}

// TestCheckpointBufferTokens pins the checkpoint count: a session that
// was compacted holds its checkpoint in the resume block, and a session
// that was not compacted has no checkpoint.
func TestCheckpointBufferTokens(t *testing.T) {
	block := compaction.ResumingBlock([]string{"do the thing"}, "<summary>done</summary>")
	msgs := []backend.Message{{Role: "user", Content: block}}

	want := compaction.EstimateTokens(block)
	if got := CheckpointBufferTokens(msgs); got != want {
		t.Errorf("CheckpointBufferTokens = %d, want %d", got, want)
	}
	if got := CheckpointBufferTokens([]backend.Message{{Role: "user", Content: "hello"}}); got != 0 {
		t.Errorf("CheckpointBufferTokens without a compaction = %d, want 0", got)
	}
}
