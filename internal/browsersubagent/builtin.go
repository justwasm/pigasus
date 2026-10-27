package browsersubagent

import (
	"context"
	"errors"

	"github.com/justwasm/pigasus/internal/agyprompt"
	"github.com/justwasm/pigasus/internal/builtinsubagent"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/tools"
)

// The browser agent as a built-in subagent type.
//
// Why a type and not a tool
// -------------------------
// The clone advertises `browser_subagent` among its sixty tool names,
// but the model's twelve wire declarations do not carry it
// (internal/tools/model_tools_1.4.6.json) — so a model that only sees
// the reference's own declarations cannot call it. The reference reaches
// its browser agent a different way: the browser agent is one of the
// built-in agent *types* (【反编译】
// cortex/customizations/builtin/agents.BrowserAgent), and the model
// names it in `invoke_subagent`'s TypeName. So the clone registers the
// browser agent under the reference's own name and lets
// `invoke_subagent` route to it — the same route the reference uses.
//
// Description is the catalogue line the model reads.
//
// It is the clone's own prose. The reference's line for its browser
// agent is not in the captured prompt: the reference gates the agent on
// its browser configuration (【反编译】
// cortex/customizations/builtin/agents.CalculateAllowedSubagents) and
// the 1.4.6 capture was taken with the browser off.
const Description = "Browser agent. It drives a real Chrome — the browser you already have open when the Open Claude in Chrome extension is attached — reads and fills pages, and answers with what the page says."

// Subagent returns the prompt's "Available subagents" entry for the
// browser agent.
//
// The entry and the registry entry must agree: the prompt is what tells
// the model the name, and the registry is what `invoke_subagent` routes
// that name to. Both are written here, so they cannot drift.
//
// The entry is *not* part of agyprompt.BuiltinSubagents(): that list is
// the reference's three always-present agents, pinned against the 1.4.4
// capture (internal/agyprompt's TestBuiltinSubagentsMatchCapture). The
// browser agent is the reference's *conditional* one, so the session
// adds it — the way it adds the user's own agents.
func Subagent() agyprompt.Subagent {
	return agyprompt.Subagent{
		Name:        string(builtinsubagent.KindBrowser),
		Description: Description,
	}
}

// InstallBuiltin registers the browser agent as a built-in subagent
// type, so `invoke_subagent` can route to it by name.
//
// The handler builds its runner per call, so the surface it drives is
// the one in force at that moment: the live surface when the Open Claude
// in Chrome extension answers, the managed one otherwise (see
// internal/browsersubagent/surface.go).
//
// The registry is the caller's. The session owns one and installs into
// it, so the catalogue the model reads and the handler it reaches are
// the same registry. A registry that already carries the name is a
// programming error — Register says so rather than shadowing the
// earlier entry.
func InstallBuiltin(reg *builtinsubagent.Registry, deps subagenttools.Deps, catalog *tools.Registry) error {
	if reg == nil {
		return errors.New("browsersubagent: nil built-in registry")
	}
	if catalog == nil {
		return errors.New("browsersubagent: nil tool catalogue")
	}
	return reg.Register(builtinsubagent.Spec{
		Name:        builtinsubagent.KindBrowser,
		Description: Description,
		Handler: func(ctx context.Context, prompt string) (builtinsubagent.Result, error) {
			runner := New(deps, Tools(catalog))
			answer, err := runner.RunBrowserTask(ctx, prompt)
			if err != nil {
				return builtinsubagent.Result{}, err
			}
			return builtinsubagent.Result{Text: answer}, nil
		},
	})
}
