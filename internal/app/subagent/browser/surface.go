package browser

import (
	"context"
	"encoding/json"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/browserbridge"
	"github.com/justwasm/pigasus/browsercdp"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/browser"
)

// The browser the agent drives.
//
// Two surfaces, one agent.
//
// The clone's own surface is a Chrome the clone starts, driven over the
// Chrome DevTools Protocol: the clone's twenty-three browser tools, in
// internal/tools/browser.go.
//
// The live surface is the browser the *user* already runs — their window,
// their tabs, their logins — reached through the Open Claude in Chrome
// extension (browserbridge). The clone's own tools cannot do
// that: they drive a Chrome of the clone's own, with a profile of its own
// and no way to reach the session the user is already logged into.

// Surface is the browser the agent drives.
type Surface int

const (
	// SurfaceManaged is the clone's own Chrome, over CDP.
	SurfaceManaged Surface = iota

	// SurfaceLive is the browser the user already runs, through the
	// Open Claude in Chrome extension.
	SurfaceLive
)

// String renders the surface for a message.
func (s Surface) String() string {
	if s == SurfaceLive {
		return "live"
	}
	return "managed"
}

// bridgeMarker is the tool that tells a live tool set from a managed one.
const bridgeMarker = "tabs_context_mcp"

// CurrentSurface reports the surface the browser agent drives.
func CurrentSurface() Surface {
	if !browsercdp.CurrentConfig().UseLocalChrome {
		return SurfaceManaged
	}
	if browserbridge.Check(context.Background()) == nil {
		return SurfaceLive
	}
	return SurfaceManaged
}

// Probe answers whether the browser can be reached at all, and on which
// surface.
func Probe(ctx context.Context) (Surface, error) {
	if browsercdp.CurrentConfig().UseLocalChrome {
		if err := browserbridge.Check(ctx); err == nil {
			return SurfaceLive, nil
		}
	}
	return SurfaceManaged, browsercdp.CheckBrowser(ctx)
}

// SurfaceOf reports the surface a tool set belongs to.
func SurfaceOf(agentTools []actor.Tool) Surface {
	for _, t := range agentTools {
		if t != nil && t.Name() == bridgeMarker {
			return SurfaceLive
		}
	}
	return SurfaceManaged
}

// Tools returns the browser agent's tool set for the surface in force.
func Tools(reg *tools.Registry) []actor.Tool {
	if CurrentSurface() == SurfaceLive {
		return BridgeTools(browser.BridgeCatalog())
	}
	return BrowserTools(reg)
}

// BridgeTools collects the bridge tools out of a registry and adapts them
// to the actor.Tool interface.
func BridgeTools(reg *tools.Registry) []actor.Tool {
	if reg == nil {
		return nil
	}
	var out []actor.Tool
	for _, name := range browser.BridgeToolNames {
		tool, ok := reg.Get(name)
		if !ok || tool == nil {
			continue
		}
		out = append(out, adaptTool(tool))
	}
	return out
}

func adaptTool(t tools.Tool) actor.Tool {
	if t == nil {
		return nil
	}
	var params map[string]any
	desc := t.Name()
	if raw := t.Schema(); len(raw) > 0 {
		_ = json.Unmarshal(raw, &params)
		if d, ok := params["description"].(string); ok && d != "" {
			desc = d
		}
	}
	return actor.NewFuncTool(
		t.Name(),
		desc,
		func(ctx context.Context, input string) (string, error) {
			out, err := t.Run(ctx, json.RawMessage(input))
			if err != nil {
				return "", err
			}
			text := tools.RenderResult(out)
			budget := tools.ApplyOutputBudget(text, "", tools.DefaultMaxResultTokens)
			return budget.Content, nil
		},
		actor.WithParameters(params),
	)
}
