package composer

import (
	"strings"
	"testing"

	tas "charm.land/bubbles/v2/textarea"
	"github.com/justwasm/pigasus/palette"
)

func TestConfigureStandardTextArea(t *testing.T) {
	ta := tas.New()
	ConfigureStandardTextArea(&ta, StandardOptions{
		Prompt:      "> ",
		ContPrompt:  "  ",
		Placeholder: "Type here...",
		MinHeight:   1,
		MaxHeight:   6,
		Width:       80,
	})

	if !ta.DynamicHeight {
		t.Errorf("expected DynamicHeight = true")
	}
	if ta.MinHeight != 1 {
		t.Errorf("expected MinHeight = 1, got %d", ta.MinHeight)
	}
	if ta.MaxHeight != 6 {
		t.Errorf("expected MaxHeight = 6, got %d", ta.MaxHeight)
	}
	if ta.Height() != 1 {
		t.Errorf("expected initial Height = 1, got %d", ta.Height())
	}

	// Verify multi-line input expands height dynamically
	ta.SetValue("first line\nsecond line\nthird line")
	if ta.Height() != 3 {
		t.Errorf("expected Height = 3 after 3 lines, got %d", ta.Height())
	}

	// Reset shrinks height back to MinHeight
	ta.Reset()
	if ta.Height() != 1 {
		t.Errorf("expected Height = 1 after Reset, got %d", ta.Height())
	}
}

func TestDualRulesFrame(t *testing.T) {
	width := 40
	inputView := "> Hello world\n"
	frame := DualRulesFrame(width, inputView)

	lines := strings.Split(frame, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (top rule, input, bottom rule), got %d: %q", len(lines), lines)
	}

	expectedRule := palette.Rule(width)
	if lines[0] != expectedRule {
		t.Errorf("top rule mismatch: got %q, want %q", lines[0], expectedRule)
	}
	if lines[1] != "> Hello world" {
		t.Errorf("middle line mismatch: got %q, want '> Hello world'", lines[1])
	}
	if lines[2] != expectedRule {
		t.Errorf("bottom rule mismatch: got %q, want %q", lines[2], expectedRule)
	}
}

func TestCalculateViewportHeight(t *testing.T) {
	// 24 rows terminal: 1 row input, 1 row status, 2 rules -> 20 rows viewport
	if vp := CalculateViewportHeight(24, 1, 1); vp != 20 {
		t.Errorf("CalculateViewportHeight(24, 1, 1) = %d, want 20", vp)
	}

	// 24 rows terminal: 3 rows input (multi-line), 1 row status, 2 rules -> 18 rows viewport
	if vp := CalculateViewportHeight(24, 3, 1); vp != 18 {
		t.Errorf("CalculateViewportHeight(24, 3, 1) = %d, want 18", vp)
	}

	// Tiny terminal: clamped to minimum 4
	if vp := CalculateViewportHeight(5, 2, 1); vp != 4 {
		t.Errorf("CalculateViewportHeight(5, 2, 1) = %d, want 4", vp)
	}
}
