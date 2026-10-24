package compaction

import (
	"strings"
	"testing"
)

func TestComputeCompactTokens(t *testing.T) {
	// Baseline with overhead
	summary := strings.Repeat("word ", 20) // ~100 chars -> ~33 tokens
	before, after, sumTok := ComputeCompactTokens(500, 0, 100, 0, 50, summary)
	if before != 550 {
		t.Errorf("before = %d, want 550", before)
	}
	if sumTok <= 0 {
		t.Errorf("sumTok should be > 0, got %d", sumTok)
	}
	if after != 50+sumTok {
		t.Errorf("after = %d, want %d", after, 50+sumTok)
	}

	// Explicit input / output tokens override
	before, after, sumTok = ComputeCompactTokens(500, 1200, 100, 40, 50, summary)
	if before != 1200 {
		t.Errorf("before override = %d, want 1200", before)
	}
	if sumTok != 40 {
		t.Errorf("sumTok override = %d, want 40", sumTok)
	}
	if after != 50+40 {
		t.Errorf("after with overhead = %d, want 90", after)
	}
}
