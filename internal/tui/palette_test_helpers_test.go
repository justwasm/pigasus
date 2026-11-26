package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/palette"
)

// restorePalette keeps tests that exercise CLI startup from leaking their
// process-global color scheme into later rendering assertions.
func restorePalette(t *testing.T) {
	t.Helper()
	previous, _ := palette.CurrentScheme()
	t.Cleanup(func() { palette.SetScheme(previous) })
}

func TestRestorePaletteRestoresPreviousScheme(t *testing.T) {
	previous, _ := palette.CurrentScheme()
	if !palette.SetScheme("dark") {
		t.Fatal("dark scheme is unavailable")
	}
	t.Cleanup(func() { palette.SetScheme(previous) })

	t.Run("scoped change", func(t *testing.T) {
		restorePalette(t)
		if !palette.SetScheme("solarized light") {
			t.Fatal("solarized light scheme is unavailable")
		}
	})

	outerHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", outerHome)
	t.Run("CLI startup", func(t *testing.T) { TestRunEntrypoint_PassesArgs(t) })
	if _, err := os.Stat(filepath.Join(outerHome, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("CLI startup leaked settings into caller's home: %v", err)
	}

	got, _ := palette.CurrentScheme()
	if got != "dark" {
		t.Fatalf("scheme after scoped test = %q, want dark", got)
	}
	t.Run("render after startup", TestBashBlockTruncates)
}
