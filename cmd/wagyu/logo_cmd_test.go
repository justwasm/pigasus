package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/branding"
)

func TestLogoSubcommand_Output(t *testing.T) {
	t.Setenv("AGY_CLI_LOGO_STYLE", "default")
	var buf bytes.Buffer
	if err := runLogoSubcommand(&buf, nil); err != nil {
		t.Fatalf("runLogoSubcommand failed: %v", err)
	}

	got := buf.String()
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines of logo output, got %d", len(lines))
	}

	for i, line := range lines {
		if !strings.ContainsAny(line, "▀▄") {
			t.Errorf("line %d does not contain block chars: %q", i, line)
		}
	}

	wantLines := BannerLogoLines()
	for i, want := range wantLines {
		if lines[i] != want {
			t.Errorf("line %d mismatch:\ngot:  %q\nwant: %q", i, lines[i], want)
		}
	}
}

func TestLogoSubcommand_SimpleStyle(t *testing.T) {
	t.Run("via flag --style=simple", func(t *testing.T) {
		t.Setenv("AGY_CLI_LOGO_STYLE", "")
		t.Setenv("TERM_PROGRAM", "")
		var buf bytes.Buffer
		if err := runLogoSubcommand(&buf, []string{"--style=simple"}); err != nil {
			t.Fatalf("runLogoSubcommand failed: %v", err)
		}

		lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
		if len(lines) != 5 {
			t.Fatalf("expected 5 lines of logo output, got %d", len(lines))
		}
		for i, line := range lines {
			if !strings.Contains(line, "\x1b[48;2;") {
				t.Errorf("line %d expected 24-bit background sequence, got %q", i, line)
			}
			visible := logoStripAnsi(line)
			if len([]rune(visible)) != 16 {
				t.Errorf("line %d visible rune count = %d, want 16", i, len([]rune(visible)))
			}
		}
	})

	t.Run("via iTerm2 terminal auto-detection", func(t *testing.T) {
		t.Setenv("AGY_CLI_LOGO_STYLE", "")
		t.Setenv("TERM_PROGRAM", "iTerm.app")
		var buf bytes.Buffer
		if err := runLogoSubcommand(&buf, nil); err != nil {
			t.Fatalf("runLogoSubcommand failed: %v", err)
		}

		lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
		if len(lines) != 5 {
			t.Fatalf("expected 5 lines of logo output, got %d", len(lines))
		}
		for i, line := range lines {
			if !strings.Contains(line, "\x1b[48;2;") {
				t.Errorf("line %d expected 24-bit background sequence, got %q", i, line)
			}
		}
	})

	t.Run("180 degree flip ref vs pigasus", func(t *testing.T) {
		var bufRef, bufBoth bytes.Buffer
		if err := runLogoSubcommand(&bufRef, []string{"--style=simple", "--flip=ref"}); err != nil {
			t.Fatalf("runLogoSubcommand ref failed: %v", err)
		}
		if err := runLogoSubcommand(&bufBoth, []string{"--style=simple", "--flip=both"}); err != nil {
			t.Fatalf("runLogoSubcommand both failed: %v", err)
		}
		if bufRef.String() == bufBoth.String() {
			t.Fatalf("ref and both must differ under simple 5x6 style")
		}
	})
}

