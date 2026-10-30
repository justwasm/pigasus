package native

import (
	featurenative "github.com/justwasm/pigasus/internal/features/tools/native"
)

// Provider is a backward-compatible facade for internal/features/tools/native.Provider.
type Provider = featurenative.Provider

// NewProvider forwards to internal/features/tools/native.NewProvider.
func NewProvider(workspace string) *Provider {
	return featurenative.NewProvider(workspace)
}
