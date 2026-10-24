package contextpanel

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/backend"
)

func TestClampedUsage(t *testing.T) {
	for _, tc := range []struct {
		name       string
		contextMax int
		rawUsed    int
		wantUsed   int
		wantFree   int
	}{
		{"zero", 1000, 0, 0, 1000},
		{"partial", 1000, 400, 400, 600},
		{"exact", 1000, 1000, 1000, 0},
		{"overflow", 1000, 1500, 1000, 0},
		{"negative used", 1000, -50, 0, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			used, free := ClampedUsage(tc.contextMax, tc.rawUsed)
			if used != tc.wantUsed || free != tc.wantFree {
				t.Errorf("ClampedUsage(%d, %d) = (%d, %d), want (%d, %d)",
					tc.contextMax, tc.rawUsed, used, free, tc.wantUsed, tc.wantFree)
			}
		})
	}
}

func TestFreeCapacityPercentages(t *testing.T) {
	free, usedPct, freePct := FreeCapacityPercentages(1000, 250)
	if free != 750 || usedPct != "(25.0%)" || freePct != "(75.0%)" {
		t.Errorf("FreeCapacityPercentages = (%d, %q, %q), want (750, (25.0%%), (75.0%%))",
			free, usedPct, freePct)
	}

	free, usedPct, freePct = FreeCapacityPercentages(1000, 1200)
	if free != 0 || usedPct != "(100.0%)" || freePct != "(0.0%)" {
		t.Errorf("FreeCapacityPercentages overflow = (%d, %q, %q), want (0, (100.0%%), (0.0%%))",
			free, usedPct, freePct)
	}
}

func TestCapacityPercentagesFloat(t *testing.T) {
	if got := FreeCapacityPercentage(250, 1000); got != 75.0 {
		t.Errorf("FreeCapacityPercentage(250, 1000) = %v, want 75.0", got)
	}
	if got := FreeCapacityPercentage(0, 1000); got != 100.0 {
		t.Errorf("FreeCapacityPercentage(0, 1000) = %v, want 100.0", got)
	}
	if got := FreeCapacityPercentage(1200, 1000); got != 0.0 {
		t.Errorf("FreeCapacityPercentage(1200, 1000) = %v, want 0.0", got)
	}
	if got := FreeCapacityPercentage(100, 0); got != 0.0 {
		t.Errorf("FreeCapacityPercentage(100, 0) = %v, want 0.0", got)
	}
	if got := UsedCapacityPercentage(250, 1000); got != 25.0 {
		t.Errorf("UsedCapacityPercentage(250, 1000) = %v, want 25.0", got)
	}
	if got := UsedCapacityPercentage(1500, 1000); got != 100.0 {
		t.Errorf("UsedCapacityPercentage(1500, 1000) = %v, want 100.0", got)
	}
}

func TestPercentageAndFormatting(t *testing.T) {
	if got := Percentage(50, 200); got != 25.0 {
		t.Errorf("Percentage(50, 200) = %v, want 25.0", got)
	}
	if got := Percentage(-1, 200); got != 0.0 {
		t.Errorf("Percentage(-1, 200) = %v, want 0.0", got)
	}
	if got := Percentage(50, 0); got != 0.0 {
		t.Errorf("Percentage(50, 0) = %v, want 0.0", got)
	}
	if got := FormatPercentage(50, 200); got != "(25.0%)" {
		t.Errorf("FormatPercentage(50, 200) = %q, want (25.0%%)", got)
	}
}

func TestBreakdownStructMethods(t *testing.T) {
	b := Breakdown{
		Model:        "TestModel",
		ContextMax:   1_000_000,
		User:         100_000,
		Agent:        50_000,
		Tool:         50_000,
		SystemPrompt: 100_000,
		SystemTools:  50_000,
		Skills:       30_000,
		Subagents:    20_000,
	}

	if got := b.TotalUsed(); got != 400_000 {
		t.Errorf("TotalUsed = %d, want 400000", got)
	}
	if got := b.StreamingTotalUsed(); got != 200_000 {
		t.Errorf("StreamingTotalUsed = %d, want 200000", got)
	}
	used, free := b.ClampedUsage()
	if used != 400_000 || free != 600_000 {
		t.Errorf("ClampedUsage = (%d, %d), want (400000, 600000)", used, free)
	}
	if got := b.FreePercent(); got != 60.0 {
		t.Errorf("FreePercent = %v, want 60.0", got)
	}
	if got := b.UsedPercent(); got != 40.0 {
		t.Errorf("UsedPercent = %v, want 40.0", got)
	}
	if got := b.FreePct(); got != "(60.0%)" {
		t.Errorf("FreePct = %q, want (60.0%%)", got)
	}
	if got := b.UsedPct(); got != "(40.0%)" {
		t.Errorf("UsedPct = %q, want (40.0%%)", got)
	}
}

func TestUserMessageTokens(t *testing.T) {
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	wire1 := backend.WrapUserContent("hello world", "model", false, now)
	msgs := []backend.Message{
		{Role: "user", Content: "hello world", WireContent: wire1},
		{Role: "assistant", Content: "response"},
		{Role: "user", Content: "second user prompt"}, // WireContent empty, should wrap
	}

	tokens := UserMessageTokens(msgs)
	if tokens <= 0 {
		t.Fatalf("UserMessageTokens = %d, want > 0", tokens)
	}

	emptyMsgs := []backend.Message{
		{Role: "assistant", Content: "only assistant"},
	}
	if got := UserMessageTokens(emptyMsgs); got != 0 {
		t.Errorf("UserMessageTokens(assistant only) = %d, want 0", got)
	}
}

