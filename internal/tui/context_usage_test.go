package tui

import (
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
)

func TestContextUsedPercent_MultiTurnDoesNotBalloon(t *testing.T) {
	m := &runMode{
		session: sessionState{model: "gpt-oss-120b"},
	}

	// Turn 1: 10,000 prompt tokens (window is 131,072 for gpt-oss-120b)
	m.recordTurnUsage(&backend.ChunkMeta{PromptTokens: 10000, CompletionTokens: 500})
	pct1 := m.contextUsage()
	if pct1 <= 0 || pct1 > 15 {
		t.Fatalf("turn 1 pct = %v, want ~7.6%%", pct1)
	}

	// Simulate 30 turns each with ~10,000 prompt tokens.
	// Total SessionPromptTokens would reach ~310,000 (which would be ~236% if accumulated).
	// But LastTurnPromptTokens should stay at 10,000 (~7.6%).
	for i := 0; i < 30; i++ {
		m.recordTurnUsage(&backend.ChunkMeta{PromptTokens: 10000, CompletionTokens: 500})
	}

	pct30 := m.contextUsage()
	if pct30 > 15 {
		t.Fatalf("turn 30 pct = %v, expected ~7.6%% from latest turn, got ballooned percentage", pct30)
	}

	// Status row default: 0 (no percent in status row, matching reference agy 1.4.7/1.5.0)
	s := m.statusRowState()
	if s.ContextUsedPercent != 0 {
		t.Errorf("statusRowState().ContextUsedPercent = %v, want 0 by default", s.ContextUsedPercent)
	}

	// Opt-in via environment variable
	t.Setenv("PIGASUS_STATUS_SHOW_CONTEXT", "1")
	sOpt := m.statusRowState()
	if sOpt.ContextUsedPercent <= 0 || sOpt.ContextUsedPercent > 15 {
		t.Errorf("opt-in statusRowState().ContextUsedPercent = %v, want ~7.6%%", sOpt.ContextUsedPercent)
	}
}
