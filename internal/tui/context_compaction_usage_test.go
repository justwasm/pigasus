package tui

// context_compaction_usage_test.go — the /context panel after a compaction.
//
// # What these pin
//
// Three separate ways the panel disagreed with the model's context:
//
//  1. A manual /compact replaced the session but left the compaction
//     boundary where it was, so the tool results the summary replaced kept
//     counting towards "Tool calls".
//  2. The compaction banner's "before" figure never counted tool results at
//     all, so it described a smaller conversation than /context did for the
//     same moment.
//  3. The boundary was re-taken at the turn's end from the block count,
//     which swallowed every tool the turn ran *after* compacting.
//
// The boundary is now a *step* count taken at the compaction itself, which
// survives the turn's finalisation (it moves steps from liveSteps into
// blocks without changing how many there are) and cannot swallow the turn's
// later tools.

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/views/contextpanel"
	"github.com/justwasm/pigasus/palette"
)

// bigToolOutput is roughly 40k tokens at the reference's 3-bytes-per-token
// divisor — large enough that leaving it in the count is unmistakable.
func bigToolOutput() string {
	return strings.Repeat("line of code from read_file\n", 4000)
}

// compactedModel is a model holding `toolSteps` tool blocks of large output
// and a two-message conversation, ready to compact.
func compactedModel(t *testing.T, toolSteps int) *RootModel {
	t.Helper()
	m := mkCompactNoProgModel(t, &fakeStreamClient{summary: "compacted summary line."})
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120
	for i := 0; i < toolSteps; i++ {
		m.conversation.blocks = append(m.conversation.blocks, transcript.Block{
			Steps: []toolsteps.Step{
				toolsteps.NewToolStep("read_file", []byte(`{"path":"/a.go"}`), bigToolOutput(), false),
			},
		})
	}
	m.setSessionMessages([]backend.Message{
		{Role: "user", Content: "old prompt"},
		{Role: "assistant", Content: "old reply"},
	})
	return m
}

// TestManualCompactDropsTheCompactedToolTokens pins fix 1: after /compact the
// panel must stop counting the tool output the summary replaced.
func TestManualCompactDropsTheCompactedToolTokens(t *testing.T) {
	m := compactedModel(t, 3)

	before := m.contextState()
	if before.Tool <= 0 {
		t.Fatalf("Tool before compaction = %d, want the tool output counted", before.Tool)
	}
	totalBefore := before.TotalUsed()

	handled, _ := m.handleSlashCommand("/compact")
	if !handled {
		t.Fatal("/compact should be handled")
	}
	waitForCompactSettle(t, m, 2*time.Second)

	after := m.contextState()
	if after.Tool != 0 {
		t.Errorf("Tool after /compact = %d, want 0 — the summary replaced those results", after.Tool)
	}
	if after.TotalUsed() >= totalBefore/2 {
		t.Errorf("TotalUsed after /compact = %d, want well below the pre-compaction %d",
			after.TotalUsed(), totalBefore)
	}
	// The same compaction must keep the checkpoint buffer, which is what
	// /context reports in place of the compressed turns.
	if after.CheckpointBuffer <= 0 {
		t.Errorf("CheckpointBuffer after /compact = %d, want > 0", after.CheckpointBuffer)
	}
}

// TestInTurnCompactionKeepsTheTurnsLaterTools pins fix 3: a compaction that
// lands mid-turn must not swallow the tools the turn ran afterwards.
func TestInTurnCompactionKeepsTheTurnsLaterTools(t *testing.T) {
	m := &RootModel{}
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120
	m.conversation.streaming = true

	pre := toolsteps.NewToolStep("read_file", []byte(`{"path":"/pre.go"}`), bigToolOutput(), false)
	m.conversation.liveSteps = []toolsteps.Step{pre}

	// The automatic checkpoint lands here.
	m.updateCompactEndMsg(compactEndMsg{
		Reason:               "threshold",
		EndedAt:              time.Now(),
		Summary:              "summary",
		Resume:               compaction.ResumingBlock([]string{"old prompt"}, "summary"),
		Steps:                1,
		TokensBefore:         40000,
		EstimatedTokensAfter: 1100,
	})

	// The turn carries on, runs one more tool, and ends.
	post := toolsteps.NewToolStep("read_file", []byte(`{"path":"/post.go"}`), bigToolOutput(), false)
	m.conversation.liveSteps = append(m.conversation.liveSteps, post)
	steps := append([]toolsteps.Step(nil), m.conversation.liveSteps...)
	m.Update(toolsteps.TurnDoneMsg{Reply: "done", Steps: steps})

	st := m.contextState()
	if st.Tool <= 0 {
		t.Fatalf("Tool after the turn = %d, want the post-compaction tool counted", st.Tool)
	}
	// Exactly one tool's worth: the pre-compaction one is replaced by the
	// summary, the post-compaction one is live context.
	oneTool := contextpanel.ToolResultTokens(post.Result)
	if st.Tool != oneTool {
		t.Errorf("Tool after the turn = %d, want %d (only the post-compaction tool)", st.Tool, oneTool)
	}
}

