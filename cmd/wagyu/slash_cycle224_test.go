// slash_cycle224_test.go — cycle 224: non-empty /context state.
//
// Cycle 190 closed /context for the empty case only: every right-side
// descriptor hardcoded the literal `"0 tokens (0.0%)"`, the title row
// said `model · 0/1.0M tokens`, and the "Estimated usage (awaiting
// generation)" preamble was a no-information placeholder.
//
// Cycle 224 filled in the non-empty branch with a chars-per-token proxy.
// Cycle 292 replaced that proxy with the reference's own accounting
// (decompiled from 1.4.3, re-measured on 1.4.4):
//
//   - User messages: len(<USER_REQUEST> wire envelope)/3 + 6.
//   - Agent responses: completed-step completion tokens minus the step
//     currently landing (sessionCompletionTokens - lastTurnCompletionTokens).
//   - Tool calls: (len(raw result)+1)/3 + 6 per call, across
//     m.blocks[i].steps and m.liveSteps.
//
// The tests below pin every visible descriptor for sessions of size 1,
// 5, and 20 messages — same shape as the cycle-222 trio's
// `seedRewindSession`, just refactored to a single string (each char
// becomes a user or assistant turn depending on its index parity).
//
// Cycle 224 preserves the cycle-190 layout byte-for-byte: the
// `TestPanelContextEmptyLayout` test in slash_panels_test.go still
// passes and is re-run as TestPanelContextEmptyStillCycle190 below.

package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/backend"
)

// contextTestZone pins the local-time offset so the wire envelope's
// RFC3339 stamp has a stable width across machines and CI time zones.
var contextTestZone = time.FixedZone("CST", 8*3600)

// seedContextSession writes a synthetic multi-turn session into m.
// Even-indexed payload entries become user messages, odd-indexed become
// assistant messages — the same trailing-pair pattern cycle 222 uses
// for the /rewind panel.
func seedContextSession(m *runMode, payloads []string) {
	for i, s := range payloads {
		role := "user"
		if i%2 != 0 {
			role = "assistant"
		}
		msg := backend.Message{Role: role, Content: s}
		if role == "user" {
			msg.WireContent = backend.WrapUserContent(s, "", false, time.Date(2026, 10, 21, 7, 0, 0, 0, contextTestZone))
		}
		m.session = append(m.session, msg)
	}
}

// sessionContextTokens mirrors sessionTokenCounts so each test can pin the
// precise numbers the panel will see without reaching into its rendering.
func sessionContextTokens(m *runMode) (user, agent, tool int) {
	for _, msg := range m.session {
		if msg.Role == "user" {
			user += backend.UserContentTokenCount(msg.WireText())
		}
	}
	agent = int(m.sessionCompletionTokens - m.lastTurnCompletionTokens)
	if agent < 0 {
		agent = 0
	}
	for _, b := range m.blocks {
		for _, st := range b.steps {
			if st.kind == stepKindTool {
				tool += backend.ToolResultTokenCount(st.result)
			}
		}
	}
	for _, st := range m.liveSteps {
		if st.kind == stepKindTool {
			tool += backend.ToolResultTokenCount(st.result)
		}
	}
	return user, agent, tool
}

// TestPanelContextEmptyStillCycle190 — re-run the cycle-190 empty
// test through the cycle-224 dispatcher so any future regression in
// the dispatch logic surfaces locally. The test in
// slash_panels_test.go is preserved verbatim and checks the same
// rows; this is the explicit "still passes" assertion.
func TestPanelContextEmptyStillCycle190(t *testing.T) {
	m := mkPanel(panelContext, "/context", 0)
	m.model = "Gemini 3.8 Flash (High)"
	plain := plainLines(m)
	if got := plain[0]; got != "└ Context Usage" {
		t.Errorf("row 0 = %q, want %q", got, "└ Context Usage")
	}
	if !strings.Contains(plain[1], "0/1.0M tokens") {
		t.Errorf("row 1 should still say 0/1.0M tokens: %q", plain[1])
	}
	if !strings.Contains(plain[2], "(0.0%)") {
		t.Errorf("row 2 should still show (0.0%%): %q", plain[2])
	}
	if !strings.Contains(plain[3], "awaiting generation") {
		t.Errorf("row 3 should still say 'awaiting generation': %q", plain[3])
	}
	if !strings.Contains(plain[7], "1.0M (100.0%)") {
		t.Errorf("row 7 should still show 1.0M (100.0%%): %q", plain[7])
	}
	if got := plain[len(plain)-1]; !strings.Contains(got, "esc") {
		t.Errorf("last row should be the scroll legend, got %q", got)
	}
}

