// Package agents — flags.go: the `--agent` flag's own reading.
//
// The two helpers here are the flag's face of the catalogue: they read
// the `--agent` value and turn it into a custom agent. They live beside
// the catalogue because the catalogue is what they read — a host that
// names them no longer has to reach another host for them.

package agents

import (
	"flag"

	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// CustomAgentFromFlags instantiates a custom agent configured from --agent.
//
// The empty value and "default" are the built-in agent: they name no
// custom agent, so the result is nil. A name that matches no discovered
// agent is nil too — the caller decides what to say about it.
func CustomAgentFromFlags(fs *flag.FlagSet) *Agent {
	name := cliflags.StringValue(fs, "agent")
	if name == "" || name == "default" {
		return nil
	}
	if a, ok := Lookup(name); ok {
		return &a
	}
	return nil
}

// AgentBody returns a custom agent's Markdown body, or "" when there is none.
func AgentBody(a *Agent) string {
	if a == nil {
		return ""
	}
	return a.Body
}
