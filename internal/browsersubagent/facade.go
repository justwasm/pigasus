// Package browsersubagent is a backward-compatible facade over internal/app/subagent/browser.
package browsersubagent

import (
	"context"

	"github.com/justwasm/pigasus/internal/agyprompt"
	"github.com/justwasm/pigasus/internal/app/subagent/browser"
	"github.com/justwasm/pigasus/internal/builtinsubagent"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/tools"
	piagent "github.com/MichaelKinsy/PiG/agent"
)

const (
	ToolName    = browser.ToolName
	PromptLead  = browser.PromptLead
	Description = browser.Description
)

var BrowserToolNames = browser.BrowserToolNames

var (
	Prompt         = browser.Prompt
	BrowserTools   = browser.BrowserTools
	Subagent       = browser.Subagent
	InstallBuiltin = browser.InstallBuiltin
	CurrentSurface = browser.CurrentSurface
	Probe          = browser.Probe
	SurfaceOf      = browser.SurfaceOf
	Tools          = browser.Tools
	BridgeTools    = browser.BridgeTools
)

type Runner = browser.Runner
type Surface = browser.Surface

const (
	SurfaceManaged = browser.SurfaceManaged
	SurfaceLive    = browser.SurfaceLive
)

func New(deps subagenttools.Deps, browserTools []piagent.AgentTool) *Runner {
	return browser.New(deps, browserTools)
}

func _() {
	var _ func(reg *builtinsubagent.Registry, deps subagenttools.Deps, catalog *tools.Registry) error = InstallBuiltin
	var _ func() agyprompt.Subagent = Subagent
	var _ func(ctx context.Context) (Surface, error) = Probe
}