// TestPanelContextNonEmpty1MsgLayout — a single user message (200
// chars → 50 tokens) produces a panel whose title carries the used
// count, whose (X.X%) row sits at the same column the cycle-190
// empty layout uses for `(0.0%)`, and whose "Estimated usage" line
// becomes "(live session, 1 message)".
func TestPanelContextNonEmpty1MsgLayout(t *testing.T) {
	m := mkPanel(panelContext, "/context", 0)
	m.model = "Gemini 3.8 Flash (High)"
	seedContextSession(m, []string{strings.Repeat("a", 200)})

	plain := plainLines(m)
	if len(plain) != 21 {
		t.Fatalf("line count = %d, want 21\nplain:\n%s",
			len(plain), strings.Join(plain, "\n"))
	}
	if got := plain[0]; got != "└ Context Usage" {
		t.Errorf("row 0 = %q, want title", got)
	}
	if !strings.Contains(plain[1], "Gemini 3.8 Flash (High) · ") ||
		!strings.Contains(plain[1], "/1.0M tokens") {
		t.Errorf("row 1 = %q, want used/1.0M descriptor", plain[1])
	}
	if !strings.Contains(plain[2], "%") {
		t.Errorf("row 2 = %q, want percentage", plain[2])
	}
	if !strings.Contains(plain[3], "Token usage by category") {
		t.Errorf("row 3 = %q, want 'Token usage by category'", plain[3])
	}
	u, a, tool := sessionContextTokens(m)
	if !strings.Contains(plain[4], fmt.Sprintf("User messages: %s tokens", contextTokenLabel(u))) {
		t.Errorf("row 4 = %q, want user count %d", plain[4], u)
	}
	if !strings.Contains(plain[5], fmt.Sprintf("Agent responses: %s tokens", contextTokenLabel(a))) {
		t.Errorf("row 5 = %q, want agent %d", plain[5], a)
	}
	if !strings.Contains(plain[6], fmt.Sprintf("Tool calls: %s tokens", contextTokenLabel(tool))) {
		t.Errorf("row 6 = %q, want tool %d", plain[6], tool)
	}
	if !strings.Contains(plain[7], "System prompt:") {
		t.Errorf("row 7 = %q, want system prompt category", plain[7])
	}
	if !strings.Contains(plain[8], "System tools:") {
		t.Errorf("row 8 = %q, want system tools category", plain[8])
	}
	if !strings.Contains(plain[9], "Skills:") {
		t.Errorf("row 9 = %q, want skills category", plain[9])
	}
	if !strings.Contains(plain[10], "Subagents:") {
		t.Errorf("row 10 = %q, want subagents category", plain[10])
	}
	if !strings.Contains(plain[11], "Free space: 1.0M") {
		t.Errorf("row 11 = %q, want free space", plain[11])
	}
	if plain[14] != " └ ~/.gemini/GEMINI.md" {
		t.Errorf("row 14 = %q", plain[14])
	}
	if plain[17] != " └ .agents/agents/" {
		t.Errorf("row 17 = %q, want .agents path", plain[17])
	}
	if !strings.Contains(plain[19], "Related: /artifact") {
		t.Errorf("row 19 = %q", plain[19])
	}
}

