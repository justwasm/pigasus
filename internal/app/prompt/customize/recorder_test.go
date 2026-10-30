package customize

import (
	"strings"
	"testing"
)

// TestRecorderTrimTriggersCustomize verifies the end-to-end
// bridge between the recorder and the internal/customize trimmer:
// a recorded /customize body is the first to be trimmed when the
// budget is tight. The bridge must not swallow or re-prioritise any
// token — the package's priority table is the source of truth.
func TestRecorderTrimTriggersCustomize(t *testing.T) {
	ResetRecorder()
	t.Cleanup(ResetRecorder)

	big := strings.Repeat("a", 4000) // 1000 tokens each
	RecordToken("config", big)
	RecordToken("agents", big)
	RecordToken("skills", big)
	// budget: capacity = 1000 tokens (one body fits).
	budget := NewBudget(200_000, 50_000, 149_000)

	kept, dropped := TrimTokens(budget)
	if len(kept) != 1 {
		t.Fatalf("Trim kept %d, want 1", len(kept))
	}
	// Highest priority is /skills (60); /config (10) and /agents
	// (20) are both lower and trimmed. /skills wins.
	if kept[0].Name != "skills" {
		t.Errorf("kept[0] = %q, want %q (highest priority survives)", kept[0].Name, "skills")
	}
	if len(dropped) != 2 {
		t.Fatalf("Dropped = %d, want 2", len(dropped))
	}
	if dropped[0].Name != "config" {
		t.Errorf("dropped[0] = %q, want %q (lower priority trimmed first)", dropped[0].Name, "config")
	}
	if dropped[1].Name != "agents" {
		t.Errorf("dropped[1] = %q, want %q", dropped[1].Name, "agents")
	}
}

// TestRecorderEmptyContributionsIgnored verifies the bridge's
// no-op-on-empty contract: a recorder call with an empty body
// must not allocate a token, so a /config handler that renders
// "" (the "no settings" case) does not pollute the trimmer with
// a zero-cost placeholder.
func TestRecorderEmptyContributionsIgnored(t *testing.T) {
	ResetRecorder()
	t.Cleanup(ResetRecorder)

	RecordToken("customize", "")
	RecordToken("config", "")
	if got := SnapshotTokens(); len(got) != 0 {
		t.Errorf("snapshot = %d tokens, want 0 (empty contributions are no-ops)", len(got))
	}
	kept, dropped := TrimTokens(DefaultBudget())
	if kept != nil || dropped != nil {
		t.Errorf("Trim on empty recorder returned kept=%v dropped=%v, want nil", kept, dropped)
	}
}

// TestRecorderReplaysAcrossTrims verifies the bridge does not
// mutate the recorded list on Trim: a later Trim sees every
// token the live session has contributed, not just the last few.
// This is the property the context-assembly step relies on —
// every Send pays a snapshot cost, never a re-record cost.
func TestRecorderReplaysAcrossTrims(t *testing.T) {
	ResetRecorder()
	t.Cleanup(ResetRecorder)

	RecordToken("config", "body-1")
	RecordToken("skills", "body-2")
	// First Trim with a tight budget.
	budget := NewBudget(200_000, 50_000, 150_000) // capacity = 0
	if _, dropped := TrimTokens(budget); len(dropped) != 2 {
		t.Fatalf("first Trim dropped %d, want 2", len(dropped))
	}
	// Second Trim with a generous budget — both tokens survive.
	budget = DefaultBudget()
	kept, dropped := TrimTokens(budget)
	if len(kept) != 2 {
		t.Fatalf("second Trim kept %d, want 2", len(kept))
	}
	if len(dropped) != 0 {
		t.Errorf("second Trim dropped %d, want 0", len(dropped))
	}
}
