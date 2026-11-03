package surface

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/tools"
)

// Cycle 306 — the binary holds a second variant of four model-facing tool
// descriptions. The clone ships the default one (the text a live session
// sends); the alternates are evidence.
//
// The addresses in the fixture were re-derived against agy 1.4.8 in cycle 359:
// the texts are byte-identical to the 1.4.7 capture, only their addresses
// moved.
//
// These are the halves that compare a variant with what the clone *ships*:
// the declaration the model receives and the text the catalogue advertises.

func TestShippedDeclarationIsTheReferenceDefaultVariant(t *testing.T) {
	decls := map[string]ModelToolDecl{}
	for _, d := range ModelDeclarations() {
		decls[d.Name] = d
	}
	for name, variant := range tools.ReferenceVariants() {
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

// The alternate must be a *different* text from the shipped one, and the
// shipped one must be the default. Otherwise the fixture could quietly
// describe the variant the clone does not send.
func TestAlternateDiffersFromTheShippedDeclaration(t *testing.T) {
	decls := map[string]ModelToolDecl{}
	for _, d := range ModelDeclarations() {
		decls[d.Name] = d
	}
	for name, variant := range tools.ReferenceVariants() {
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
	for name, variant := range tools.ReferenceVariants() {
		tool, ok := reg.Get(name)
		if !ok {
			continue // edit_file is model-facing, not an advertised name
		}
		if got := tools.Describe(tool); got == variant.AlternateText {
			t.Errorf("%s: the catalogue advertises the alternate variant", name)
		}
	}
}

// The default variant of a tool whose text the catalogue carries must be the
// text in the descriptions fixture — the two fixtures are two views of the
// same surface and must not drift.
func TestDefaultVariantAgreesWithTheDescriptionsFixture(t *testing.T) {
	descriptions := tools.ReferenceDescriptions()
	decls := map[string]string{}
	for _, d := range ModelDeclarations() {
		decls[d.Name] = d.Description
	}
	for name := range tools.ReferenceVariants() {
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

// The reference's own fixture carries the variant's provenance; the surface
// reads it through the contract package's door.
func TestVariantsFixtureCarriesItsProvenance(t *testing.T) {
	source, defaultState := tools.ReferenceVariantsDoc()
	if source == "" {
		t.Error("the variants fixture carries no source text")
	}
	if defaultState == "" {
		t.Error("the variants fixture carries no default-state text")
	}
	if !strings.Contains(source, "1.4") {
		t.Errorf("the variants fixture names no reference version: %q", source)
	}
}
