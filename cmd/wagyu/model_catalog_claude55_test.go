package main

import (
	"os"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/cli"
)

// hasModel returns whether the supplied id appears in `cli.ReferenceModels()`.
// Used by the three Claude 5.5 catalogue tests below — the "appears in /model"
// contract from the 1.3.6 changelog is a property of the catalogue, not the
// picker (the picker reads from the same catalogue via modelRowsView).
func hasModel(t *testing.T, id string) bool {
	t.Helper()
	for _, m := range cli.ReferenceModels() {
		if m.ID == id {
			return true
		}
	}
	return false
}

// resetClaude55Env clears AGY_ADC_AUTH and AGY_CLAUDE55_PROJECT in the
// current process. The 1.3.6 catalogue gate depends on both, so a
// single test that forgets to clear them would leak across tests in
// the same package. The explicit os.Unsetenv is a belt-and-braces
// against any test that uses t.Setenv("…", "") — t.Setenv restores
// the original value at end-of-test but does not actively unset.
func resetClaude55Env(t *testing.T) {
	t.Helper()
	t.Setenv("AGY_ADC_AUTH", "")
	os.Unsetenv("AGY_ADC_AUTH")
	t.Setenv("AGY_CLAUDE55_PROJECT", "")
	os.Unsetenv("AGY_CLAUDE55_PROJECT")
}

// TestModelCatalog_Claude55Present_ADCTrue pins the AGY 1.3.6 changelog
// line 1 happy path: AGY_ADC_AUTH=true AND AGY_CLAUDE55_PROJECT set ⇒
// both Opus 5.5 and Sonnet 5.5 appear in `agy models` / `/model`.
func TestModelCatalog_Claude55Present_ADCTrue(t *testing.T) {
	resetClaude55Env(t)
	t.Setenv("AGY_ADC_AUTH", "true")
	t.Setenv("AGY_CLAUDE55_PROJECT", "test-project")

	if !cli.Claude55Available() {
		t.Fatal("claude55Available() = false under ADC=true+project; want true")
	}
	for _, id := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
		if !hasModel(t, id) {
			t.Errorf("model %q absent from cli.ReferenceModels(); want present", id)
		}
	}

	// Pickup test: the /model picker must include both rows too.
	picker := modelRowsView()
	var seenOpus, seenSonnet bool
	for _, r := range picker {
		switch r.slug {
		case "claude-opus-5-5":
			seenOpus = true
			if r.display != "Claude Opus 5.5" {
				t.Errorf("claude-opus-5-5 picker display = %q, want %q", r.display, "Claude Opus 5.5")
			}
		case "claude-sonnet-5-5":
			seenSonnet = true
			if r.display != "Claude Sonnet 5.5" {
				t.Errorf("claude-sonnet-5-5 picker display = %q, want %q", r.display, "Claude Sonnet 5.5")
			}
		}
	}
	if !seenOpus {
		t.Errorf("claude-opus-5-5 not in modelRowsView() under ADC=true+project")
	}
	if !seenSonnet {
		t.Errorf("claude-sonnet-5-5 not in modelRowsView() under ADC=true+project")
	}
}

// TestModelCatalog_Claude55Absent_ADCMissing pins the contract's
// negative cases for the AND clause of the changelog: if AGY_ADC_AUTH
// or AGY_CLAUDE55_PROJECT is missing, both 5.5 entries are hidden.
// The test is split into two subtests so a failure points at which
// half of the AND broke.
func TestModelCatalog_Claude55Absent_ADCMissing(t *testing.T) {
	t.Run("adc_unset", func(t *testing.T) {
		resetClaude55Env(t)
		// Leave both unset explicitly.
		if cli.Claude55Available() {
			t.Fatal("claude55Available() = true with both env vars unset; want false")
		}
		for _, id := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
			if hasModel(t, id) {
				t.Errorf("model %q present in cli.ReferenceModels(); want absent (ADC unset)", id)
			}
		}
		for _, r := range modelRowsView() {
			if r.slug == "claude-opus-5-5" || r.slug == "claude-sonnet-5-5" {
				t.Errorf("model %q present in modelRowsView(); want absent (ADC unset)", r.slug)
			}
		}
	})

	t.Run("project_unset", func(t *testing.T) {
		resetClaude55Env(t)
		t.Setenv("AGY_ADC_AUTH", "true")
		// AGY_CLAUDE55_PROJECT left unset.
		if cli.Claude55Available() {
			t.Fatal("claude55Available() = true with project unset; want false")
		}
		for _, id := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
			if hasModel(t, id) {
				t.Errorf("model %q present in cli.ReferenceModels(); want absent (project unset)", id)
			}
		}
		for _, r := range modelRowsView() {
			if r.slug == "claude-opus-5-5" || r.slug == "claude-sonnet-5-5" {
				t.Errorf("model %q present in modelRowsView(); want absent (project unset)", r.slug)
			}
		}
	})
}

// TestModelCatalog_Claude55Absent_ADCFalse pins the contract's
// negative case for the AGY_ADC_AUTH leg: when the operator sets
// AGY_ADC_AUTH to a non-"true" value (here, "false"), the catalogue
// hides both 5.5 entries. This is the divergence from the cycle-ADC
// 1.3.3 gate (which accepts "1" / "true" / any-non-empty); the 1.3.6
// catalogue gate is stricter and accepts only the literal "true".
func TestModelCatalog_Claude55Absent_ADCFalse(t *testing.T) {
	resetClaude55Env(t)
	t.Setenv("AGY_ADC_AUTH", "false")
	t.Setenv("AGY_CLAUDE55_PROJECT", "test-project")
	if cli.Claude55Available() {
		t.Fatal("claude55Available() = true with AGY_ADC_AUTH=false; want false (strict gate)")
	}
	for _, id := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
		if hasModel(t, id) {
			t.Errorf("model %q present in cli.ReferenceModels(); want absent (ADC=false)", id)
		}
	}
	for _, r := range modelRowsView() {
		if r.slug == "claude-opus-5-5" || r.slug == "claude-sonnet-5-5" {
			t.Errorf("model %q present in modelRowsView(); want absent (ADC=false)", r.slug)
		}
	}

	// And the "Available models:" block used by the unknown-model
	// error path must not mention either 5.5 entry either — this is
	// the user-visible surface for the contract.
	got := availableModelsBlock()
	if strings.Contains(got, "Claude Opus 5.5") || strings.Contains(got, "Claude Sonnet 5.5") {
		t.Errorf("availableModelsBlock() leaks 5.5 entries under ADC=false:\n%s", got)
	}
}
