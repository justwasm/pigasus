package seasonal_test

import (
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/seasonal"
)

func TestSeasonalFacade(t *testing.T) {
	if seasonal.OptInKey != "seasonalThemesOptIn" {
		t.Fatalf("unexpected OptInKey: %s", seasonal.OptInKey)
	}
	ev := seasonal.Halloween(2026)
	if ev == nil || ev.ID != "halloween-2026" {
		t.Fatalf("unexpected event: %v", ev)
	}
	_ = seasonal.Enabled("halloween-2026", time.Now())
}
