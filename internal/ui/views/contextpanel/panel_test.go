package contextpanel

import (
	"regexp"
	"strings"
	"testing"
)

// sgrPattern matches every ANSI SGR escape sequence.
var sgrPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripSGR removes the SGR escape sequences so a row can be compared as
// plain text.
func stripSGR(s string) string {
	return sgrPattern.ReplaceAllString(s, "")
}

// plainLines strips the SGR sequences off every row.
func plainLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = stripSGR(l)
	}
	return out
}

// containsLine reports whether the exact row is present.
func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

// TestLinesEmptyLayout — the cycle-190 layout: the title row, the
// 44-cell grid with the model's descriptor, the percentage row, the
// "awaiting generation" placeholder, the three zeroed categories, the
// free-space row and the shared tail.
func TestLinesEmptyLayout(t *testing.T) {
	plain := plainLines(Lines(State{
		Mode:  ModeEmpty,
		Model: "Gemini 3.8 Flash (High)",
	}))
	if len(plain) != 19 {
		t.Fatalf("line count = %d, want 19\nplain:\n%s", len(plain), strings.Join(plain, "\n"))
	}
	if plain[0] != "└ Context Usage" {
		t.Errorf("row 0 = %q, want the title", plain[0])
	}
	if !strings.Contains(plain[1], "□ □ □ □ □") {
		t.Errorf("row 1 should have grid cells, got %q", plain[1])
	}
	if !strings.Contains(plain[1], "Gemini 3.8 Flash (High) · 0/1.0M tokens") {
		t.Errorf("row 1 should have the model descriptor, got %q", plain[1])
	}
	if !strings.Contains(plain[2], "(0.0%)") {
		t.Errorf("row 2 should show (0.0%%), got %q", plain[2])
	}
	if !strings.Contains(plain[3], "awaiting generation") {
		t.Errorf("row 3 should say 'awaiting generation', got %q", plain[3])
	}
	if !strings.Contains(plain[4], "User messages: 0 tokens (0.0%)") {
		t.Errorf("row 4 = %q, want the zeroed user row", plain[4])
	}
	if !strings.Contains(plain[5], "Agent responses: 0 tokens (0.0%)") {
		t.Errorf("row 5 = %q, want the zeroed agent row", plain[5])
	}
	if !strings.Contains(plain[6], "Tool calls: 0 tokens (0.0%)") {
		t.Errorf("row 6 = %q, want the zeroed tool row", plain[6])
	}
	if !strings.Contains(plain[7], "1.0M (100.0%)") {
		t.Errorf("row 7 = %q, want the free-space row", plain[7])
	}
}

// TestLinesStreamingLayout — the in-flight layout (cycle 289): the
// "awaiting generation" placeholder and the user/agent/tool rows, but
// not the completed-turn categories.
func TestLinesStreamingLayout(t *testing.T) {
	plain := plainLines(Lines(State{
		Mode:  ModeStreaming,
		Model: "Gemini 3.8 Flash (High)",
		User:  6,
		Agent: 29,
	}))
	if len(plain) != 19 {
		t.Fatalf("line count = %d, want 19\nplain:\n%s", len(plain), strings.Join(plain, "\n"))
	}
	if !strings.Contains(plain[3], "awaiting generation") {
		t.Errorf("row 3 = %q, want the awaiting-generation row", plain[3])
	}
	if strings.Contains(strings.Join(plain, "\n"), "Token usage by category") {
		t.Error("the in-flight layout rendered the completed-turn categories")
	}
	if !strings.Contains(plain[4], "User messages: 6 tokens (0.0%)") {
		t.Errorf("row 4 = %q, want the user row", plain[4])
	}
	if !strings.Contains(plain[5], "Agent responses: 29 tokens (0.0%)") {
		t.Errorf("row 5 = %q, want the agent row", plain[5])
	}
	if !strings.Contains(plain[6], "Tool calls: 0 tokens (0.0%)") {
		t.Errorf("row 6 = %q, want the tool row", plain[6])
	}
	if !strings.Contains(plain[7], "Free space: 1.0M (100.0%)") {
		t.Errorf("row 7 = %q, want the free-space row", plain[7])
	}
}

