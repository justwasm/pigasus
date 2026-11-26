package modelpicker

import (
	"reflect"
	"testing"

	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/features/models/configtest"
)

func TestEffortSnapshotPreservesCatalogueAndSaveInvalidation(t *testing.T) {
	configtest.Isolate(t)
	for _, name := range []string{"", "gemini-3.8-flash-high", "Gemini 3.8 Flash (High)", "unknown"} {
		if HasEffortTiersSnapshot(name) != HasEffortTiers(name) {
			t.Fatalf("snapshot mismatch for %q", name)
		}
	}
	if !reflect.DeepEqual(catalog.ModelCatalogueSnapshot(), catalog.ModelCatalogue()) {
		t.Fatal("reference catalogue snapshot differs")
	}
	providers := map[string]models.ProviderConfig{"test": {Type: "openai", Models: []models.ProviderModel{{ID: "cpu-fixture", Name: "CPU Fixture", Model: "cpu-fixture"}}}}
	if err := models.SaveProviders(models.ProvidersPath(), providers); err != nil {
		t.Fatal(err)
	}
	if HasEffortTiersSnapshot("CPU Fixture") {
		t.Fatal("saved configured model not immediately reflected in status tiers")
	}
	if !reflect.DeepEqual(catalog.ModelCatalogueSnapshot(), catalog.ModelCatalogue()) {
		t.Fatal("saved catalogue snapshot differs")
	}
	if err := models.SaveProviders(models.ProvidersPath(), map[string]models.ProviderConfig{}); err != nil {
		t.Fatal(err)
	}
	if !HasEffortTiersSnapshot("CPU Fixture") {
		t.Fatal("removed model remained in snapshot after save")
	}
}