// TestLogoSubcommand_FlipFlag exercises the `--flip=...` surface.
// Pigasus CLI displays the 180°-rotated ("both") logo by default.
func TestLogoSubcommand_FlipFlag(t *testing.T) {
	baseline := captureLogo(t, nil)

	t.Run("--flip=both matches baseline Pigasus logo", func(t *testing.T) {
		out := captureLogo(t, []string{"--flip=both"})
		if out != baseline {
			t.Fatalf("--flip=both must match unflagged Pigasus CLI banner logo")
		}
	})

	for _, mode := range []string{"ref", "vert", "row-rev", "horiz", "invert"} {
		t.Run("--flip="+mode, func(t *testing.T) {
			out := captureLogo(t, []string{"--flip=" + mode})
			if got := countSubstantiveLines(out); got != 5 {
				t.Fatalf("--flip=%s produced %d lines, want 5:\n%s",
					mode, got, out)
			}
			if out == baseline {
				t.Fatalf("--flip=%s produced the same bytes as the Pigasus baseline", mode)
			}
		})
	}

	// `vert` and `row-rev` are deliberately different operations:
	// `vert` mirrors the cells through their centre line (top ↔ bot),
	// `row-rev` only reorders rows.
	t.Run("--flip=vert differs from --flip=row-rev", func(t *testing.T) {
		v := captureLogo(t, []string{"--flip=vert"})
		r := captureLogo(t, []string{"--flip=row-rev"})
		if v == r {
			t.Fatalf("--flip=vert and --flip=row-rev produced identical output; "+
				"vert must mirror the cells through their centre line and row-rev must not")
		}
	})

	// Old `vert-invert` (== row-rev after `flipVert` started
	// including the centre-line swap) should fail loudly now.
	t.Run("--flip=vert-invert returns error", func(t *testing.T) {
		var buf bytes.Buffer
		err := runLogoSubcommand(&buf, []string{"--flip=vert-invert"})
		if err == nil {
			t.Fatal("expected error for removed --flip=vert-invert")
		}
		if !strings.Contains(err.Error(), "vert-invert") {
			t.Errorf("error did not echo the bad value: %v", err)
		}
	})

	// And the unknown-mode error path.
	t.Run("--flip=garbage returns error", func(t *testing.T) {
		var buf bytes.Buffer
		err := runLogoSubcommand(&buf, []string{"--flip=garbage"})
		if err == nil {
			t.Fatal("expected error for --flip=garbage, got nil")
		}
		if !strings.Contains(err.Error(), "garbage") {
			t.Errorf("error message did not echo the bad value: %v", err)
		}
	})
}

func TestLogoSubcommand_Help(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"-h"},
		{"--style=simple", "--help"},
	} {
		var buf bytes.Buffer
		err := runLogoSubcommand(&buf, args)
		if err != nil {
			t.Fatalf("runLogoSubcommand(%v) failed: %v", args, err)
		}
		got := buf.String()
		if !strings.Contains(got, "Usage: wagyu logo [flags]") {
			t.Errorf("runLogoSubcommand(%v): expected Usage in output, got: %q", args, got)
		}
		if !strings.Contains(got, "--flip") || !strings.Contains(got, "--style") {
			t.Errorf("runLogoSubcommand(%v): expected flags in output, got: %q", args, got)
		}
	}
}

func captureLogo(t *testing.T, args []string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := runLogoSubcommand(&buf, args); err != nil {
		t.Fatalf("runLogoSubcommand(%v) failed: %v", args, err)
	}
	return buf.String()
}

// countSubstantiveLines returns the number of non-empty lines in s.
// Used to assert that every flip mode produces a 5-line logo and not,
// say, a single squashed row.
func countSubstantiveLines(s string) int {
	var n int
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if line != "" {
			n++
		}
	}
	return n
}

func TestLogoSubcommand_AntigravityBrand(t *testing.T) {
	defer branding.Reset()
	t.Setenv("AGY_CLI_LOGO_STYLE", "default")

	// In Pigasus brand, unflagged logo is the flipped (both) logo.
	branding.SetCurrent(branding.BrandPigasus)
	baselinePigasus := captureLogo(t, nil)
	bothLogo := captureLogo(t, []string{"--flip=both"})
	if baselinePigasus != bothLogo {
		t.Fatalf("under BrandPigasus, unflagged logo must match --flip=both")
	}

	// In Antigravity brand, unflagged logo is the reference logo.
	branding.SetCurrent(branding.BrandAntigravity)
	baselineAntigravity := captureLogo(t, nil)
	refLogo := captureLogo(t, []string{"--flip=ref"})
	if baselineAntigravity != refLogo {
		t.Fatalf("under BrandAntigravity, unflagged logo must match --flip=ref")
	}

	// Antigravity and Pigasus logos must not be identical.
	if baselinePigasus == baselineAntigravity {
		t.Fatalf("Pigasus logo and Antigravity logo must differ")
	}
}
