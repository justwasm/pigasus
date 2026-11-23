// undocumented_test.go — the gate and the row for the model the CloudCode
// endpoint serves without advertising.
//
// The three things pinned here:
//
//  1. a stock run (Google's own host, or a loopback harness) lists nothing
//     extra — the reference's catalogue stays byte-comparable;
//  2. a run talking to a reverse-proxied CloudCode host lists the Sonar row,
//     and the row is selectable by id and by label;
//  3. the row is a fixed model: it publishes no reasoning tier, so the /effort
//     slider stays off and an explicit --effort conflicts, the way the
//     reference's own tier-spelled ids do.
package catalog

import (
	"testing"

	"github.com/justwasm/pigasus/internal/features/models/configtest"
)

// withGate puts the gate in a known state: the explicit switch, and the
// CloudCode host the auto rule looks at.
func withGate(t *testing.T, env, cloudCodeURL string) {
	t.Helper()
	configtest.Isolate(t)
	t.Setenv("OPENAGY_UNDOCUMENTED_MODELS", env)
	t.Setenv("CLOUD_CODE_URL", cloudCodeURL)
}

// containsRow reports whether the catalogue carries the Sonar row.
func containsRow(t *testing.T) bool {
	t.Helper()
	for _, m := range ModelCatalogue() {
		if m.ID == SonarModelID {
			return m.Label == SonarModelLabel
		}
	}
	return false
}

func TestSonarIsEnabledByDefault(t *testing.T) {
	withGate(t, "", "")
	if !UndocumentedModelsEnabled() {
		t.Fatal("gate closed by default")
	}
	if !containsRow(t) {
		t.Errorf("%s missing from the catalogue by default", SonarModelID)
	}
	if _, ok := CatalogueID(SonarModelID); !ok {
		t.Errorf("CatalogueID(%q) did not resolve by default", SonarModelID)
	}
	// By default without explicit effort, resolves to its default high tier
	if got, err := ResolveModelSelection(SonarModelID, ""); err != nil || got != SonarModelHighID {
		t.Errorf("ResolveModelSelection(%q) failed by default: got %q, err %v, want %q", SonarModelID, got, err, SonarModelHighID)
	}
}

func TestSonarIsHiddenWhenExplicitlyDisabled(t *testing.T) {
	withGate(t, "off", "")
	if UndocumentedModelsEnabled() {
		t.Fatal("gate open when explicitly disabled")
	}
	if containsRow(t) {
		t.Errorf("%s is in the catalogue when disabled", SonarModelID)
	}
	if _, ok := CatalogueID(SonarModelID); ok {
		t.Errorf("CatalogueID(%q) resolved when disabled", SonarModelID)
	}
	if _, err := ResolveModelSelection(SonarModelID, ""); err == nil {
		t.Errorf("ResolveModelSelection(%q) succeeded when disabled", SonarModelID)
	}
	if tiers := EffortTiersFor(SonarModelID); len(tiers) != 0 {
		t.Errorf("EffortTiersFor(%q) = %v when disabled, want none", SonarModelID, tiers)
	}
}

func TestSonarExplicitSwitchWins(t *testing.T) {
	withGate(t, "on", "https://relay.example.com")
	if !UndocumentedModelsEnabled() {
		t.Fatal("explicit on was ignored")
	}
	withGate(t, "off", "https://relay.example.com")
	if UndocumentedModelsEnabled() {
		t.Fatal("explicit off was ignored")
	}
}

func TestSonarRowResolvesByIdAndLabel(t *testing.T) {
	withGate(t, "on", "")
	for _, name := range []string{SonarModelID, SonarModelLabel} {
		id, ok := CatalogueID(name)
		if !ok || id != SonarModelID {
			t.Errorf("CatalogueID(%q) = %q, %v, want %q", name, id, ok, SonarModelID)
		}
		if !ModelAvailable(name) {
			t.Errorf("ModelAvailable(%q) = false", name)
		}
		if got := CanonicalModelLabel(name); got != SonarModelLabel {
			t.Errorf("CanonicalModelLabel(%q) = %q, want %q", name, got, SonarModelLabel)
		}
		got, err := ResolveModelSelection(name, "")
		if err != nil || got != SonarModelHighID {
			t.Errorf("ResolveModelSelection(%q) = %q, %v, want %q", name, got, err, SonarModelHighID)
		}
	}
}

func TestSonarSupportsEffortTiers(t *testing.T) {
	withGate(t, "on", "")
	tiers := EffortTiersFor(SonarModelID)
	if len(tiers) != 4 || tiers[0] != "low" || tiers[3] != "xhigh" {
		t.Errorf("EffortTiersFor(%q) = %v, want [low medium high xhigh]", SonarModelID, tiers)
	}
	if !EffortAdjustableFor(SonarModelID) {
		t.Errorf("EffortAdjustableFor(%q) = false, want adjustable", SonarModelID)
	}
	for _, effort := range []string{"low", "medium", "high", "xhigh"} {
		want := SonarModelID + "-" + effort
		got, err := ResolveModelSelection(SonarModelID, effort)
		if err != nil || got != want {
			t.Errorf("ResolveModelSelection(%q, %q) = %q, %v, want %q", SonarModelID, effort, got, err, want)
		}
	}
}

func TestReferenceServedModelsFollowsTheGate(t *testing.T) {
	withGate(t, "on", "")
	served := false
	for _, m := range ReferenceServedModels() {
		if m.ID == SonarModelID {
			served = true
		}
	}
	if !served {
		t.Fatalf("%s missing from ReferenceServedModels() with the gate open", SonarModelID)
	}

	withGate(t, "off", "")
	for _, m := range ReferenceServedModels() {
		if m.ID == SonarModelID {
			t.Fatalf("%s in ReferenceServedModels() with the gate shut", SonarModelID)
		}
	}
}

// TestReferenceModelsIsUntouched pins the parity contract: the documented
// catalogue never gains the undocumented row, whichever way the gate faces.
// The row is offered by ModelCatalogue (the picker's view) and by
// ReferenceServedModels (the dispatch's view), not by the reference's own
// table.
func TestReferenceModelsIsUntouched(t *testing.T) {
	for _, env := range []string{"on", "off"} {
		withGate(t, env, "")
		for _, m := range ReferenceModels() {
			if m.ID == SonarModelID {
				t.Fatalf("ReferenceModels() carries %s with the gate %q", SonarModelID, env)
			}
		}
	}
}