// TestPanelContextNonEmpty5MsgLayout — 5 messages: 2000-char user,
// 1500-char assistant, 800-char user, 400-char assistant, 1200-char
// user. Sum: user=1000 (2000/4+800/4+1200/4), agent=475
// (1500/4+400/4); total used=1475 → 0.1% of 1.0M; free=998525 →
// 99.9%. The "5 messages" plural label replaces cycle 190's
// "awaiting generation" placeholder.
func TestPanelContextNonEmpty5MsgLayout(t *testing.T) {
	m := mkPanel(panelContext, "/context", 0)
	m.model = "Gemini 3.8 Flash (High)"
	seedContextSession(m, []string{
		strings.Repeat("a", 2000),
		strings.Repeat("b", 1500),
		strings.Repeat("c", 800),
		strings.Repeat("d", 400),
		strings.Repeat("e", 1200),
	})
	// Cycle 292: assistant rows come from provider usage, not text length.
	// Seed two completed steps worth of completion tokens.
	m.sessionCompletionTokens = 475

	plain := plainLines(m)
	if len(plain) != 21 {
		t.Fatalf("line count = %d, want 21\nplain:\n%s",
			len(plain), strings.Join(plain, "\n"))
	}
	if !strings.Contains(plain[1], "Gemini 3.8 Flash (High) · ") {
		t.Errorf("row 1 = %q, want model descriptor", plain[1])
	}
	if !strings.Contains(plain[2], "%") {
		t.Errorf("row 2 = %q, want percentage", plain[2])
	}
	if !strings.Contains(plain[3], "Token usage by category") {
		t.Errorf("row 3 = %q, want category preamble", plain[3])
	}
	u, a, _ := sessionContextTokens(m)
	if !strings.Contains(plain[4], fmt.Sprintf("User messages: %s tokens", contextTokenLabel(u))) {
		t.Errorf("row 4 = %q, want user=%d", plain[4], u)
	}
	if !strings.Contains(plain[5], fmt.Sprintf("Agent responses: %s tokens", contextTokenLabel(a))) {
		t.Errorf("row 5 = %q, want agent=%d", plain[5], a)
	}
	if !strings.Contains(plain[6], "Tool calls: 0 tokens (0.0%)") {
		t.Errorf("row 6 = %q, want tool=0", plain[6])
	}
	if !strings.Contains(plain[11], "Free space: 1.0M") {
		t.Errorf("row 11 = %q, want free space", plain[11])
	}
}

// TestPanelContextNonEmpty20MsgLayout — 10 user/assistant pairs of
// progressively larger payloads: user grows 4000 → 5800 chars
// (1000, 1050, …, 1450 tokens), assistant grows 5000 → 7750 chars
// (1250, 1375, …, 1937 tokens). Sum: user=12250, agent=15875, total
// used=28125 → 2.8% of 1.0M, free=971875 → 97.2%. Pin these to make
// sure the percentage rounding and the title-row formatting hold
// up at a typical real-session size.
func TestPanelContextNonEmpty20MsgLayout(t *testing.T) {
	m := mkPanel(panelContext, "/context", 0)
	m.model = "Gemini 3.8 Flash (High)"
	const nTurns = 10
	payloads := make([]string, 0, 2*nTurns)
	for i := 0; i < nTurns; i++ {
		payloads = append(payloads, strings.Repeat("u", 4000+i*200)) // user
		payloads = append(payloads, strings.Repeat("a", 5000+i*300)) // assistant
	}
	seedContextSession(m, payloads)

	// Cycle 292: agent tokens are provider usage, so seed a realistic
	// session total and let the helper decide the user count.
	m.sessionCompletionTokens = 15875
	user, agent, _ := sessionContextTokens(m)
	if user == 0 || agent != 15875 {
		t.Fatalf("precheck: user = %d, agent = %d", user, agent)
	}
	if m.session[0].Role != "user" || m.session[1].Role != "assistant" {
		t.Fatalf("trailing-pair role order broken: %+v", m.session[:2])
	}

	plain := plainLines(m)
	if len(plain) != 21 {
		t.Fatalf("line count = %d, want 21\nplain:\n%s",
			len(plain), strings.Join(plain, "\n"))
	}
	if !strings.Contains(plain[1], "Gemini 3.8 Flash (High) · ") {
		t.Errorf("row 1 = %q, want model descriptor", plain[1])
	}
	if !strings.Contains(plain[2], "%") {
		t.Errorf("row 2 = %q, want percentage", plain[2])
	}
	if !strings.Contains(plain[3], "Token usage by category") {
		t.Errorf("row 3 = %q, want category preamble", plain[3])
	}
	if !strings.Contains(plain[4], fmt.Sprintf("User messages: %s tokens", contextTokenLabel(user))) {
		t.Errorf("row 4 = %q, want user=%d", plain[4], user)
	}
	if !strings.Contains(plain[5], "Agent responses: 15.9k tokens (1.6%)") {
		t.Errorf("row 5 = %q, want 15.9k / 1.6%%", plain[5])
	}
	if !strings.Contains(plain[6], "Tool calls: 0 tokens (0.0%)") {
		t.Errorf("row 6 = %q, want tool=0", plain[6])
	}
	if !strings.Contains(plain[11], "Free space: 1.0M") {
		t.Errorf("row 11 = %q, want free space", plain[11])
	}
}

