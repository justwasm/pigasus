package pluginwatch

import (
	"testing"
)

func TestFacadePluginWatch(t *testing.T) {
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = w.Close()
}
