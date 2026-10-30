package modelctx

import (
	"testing"
)

func TestFacadeModelCtx(t *testing.T) {
	lim := For("claude-3-5-sonnet")
	if lim.ContextWindow <= 0 {
		t.Fatalf("expected positive context window, got %d", lim.ContextWindow)
	}
	win := ContextWindowFor("claude-3-5-sonnet")
	if win != lim.ContextWindow {
		t.Fatalf("mismatch: ContextWindowFor = %d, lim.ContextWindow = %d", win, lim.ContextWindow)
	}
	p, ok := ProfileFor("gemini-2.5-flash")
	if !ok || p.MaxTokenLimit <= 0 {
		t.Fatalf("expected profile for flash, got ok=%v, p=%+v", ok, p)
	}
	cfg := Config{MaxTokenLimit: 300000, MaxOutputTokens: 20000}
	clamped := Clamp(cfg)
	if clamped.MaxTokenLimit > MaxCheckpointTokenLimit {
		t.Fatalf("expected clamped token limit <= %d, got %d", MaxCheckpointTokenLimit, clamped.MaxTokenLimit)
	}
}
