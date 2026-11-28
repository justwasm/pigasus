package compaction

import (
	"testing"
	"time"
)

func TestCompactionHistory(t *testing.T) {
	ResetHistory()
	defer ResetHistory()

	if len(History()) != 0 {
		t.Fatalf("expected empty history, got %d", len(History()))
	}

	e1 := RecordEvent(Event{
		Reason:       "manual",
		TokensBefore: 100_000,
		TokensAfter:  2_000,
		Duration:     time.Second,
		Summary:      "Summarized turn 1-5",
	})
	if e1.Seq != 1 {
		t.Errorf("e1.Seq = %d, want 1", e1.Seq)
	}
	if e1.SavedTokens() != 98_000 {
		t.Errorf("SavedTokens = %d, want 98000", e1.SavedTokens())
	}
	if pct := e1.PercentReduced(); pct < 97.9 || pct > 98.1 {
		t.Errorf("PercentReduced = %f, want ~98.0", pct)
	}

	e2 := RecordEvent(Event{
		Reason:       "threshold",
		TokensBefore: 200_000,
		TokensAfter:  10_000,
	})
	if e2.Seq != 2 {
		t.Errorf("e2.Seq = %d, want 2", e2.Seq)
	}

	hist := History()
	if len(hist) != 2 {
		t.Fatalf("len(History()) = %d, want 2", len(hist))
	}
	if hist[0].Reason != "manual" || hist[1].Reason != "threshold" {
		t.Errorf("History entries mismatch: %+v", hist)
	}
}
