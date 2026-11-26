package backendselect

import (
	"io"

	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/failover"
)

// ImageGeneratorFor resolves a backend.ImageGenerator from be (unwrapping
// account-pool and failover wrappers). If be is a non-mock backend without
// native image generation (e.g. an OpenAI-compatible gateway) and an OAuth
// token is available, it falls back to a CloudCode image generation client.
// Returns nil when be is a mock/shim backend or no image backend is available.
func ImageGeneratorFor(be backend.Client) backend.ImageGenerator {
	if gen := findImageGenerator(be); gen != nil {
		return gen
	}
	if !imageBackendNeedsOAuthFallback(be) || !HasOAuthToken() {
		return nil
	}
	cc := newOfficialBackend(geminicode.DefaultImageGenerationModel, io.Discard, nil)
	return findImageGenerator(cc)
}

// imageBackendNeedsOAuthFallback distinguishes a real unsupported backend from
// a mock-only graph. A failover container is not itself a backend: inspect all
// of its targets so a mock primary does not hide a real image-capable target.
func imageBackendNeedsOAuthFallback(c backend.Client) bool {
	if c == nil {
		return false
	}
	if _, ok := c.(backend.ImageGenerator); ok {
		return false
	}
	if fc, ok := c.(interface{ Targets() []failover.Target }); ok {
		for _, target := range fc.Targets() {
			if imageBackendNeedsOAuthFallback(target.Client) {
				return true
			}
		}
		return false
	}
	if u, ok := c.(interface{ Unwrap() backend.Client }); ok {
		return imageBackendNeedsOAuthFallback(u.Unwrap())
	}
	return !backend.IsMock(c)
}

func findImageGenerator(c backend.Client) backend.ImageGenerator {
	if c == nil {
		return nil
	}
	if gen, ok := c.(backend.ImageGenerator); ok {
		return gen
	}
	if u, ok := c.(interface{ Unwrap() backend.Client }); ok {
		if gen := findImageGenerator(u.Unwrap()); gen != nil {
			return gen
		}
	}
	if fc, ok := c.(interface{ Targets() []failover.Target }); ok {
		for _, t := range fc.Targets() {
			if gen := findImageGenerator(t.Client); gen != nil {
				return gen
			}
		}
	}
	return nil
}
