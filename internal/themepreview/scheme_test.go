package themepreview

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/onboarding"
)

func TestColorsFor(t *testing.T) {
	// The prompt wears the scheme's accent and the placeholder its dim
	// foreground.
	p, ph := ColorsFor("Tokyo Night")
	if p != "#7aa2f7" || ph != "#9aa5ce" {
		t.Errorf("ColorsFor(Tokyo Night) = (%s, %s)", p, ph)
	}

	p, ph = ColorsFor("dark")
	if p != "#8ab4f8" || ph != "#9aa0a6" {
		t.Errorf("ColorsFor(dark) = (%s, %s)", p, ph)
	}

	// The terminal scheme's slots are terminal indices.
	p, ph = ColorsFor("terminal")
	if p != "12" || ph != "8" {
		t.Errorf("ColorsFor(terminal) = (%s, %s)", p, ph)
	}

	// An unknown name falls back to the solarized light scheme.
	p, ph = ColorsFor("nope")
	if p != "#268bd2" || ph != "#657b83" {
		t.Errorf("ColorsFor(nope) = (%s, %s)", p, ph)
	}
}

func TestSchemeFromString(t *testing.T) {
	if got := SchemeFromString("solarized light"); got != onboarding.SchemeSolarizedLight {
		t.Errorf("SchemeFromString(solarized light) = %v", got)
	}
	if got := SchemeFromString("dark"); got != onboarding.SchemeDark {
		t.Errorf("SchemeFromString(dark) = %v", got)
	}
	if got := SchemeFromString("unknown"); got != onboarding.SchemeSolarizedLight {
		t.Errorf("SchemeFromString(unknown) = %v", got)
	}
}

func TestBuildPreviewLines(t *testing.T) {
	displays := []string{"Dark", "Light"}
	lines := BuildPreviewLines(displays, 0, "Dark", "<blue>", "</blue>")
	if len(lines) != 22 {
		t.Fatalf("expected 22 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[1], "Dark (current)") {
		t.Errorf("expected current marker, got %q", lines[1])
	}
}
