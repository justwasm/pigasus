// Package piagentbridge — native_provider.go: the function that
// derives a PiG-native agw Provider from a backend.Client.
//
// Cycle rc-piagent-bind moves the type-assertion helper out of
// internal/agent/pig.go into the bridge package so the cycle's
// remote-control factory (piagent_remote.go) can call it
// without dragging the loop package into its dependency
// graph. The behaviour is identical: a backend that knows how
// to describe itself as an agw endpoint (today only
// *backend.AgwK0sClient) yields a native provider; anything
// else falls through to the StreamFn path of
// NewBackendProvider.
//
// Why it lives here:
//
//   - piagentbridge already imports internal/backend and is the
//     package that owns `Provider`. Re-exporting the helper
//     here keeps the bridge self-contained.
//
//   - internal/agent.NativeProvider keeps working as a thin
//     pass-through; the existing print_pig.go and
//     slash_btw.go callers don't change.
//
//   - The remote-control path's factory needs the same
//     derivation. Keeping it here means the cascade seam and
//     the existing TUI/print seams see consistent behaviour
//     without an import cycle through internal/agent.

package piagentbridge

import (
	"time"

	"github.com/justwasm/pigasus/internal/backend"
)

// agwBackend is implemented by backend clients that can describe
// how they reach agw. The interface is private — the only
// implementor today is *backend.AgwK0sClient, and the goal is to
// keep the seam invisible to the rest of the codebase.
type agwBackend interface {
	AgwConfig() backend.AgwK0sConfig
}

// NativeProvider derives a PiG-native agw Provider from the
// supplied backend.Client, when the backend is one this
// migration knows how to mirror.
//
// The derivation is deliberately implicit: cmd/wagyu builds
// the backend in one place (pickBackendWithLog) but hands it
// to several loops — the print path, the TUI, the cycle's
// cascade seam — so threading an explicit *Provider through
// each would mean editing every caller. Taking it from the
// client keeps each seam at zero changed lines.
//
// A backend that does not describe itself (backend.NewMock,
// the lifecycle wrappers, anything not type-assertable to
// agwBackend) returns nil — the signal to fall through to
// NewBackendProvider's StreamFn path, which keeps the shims
// and the parity harnesses on the transport they already
// exercise.
func NativeProvider(client backend.Client) *Provider {
	if client == nil {
		return nil
	}
	described, ok := client.(agwBackend)
	if !ok {
		return nil
	}
	cfg := described.AgwConfig()
	return NewProvider(ProviderConfig{
		URL:        cfg.URL,
		APIKey:     cfg.APIKey,
		Model:      cfg.ExternalModel,
		Timeout:    time.Duration(cfg.TimeoutSeconds) * time.Second,
		HTTPClient: cfg.HTTPClient,
	})
}
