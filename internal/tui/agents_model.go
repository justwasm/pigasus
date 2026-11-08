package tui

// agents_model.go — the subagents sub-model.
//
// AgentsModel owns the conversation's subagent facts: the definition store,
// the live-subagent registry and the @-mention router. They are scoped to
// the conversation (a resumed conversation gets its definitions back from
// the trajectory), so they belong together rather than beside the turn's
// transports.
//
// Upstream: google3/third_party/jetski/cli/model/model.AgentsModel.

import (
	subagentrouter "github.com/justwasm/pigasus/internal/app/subagent/router"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
)

// AgentsModel owns the subagent definitions, live children and router.
type AgentsModel struct {
	defs     *subagenttools.Store
	registry *subagenttools.Registry
	router   *subagentrouter.Manager
}