// TestCompactionBannerCountsToolResults pins fix 2: the banner's "before"
// figure counts the tool results the model actually carried, so it describes
// the same conversation /context does instead of a smaller one.
func TestCompactionBannerCountsToolResults(t *testing.T) {
	m := compactedModel(t, 1)
	panelBefore := m.contextState()

	handled, _ := m.handleSlashCommand("/compact")
	if !handled {
		t.Fatal("/compact should be handled")
	}
	waitForCompactSettle(t, m, 2*time.Second)

	// The banner's before figure is the transcript estimate plus the base
	// context plus the tool results — never less than the tool row alone.
	overhead := compaction.ContextOverheadTokens(m.systemPrompt(), modelDeclarationsJSON())
	transcriptTokens := compaction.TranscriptTokens([]backend.Message{
		{Role: "user", Content: "old prompt"},
		{Role: "assistant", Content: "old reply"},
	})
	wantBefore := overhead + transcriptTokens + panelBefore.Tool
	want := "context " + compaction.TokenMagnitude(wantBefore) + " →"
	if !responseContains(m, want) {
		t.Errorf("banner missing %q (transcript %d + overhead %d + tools %d):\n%q",
			want, transcriptTokens, overhead, panelBefore.Tool, m.conversation.response)
	}
	// And it is larger than the transcript alone — the old figure.
	if transcriptOnly := "context " + compaction.TokenMagnitude(overhead+transcriptTokens) + " →"; responseContains(m, transcriptOnly) {
		t.Errorf("banner still reports the tool-less figure %q:\n%q", transcriptOnly, m.conversation.response)
	}
}

// TestFallbackCountsAreMarkedEstimated pins fix 4: a row filled from a
// built-in figure says so, so a number that never moves is not read as a
// measurement.
func TestFallbackCountsAreMarkedEstimated(t *testing.T) {
	m := &RootModel{session: sessionState{model: "Gemini 3.8 Flash (High)"}}
	m.viewport.width = 120
	m.setSessionMessages([]backend.Message{{Role: "user", Content: "hello"}})

	st := m.contextState()
	if !st.SystemMessagesEstimated {
		t.Fatalf("SystemMessagesEstimated = false; the run mode measured no system messages")
	}
	// The mark must be a span, not a character: the reference pins this
	// panel's layout byte for byte, so a "~" would shift every cell after it.
	// The visible width of the row is what has to stay put.
	// The mark must be a span, not a character: the reference pins this
	// panel's layout byte for byte, so a "~" would shift every cell after it.
	// Check the row itself — the file list below carries "~" in home paths.
	var row string
	for _, l := range contextpanel.Lines(st) {
		if strings.Contains(l, "System messages:") {
			row = l
			break
		}
	}
	if row == "" {
		t.Fatalf("no System messages row in the panel")
	}
	if strings.Contains(row, "~") {
		t.Errorf("the estimated row inserts a character; the reference's layout is pinned byte for byte:\n%q", row)
	}
	if !strings.Contains(row, palette.SubtleOpen+contextpanel.TokenLabel(st.SystemMessages)) {
		t.Errorf("the estimated count is not marked with its own span:\n%q", row)
	}

	// A measured count is not marked.
	m.setSessionMessages([]backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "system", Content: "<SYSTEM_MESSAGE>task finished</SYSTEM_MESSAGE>"},
	})
	st = m.contextState()
	if st.SystemMessagesEstimated {
		t.Errorf("SystemMessagesEstimated = true, want false — the count was measured")
	}
}

// TestCompactionBoundaryDoesNotLeakAcrossConversations pins the boundary's
// lifetime: it counts steps of *this* conversation, so a new or resumed
// conversation must start at zero.
//
// Without the reset the boundary over-counts the next conversation's steps
// and its tools silently vanish from /context — the same class of bug the
// boundary exists to fix, in the opposite direction.
func TestCompactionBoundaryDoesNotLeakAcrossConversations(t *testing.T) {
	m := compactedModel(t, 3)
	if _, _ = m.handleSlashCommand("/compact"); true {
		waitForCompactSettle(t, m, 2*time.Second)
	}
	if m.conversation.compactedToolSteps == 0 {
		t.Fatalf("the /compact did not record a boundary; the test cannot proceed")
	}

	// A fresh conversation: different blocks, no compaction, same model.
	m.conversation.blocks = []transcript.Block{
		{Steps: []toolsteps.Step{
			toolsteps.NewToolStep("read_file", []byte(`{"path":"/fresh.go"}`), bigToolOutput(), false),
		}},
	}
	m.session.store.CompactJustHappened = false
	m.session.store.CompactTokensAfter = 0
	m.session.store.CompactTokensBefore = 0
	m.setSessionMessages([]backend.Message{{Role: "user", Content: "a new question"}})

	// Seeding a conversation is the path every switch goes through.
	m.seedConversation(&convstore.Conversation{ID: "fresh"})
	if m.conversation.compactedToolSteps != 0 {
		t.Errorf("compactedToolSteps = %d after seeding a new conversation, want 0",
			m.conversation.compactedToolSteps)
	}
}
