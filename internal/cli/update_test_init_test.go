package cli

import (
	"github.com/justwasm/pigasus/internal/branding"
)

func init() {
	// During package cli tests, initialize BrandAntigravity by default so that
	// reference parity tests (like TestUpdate_Offline) execute the reference
	// offline update behavior without making network calls.
	branding.SetCurrent(branding.BrandAntigravity)
}
