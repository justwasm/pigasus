package compaction

import (
	"strings"
	"testing"
)

func TestComputeCompactTokens(t *testing.T) {
	// Baseline with overhead
	summary := strings.Repeat("word ", 20) // ~100 chars -> ~33 tokens
	before, after, sumTok := ComputeCompactTokens(500, 0, 100, 0, 50, summary, 0)
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
	before, after, sumTok = ComputeCompactTokens(500, 1200, 100, 40, 50, summary, 0)
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

// TestComputeCompactTokens_CountsTheCarriedContent pins the 1.4.10
// rule: the after-compaction size counts what the summary carries into
// the next prompt, and the "(N summary)" label still shows the summary
// alone.
func TestComputeCompactTokens_CountsTheCarriedContent(t *testing.T) {
	const summary = "<summary>done</summary>"
	const carried = 700

	_, after, sumTok := ComputeCompactTokens(500, 0, 100, 0, 50, summary, carried)
	if sumTok != EstimateTokens(summary) {
		t.Errorf("the summary figure = %d, want %d — the label shows the summary alone",
			sumTok, EstimateTokens(summary))
	}
	if want := 50 + sumTok + carried; after != want {
		t.Errorf("after = %d, want %d — the carried content counts too", after, want)
	}
}

// TestComputeCompactTokens_NoOverheadKeepsTheReportedAfter pins the
// other branch: without a base-context estimate the model-reported
// figure is the one shown.
func TestComputeCompactTokens_NoOverheadKeepsTheReportedAfter(t *testing.T) {
	_, after, _ := ComputeCompactTokens(500, 0, 100, 0, 0, "s", 700)
	if after != 100 {
		t.Errorf("after = %d, want 100", after)
	}
}
