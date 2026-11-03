package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// Cycle 306 — the binary holds a second variant of four model-facing tool
// descriptions. The clone ships the default one (the text a live session
// sends); the alternates are evidence.
//
// The addresses in the fixture were re-derived against agy 1.4.8 in cycle 359:
// the texts are byte-identical to the 1.4.7 capture, only their addresses
// moved.
//
// The test pins both halves: the shipped declaration must be the default, and
// the alternates must be the reference's own bytes. The half that compares a
// variant with the *shipped declaration* lives with the declarations —
// internal/app/tools/surface.

// The alternate texts are the reference's bytes. The lengths are pinned
// because they are what the extraction reports, and the text itself is
// checked for the clause that distinguishes it from the default.
func TestAlternateVariantsAreTheReferencesBytes(t *testing.T) {
	for name, want := range map[string]struct {
		length int
		marker string
	}{
		"run_command":      {499, "Instead, set NotificationTimeoutSeconds to be notified"},
		"send_message":     {164, "Recipient='@children'"},
		"manage_subagents": {388, "'list_peers'"},
		"edit_file":        {656, "set Edits to a list of {Target, Replacement} pairs"},
	} {
		variant, ok := ReferenceVariants()[name]
		if !ok {
			t.Errorf("%s has no recorded variant", name)
			continue
		}
		if got := len(variant.AlternateText); got != want.length {
			t.Errorf("%s alternate = %d B, want %d B", name, got, want.length)
		}
		if variant.AlternateLength != want.length {
			t.Errorf("%s alternate_length = %d, want %d", name, variant.AlternateLength, want.length)
		}
		if !strings.Contains(variant.AlternateText, want.marker) {
			t.Errorf("%s alternate does not carry %q", name, want.marker)
		}
	}
}

// The variants fixture's own shape: four tools, each with a default and an
// alternate, and every length self-consistent.
func TestVariantsFixtureShape(t *testing.T) {
	variants := ReferenceVariants()
	if len(variants) != 4 {
		t.Errorf("the fixture holds %d variants, want 4", len(variants))
	}
	for name, v := range variants {
		if v.DefaultLength == 0 || v.AlternateLength == 0 {
			t.Errorf("%s: a length is zero", name)
		}
		if len(v.DefaultVMAddr) == 0 || len(v.AlternateVMAddr) == 0 {
			t.Errorf("%s: no provenance address", name)
		}
	}
}

// The descriptions fixture is embedded and parsed; a malformed variant entry
// would be a silent data loss.
func TestVariantsFixtureParses(t *testing.T) {
	var doc struct {
		CapturedAt string                     `json:"captured_at"`
		Source     string                     `json:"source"`
		Variants   map[string]json.RawMessage `json:"variants"`
	}
	if err := json.Unmarshal(referenceVariantsJSON, &doc); err != nil {
		t.Fatalf("parse the variants fixture: %v", err)
	}
	if doc.CapturedAt != "1.4.8" {
		t.Errorf("captured_at = %q, want 1.4.8", doc.CapturedAt)
	}
	if doc.Source == "" {
		t.Error("the fixture carries no source text")
	}
}
