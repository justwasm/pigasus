package tui

import (
	"testing"
	"time"
)

// TestDisplayModel_OwnsLifecycleFlags pins the ownership move: the quit
// flag, the two-step exit confirmation and the display-only overrides
// hang off DisplayModel.
func TestDisplayModel_OwnsLifecycleFlags(t *testing.T) {
	var d DisplayModel
	if d.IsQuitting() {
		t.Fatal("zero model is quitting")
	}
	d.SetQuitting(true)
	if !d.IsQuitting() {
		t.Fatal("SetQuitting did not stick")
	}

	armed := time.Now()
	d.exitConfirm = true
	d.exitConfirmAt = armed
	if !d.exitConfirm || !d.exitConfirmAt.Equal(armed) {
		t.Fatal("exit confirmation fields did not stick")
	}
	d.verbosity = "high"
	d.quotaFrame = 3
	d.streamingSearch = true
	if d.verbosity != "high" || d.quotaFrame != 3 || !d.streamingSearch {
		t.Fatal("display overrides did not stick")
	}
}
