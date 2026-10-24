// Package piagentbridge — native_provider.go: the function that
// derives a PiG-native gateway Provider from a backend.Client.
//
// Cycle rc-piagent-bind moves the type-assertion helper out of
// internal/agent/pig.go into the bridge package so the cycle's
// remote-control factory (piagent_remote.go) can call it
// without dragging the loop package into its dependency
// graph. The behaviour is identical: a backend that knows how
// to describe itself as a gateway endpoint (today only
// *backend.GatewayClient) yields a native provider; anything
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

// gatewayBackend is implemented by backend clients that can describe
// how they reach their endpoint. The interface is private — the only
// implementor today is *backend.GatewayClient, and the goal is to
// keep the seam invisible to the rest of the codebase.
type gatewayBackend interface {
	GatewayConfig() backend.GatewayConfig
}

// NativeProvider derives a PiG-native gateway Provider from the
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
// gatewayBackend) returns nil — the signal to fall through to
// NewBackendProvider's StreamFn path, which keeps the shims
// and the parity harnesses on the transport they already
// exercise.
//
// Two configured endpoints also return nil:
//
//   - one that speaks the Anthropic Messages dialect. This provider
//     speaks the OpenAI Chat-Completions dialect, and mirroring a
//     protocol it does not speak would put the wrong bytes on the
//     wire. The Anthropic dialect is served by the clone's own
//     transport (internal/backend/gateway_anthropic.go) through the
//     StreamFn path.
//   - one with no endpoint configured. There is nothing to talk to,
//     and a provider that would fail on its first request is worse
//     than the transport's own "no endpoint configured" error.
func NativeProvider(client backend.Client) *Provider {
	if client == nil {
		return nil
	}
	described, ok := client.(gatewayBackend)
	if !ok {
		return nil
	}
	cfg := described.GatewayConfig()
	if cfg.Type != "" && cfg.Type != backend.GatewayProtocolOpenAI {
		return nil
	}
	if cfg.URL == "" || cfg.ExternalModel == "" {
		return nil
	}
	return NewProvider(ProviderConfig{
		URL:        cfg.URL,
		APIKey:     cfg.APIKey,
		Model:      cfg.ExternalModel,
		Timeout:    time.Duration(cfg.TimeoutSeconds) * time.Second,
		HTTPClient: cfg.HTTPClient,
		Headers:    cfg.Headers,
	})
}
