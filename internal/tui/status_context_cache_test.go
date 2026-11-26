package tui

import (
	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/models/configtest"
	"github.com/justwasm/pigasus/internal/features/models/modelctx"
	"testing"
)

func TestStatusContextSnapshotRefreshesOnProviderSave(t *testing.T) {
	configtest.Isolate(t)
	m := seedRootModel()
	for _, window := range []int{128000, 256000} {
		cfg := map[string]models.ProviderConfig{"fixture": {Type: "openai", Models: []models.ProviderModel{{ID: "cpu-context", Model: "cpu-context", ContextWindow: window}}}}
		if err := models.SaveProviders(models.ProvidersPath(), cfg); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 100; i++ {
			if got := m.statusContextWindow("cpu-context"); got != window {
				t.Fatalf("window=%d want %d", got, window)
			}
		}
	}
	if got := m.statusContextWindow("gemini-3.8-flash-high"); got != modelctx.ContextWindowFor("gemini-3.8-flash-high") {
		t.Fatal("switching model reused stale context window")
	}
}
