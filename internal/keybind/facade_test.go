package keybind_test

import (
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/keybind"
)

func TestKeybindFacade(t *testing.T) {
	if keybind.EscReleaseWindow != 50*time.Millisecond {
		t.Fatalf("unexpected EscReleaseWindow: %v", keybind.EscReleaseWindow)
	}
	p := keybind.DefaultPath()
	if p == "" {
		t.Fatal("expected non-empty DefaultPath")
	}
}
