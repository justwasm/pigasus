package plugins_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/plugins"
)

func TestPluginsFacade(t *testing.T) {
	if plugins.WatcherPtr == nil {
		t.Fatal("expected non-nil WatcherPtr")
	}
	names := plugins.InstalledNames()
	if names == nil {
		t.Fatal("expected non-nil InstalledNames map")
	}
}
