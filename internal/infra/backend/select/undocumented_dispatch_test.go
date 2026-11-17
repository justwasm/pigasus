// undocumented_dispatch_test.go — the dispatch half of the Sonar row.
//
// A model the picker offered must reach the transport that serves it. The
// Sonar id is not in the reference's catalogue, so the record lookup that
// decides "reference model or configured model" has to know it: without that,
// the dispatch would look for a configured endpoint, find none, and answer
// from the shim — a working-looking turn from a model nothing ran.
package backendselect

import (
	"flag"
	"testing"

	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/features/models/configtest"
	"github.com/justwasm/pigasus/internal/infra/backend/cloudcode"
)

func sonarGate(t *testing.T, env string) {
	t.Helper()
	configtest.Isolate(t)
	t.Setenv("OPENAGY_UNDOCUMENTED_MODELS", env)
	t.Setenv("CLOUD_CODE_URL", "")
	// The reference transport needs a credential to exist at all; without
	// one the dispatch answers the shim before it asks which model this is.
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	t.Setenv("OPENAGY_CONFIG", t.TempDir()+"/config.json")
}

func pickFor(t *testing.T, model string) bool {
	t.Helper()
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("backend", "", "")
	client := Pick(fs, model, noCatalogueLookup)
	if client == nil {
		t.Fatalf("Pick(%q) returned no client", model)
	}
	_, ok := accountrouting.Unwrap(client).(*cloudcode.CloudCodeClient)
	return ok
}

func TestSonarDispatchesToCloudCode(t *testing.T) {
	sonarGate(t, "on")
	if !IsReferenceModel(catalog.SonarModelID, noCatalogueLookup) {
		t.Fatalf("IsReferenceModel(%q) = false with the gate open", catalog.SonarModelID)
	}
	if !ModelSupported(catalog.SonarModelID, noCatalogueLookup) {
		t.Fatalf("ModelSupported(%q) = false with the gate open", catalog.SonarModelID)
	}
	if !pickFor(t, catalog.SonarModelID) {
		t.Errorf("Pick(%q) did not reach the CloudCode transport", catalog.SonarModelID)
	}
	// The display label takes the same path — the picker hands over the
	// label when the row is chosen, not the slug.
	if !pickFor(t, catalog.SonarModelLabel) {
		t.Errorf("Pick(%q) did not reach the CloudCode transport", catalog.SonarModelLabel)
	}
}

func TestSonarIsNotReferenceServedWithTheGateShut(t *testing.T) {
	sonarGate(t, "off")
	if IsReferenceModel(catalog.SonarModelID, noCatalogueLookup) {
		t.Fatalf("IsReferenceModel(%q) = true with the gate shut", catalog.SonarModelID)
	}
	if pickFor(t, catalog.SonarModelID) {
		t.Errorf("Pick(%q) reached cloudcode with the gate shut", catalog.SonarModelID)
	}
}

// TestSonarSelectionStopsAtTheCatalogue keeps the gate honest: with it shut
// the id is not a model at all, so a settings.json left behind by an earlier
// reverse-proxied run is reported as unavailable rather than sent to a
// transport that never heard of it.
func TestSonarSelectionStopsAtTheCatalogue(t *testing.T) {
	sonarGate(t, "off")
	if _, ok := catalog.CatalogueID(catalog.SonarModelID); ok {
		t.Fatalf("CatalogueID(%q) resolved with the gate shut", catalog.SonarModelID)
	}
	if _, err := catalog.ResolveModelSelection(catalog.SonarModelID, ""); err == nil {
		t.Errorf("ResolveModelSelection(%q) succeeded with the gate shut", catalog.SonarModelID)
	}
}
