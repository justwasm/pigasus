package tui

import (
	"testing"

	subagentrouter "github.com/justwasm/pigasus/internal/app/subagent/router"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
)

// TestAgentsModel_OwnsConversationScope pins the ownership move: the
// definition store, live-child registry and @-mention router hang off
// AgentsModel, not the engine bucket.
func TestAgentsModel_OwnsConversationScope(t *testing.T) {
	var empty AgentsModel
	if empty.defs != nil || empty.registry != nil || empty.router != nil {
		t.Fatal("zero model has subagent state")
	}

	defs := subagenttools.NewStore()
	registry := subagenttools.NewRegistry("conv-1")
	router := subagentrouter.NewManager()
	m := diffTestModel()
	m.agents = AgentsModel{defs: defs, registry: registry, router: router}
	if m.agents.defs != defs || m.agents.registry != registry || m.agents.router != router {
		t.Fatal("the model did not keep the subagent state")
	}
	if got := m.agents.registry.ParentID(); got != "conv-1" {
		t.Fatalf("registry parent = %q", got)
	}
}
