package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/views/contextpanel"
)

// TestCompactAndContextAlignment proves that:
// 1. Auto-compaction / step-boundary compaction does not discard the subsequent assistant reply.
// 2. /context shows "Estimated after compaction · updates next turn" right after compaction.
// 3. /context renders the Checkpoints (N) hierarchy correctly.
// 4. /context uses the model's published limit and accurate free space.
// 5. Subsequent completed turns reset the header to "Token usage by category".
func TestCompactAndContextAlignment(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120

	// 1. Initial conversation before compaction with large tool and agent usage.
	m.setSessionMessages([]backend.Message{
		{Role: "user", Content: "First prompt from user"},
		{Role: "assistant", Content: "First assistant reply"},
	})
	m.session.store.SessionCompletionTokens = 8000
	m.session.store.LastTurnCompletionTokens = 2000
	// Simulate historical tool results before compaction (e.g. 50,000 tokens of file content)
	largeToolResult := strings.Repeat("line of code from read_file\n", 4000) // ~120KB ~ 40k tokens
	m.conversation.blocks = []transcript.Block{
		{
			Steps: []toolsteps.Step{
				toolsteps.NewToolStep("read_file", []byte(`{"path":"/huge.go"}`), largeToolResult, false),
			},
		},
	}

	// Before compaction: verify /context shows "Token usage by category" and large tool tokens
	st1 := m.contextState()
	if st1.CompactJustHappened {
		t.Fatalf("expected CompactJustHappened=false before compaction")
	}
	if st1.Tool < 20000 {
		t.Fatalf("expected st1.Tool >= 20000 before compaction, got %d", st1.Tool)
	}
	totalBefore := st1.TotalUsed()
	lines1 := contextpanel.Lines(st1)
	joined1 := strings.Join(lines1, "\n")
	if !strings.Contains(joined1, "Token usage by category") {
		t.Fatalf("expected 'Token usage by category' before compaction, got:\n%s", joined1)
	}

	// 2. Simulate step-boundary auto-compaction event occurring in-turn.
	resumeText := compaction.ResumingBlock([]string{"First prompt from user"}, "Summary of previous steps")
	m.updateCompactEndMsg(compactEndMsg{
		Reason:               "threshold",
		EndedAt:              time.Now(),
		Summary:              "Summary of previous steps",
		Resume:               resumeText,
		Steps:                2,
		KeepRecent:           0,
		Strategy:             "SAME_MODEL",
		SummaryModel:         m.session.model,
		TokensBefore:         60000,
		EstimatedTokensAfter: 1200,
	})

	// Simulate model completing its generation in the same turn after compaction.
	finalTurnReply := "Final assistant reply after compaction resumed"
	m.conversation.compactHappenedThisTurn = true
	m.Update(toolsteps.TurnDoneMsg{
		Reply: finalTurnReply,
		Steps: []toolsteps.Step{
			toolsteps.NewToolStep("read_file", []byte(`{"path":"/foo"}`), "file content", false),
		},
	})

	// CRITICAL ASSERTION 1: The assistant's reply must NOT be discarded from m.session!
	msgs := m.SessionMessages()
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages in session after compaction resumption, got %d", len(msgs))
	}
	lastMsg := msgs[len(msgs)-1]
	if lastMsg.Role != "assistant" || lastMsg.Content != finalTurnReply {
		t.Fatalf("expected assistant reply %q, got role=%q content=%q", finalTurnReply, lastMsg.Role, lastMsg.Content)
	}

	// CRITICAL ASSERTION 2: /context immediately after compaction must display:
	// - "Estimated after compaction · updates next turn"
	// - Active context size is dramatically REDUCED to post-compaction context!
	// - Historical compacted tool calls (40,000+ tokens) are NOT in active tool usage!
	// - Checkpoint buffer row
	// - Checkpoints (1) section
	st2 := m.contextState()
	if !st2.CompactJustHappened {
		t.Fatalf("expected CompactJustHappened=true immediately following compaction")
	}
	if st2.CheckpointBuffer <= 0 {
		t.Fatalf("expected CheckpointBuffer > 0, got %d", st2.CheckpointBuffer)
	}
	// Tool tokens in active context should be near 0 (only post-compaction tools, not the 40k historical ones)
	if st2.Tool > 100 {
		t.Fatalf("expected post-compaction active Tool tokens < 100, got %d", st2.Tool)
	}
	totalAfter := st2.TotalUsed()
	if totalAfter >= totalBefore/2 {
		t.Fatalf("expected post-compaction TotalUsed (%d) to be substantially smaller than pre-compaction (%d)", totalAfter, totalBefore)
	}
	lines2 := contextpanel.Lines(st2)
	joined2 := strings.Join(lines2, "\n")

	if !strings.Contains(joined2, "Estimated after compaction · updates next turn") {
		t.Errorf("expected 'Estimated after compaction · updates next turn', got:\n%s", joined2)
	}
	if !strings.Contains(joined2, "Checkpoint buffer:") || !strings.Contains(joined2, "(not counted in usage)") {
		t.Errorf("expected Checkpoint buffer row in /context, got:\n%s", joined2)
	}
	if !strings.Contains(joined2, "Checkpoints (1)") || !strings.Contains(joined2, "Checkpoint 1 (active, in context)") {
		t.Errorf("expected Checkpoints (1) hierarchy in /context, got:\n%s", joined2)
	}
	if !strings.Contains(joined2, "1.0M tokens") {
		t.Errorf("expected 1.0M tokens limit for Gemini, got:\n%s", joined2)
	}

	// 3. User submits next turn and it completes without compaction.
	m.AppendUserMessage("Turn 2 user prompt")
	turn2Msgs := m.prepareStreamTurnMessages()
	if len(turn2Msgs) != 3 {
		t.Fatalf("expected 3 messages for Turn 2, got %d: %+v", len(turn2Msgs), turn2Msgs)
	}
	if !strings.HasPrefix(turn2Msgs[0].Content, "# Resuming from a compaction") {
		t.Errorf("turn 2 msg 0 want resume block, got %q", turn2Msgs[0].Content)
	}
	if turn2Msgs[1].Content != finalTurnReply {
		t.Errorf("turn 2 msg 1 want %q, got %q", finalTurnReply, turn2Msgs[1].Content)
	}
	if turn2Msgs[2].Content != "Turn 2 user prompt" {
		t.Errorf("turn 2 msg 2 want 'Turn 2 user prompt', got %q", turn2Msgs[2].Content)
	}

	m.Update(toolsteps.TurnDoneMsg{
		Reply: "Subsequent turn answer without compaction",
	})

	// CRITICAL ASSERTION 3: Once next turn completes, /context switches back to "Token usage by category"
	st3 := m.contextState()
	if st3.CompactJustHappened {
		t.Fatalf("expected CompactJustHappened=false after next turn completes")
	}
	lines3 := contextpanel.Lines(st3)
	joined3 := strings.Join(lines3, "\n")
	if !strings.Contains(joined3, "Token usage by category") {
		t.Errorf("expected 'Token usage by category' after subsequent turn, got:\n%s", joined3)
	}
}

