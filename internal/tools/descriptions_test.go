package tools

import (
	"runtime"
	"strings"
	"testing"
)

// The catalogue used to carry a hand-written paraphrase for every advertised
// tool. These tests pin the descriptions to the reference's own bytes.

// TestCatalogueDescriptionsComeFromTheReference pins every advertised tool's
// description against the fixture extracted from the 1.4.8 binary.
//
// Two tools are special: `run_command`'s text is the reference's *converter*
// layer and is rendered per host (the fixture holds the darwin rendering),
// and `view_file`'s text is the reference's converter layer too, held in the
// source as a constant the executor also uses.
func TestCatalogueDescriptionsComeFromTheReference(t *testing.T) {
	reg := Catalog(t.TempDir())
	descriptions := ReferenceDescriptions()
	for name, desc := range descriptions {
		if !desc.Complete {
			// run_workflow: only fragments were recovered.
			continue
		}
		tool, ok := reg.Get(name)
		if !ok {
			t.Errorf("%s is described but not advertised", name)
			continue
		}
		got := Describe(tool)
		switch name {
		case "run_command":
			if runtime.GOOS != "darwin" {
				// The fixture is the darwin rendering; the clone
				// renders its own host's operating system and
				// shell.
				if !strings.Contains(got, "PROPOSE a command to run on behalf of the user.") {
					t.Errorf("run_command does not carry the reference's converter text")
				}
				continue
			}
		}
		if got != desc.Text {
			t.Errorf("%s description is not the reference's text\n got: %q\nwant: %q", name, got, desc.Text)
		}
	}
}

// TestDescriptionsFixtureCoversAdvertisedTools pins the two halves of the
// catalogue: the thirty-seven advertised names the reference describes
// (thirty-six of them whole) and the twenty-three it describes nowhere.
//
// Cycle 311 moved one name across the line: `browser_subagent`'s
// description lives in the reference's built-in agents package, so it is the
// reference's text after all.
func TestDescriptionsFixtureCoversAdvertisedTools(t *testing.T) {
	reg := Catalog(t.TempDir())
	advertised := reg.List()
	descriptions := ReferenceDescriptions()

	for name := range descriptions {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("the fixture describes %q, which the catalogue does not advertise", name)
		}
	}
	undescribed := 0
	for _, name := range advertised {
		if _, ok := descriptions[name]; !ok {
			undescribed++
		}
	}
	if want := 23; undescribed != want {
		t.Errorf("%d advertised tools carry no reference text, want %d", undescribed, want)
	}
	if len(descriptions) != 37 {
		t.Errorf("the fixture holds %d descriptions, want 37", len(descriptions))
	}
}

// TestModelFacingTextsAgreeWithTheDeclarationFixture cross-checks the
// fixture's model-facing texts against the declaration capture the clone
// sends (model_tools_1.4.6.json).
//
// Eight of the twelve declared tools have a model-facing text in the
// descriptions fixture. Six of them are byte-identical to the 1.4.6
// declaration the clone ships — which is how the extraction was validated.
// The other two are the tools the reference changed between 1.4.6 and 1.4.7;
// the declaration fixture has not been re-captured, so the difference is
// pinned here rather than hidden.
func TestModelFacingTextsAgreeWithTheDeclarationFixture(t *testing.T) {
	declarations := map[string]string{}
	for _, decl := range ModelDeclarations() {
		declarations[decl.Name] = decl.Description
	}
	descriptions := ReferenceDescriptions()
	changed := map[string]bool{
		"manage_subagents": true,
		"send_message":     true,
	}
	checked := 0
	for name, desc := range descriptions {
		if desc.Layer != "model-facing (runtime)" {
			continue
		}
		want, ok := declarations[name]
		if !ok {
			t.Errorf("%s has a model-facing text but is not a declared tool", name)
			continue
		}
		checked++
		if desc.Text == want {
			continue
		}
		if !changed[name] {
			t.Errorf("%s model-facing text differs from the declaration fixture\n got: %q\nwant: %q",
				name, desc.Text, want)
		}
	}
	if checked != 8 {
		t.Errorf("cross-checked %d model-facing texts, want 8", checked)
	}
}

// TestRunCommandDescriptionIsByteExact pins the converter text the clone
// renders. It is the reference's own template (a string literal at file
// offset 0x36cb4c0 — vmaddr 0x1036cb4c0 — of the 1.4.8 binary) with the
// operating system, the shell and the two prose parts substituted.
func TestRunCommandDescriptionIsByteExact(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the reference capture was taken on darwin")
	}
	desc := ReferenceDescriptions()["run_command"]
	if got := runCommandDescription(); got != desc.Text {
		t.Errorf("run_command description is not the reference's bytes\n got: %q\nwant: %q", got, desc.Text)
	}
}
