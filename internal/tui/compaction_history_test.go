// compaction_history_test.go — one compaction is one history entry.
//
// The history exists so a reader can ask "why did my context shrink, and
// when" and get an answer that matches what happened. A recorder that counts
// one compaction twice does not merely show a wrong number: it shows a
// history whose length contradicts the transcript's own stats rows, and the
// reader has no way to tell which of the two is lying.
package tui

import (
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/core/compaction"
)

// TestOneCompactionIsOneHistoryEntry — the collector records the frame, and
// the handler that renders it does not record again.
//
// The two halves are the two places this used to happen: the engine recorded
// when the checkpoint policy reported the end, and the TUI recorded when the
// same event arrived as a frame. Both run in a local turn, so the history
// said "2 runs" for one compaction.
func TestOneCompactionIsOneHistoryEntry(t *testing.T) {
	compaction.ResetHistory()
	t.Cleanup(compaction.ResetHistory)

	m := newRootModel(nil, "", "", "", "", false)
	m.viewport.width, m.viewport.height = 120, 40
	m.session.store.CompactStart = time.Now()

	// The frame the engine emits for a finished compaction, as the collector
	// receives it.
	cm := &agentlink.Compact{
		Phase:           agentlink.CompactEnd,
		Reason:          "threshold",
		Summary:         "summarised turns 1-3",
		StepsSummarized: 3,
		TokensBefore:    100_000,
		TokensAfter:     20_000,
		ElapsedMS:       1_500,
	}

	c := &turnCollector{prog: nil}
	c.compactStart = time.Now().Add(-1500 * time.Millisecond)
	c.compact(cm)

	if got := len(compaction.History()); got != 1 {
		t.Fatalf("the frame recorded %d history entries, want 1", got)
	}

	// Now the handler that renders the same frame must not add a second.
	m.updateCompactEndMsg(compactEndMsg{
		Reason:               cm.Reason,
		EndedAt:              time.Now(),
		Summary:              cm.Summary,
		Steps:                cm.StepsSummarized,
		TokensBefore:         cm.TokensBefore,
		EstimatedTokensAfter: cm.TokensAfter,
	})
	if got := len(compaction.History()); got != 1 {
		t.Fatalf("rendering the same compaction made the history %d entries, want 1", got)
	}
}

// TestTheHistoryRecordsWhatTheFrameReported — the recorded numbers are the
// ones on the wire, not a re-derivation.
//
// The handler re-derives before/after token counts for its stats row (it adds
// the context overhead and the carried block). Those derived figures are
// right for the row and wrong for the history: the history's job is to say
// what the compaction did, so it keeps the reported figures.
func TestTheHistoryRecordsWhatTheFrameReported(t *testing.T) {
	compaction.ResetHistory()
	t.Cleanup(compaction.ResetHistory)

	c := &turnCollector{prog: nil}
	c.compactStart = time.Now().Add(-2 * time.Second)
	c.compact(&agentlink.Compact{
		Phase:           agentlink.CompactEnd,
		Reason:          "overflow",
		Summary:         "s",
		StepsSummarized: 7,
		TokensBefore:    150_000,
		TokensAfter:     30_000,
		ElapsedMS:       2_000,
	})

	hist := compaction.History()
	if len(hist) != 1 {
		t.Fatalf("history has %d entries, want 1", len(hist))
	}
	ev := hist[0]
	if ev.Reason != "overflow" {
		t.Errorf("reason = %q, want overflow", ev.Reason)
	}
	if ev.TokensBefore != 150_000 || ev.TokensAfter != 30_000 {
		t.Errorf("tokens = %d -> %d, want 150000 -> 30000", ev.TokensBefore, ev.TokensAfter)
	}
	if ev.StepsSummarized != 7 {
		t.Errorf("steps = %d, want 7", ev.StepsSummarized)
	}
	if ev.Duration != 2*time.Second {
		t.Errorf("duration = %v, want 2s", ev.Duration)
	}
	if ev.SavedTokens() != 120_000 {
		t.Errorf("saved = %d, want 120000", ev.SavedTokens())
	}
}

// TestAnAbortedCompactionIsNotRecorded — a compaction that was cancelled did
// not compact anything, and a history row claiming it did would be a lie
// about the context's size.
func TestAnAbortedCompactionIsNotRecorded(t *testing.T) {
	compaction.ResetHistory()
	t.Cleanup(compaction.ResetHistory)

	c := &turnCollector{prog: nil}
	c.compact(&agentlink.Compact{
		Phase:   agentlink.CompactEnd,
		Reason:  "threshold",
		Aborted: true,
	})
	if got := len(compaction.History()); got != 0 {
		t.Errorf("an aborted compaction recorded %d entries, want 0", got)
	}
}

// TestTheHistorySeesEveryCompactionWhateverTheBackend — the record point is
// the frame, which every backend reports, not the local checkpoint policy.
//
// A remote backend has no local policy to fire, so a record point inside the
// engine would silently miss every compaction on a remote session — the one
// case where a user most wants to know the context was rewritten under them.
func TestTheHistorySeesEveryCompactionWhateverTheBackend(t *testing.T) {
	compaction.ResetHistory()
	t.Cleanup(compaction.ResetHistory)

	// Two compactions, as a remote backend would report them: frames only,
	// no engine, no checkpoint policy.
	c := &turnCollector{prog: nil}
	for i := 0; i < 2; i++ {
		c.compact(&agentlink.Compact{
			Phase:        agentlink.CompactEnd,
			Reason:       "threshold",
			Summary:      "s",
			TokensBefore: 100_000,
			TokensAfter:  20_000,
		})
	}
	if got := len(compaction.History()); got != 2 {
		t.Fatalf("two framed compactions recorded %d entries, want 2", got)
	}
}