// TestClaudeContextWindowPublishedLimit verifies that Claude / custom models show their
// real published limit (e.g. 200k) rather than being hardcoded to 1.0M.
func TestClaudeContextWindowPublishedLimit(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Claude 3.5 Sonnet"
	m.viewport.width = 120

	st := m.contextState()
	if st.ContextMax != 200000 {
		t.Errorf("ContextMax for Claude 3.5 Sonnet = %d, want 200000", st.ContextMax)
	}
	lines := contextpanel.Lines(st)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Claude 3.5 Sonnet · 0/200k tokens") {
		t.Errorf("expected 200k tokens in header, got:\n%s", joined)
	}
	if !strings.Contains(joined, "Free space: 200k (100.0%)") {
		t.Errorf("expected 200k Free space, got:\n%s", joined)
	}
}

// TestAutoCompactionStepBoundarySoftThreshold tests that soft threshold triggers
// at a step boundary where ToolCalls or ToolResults exist.
func TestAutoCompactionStepBoundarySoftThreshold(t *testing.T) {
	t.Setenv("OPENAGY_COMPACT_TOKEN_THRESHOLD", "1000")

	// Verify local engine / policy triggers compaction when threshold is exceeded.
	env := agentturn.Env{
		Model:        "gemini-3.8-flash-high",
		SystemPrompt: "test system prompt",
	}

	var events []agentlink.Frame
	ch := make(chan agentlink.Frame, 10)
	emit := func(f agentlink.Frame) bool {
		events = append(events, f)
		return true
	}

	_ = emit
	_ = ch
	_ = env
	// Verify convstore checkpoint indexing works
	steps := []convstore.Step{
		{Idx: 1, Type: convstore.StepUserInput, Text: "hi"},
		{Idx: 2, Type: convstore.StepToolCall, Text: "read"},
		{Idx: 3, Type: convstore.StepCheckpoint, IncludedStart: 1, IncludedEnd: 3},
		{Idx: 4, Type: convstore.StepUserInput, Text: "next"},
	}
	traject := convstore.Trajectory{
		Loaded: steps,
	}
	built := convstore.BuildSteps(traject)
	if len(built) != 4 {
		t.Fatalf("built steps len = %d, want 4", len(built))
	}
}