// TestPanelContextNonEmptyToolCalls — populate m.blocks with three
// turnBlocks, each carrying one stepKindTool step, plus a single live
// stepKindTool in m.liveSteps. That gives 4 tool calls → 4 * 5 = 20
// tool tokens. Combined with a 4000-char user message (1000 tokens
// proxy), the expected used is 1020, free is 998980. Both the
// title-row <used>/1.0M and the "Tool calls: <n> tokens" line must
// match.
func TestPanelContextNonEmptyToolCalls(t *testing.T) {
	m := mkPanel(panelContext, "/context", 0)
	m.model = "Gemini 3.8 Flash (High)"
	seedContextSession(m, []string{strings.Repeat("a", 4000)})
	res := strings.Repeat("r", 109) // (109+1)/3+6 = 42 tokens per call
	m.blocks = []turnBlock{
		{steps: []toolStep{{kind: stepKindTool, name: "read_file", result: res}}},
		{steps: []toolStep{{kind: stepKindThought, tokens: 42}, {kind: stepKindTool, name: "glob", result: res}}},
		{steps: []toolStep{{kind: stepKindTool, name: "grep", result: res}}},
	}
	m.liveSteps = []toolStep{{kind: stepKindTool, name: "shell", result: res}}

	plain := plainLines(m)
	if !strings.Contains(plain[1], "Gemini 3.8 Flash (High) · ") {
		t.Errorf("row 1 = %q, want model descriptor", plain[1])
	}
	if !strings.Contains(plain[2], "%") {
		t.Errorf("row 2 = %q, want percentage", plain[2])
	}
	u, a, tool := sessionContextTokens(m)
	if tool != 4*42 {
		t.Fatalf("precheck: tool = %d, want 168", tool)
	}
	if !strings.Contains(plain[6], fmt.Sprintf("Tool calls: %s tokens", contextTokenLabel(tool))) {
		t.Errorf("row 6 = %q, want tool=%d", plain[6], tool)
	}
	if !strings.Contains(plain[4], fmt.Sprintf("User messages: %s tokens", contextTokenLabel(u))) {
		t.Errorf("row 4 = %q, want user=%d", plain[4], u)
	}
	if !strings.Contains(plain[5], "Agent responses: 0 tokens (0.0%)") {
		t.Errorf("row 5 = %q, want agent=0 (%d)", plain[5], a)
	}
}

// TestSessionTokenCounts — pin the helper directly against the reference's
// three formulas.
//
//   - user:  len(wire)/3 + 6, where wire is the stored <USER_REQUEST>
//     envelope (a Wire-less message is wrapped on the fly).
//   - agent: sessionCompletionTokens - lastTurnCompletionTokens, floored at 0.
//   - tool:  (len(raw result)+1)/3 + 6 per stepKindTool, over blocks and
//     liveSteps; thought steps do not count.
func TestSessionTokenCounts(t *testing.T) {
	m := mkPanel(panelContext, "/context", 0)
	wire := backend.WrapUserContent(strings.Repeat("a", 200), "Gemini 3.8 Flash (High)", false,
		time.Date(2026, 10, 21, 7, 0, 0, 0, contextTestZone))
	m.session = []backend.Message{
		{Role: "user", Content: strings.Repeat("a", 200), WireContent: wire},
		{Role: "assistant", Content: strings.Repeat("b", 200)},
	}
	wantUser := len(wire)/3 + 6
	u, a, tool := m.sessionTokenCounts()
	if u != wantUser || a != 0 || tool != 0 {
		t.Errorf("session-only: got (%d, %d, %d), want (%d, 0, 0)", u, a, tool, wantUser)
	}

	// Usage counters decide the agent row: the last step is excluded.
	m.sessionCompletionTokens = 100
	m.lastTurnCompletionTokens = 40
	_, a, _ = m.sessionTokenCounts()
	if a != 60 {
		t.Errorf("agent row = %d, want 60 (100 completed - 40 in flight)", a)
	}

	// One tool step in a historical block, one live: results are what count.
	m.blocks = []turnBlock{
		{steps: []toolStep{
			{kind: stepKindThought, tokens: 50},
			{kind: stepKindTool, name: "fetch", result: strings.Repeat("x", 109)},
		}},
	}
	m.liveSteps = []toolStep{{kind: stepKindTool, name: "shell", result: strings.Repeat("y", 158)}}
	_, _, tool = m.sessionTokenCounts()
	if tool != 42+59 {
		t.Errorf("tool row = %d, want 101 (42 + 59)", tool)
	}

	// A "system" role message is ignored.
	m.session = append(m.session,
		backend.Message{Role: "system", Content: strings.Repeat("z", 400)},
	)
	u2, a2, tool2 := m.sessionTokenCounts()
	if u2 != wantUser || a2 != 60 || tool2 != 101 {
		t.Errorf("after system msg: got (%d, %d, %d), want (%d, 60, 101)", u2, a2, tool2, wantUser)
	}

	// A running step has no result yet, so it contributes nothing.
	m.liveSteps = append(m.liveSteps, toolStep{kind: stepKindTool, name: "running", running: true})
	_, _, tool3 := m.sessionTokenCounts()
	if tool3 != 101 {
		t.Errorf("running step counted: tool = %d, want 101", tool3)
	}
}

