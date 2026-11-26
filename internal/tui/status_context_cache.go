package tui

import (
	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/models/modelctx"
)

// statusContextWindow keeps the renderer off the immediate catalogue/config
// readers. Transport and compaction callers continue to use modelctx directly.
func (m *RootModel) statusContextWindow(model string) int {
	reg := models.CachedRegistrySnapshot()
	generation := reg.Gen()
	state := &m.statusLine
	if state.contextWindow > 0 && state.contextModel == model &&
		state.contextRegistry == reg && state.contextGeneration == generation {
		return state.contextWindow
	}
	state.contextModel = model
	state.contextRegistry = reg
	state.contextGeneration = generation
	state.contextWindow = modelctx.ContextWindowFor(model)
	return state.contextWindow
}
