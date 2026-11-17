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

func TestSonarIsHiddenOnAStockRun(t *testing.T) {
	withGate(t, "", "")
	if UndocumentedModelsEnabled() {
		t.Fatal("gate open on a stock run")
	}
	if containsRow(t) {
		t.Errorf("%s is in the catalogue on a stock run", SonarModelID)
	}
	if _, ok := CatalogueID(SonarModelID); ok {
		t.Errorf("CatalogueID(%q) resolved on a stock run", SonarModelID)
	}
	if _, err := ResolveModelSelection(SonarModelID, ""); err == nil {
		t.Errorf("ResolveModelSelection(%q) succeeded on a stock run", SonarModelID)
	}
}

func TestSonarFollowsTheReverseProxy(t *testing.T) {
	withGate(t, "", "https://relay.example.com")
	if !UndocumentedModelsEnabled() {
		t.Fatal("gate closed against a reverse-proxied CloudCode host")
	}
	if !containsRow(t) {
		t.Fatalf("%s missing from the catalogue against a reverse-proxied host", SonarModelID)
	}
}

func TestSonarStaysHiddenBehindALoopbackHarness(t *testing.T) {
	for _, host := range []string{
		"http://127.0.0.1:8080",
		"http://localhost:9999",
		"http://[::1]:1234",
	} {
		withGate(t, "", host)
		if UndocumentedModelsEnabled() {
			t.Errorf("gate open against loopback host %s", host)
		}
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
		if err != nil || got != SonarModelID {
			t.Errorf("ResolveModelSelection(%q) = %q, %v, want %q", name, got, err, SonarModelID)
		}
	}
}

func TestSonarIsAFixedModel(t *testing.T) {
	withGate(t, "on", "")
	if tiers := EffortTiersFor(SonarModelID); len(tiers) != 0 {
		t.Errorf("EffortTiersFor(%q) = %v, want none", SonarModelID, tiers)
	}
	if EffortAdjustableFor(SonarModelID) {
		t.Errorf("EffortAdjustableFor(%q) = true, want a fixed model", SonarModelID)
	}
	// The id carries its variant, so an explicit tier conflicts — the rule
	// the reference applies to a catalogue id with its tier spelled in.
	if _, err := ResolveModelSelection(SonarModelID, "high"); err == nil {
		t.Errorf("ResolveModelSelection(%q, high) succeeded, want a conflict", SonarModelID)
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
