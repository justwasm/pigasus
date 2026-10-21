package cli

import (
	"os"
	"testing"
)

// TestClaude55Available_PinsGating pins the AGY 1.3.6 changelog line 1
// contract: Claude Opus 5.5 and Claude Sonnet 5.5 appear in /model only
// when AGY_ADC_AUTH=true *and* AGY_CLAUDE55_PROJECT is set. The matrix
// below covers every combination the recovered shell should handle.
//
// Each case uses t.Setenv so the env mutation is restored at the end of
// the subtest — Go's testing package guarantees Setenv's restoration even
// on t.Fatal, so two cases that touch the same var cannot leak between
// each other when run in parallel.
func TestClaude55Available_PinsGating(t *testing.T) {
	cases := []struct {
		name          string
		adcAuth       string // value of AGY_ADC_AUTH; "" leaves it unset
		claude55Proj  string // value of AGY_CLAUDE55_PROJECT; "" leaves it unset
		wantAvailable bool
	}{
		// Both set: gate is open. This is the matrix the changelog
		// intends — ADC enabled and the project allowlisted.
		{
			name:          "adc_true_with_project",
			adcAuth:       "true",
			claude55Proj:  "test-project",
			wantAvailable: true,
		},
		// Case-insensitive on AGY_ADC_AUTH. Real-world shells sometimes
		// produce "True" or "TRUE"; the upstream's grpc metadata
		// header normalises to lowercase before the rollout check.
		{
			name:          "adc_TRUE_uppercase_with_project",
			adcAuth:       "TRUE",
			claude55Proj:  "prod-claude-55",
			wantAvailable: true,
		},
		// AGY_ADC_AUTH missing: gate closes regardless of project.
		{
			name:          "adc_unset_with_project",
			claude55Proj:  "test-project",
			wantAvailable: false,
		},
		// AGY_ADC_AUTH explicitly "false" (not just unset): gate closes.
		{
			name:          "adc_false_with_project",
			adcAuth:       "false",
			claude55Proj:  "test-project",
			wantAvailable: false,
		},
		// AGY_CLAUDE55_PROJECT missing: gate closes even if ADC says true.
		// This is the second half of the changelog's "AND" clause.
		{
			name:          "adc_true_no_project",
			adcAuth:       "true",
			wantAvailable: false,
		},
		// AGY_CLAUDE55_PROJECT explicitly empty: same as missing.
		{
			name:          "adc_true_empty_project",
			adcAuth:       "true",
			claude55Proj:  "   ", // whitespace-only also rejected
			wantAvailable: false,
		},
		// AGY_ADC_AUTH="1" — the 1.3.3 cycle accepts this form for the
		// remote-control gate, but the 1.3.6 catalogue gate is stricter
		// (changelog pin: "AGY_ADC_AUTH=true" literally). Documenting
		// that difference as a test pin keeps the divergence visible.
		{
			name:          "adc_one_with_project_strict_gate",
			adcAuth:       "1",
			claude55Proj:  "test-project",
			wantAvailable: false,
		},
		// Both unset: gate closes (the default state — the test surface
		// for the "no env vars" branch).
		{
			name:          "both_unset",
			wantAvailable: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.adcAuth != "" {
				t.Setenv("AGY_ADC_AUTH", tc.adcAuth)
			} else {
				// Clear explicitly so a t.Setenv from a parent test
				// (or a leakage from another package's test) cannot
				// sneak through. t.Setenv to "" on an unset key is a
				// no-op; os.Unsetenv is the explicit clear.
				t.Setenv("AGY_ADC_AUTH", "")
				os.Unsetenv("AGY_ADC_AUTH")
			}
			if tc.claude55Proj != "" {
				t.Setenv("AGY_CLAUDE55_PROJECT", tc.claude55Proj)
			} else {
				t.Setenv("AGY_CLAUDE55_PROJECT", "")
				os.Unsetenv("AGY_CLAUDE55_PROJECT")
			}

			got := Claude55Available()
			if got != tc.wantAvailable {
				t.Errorf("Claude55Available(AGY_ADC_AUTH=%q, AGY_CLAUDE55_PROJECT=%q) = %v, want %v",
					tc.adcAuth, tc.claude55Proj, got, tc.wantAvailable)
			}
		})
	}
}

// TestClaude55Available_DefaultFalse documents the env-unset default.
// The test isolates from any process-level env leakage by clearing
// both vars explicitly at the start.
func TestClaude55Available_DefaultFalse(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "")
	os.Unsetenv("AGY_ADC_AUTH")
	t.Setenv("AGY_CLAUDE55_PROJECT", "")
	os.Unsetenv("AGY_CLAUDE55_PROJECT")
	if Claude55Available() {
		t.Fatal("Claude55Available() = true with both env vars unset; want false (default state)")
	}
}