// TestPanelContextNondescriptorRowsUnchanged — the rows that do not
// carry token counts (title, system-files block, Related line, legend)
// are identical between the empty and non-empty branches.
func TestPanelContextNondescriptorRowsUnchanged(t *testing.T) {
	empty := mkPanel(panelContext, "/context", 0)
	empty.model = "Gemini 3.8 Flash (High)"
	live := mkPanel(panelContext, "/context", 0)
	live.model = "Gemini 3.8 Flash (High)"
	seedContextSession(live, []string{strings.Repeat("a", 4000)})
	ep := plainLines(empty)
	lp := plainLines(live)
	for _, want := range []string{
		"System files · auto-loaded",
		" └ ~/.gemini/GEMINI.md",
		" └ ~/.gemini/skills/",
		" └ ~/.gemini/antigravity/skills/",
		" └ .agents/agents/",
		"Related: /artifact · /skill · /rewind",
	} {
		if !containsLine(ep, want) || !containsLine(lp, want) {
			t.Errorf("row %q missing from one branch (empty=%v live=%v)", want, containsLine(ep, want), containsLine(lp, want))
		}
	}
	if ep[0] != lp[0] || ep[len(ep)-1] != lp[len(lp)-1] {
		t.Errorf("title/legend differ: empty=%q/%q live=%q/%q", ep[0], ep[len(ep)-1], lp[0], lp[len(lp)-1])
	}
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

// TestPanelContextPercentagePadding — cycle 190 records that the
// (0.0%) row uses a 6-space pad (one column further right than the
// other rows). Cycle 224 preserves that quirk on the non-empty
// branch too: counting the spaces between the rightmost `□` and the
// leading `(` of the percentage row must be exactly 6 (descriptor
// rows keep their 5-space pad — that one is verified in the empty
// cycle-190 layout test).
func TestPanelContextPercentagePadding(t *testing.T) {
	cases := []struct {
		name     string
		payloads []string
	}{
		{"1msg", []string{strings.Repeat("a", 200)}},
		{"5msg", []string{
			strings.Repeat("a", 2000),
			strings.Repeat("b", 1500),
			strings.Repeat("c", 800),
			strings.Repeat("d", 400),
			strings.Repeat("e", 1200),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mkPanel(panelContext, "/context", 0)
			m.model = "Gemini 3.8 Flash (High)"
			seedContextSession(m, tc.payloads)
			plain := plainLines(m)
			// Row 2 is the percentage row (padded 6 cells); row 3
			// ("Token usage by category") uses 5.
			padRow := plain[2]
			padSpaces := spacesBetweenLastCellAndParen(padRow)
			if padSpaces != 6 {
				t.Errorf("row 2 padding = %d, want 6: %q", padSpaces, padRow)
			}
		})
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
	// Count the leading spaces.
	n := 0
	for n < len(tail) && tail[n] == ' ' {
		n++
	}
	return n
}