func TestAdjustStreamingUserTokens(t *testing.T) {
	content := strings.Repeat("x", 400)
	wire := backend.WrapUserContent(content, "model", false, time.Now())
	msgs := []backend.Message{
		{Role: "user", Content: content, WireContent: wire},
	}
	baseTokens := UserMessageTokens(msgs)

	adjusted := AdjustStreamingUserTokens(msgs, baseTokens)
	if adjusted != 100 { // 400 / 4 = 100
		t.Errorf("AdjustStreamingUserTokens = %d, want 100", adjusted)
	}

	shortMsgs := []backend.Message{
		{Role: "user", Content: "a", WireContent: ""},
	}
	shortBase := UserMessageTokens(shortMsgs)
	adjShort := AdjustStreamingUserTokens(shortMsgs, shortBase)
	if adjShort != 1 {
		t.Errorf("AdjustStreamingUserTokens short = %d, want 1", adjShort)
	}
}

func TestAgentTokens(t *testing.T) {
	if got := AgentTokens(150, 50); got != 100 {
		t.Errorf("AgentTokens(150, 50) = %d, want 100", got)
	}
	if got := AgentTokens(50, 150); got != 0 {
		t.Errorf("AgentTokens negative floored = %d, want 0", got)
	}

	if got := StreamingAgentTokens(150, 50, 40); got != 110 { // 100 + 40/4
		t.Errorf("StreamingAgentTokens = %d, want 110", got)
	}
}

func TestToolTokens(t *testing.T) {
	decls := []backend.ToolDecl{
		{Name: "test_tool", Description: "a test tool"},
	}
	est := ToolTokenEstimate(decls)
	if est <= 0 {
		t.Errorf("ToolTokenEstimate = %d, want > 0", est)
	}

	raw := []byte("123456789")
	if got := ToolTokenEstimateFromBytes(raw); got != 3 {
		t.Errorf("ToolTokenEstimateFromBytes = %d, want 3", got)
	}

	res1 := strings.Repeat("a", 100)
	res2 := strings.Repeat("b", 200)
	tokens := ToolResultTokens(res1, res2)
	want := backend.ToolResultTokenCount(res1) + backend.ToolResultTokenCount(res2)
	if tokens != want {
		t.Errorf("ToolResultTokens = %d, want %d", tokens, want)
	}
}

func TestPromptTokens(t *testing.T) {
	if got := PromptSectionTokens("", ""); got != 0 {
		t.Errorf("PromptSectionTokens empty = %d, want 0", got)
	}
	body := "sample body text"
	name := "skills"
	want := (len(body) + 2*len(name) + 3) / 3
	if got := PromptSectionTokens(name, body); got != want {
		t.Errorf("PromptSectionTokens = %d, want %d", got, want)
	}

	if got := SystemPromptTokens(900, 100, 50); got != 150 { // (900 - 300 - 150)/3 = 150
		t.Errorf("SystemPromptTokens = %d, want 150", got)
	}
	if got := SystemPromptTokens(100, 200, 200); got != 0 {
		t.Errorf("SystemPromptTokens negative = %d, want 0", got)
	}
}

func TestEmptyRowsAndGridLines(t *testing.T) {
	rows := EmptyRows("Gemini Model")
	if len(rows) != 7 {
		t.Fatalf("EmptyRows count = %d, want 7", len(rows))
	}
	if rows[0].Text != "Gemini Model · 0/1.0M tokens" {
		t.Errorf("EmptyRows[0].Text = %q", rows[0].Text)
	}

	lines := EmptyGridLines("Gemini Model", 200)
	if len(lines) != 7 {
		t.Fatalf("EmptyGridLines count = %d, want 7", len(lines))
	}
	for _, l := range lines {
		if !strings.Contains(l, Cell) {
			t.Errorf("line missing empty cell: %q", l)
		}
	}
}

func TestStreamingAndLiveGridLines(t *testing.T) {
	stLines := StreamingGridLines("TestModel", 200, 1_000_000, 1000, 2000, 3000)
	if len(stLines) != 7 {
		t.Fatalf("StreamingGridLines count = %d, want 7", len(stLines))
	}
	if !strings.Contains(stLines[0], "TestModel") {
		t.Errorf("StreamingGridLines title = %q", stLines[0])
	}

	liveLines := LiveGridLines("TestModel", 200, 1_000_000, 1000, 2000, 3000, 4000, 1000, 500, 500)
	if len(liveLines) != 11 {
		t.Fatalf("LiveGridLines count = %d, want 11", len(liveLines))
	}
	if !strings.Contains(liveLines[0], "TestModel") {
		t.Errorf("LiveGridLines title = %q", liveLines[0])
	}
}

func TestBreakdownGridLinesMethods(t *testing.T) {
	b := Breakdown{
		Model:        "TestModel",
		ContextMax:   1_000_000,
		User:         1000,
		Agent:        2000,
		Tool:         3000,
		SystemPrompt: 4000,
		SystemTools:  1000,
		Skills:       500,
		Subagents:    500,
	}

	liveLines := b.LiveGridLines(200)
	if len(liveLines) != 11 {
		t.Fatalf("b.LiveGridLines count = %d, want 11", len(liveLines))
	}

	stLines := b.StreamingGridLines(200)
	if len(stLines) != 7 {
		t.Fatalf("b.StreamingGridLines count = %d, want 7", len(stLines))
	}
}