// TestLinesLiveLayout — the completed-turn layout (cycle 224): every
// category the reference's documentation names, in its order, the
// "Token usage by category" preamble and the free-space row.
func TestLinesLiveLayout(t *testing.T) {
	plain := plainLines(Lines(State{
		Mode:           ModeLive,
		Model:          "Gemini 3.8 Flash (High)",
		User:           1000,
		Agent:          475,
		Tool:           168,
		SystemPrompt:   100,
		SystemTools:    50,
		SystemOverhead: 40,
		Rules:          30,
		Skills:         20,
		Subagents:      10,
		Workflows:      5,
		Plugins:        4,
		MCPTools:       3,
		SystemMessages: 2,
	}))
	if len(plain) != 29 {
		t.Fatalf("line count = %d, want 29\nplain:\n%s", len(plain), strings.Join(plain, "\n"))
	}
	if !strings.Contains(plain[1], "Gemini 3.8 Flash (High) · 1.9k/1.0M tokens") {
		t.Errorf("row 1 = %q, want the used/1.0M descriptor", plain[1])
	}
	if !strings.Contains(plain[2], "(0.2%)") {
		t.Errorf("row 2 = %q, want the used percentage", plain[2])
	}
	if !strings.Contains(plain[3], "Token usage by category") {
		t.Errorf("row 3 = %q, want the category preamble", plain[3])
	}
	want := []string{
		"◉ User messages: 1.0k tokens (0.1%)",
		"◉ Agent responses: 475 tokens (0.0%)",
		"◉ Tool calls: 168 tokens (0.0%)",
		"⛁ System prompt: 100 tokens (0.0%)",
		"⛁ System tools: 50 tokens (0.0%)",
		"⛁ System overhead: 40 tokens (0.0%)",
		"⛁ Rules: 30 tokens (0.0%)",
		"⛁ Skills: 20 tokens (0.0%)",
		"⛁ Subagents: 10 tokens (0.0%)",
		"⛁ Workflows: 5 tokens (0.0%)",
		"⛁ Plugins: 4 tokens (0.0%)",
		"⛁ MCP tools: 3 tokens (0.0%)",
		"◉ System messages: 2 tokens (0.0%)",
		"□ Free space: 1.0M (99.8%)",
	}
	for i, w := range want {
		if !strings.Contains(plain[4+i], w) {
			t.Errorf("row %d = %q, want %q", 4+i, plain[4+i], w)
		}
	}
}

// TestLinesLiveLayoutHidesZeroCategories — a category that consumes no
// tokens is not a row: the reference's documentation says the remaining
// rows appear only when that category is consuming tokens.
func TestLinesLiveLayoutHidesZeroCategories(t *testing.T) {
	plain := plainLines(Lines(State{
		Mode:         ModeLive,
		Model:        "Gemini 3.8 Flash (High)",
		User:         1000,
		Agent:        475,
		Tool:         168,
		SystemPrompt: 100,
		SystemTools:  50,
	}))
	joined := strings.Join(plain, "\n")
	for _, gone := range []string{"Skills:", "Subagents:", "Workflows:", "Plugins:", "MCP tools:", "System messages:", "System overhead:", "Rules:"} {
		if strings.Contains(joined, gone) {
			t.Errorf("the live layout rendered the empty category %q", gone)
		}
	}
	for _, kept := range []string{"User messages:", "Agent responses:", "Tool calls:", "System prompt:", "System tools:", "Free space:"} {
		if !strings.Contains(joined, kept) {
			t.Errorf("the live layout dropped the row %q", kept)
		}
	}
}

