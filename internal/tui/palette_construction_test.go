package tui

import (
	"testing"

	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/palette"
)

// TestNewRootModelInstallsTheSchemeWhereItBelongs pins the two colour globals a
// session's construction touches, and the one it must leave alone.
//
// The scheme has to reach the markdown renderer, which keeps a process-global
// scheme with no frame-scoped push/pop — the constructor is its only chance.
//
// The palette's global must NOT be left in the session's scheme. Update and
// View each PushScheme/pop it, so a frame paints in the selected scheme and
// the package keeps its default colours outside one. A constructor that also
// set the palette global would leave the whole process in the configured
// scheme: every render that happens outside a frame, and every later test in
// the same binary, would wear colours nobody selected. That is the leak behind
// the palette-ordering failures in this package.
func TestNewRootModelInstallsTheSchemeWhereItBelongs(t *testing.T) {
	restorePalette(t)
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("HOME", home)
	writeSettings(t, home, `{"colorScheme":"terminal"}`)

	before, _ := palette.CurrentScheme()
	if before == "terminal" {
		t.Fatalf("test starts already in %q; the assertion below would prove nothing", before)
	}
	mdrenderBefore := mdrender.CurrentColorScheme()
	if mdrenderBefore == "terminal" {
		t.Fatalf("mdrender starts already in %q; the assertion below would prove nothing", mdrenderBefore)
	}
	t.Cleanup(func() { mdrender.SetColorScheme(mdrenderBefore) })

	m := newRootModel(&fakeClient{}, "u@t", "m", "t", t.TempDir(), true)

	if got := mdrender.CurrentColorScheme(); got != "terminal" {
		t.Errorf("mdrender scheme = %q after construction, want terminal", got)
	}
	if got, _ := palette.CurrentScheme(); got != before {
		t.Errorf("palette scheme = %q after construction, want %q left untouched", got, before)
	}
	if got := m.viewport.colorScheme; got != "terminal" {
		t.Errorf("viewport.colorScheme = %q, want terminal", got)
	}
}
