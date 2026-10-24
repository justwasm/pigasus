package tuilib

import (
	"bytes"
	"strings"
	"testing"
)

func TestLogoPalette_SentinelAndLength(t *testing.T) {
	if len(LogoPalette) == 0 {
		t.Fatal("LogoPalette is empty")
	}
	if LogoPalette[0] != ([3]byte{0, 0, 0}) {
		t.Fatalf("LogoPalette[0] = %v, want {0,0,0}", LogoPalette[0])
	}
	for i := 1; i < len(LogoPalette); i++ {
		if LogoPalette[i] == ([3]byte{0, 0, 0}) {
			t.Errorf("LogoPalette[%d] collides with transparent sentinel", i)
		}
	}
}

func TestLogoPixels_BoundsAndCount(t *testing.T) {
	if len(LogoPixels) != 10 {
		t.Fatalf("LogoPixels has %d rows, want 10", len(LogoPixels))
	}
	count := 0
	for _, row := range LogoPixels {
		if len(row) != 16 {
			t.Errorf("expected 16 cols, got %d", len(row))
		}
		for _, p := range row {
			if p != 0 {
				count++
			}
		}
	}
	if count != 50 {
		t.Fatalf("active pixel count = %d, want 50", count)
	}
}

func TestLogoBlockGrid_BoundsAndSubpixels(t *testing.T) {
	if len(RefLogoBlockGrid) != 5 {
		t.Fatalf("RefLogoBlockGrid has %d rows, want 5", len(RefLogoBlockGrid))
	}
	sub := RefLogoBlockGrid.Subpixels()
	if len(sub) != 10 || len(sub[0]) != 12 {
		t.Fatalf("Subpixels dimensions = (%d,%d), want (10,12)", len(sub), len(sub[0]))
	}
}

func TestRenderTransformed(t *testing.T) {
	modes := []string{"none", "both", "ref", "vert", "row-rev", "horiz", "invert"}
	for _, m := range modes {
		lines, err := RenderTransformed(m, LogoStyleDefault)
		if err != nil {
			t.Fatalf("RenderTransformed(%q, Default) failed: %v", m, err)
		}
		if len(lines) != 5 {
			t.Fatalf("RenderTransformed(%q) returned %d lines, want 5", m, len(lines))
		}
	}

	blockModes := []string{"none", "both", "ref", "vert", "horiz"}
	for _, m := range blockModes {
		lines, err := RenderTransformed(m, LogoStyleSimple)
		if err != nil {
			t.Fatalf("RenderTransformed(%q, Simple) failed: %v", m, err)
		}
		if len(lines) != 5 {
			t.Fatalf("RenderTransformed(%q, Simple) returned %d lines, want 5", m, len(lines))
		}
	}

	if _, err := RenderTransformed("badmode", LogoStyleDefault); err == nil {
		t.Fatal("expected error for badmode in default style")
	}
	if _, err := RenderTransformed("badmode", LogoStyleSimple); err == nil {
		t.Fatal("expected error for badmode in simple style")
	}
}

func TestPrintBannerLogo(t *testing.T) {
	var buf bytes.Buffer
	PrintBannerLogo(&buf)
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("PrintBannerLogo output %d lines, want 5", len(lines))
	}
}