// TestLinesCheckpointBufferRow — the checkpoint buffer is tracked
// outside the grid: its row sits below the grid's rows and above the
// shared tail, and it carries no percentage.
func TestLinesCheckpointBufferRow(t *testing.T) {
	plain := plainLines(Lines(State{
		Mode:             ModeLive,
		Model:            "Gemini 3.8 Flash (High)",
		User:             1000,
		Agent:            475,
		Tool:             168,
		SystemPrompt:     100,
		SystemTools:      50,
		Skills:           20,
		Subagents:        10,
		CheckpointBuffer: 1234,
	}))
	want := "⊠ Checkpoint buffer: 1.2k tokens (not counted in usage)"
	idx := -1
	for i, l := range plain {
		if strings.Contains(l, "Checkpoint buffer") {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("the panel rendered no checkpoint row:\n%s", strings.Join(plain, "\n"))
	}
	if !strings.Contains(plain[idx], want) {
		t.Errorf("row %d = %q, want %q", idx, plain[idx], want)
	}
	// The row is below the grid and above the tail.
	if !strings.Contains(plain[idx-1], "Free space:") {
		t.Errorf("row %d = %q, want the free-space row above the checkpoint row", idx-1, plain[idx-1])
	}
	if plain[idx+1] != "" || !strings.Contains(plain[idx+2], "System files · auto-loaded") {
		t.Errorf("rows %d/%d = %q/%q, want the tail below the checkpoint row",
			idx+1, idx+2, plain[idx+1], plain[idx+2])
	}
}

// TestLinesTail — the shared bottom of every layout: the auto-loaded
// system files, the Related rows and the scroll legend.
//
// The three Related rows are the reference's own bytes: its string
// table carries `Related: /artifact \n /skill \n /rewind` with the
// *two characters* `\n` between the commands
// (the surviving pruned index of that table,
// out/local-macos-1.4.11/strings-index.txt, carries the head
// "Related: /artifact" and, in the context window around it, the tail
// " /skill " and " /rewind"; the full dump is retired), so the hint is
// three rows
// and the spaces around the separator are kept.
func TestLinesTail(t *testing.T) {
	tail := plainLines(TailLines())
	want := []string{
		"",
		"System files · auto-loaded",
		" └ ~/.gemini/GEMINI.md",
		" └ ~/.gemini/skills/",
		" └ ~/.gemini/antigravity/skills/",
		" └ .agents/agents/",
		"",
		"Related: /artifact ",
		" /skill ",
		" /rewind",
	}
	if len(tail) != len(want)+1 {
		t.Fatalf("tail length = %d, want %d (the rows plus the legend)", len(tail), len(want)+1)
	}
	for i, w := range want {
		if tail[i] != w {
			t.Errorf("tail row %d = %q, want %q", i, tail[i], w)
		}
	}
	if !strings.Contains(tail[len(tail)-1], "↑/↓ Scroll") {
		t.Errorf("legend = %q, want the scroll hint", tail[len(tail)-1])
	}
}

// TestLinesNondescriptorRowsUnchanged — the rows that do not carry
// token counts (title, system-files block, Related line, legend) are
// identical between the empty and the completed-turn layouts.
func TestLinesNondescriptorRowsUnchanged(t *testing.T) {
	ep := plainLines(Lines(State{Mode: ModeEmpty, Model: "Gemini 3.8 Flash (High)"}))
	lp := plainLines(Lines(State{Mode: ModeLive, Model: "Gemini 3.8 Flash (High)"}))
	for _, want := range []string{
		"System files · auto-loaded",
		" └ ~/.gemini/GEMINI.md",
		" └ ~/.gemini/skills/",
		" └ ~/.gemini/antigravity/skills/",
		" └ .agents/agents/",
		"Related: /artifact ",
		" /skill ",
		" /rewind",
	} {
		if !containsLine(ep, want) || !containsLine(lp, want) {
			t.Errorf("row %q missing from one layout (empty=%v live=%v)",
				want, containsLine(ep, want), containsLine(lp, want))
		}
	}
	if ep[0] != lp[0] || ep[len(ep)-1] != lp[len(lp)-1] {
		t.Errorf("title/legend differ: empty=%q/%q live=%q/%q",
			ep[0], ep[len(ep)-1], lp[0], lp[len(lp)-1])
	}
}

// TestLinesPercentagePadding — cycle 190 records that the (0.0%) row
// uses a 6-space pad (one column further right than the other rows).
// Cycle 224 preserves that quirk on the completed-turn layout too:
// counting the spaces between the rightmost `□` and the leading `(` of
// the percentage row must be exactly 6 (the descriptor rows keep their
// 5-space pad).
func TestLinesPercentagePadding(t *testing.T) {
	for _, mode := range []Mode{ModeStreaming, ModeLive} {
		plain := plainLines(Lines(State{Mode: mode, Model: "M"}))
		padSpaces := spacesBetweenLastCellAndParen(plain[2])
		if padSpaces != 6 {
			t.Errorf("mode %v: row 2 padding = %d, want 6: %q", mode, padSpaces, plain[2])
		}
	}
}

// spacesBetweenLastCellAndParen returns the number of spaces between
// the rightmost `□` in `row` and the leading `(` of the percentage.
// Cycle 190 records 6 for the percentage row and 5 for descriptor
// rows; cycle 224 must keep both.
func spacesBetweenLastCellAndParen(row string) int {
	idx := strings.LastIndex(row, "□")
	if idx < 0 {
		return -1
	}
	tail := row[idx+len("□"):]
	n := 0
	for n < len(tail) && tail[n] == ' ' {
		n++
	}
	return n
}
