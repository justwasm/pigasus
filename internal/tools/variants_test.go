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
// the alternates must be the reference's own bytes.

func TestShippedDeclarationIsTheReferenceDefaultVariant(t *testing.T) {
	decls := map[string]ModelToolDecl{}
	for _, d := range ModelDeclarations() {
		decls[d.Name] = d
	}
	for name, variant := range ReferenceVariants() {
		decl, ok := decls[name]
		if !ok {
			t.Errorf("%s has a variant but is not a declared tool", name)
			continue
		}
		if got := len(decl.Description); got != variant.DefaultLength {
			t.Errorf("%s declaration description = %d B, want the default variant (%d B)",
				name, got, variant.DefaultLength)
		}
	}
}

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

// The alternate must be a *different* text from the shipped one, and the
// shipped one must be the default. Otherwise the fixture could quietly
// describe the variant the clone does not send.
func TestAlternateDiffersFromTheShippedDeclaration(t *testing.T) {
	decls := map[string]ModelToolDecl{}
	for _, d := range ModelDeclarations() {
		decls[d.Name] = d
	}
	for name, variant := range ReferenceVariants() {
		decl, ok := decls[name]
		if !ok {
			continue
		}
		if decl.Description == variant.AlternateText {
			t.Errorf("%s: the clone ships the alternate variant, not the default", name)
		}
	}
}

// The catalogue's descriptions come from the descriptions fixture, which now
// carries the default variant. This test makes the two fixtures agree: the
// catalogue must not advertise a text the reference does not send.
func TestCatalogueDescriptionsAreTheDefaultVariant(t *testing.T) {
	reg := Catalog(t.TempDir())
	for name, variant := range ReferenceVariants() {
		tool, ok := reg.Get(name)
		if !ok {
			continue // edit_file is model-facing, not an advertised name
		}
		if got := Describe(tool); got == variant.AlternateText {
			t.Errorf("%s: the catalogue advertises the alternate variant", name)
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

// The default variant of a tool whose text the catalogue carries must be the
// text in the descriptions fixture — the two fixtures are two views of the
// same surface and must not drift.
func TestDefaultVariantAgreesWithTheDescriptionsFixture(t *testing.T) {
	descriptions := ReferenceDescriptions()
	decls := map[string]string{}
	for _, d := range ModelDeclarations() {
		decls[d.Name] = d.Description
	}
	for name := range ReferenceVariants() {
		if name == "run_command" {
			// run_command's converter text is what the catalogue
			// mirrors; its model-facing default lives in the
			// fixture's model_facing field.
			continue
		}
		desc, ok := descriptions[name]
		if !ok {
			// edit_file is model-facing but not an advertised name, so
			// the descriptions fixture does not carry it.
			continue
		}
		if len(desc.Text) != len(decls[name]) {
			t.Errorf("%s: the descriptions fixture (%d B) and the declaration (%d B) disagree",
				name, len(desc.Text), len(decls[name]))
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
