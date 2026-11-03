package mdrender

import (
	"testing"
)

func TestRenderer_FactoryAndRender(t *testing.T) {
	tests := []struct {
		name     string
		renderer *Renderer
	}{
		{"ANSI", NewANSI(80)},
		{"Plain", NewPlain(80)},
		{"User", NewUser(80)},
		{"Quiet", NewQuiet(80)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.renderer == nil {
				t.Fatalf("expected non-nil renderer for %s", tt.name)
			}
			out, err := tt.renderer.Render("# Heading\nSome text")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(out) == 0 {
				t.Fatalf("expected non-empty rendered output")
			}
		})
	}
}
