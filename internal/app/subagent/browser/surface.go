package browser

import (
	"context"

	piagent "github.com/MichaelKinsy/PiG/agent"

	"github.com/justwasm/pigasus/browserbridge"
	"github.com/justwasm/pigasus/browsercdp"
	"github.com/justwasm/pigasus/internal/piagentbridge"
	"github.com/justwasm/pigasus/internal/tools"
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
//
// The reference's own switch is --use_local_chrome: "Whether to use local
// chrome" 【strings — the 1.3.4 strings dump; the flag table in
// out/local-macos/FINDINGS.md §2, item 7】. The clone reads it as what
// it says — the user's own Chrome — and reaches it through the extension
// when the extension is there. The reference reaches it over remote
// debugging instead; both are the same promise to the caller, and the
// extension's is the one that needs neither a debugging port nor a
// profile copy.

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
//
// The bridge's tab listing is the first tool of the live surface and it
// has no counterpart in the managed one, so it marks the set.
const bridgeMarker = "tabs_context_mcp"

// CurrentSurface reports the surface the browser agent drives.
//
// The policy decides first: only the reference's --use_local_chrome asks
// for the user's own browser. Then the bridge decides whether it can
// reach it — the extension has to be installed, enabled and running in
// the browser the user is using. When it cannot, the clone falls back to
// the Chrome DevTools Protocol route it already had, which is the
// reference's own mechanism for the same flag.
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
//
// It is the question the /browser command asks before the model is
// asked: without a browser every browser tool fails, and the agent —
// which only knows that its tools failed — keeps trying new ones.
//
// The probe starts nothing: the browser starts on the first tool call,
// the reference's rule.
func Probe(ctx context.Context) (Surface, error) {
	if browsercdp.CurrentConfig().UseLocalChrome {
		// The extension first: it is the better route to the user's own
		// browser, and it needs no debugging port and no profile copy.
		if err := browserbridge.Check(ctx); err == nil {
			return SurfaceLive, nil
		}
		// The extension is not there: the DevTools road is the
		// reference's own mechanism for the same flag, so it is the
		// fallback.
	}
	return SurfaceManaged, browsercdp.CheckBrowser(ctx)
}

// SurfaceOf reports the surface a tool set belongs to.
func SurfaceOf(agentTools []piagent.AgentTool) Surface {
	for _, t := range agentTools {
		if t != nil && t.Name() == bridgeMarker {
			return SurfaceLive
		}
	}
	return SurfaceManaged
}

// Tools returns the browser agent's tool set for the surface in force.
//
// The managed set comes out of the session's own catalogue; the live set
// is the bridge's own tools, which are not in the advertised catalogue
// (internal/tools/catalog.go keeps the reference's sixty names) and are
// built here.
func Tools(reg *tools.Registry) []piagent.AgentTool {
	if CurrentSurface() == SurfaceLive {
		return BridgeTools(tools.BridgeCatalog())
	}
	return BrowserTools(reg)
}

// BridgeTools collects the bridge tools out of a registry and adapts them
// to PiG's tool interface.
//
// It mirrors BrowserTools: a registry that does not carry a tool is
// skipped.
func BridgeTools(reg *tools.Registry) []piagent.AgentTool {
	if reg == nil {
		return nil
	}
	var out []piagent.AgentTool
	for _, name := range tools.BridgeToolNames {
		tool, ok := reg.Get(name)
		if !ok || tool == nil {
			continue
		}
		out = append(out, piagentbridge.Tool(tool))
	}
	return out
}
