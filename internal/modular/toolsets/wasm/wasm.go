package wasm

import (
	featurewasm "github.com/justwasm/pigasus/internal/features/tools/wasm"
)

// Provider is a backward-compatible facade for internal/features/tools/wasm.Provider.
type Provider = featurewasm.Provider

// NewProvider forwards to internal/features/tools/wasm.NewProvider.
func NewProvider(workspace string) *Provider {
	return featurewasm.NewProvider(workspace)
}
