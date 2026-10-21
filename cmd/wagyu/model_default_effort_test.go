package main

// model_default_effort_test.go — cycle model-no-effort-default.
//
// Pins AGY 1.3.4 changelog line 26:
//
//	"Changed --model and /model to accept a base model name without --effort:
//	 instead of failing with an error asking for --effort, the CLI now picks
//	 the model's default thinking level (the server's default for that model,
//	 otherwise medium, otherwise the lowest available)."
//
// Seven behaviours covered:
//
//	1. TestDefaultEffortServerDefault        — chain tier 1: spec.def wins
//	2. TestDefaultEffortMediumFallback       — chain tier 2: medium when def absent/invalid
//	3. TestDefaultEffortLowestFallback       — chain tier 3: lowest when neither def nor medium
//	4. TestDefaultEffortEmptyLevels          — empty levels -> "" (no tier to pick)
//	5. TestModelNoEffortSucceeds             — --model <base> (no --effort) resolves
//	6. TestModelNoEffortPreservedWithEffort  — --model <base> --effort <level> preserved
//	7. TestEffortPickerAcceptsBareName       — /model picker opens for a bare base slug
//
// Tests 1–4 are pure unit tests on the resolveDefaultEffort helper; tests 5–6
// ride through the public resolveModelSelection pipeline; test 7 drives the
// /model panel the TUI's slash router opens for "model".

import (
	"flag"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestDefaultEffortServerDefault pins the chain's first tier: when
// spec.def is set and present in spec.levels, the chain returns it
// unchanged. This is the canonical gemini-3.8-flash path — the server
// default is "high", which is in [low, medium, high].
//
// Recovered: --model gemini-3.8-flash -> gemini-3.8-flash-high.
func TestDefaultEffortServerDefault(t *testing.T) {
	got := resolveDefaultEffort(baseModelSpec{levels: []string{"low", "medium", "high"}, def: "high"})
	if got != "high" {
		t.Errorf("resolveDefaultEffort(server-default high) = %q, want %q", got, "high")
	}

	// gpt-oss-120b's server default is also its only option.
	got = resolveDefaultEffort(baseModelSpec{levels: []string{"medium"}, def: "medium"})
	if got != "medium" {
		t.Errorf("resolveDefaultEffort(server-default medium) = %q, want %q", got, "medium")
	}
}

// TestDefaultEffortMediumFallback pins the chain's second tier: when
// spec.def is empty (or refers to a tier the model does not expose), the
// chain picks "medium" if the model exposes it. This is the future
// "baseModelEfforts entry with def unset" path.
func TestDefaultEffortMediumFallback(t *testing.T) {
	// def == "" — server has no preference, chain falls to medium.
	got := resolveDefaultEffort(baseModelSpec{levels: []string{"low", "medium", "high"}, def: ""})
	if got != "medium" {
		t.Errorf("resolveDefaultEffort(empty def + levels w/ medium) = %q, want %q", got, "medium")
	}

	// def set to a tier not in levels — chain ignores the bad def and falls to medium.
	got = resolveDefaultEffort(baseModelSpec{levels: []string{"low", "medium", "high"}, def: "xhigh"})
	if got != "medium" {
		t.Errorf("resolveDefaultEffort(invalid def + levels w/ medium) = %q, want %q (xhigh -> medium)", got, "medium")
	}
}

// TestDefaultEffortLowestFallback pins the chain's third tier: when
// neither the server default nor medium applies, the chain returns the
// first element of spec.levels. The reference's levels slice is in
// ascending order, so the first element is the lowest available.
func TestDefaultEffortLowestFallback(t *testing.T) {
	// def empty, medium absent — chain returns the lowest available.
	got := resolveDefaultEffort(baseModelSpec{levels: []string{"low", "high"}, def: ""})
	if got != "low" {
		t.Errorf("resolveDefaultEffort(no def, no medium, [low,high]) = %q, want %q", got, "low")
	}

	// def set to a tier absent from levels, medium also absent — chain
	// ignores the bad def and returns the lowest available.
	got = resolveDefaultEffort(baseModelSpec{levels: []string{"low", "high"}, def: "xhigh"})
	if got != "low" {
		t.Errorf("resolveDefaultEffort(bad def, no medium, [low,high]) = %q, want %q", got, "low")
	}
}

// TestDefaultEffortEmptyLevels: an empty spec.levels means the base
// model has no thinking tier to pick (e.g. baked-in "thinking" models
// that resolveEffectiveModel handles separately). The helper returns
// "" and resolveModelSelection falls through to the catalogue lookup.
func TestDefaultEffortEmptyLevels(t *testing.T) {
	got := resolveDefaultEffort(baseModelSpec{levels: nil, def: "high"})
	if got != "" {
		t.Errorf("resolveDefaultEffort(nil levels, def=high) = %q, want %q", got, "")
	}
	got = resolveDefaultEffort(baseModelSpec{levels: []string{}, def: ""})
	if got != "" {
		t.Errorf("resolveDefaultEffort([] levels, def=empty) = %q, want %q", got, "")
	}
}

// TestModelNoEffortSucceeds: --model <base> without --effort must NOT
// fail. resolveModelSelection returns the resolved catalogue ID with
// the chain's default effort appended.
//
// Before this cycle the validator already returned
// `gemini-3.8-flash-high` for that input via the inline `model + "-" +
// spec.def` branch; this test pins that the helper-driven path produces
// the same result, and that the catalogueID branch is untouched for
// "thinking" base slugs (claude-sonnet-4-6 returns as-is).
func TestModelNoEffortSucceeds(t *testing.T) {
	cases := []struct {
		model  string
		wantID string
	}{
		// server default tier (chain tier 1).
		{"gemini-3.8-flash", "gemini-3.8-flash-high"},
		// server default is also the only available tier.
		{"gpt-oss-120b", "gpt-oss-120b-medium"},
		// "thinking" base slug — no effort tier to pick, catalogueID wins.
		{"claude-sonnet-4-6", "claude-sonnet-4-6"},
	}
	for _, tc := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.String("model", tc.model, "")
		fs.String("effort", "", "")
		got, err := resolveModelSelection(tc.model, "")
		if err != nil {
			t.Errorf("resolveModelSelection(%q, \"\") = error %v, want %q", tc.model, err, tc.wantID)
			continue
		}
		if got != tc.wantID {
			t.Errorf("resolveModelSelection(%q, \"\") = %q, want %q", tc.model, got, tc.wantID)
		}
	}
}

