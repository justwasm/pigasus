package branding

import (
	"testing"
)

func TestBranding_DefaultsToPigasusInBinary(t *testing.T) {
	Reset()
	cfg := Current()
	if cfg.ID != BrandPigasus {
		t.Fatalf("default ID = %q, want %q", cfg.ID, BrandPigasus)
	}
	if cfg.CLIName != "Pigasus CLI" {
		t.Fatalf("default CLIName = %q, want %q", cfg.CLIName, "Pigasus CLI")
	}
	if !cfg.FlippedLogo {
		t.Fatalf("default FlippedLogo = false, want true")
	}
	if cfg.RepoSlug != "justwasm/pigasus" {
		t.Fatalf("default RepoSlug = %q, want %q", cfg.RepoSlug, "justwasm/pigasus")
	}
}

func TestBranding_SetCurrentAntigravity(t *testing.T) {
	defer Reset()
	SetCurrent(BrandAntigravity)
	cfg := Current()
	if cfg.ID != BrandAntigravity {
		t.Fatalf("ID = %q, want %q", cfg.ID, BrandAntigravity)
	}
	if cfg.CLIName != "Antigravity CLI" {
		t.Fatalf("CLIName = %q, want %q", cfg.CLIName, "Antigravity CLI")
	}
	if cfg.FlippedLogo {
		t.Fatalf("FlippedLogo = true, want false")
	}
	if cfg.RepoSlug != "" {
		t.Fatalf("RepoSlug = %q, want empty", cfg.RepoSlug)
	}
}

func TestBranding_EnvOverride(t *testing.T) {
	defer Reset()
	Reset()

	t.Run("PIGASUS_BRAND=antigravity", func(t *testing.T) {
		t.Setenv("PIGASUS_BRAND", "antigravity")
		cfg := Current()
		if cfg.ID != BrandAntigravity {
			t.Fatalf("got %s, want %s", cfg.ID, BrandAntigravity)
		}
	})

	t.Run("OPENAGY_BRAND=agy", func(t *testing.T) {
		t.Setenv("OPENAGY_BRAND", "agy")
		cfg := Current()
		if cfg.ID != BrandAntigravity {
			t.Fatalf("got %s, want %s", cfg.ID, BrandAntigravity)
		}
	})

	t.Run("PIGASUS_BRAND=pigasus", func(t *testing.T) {
		t.Setenv("PIGASUS_BRAND", "pigasus")
		cfg := Current()
		if cfg.ID != BrandPigasus {
			t.Fatalf("got %s, want %s", cfg.ID, BrandPigasus)
		}
	})
}
