package tokenfmt

import (
	"math"
	"testing"
)

func TestTokenAlignment_Basic(t *testing.T) {
	reconciler := NewTokenReconciler(3.0)

	// Local estimate: 300 chars / 3 = 100 tokens
	est := reconciler.Estimate(300)
	if est != 100 {
		t.Fatalf("expected estimated 100, got %d", est)
	}

	// Provider reports 105 input tokens, 20 output tokens, 0 cached tokens
	align := reconciler.Align(est, 105, 20, 0)
	if align.DriftTokens != 5 {
		t.Errorf("expected drift 5, got %d", align.DriftTokens)
	}
	if math.Abs(align.DriftRatio-(5.0/105.0)) > 1e-4 {
		t.Errorf("drift ratio mismatch: got %f", align.DriftRatio)
	}
	if align.IsDistorted {
		t.Errorf("expected not distorted at 4.7%% drift")
	}

	// Verify formatting with K
	wantFormatted := "105 (est 100, drift +4.8%)"
	if align.Formatted != wantFormatted {
		t.Errorf("formatted mismatch:\n got: %q\nwant: %q", align.Formatted, wantFormatted)
	}
}

func TestTokenAlignment_CachedTokens(t *testing.T) {
	reconciler := NewTokenReconciler(3.0)

	// Long prompt: 600,000 chars -> est 200 K tokens
	est := reconciler.Estimate(600_000)
	if est != 200_000 {
		t.Fatalf("expected 200000, got %d", est)
	}

	// Reported: 210,000 total input tokens, with 160,000 cached tokens
	align := reconciler.Align(est, 210_000, 1500, 160_000)
	if align.ReportedCachedTokens != 160_000 {
		t.Errorf("cached tokens mismatch: %d", align.ReportedCachedTokens)
	}
	if align.ActiveInputTokens != 50_000 {
		t.Errorf("active input mismatch: %d, want 50000", align.ActiveInputTokens)
	}

	formatted := FormatAlignment(align)
	if formatted != "210 K (est 200 K, cached 160 K, drift +4.8%)" {
		t.Errorf("unexpected formatted output: %q", formatted)
	}
}

func TestTokenAlignment_Calibration_200Turns(t *testing.T) {
	// Simulate 200+ turns where the model tokenizer has a divisor of ~3.6 bytes/token
	// instead of the naive 3.0 assumption.
	reconciler := NewTokenReconciler(3.0)

	for turn := 1; turn <= 200; turn++ {
		chars := 3600 + (turn%20)*100
		actualTokens := int(float64(chars) / 3.6) // True ground truth from CloudCode

		// Local estimate before calibration of this turn
		estimated := reconciler.Estimate(chars)

		// Align against provider report
		align := reconciler.Align(estimated, actualTokens, 150, 0)

		// Calibrate with observation
		reconciler.Calibrate(chars, actualTokens)

		if turn > 50 {
			// After 50 turns of adaptation, drift ratio should be small (< 10%)
			if math.Abs(align.DriftRatio) > 0.10 {
				t.Errorf("turn %d: drift ratio %f exceeds 10%% post-calibration", turn, align.DriftRatio)
			}
		}
	}

	estTotal, actTotal, _, _, driftRatio := reconciler.CumulativeStats()
	if actTotal == 0 {
		t.Fatalf("no actual tokens accumulated")
	}

	// Across 200 turns, cumulative drift should be well controlled (< 6%)
	if math.Abs(driftRatio) > 0.06 {
		t.Errorf("cumulative drift ratio too high: %f (est=%d, act=%d)", driftRatio, estTotal, actTotal)
	}
}

func TestTokenAlignment_DistortionDetection(t *testing.T) {
	reconciler := NewTokenReconciler(3.0)
	reconciler.SetDistortionThreshold(0.20) // 20%

	// Normal turn: 10% drift
	normal := reconciler.Align(100, 110, 10, 0)
	if normal.IsDistorted {
		t.Errorf("expected 10%% drift not to be distorted")
	}

	// Distorted turn: 35% drift
	distorted := reconciler.Align(100, 154, 10, 0)
	if !distorted.IsDistorted {
		t.Errorf("expected 35%% drift to be flagged as distorted")
	}
}
