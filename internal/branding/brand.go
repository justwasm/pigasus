package branding

import (
	"os"
	"strings"
	"sync"
	"testing"
)

// Brand represents the product identity (Antigravity or Pigasus).
type Brand string

const (
	// BrandAntigravity represents the reference Google Antigravity CLI identity.
	BrandAntigravity Brand = "antigravity"
	// BrandPigasus represents the Pigasus (justwasm/pigasus) community identity.
	BrandPigasus Brand = "pigasus"
)

// Config describes the branding properties for a brand.
type Config struct {
	ID              Brand  // "antigravity" or "pigasus"
	Name            string // "Antigravity" or "Pigasus"
	CLIName         string // "Antigravity CLI" or "Pigasus CLI"
	RepoSlug        string // "" for Antigravity, "justwasm/pigasus" for Pigasus
	FlippedLogo     bool   // false for Antigravity, true for Pigasus
	SharedScopeName string // "Shared with Google" or "Shared with Pigasus"
}

var (
	mu           sync.RWMutex
	currentBrand = BrandPigasus // Production default for wagyu binary
	customBrand  *Brand
)

// Current returns the active branding configuration.
//
// Resolution order:
//  1. PIGASUS_BRAND or OPENAGY_BRAND environment variable.
//  2. Programmatic override set by SetCurrent().
//  3. Default production fallback: BrandPigasus.
func Current() Config {
	mu.RLock()
	defer mu.RUnlock()

	b := currentBrand
	if customBrand != nil {
		b = *customBrand
	} else if env := os.Getenv("PIGASUS_BRAND"); env != "" {
		b = normalizeBrand(env)
	} else if env := os.Getenv("OPENAGY_BRAND"); env != "" {
		b = normalizeBrand(env)
	}

	switch b {
	case BrandAntigravity:
		return Config{
			ID:              BrandAntigravity,
			Name:            "Antigravity",
			CLIName:         "Antigravity CLI",
			RepoSlug:        "",
			FlippedLogo:     false,
			SharedScopeName: "Shared with Google",
		}
	default:
		return Config{
			ID:              BrandPigasus,
			Name:            "Pigasus",
			CLIName:         "Pigasus CLI",
			RepoSlug:        "justwasm/pigasus",
			FlippedLogo:     true,
			SharedScopeName: "Shared with Pigasus",
		}
	}
}

// SetCurrent programmatically overrides the active brand.
func SetCurrent(b Brand) {
	mu.Lock()
	defer mu.Unlock()
	normalized := normalizeBrand(string(b))
	customBrand = &normalized
}

// Reset clears any programmatic brand override.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	customBrand = nil
}

// IsTest reports whether the current process is running under `go test`.
func IsTest() bool {
	return testing.Testing()
}

func normalizeBrand(s string) Brand {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "antigravity", "agy", "google":
		return BrandAntigravity
	default:
		return BrandPigasus
	}
}
