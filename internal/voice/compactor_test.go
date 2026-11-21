package voice

import (
	"strings"
	"testing"
)

func TestVoiceHistoryCompactor_SlidingWindowAndCompaction(t *testing.T) {
	compactor := NewVoiceHistoryCompactor(
		WithKeepTail(2),
		WithCompactThreshold(4),
	)

	// Turn 1-3: below threshold (4) -> no summary
	compactor.AddTurn("User", "Hello there")
	compactor.AddTurn("Agent", "Hi! How can I assist you today?")
	compactor.AddTurn("User", "Can you explain quantum computing?")

	if compactor.Summary() != "" {
		t.Errorf("expected no summary before threshold, got: %q", compactor.Summary())
	}
	if len(compactor.AllTurns()) != 3 {
		t.Errorf("expected 3 turns total, got %d", len(compactor.AllTurns()))
	}
	if len(compactor.ActiveTail()) != 2 {
		t.Errorf("expected 2 turns in tail, got %d", len(compactor.ActiveTail()))
	}

	// Turn 4: reaches threshold -> turns 1-2 compacted into summary, turns 3-4 remain in tail
	compactor.AddTurn("Agent", "Quantum computing leverages superposition and entanglement.")

	summary := compactor.Summary()
	if summary == "" {
		t.Fatalf("expected non-empty summary after hitting threshold")
	}
	if !strings.Contains(summary, "Hello there") {
		t.Errorf("expected summary to contain earlier turn, got: %q", summary)
	}

	tail := compactor.ActiveTail()
	if len(tail) != 2 {
		t.Fatalf("expected tail of size 2, got %d", len(tail))
	}
	if tail[0].Content != "Can you explain quantum computing?" {
		t.Errorf("tail[0] = %q, want 'Can you explain quantum computing?'", tail[0].Content)
	}
	if !strings.Contains(tail[1].Content, "Quantum computing leverages") {
		t.Errorf("tail[1] = %q, want quantum answer", tail[1].Content)
	}

	// Test FormatContext
	ctx := compactor.FormatContext("Custom Header")
	if !strings.Contains(ctx, "Custom Header") {
		t.Errorf("expected header in context: %s", ctx)
	}
	if !strings.Contains(ctx, "# Previous Conversation Summary:") {
		t.Errorf("expected summary section in context: %s", ctx)
	}
	if !strings.Contains(ctx, "# Recent Utterances:") {
		t.Errorf("expected recent utterances section in context: %s", ctx)
	}
}

func TestVoiceHistoryCompactor_FormatReconnectionAndResume(t *testing.T) {
	compactor := NewVoiceHistoryCompactor(
		WithKeepTail(2),
		WithCompactThreshold(3),
	)

	compactor.AddTurn("User", "Run tests for internal/voice")
	compactor.AddTurn("Agent", "Running tests now.")
	compactor.AddTurn("Agent", "All 12 tests passed.")

	reconCtx := compactor.FormatReconnectionContext()
	if !strings.Contains(reconCtx, "Network connection was briefly disrupted") {
		t.Errorf("reconnection context missing network note: %s", reconCtx)
	}
	if !strings.Contains(reconCtx, "All 12 tests passed.") {
		t.Errorf("reconnection context missing recent turn: %s", reconCtx)
	}

	resumeCtx := compactor.FormatResumeContext()
	if !strings.Contains(resumeCtx, "User has resumed a previous LiveChat voice session") {
		t.Errorf("resume context missing resume note: %s", resumeCtx)
	}
}

func TestVoiceHistoryCompactor_CustomSummarizer(t *testing.T) {
	called := false
	compactor := NewVoiceHistoryCompactor(
		WithKeepTail(1),
		WithCompactThreshold(2),
		WithSummarizer(func(turns []LiveTurnRecord) (string, error) {
			called = true
			return "CUSTOM_SUMMARY_STUB", nil
		}),
	)

	compactor.AddTurn("User", "First step")
	compactor.AddTurn("Agent", "Second step")

	if !called {
		t.Errorf("expected custom summarizer to be invoked")
	}
	if compactor.Summary() != "CUSTOM_SUMMARY_STUB" {
		t.Errorf("summary = %q, want 'CUSTOM_SUMMARY_STUB'", compactor.Summary())
	}
}