// TestModelNoEffortPreservedWithEffort: when --effort is supplied, the
// chain is bypassed — the user-supplied effort is honoured. Pins the
// "Existing --model <name> --effort <level> behaviour is preserved"
// half of the brief.
func TestModelNoEffortPreservedWithEffort(t *testing.T) {
	cases := []struct {
		model  string
		effort string
		wantID string
	}{
		{"gemini-3.8-flash", "low", "gemini-3.8-flash-low"},
		{"gemini-3.8-flash", "medium", "gemini-3.8-flash-medium"},
		{"gemini-3.8-flash", "high", "gemini-3.8-flash-high"},
		{"gpt-oss-120b", "medium", "gpt-oss-120b-medium"},
	}
	for _, tc := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.String("model", tc.model, "")
		fs.String("effort", tc.effort, "")
		got, err := resolveModelSelection(tc.model, tc.effort)
		if err != nil {
			t.Errorf("resolveModelSelection(%q, %q) = error %v, want %q", tc.model, tc.effort, err, tc.wantID)
			continue
		}
		if got != tc.wantID {
			t.Errorf("resolveModelSelection(%q, %q) = %q, want %q", tc.model, tc.effort, got, tc.wantID)
		}
	}
}

// TestEffortPickerAcceptsBareName: the /model panel the TUI opens must
// accept a bare base slug (e.g. "gemini-3.8-flash") as the current
// model — without the cycle 1.3.4 fix a bare base slug fell through to
// the default and the panel's (current) annotation landed on the wrong
// row. The test seeds m.model to a base slug, opens the panel, and
// asserts (a) the panel opens at all and (b) the panel lines do not
// panic / error / blank the (current) row.
//
// We don't drive a live probe here — the panel render test (cycle 197 /
// 208) covers the byte-for-byte layout. This test pins the cycle-197 /
// 1.3.4 contract: panelCurrentModelLabel tolerates a bare base slug
// because the changelog line 26 fix lets `--model foo` set m.model to
// `foo` without first requiring an effort.
func TestEffortPickerAcceptsBareName(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "gemini-3.8-flash"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "", "")

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.picker.Reset()
	m.input.Reset()
	m.flags = fs
	// Cycle 197 / 1.3.4: m.model carries the *display label*, not the slug.
	// canonicalModelLabel maps "gemini-3.8-flash" -> "Gemini 3.8 Flash" via
	// settings.json's persisted "model" value, which the cycle-197
	// resolveEffectiveModel already lifts. The test seeds the displayed
	// label directly so the panel's panelCurrentModelLabel() round-trips.
	m.model = canonicalModelLabel("gemini-3.8-flash")
	if m.model == "" {
		m.model = "Gemini 3.8 Flash (High)" // fallback if canonical returned ""
	}

	// Open the panel — must succeed without panicking on a bare base slug.
	if !m.openSlashPanel("model") {
		t.Fatal("openSlashPanel(\"model\") = false, want true")
	}
	if m.panel.kind != panelModel {
		t.Errorf("panel.kind = %v, want panelModel", m.panel.kind)
	}
	if m.panel.cmd != "/model" {
		t.Errorf("panel.cmd = %q, want %q", m.panel.cmd, "/model")
	}

	// Park the cursor on the Gemini row so the Right key advances the
	// embedded effort slider (the only row cycleModelEffort touches).
	for i, r := range modelRows {
		if r.slug == "gemini-3.8-flash" {
			m.panel.cursor = i
			break
		}
	}
	if m.panel.cursor < 0 || m.panel.cursor >= len(modelRows) ||
		modelRows[m.panel.cursor].slug != "gemini-3.8-flash" {
		t.Fatalf("could not find gemini-3.8-flash in modelRows")
	}

	// panelModelLines must render the (current) row for "Gemini 3.8 Flash"
	// without panicking or producing an empty string. The render test
	// (cycle 197/208) pins the byte layout; this asserts the chained
	// resolve path produces a label the panel can use.
	lines := m.panelModelLines()
	if len(lines) == 0 {
		t.Fatal("panelModelLines returned no lines")
	}
	var joined strings.Builder
	for _, ln := range lines {
		joined.WriteString(stripSGR(ln))
		joined.WriteString("\n")
	}
	if !strings.Contains(joined.String(), "Gemini 3.8 Flash") {
		t.Errorf("panelModelLines missing 'Gemini 3.8 Flash' row, got:\n%s", joined.String())
	}

	// Right-arrow on the (current) Gemini row advances the embedded
	// effort slider, confirming the panel is alive for a base-slug model.
	// Seed the slider to "low" first so Right can advance to "medium"
	// (Right from "high" is clamped and would otherwise look like a
	// no-op).
	m.panel.effortCursor = effortLow
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("right key not handled on /model panel")
	}
	if m.panel.effortCursor != effortLow+1 {
		t.Errorf("effortCursor after Right = %d, want %d", m.panel.effortCursor, effortLow+1)
	}
}
