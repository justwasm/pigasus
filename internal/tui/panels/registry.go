package panels

import (
	tea "charm.land/bubbletea/v2"

	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/artifactpanel"
	"github.com/justwasm/pigasus/internal/ui/views/contextpanel"
	featdiff "github.com/justwasm/pigasus/internal/ui/views/diff/feat"
	"github.com/justwasm/pigasus/internal/ui/views/mcppanel"
)

// RegistryHost defines the minimal host environment needed by declarative panel specs.
type RegistryHost interface {
	TerminalHeight() int
	TerminalWidth() int
	Panel(kind panelregistry.Kind) any
	ContextState() contextpanel.State
	PanelUsageLines() []string
	UsageViewerSpec() tuiviewer.Spec
	PanelChangelogLines() []string
	ChangelogViewerSpec() tuiviewer.Spec
}

// StandardPanel is the common interface implemented by interactive panels.
type StandardPanel interface {
	Lines() []string
	HandleKey(key tea.Key) (bool, tea.Cmd)
}

// SizedWidthPanel is implemented by panels that format with width.
type SizedWidthPanel interface {
	Lines(width int) []string
	HandleKey(key tea.Key) (bool, tea.Cmd)
}

// SizedPanel is implemented by panels that format with height and width.
type SizedPanel interface {
	Lines(height, width int) []string
	HandleKey(key tea.Key) (bool, tea.Cmd)
}

// LinesPanel is implemented by panels that only render lines.
type LinesPanel interface {
	Lines() []string
}

// PanelSpec describes a declarative panel specification.
type PanelSpec struct {
	Kind    panelregistry.Kind
	Handler func(h RegistryHost) panelregistry.Handler
}

func makeStandardSpec(kind panelregistry.Kind) PanelSpec {
	return PanelSpec{
		Kind: kind,
		Handler: func(h RegistryHost) panelregistry.Handler {
			return panelregistry.FuncHandler{
				OnLines: func() []string {
					if p, ok := h.Panel(kind).(StandardPanel); ok {
						return p.Lines()
					}
					return nil
				},
				OnHandleKey: func(key tea.Key) (bool, tea.Cmd) {
					if p, ok := h.Panel(kind).(StandardPanel); ok {
						return p.HandleKey(key)
					}
					return false, nil
				},
				OnRowCount: func() int {
					if rc, ok := h.Panel(kind).(panelregistry.RowCounter); ok {
						return rc.RowCount()
					}
					return 0
				},
			}
		},
	}
}

// PanelSpecs lists the declarative specifications for all registered panels.
var PanelSpecs = []PanelSpec{
	// Static / View-only panels
	{
		Kind: panelregistry.MCP,
		Handler: func(h RegistryHost) panelregistry.Handler {
			return panelregistry.FuncHandler{
				OnLines: mcppanel.Lines,
			}
		},
	},
	{
		Kind: panelregistry.Artifact,
		Handler: func(h RegistryHost) panelregistry.Handler {
			return panelregistry.FuncHandler{
				OnLines: artifactpanel.Lines,
			}
		},
	},
	{
		Kind: panelregistry.Context,
		Handler: func(h RegistryHost) panelregistry.Handler {
			return panelregistry.FuncHandler{
				OnLines: func() []string { return contextpanel.Lines(h.ContextState()) },
			}
		},
	},
	{
		Kind: panelregistry.CodeSearch,
		Handler: func(h RegistryHost) panelregistry.Handler {
			return panelregistry.FuncHandler{
				OnLines: func() []string {
					if p, ok := h.Panel(panelregistry.CodeSearch).(LinesPanel); ok {
						return p.Lines()
					}
					return nil
				},
			}
		},
	},

	// Standard interactive panels (Lines + HandleKey + RowCount)
	makeStandardSpec(panelregistry.Tasks),
	makeStandardSpec(panelregistry.Effort),
	makeStandardSpec(panelregistry.Hooks),
	makeStandardSpec(panelregistry.Permissions),
	makeStandardSpec(panelregistry.Agents),
	makeStandardSpec(panelregistry.Credits),
	makeStandardSpec(panelregistry.Feedback),
	makeStandardSpec(panelregistry.Rewind),
	makeStandardSpec(panelregistry.Skills),
	makeStandardSpec(panelregistry.Accounts),
	makeStandardSpec(panelregistry.Diff),
	makeStandardSpec(panelregistry.AddDir),
	makeStandardSpec(panelregistry.Rename),

	// Dimension-aware panels
	{
		Kind: panelregistry.Model,
		Handler: func(h RegistryHost) panelregistry.Handler {
			return panelregistry.FuncHandler{
				OnLines: func() []string {
					if p, ok := h.Panel(panelregistry.Model).(interface{ Lines(...int) []string }); ok {
						return p.Lines(h.TerminalHeight())
					}
					return nil
				},
				OnRowCount: func() int {
					if rc, ok := h.Panel(panelregistry.Model).(panelregistry.RowCounter); ok {
						return rc.RowCount()
					}
					return 0
				},
			}
		},
	},
	{
		Kind: panelregistry.Config,
		Handler: func(h RegistryHost) panelregistry.Handler {
			return panelregistry.FuncHandler{
				OnLines: func() []string {
					if p, ok := h.Panel(panelregistry.Config).(SizedPanel); ok {
						return p.Lines(h.TerminalHeight(), h.TerminalWidth())
					}
					return nil
				},
				OnHandleKey: func(key tea.Key) (bool, tea.Cmd) {
					if p, ok := h.Panel(panelregistry.Config).(SizedPanel); ok {
						return p.HandleKey(key)
					}
					return false, nil
				},
				OnRowCount: func() int {
					if rc, ok := h.Panel(panelregistry.Config).(panelregistry.RowCounter); ok {
						return rc.RowCount()
					}
					return 0
				},
			}
		},
	},
	{
		Kind: panelregistry.Plugin,
		Handler: func(h RegistryHost) panelregistry.Handler {
			return panelregistry.FuncHandler{
				OnLines: func() []string {
					if p, ok := h.Panel(panelregistry.Plugin).(SizedWidthPanel); ok {
						return p.Lines(h.TerminalWidth())
					}
					return nil
				},
				OnHandleKey: func(key tea.Key) (bool, tea.Cmd) {
					if p, ok := h.Panel(panelregistry.Plugin).(SizedWidthPanel); ok {
						return p.HandleKey(key)
					}
					return false, nil
				},
				OnRowCount: func() int {
					if rc, ok := h.Panel(panelregistry.Plugin).(panelregistry.RowCounter); ok {
						return rc.RowCount()
					}
					return 0
				},
			}
		},
	},
	{
		Kind: panelregistry.Projects,
		Handler: func(h RegistryHost) panelregistry.Handler {
			return panelregistry.FuncHandler{
				OnLines: func() []string {
					if p, ok := h.Panel(panelregistry.Projects).(SizedWidthPanel); ok {
						return p.Lines(h.TerminalWidth())
					}
					return nil
				},
				OnHandleKey: func(key tea.Key) (bool, tea.Cmd) {
					if p, ok := h.Panel(panelregistry.Projects).(SizedWidthPanel); ok {
						return p.HandleKey(key)
					}
					return false, nil
				},
			}
		},
	},
	{
		Kind: panelregistry.Provider,
		Handler: func(h RegistryHost) panelregistry.Handler {
			return panelregistry.FuncHandler{
				OnLines: func() []string {
					if p, ok := h.Panel(panelregistry.Provider).(interface{ Lines(int, ...int) []string }); ok {
						return p.Lines(h.TerminalHeight(), h.TerminalWidth())
					}
					return nil
				},
				OnHandleKey: func(key tea.Key) (bool, tea.Cmd) {
					if p, ok := h.Panel(panelregistry.Provider).(interface{ HandleKey(tea.Key) (bool, tea.Cmd) }); ok {
						return p.HandleKey(key)
					}
					return false, nil
				},
				OnRowCount: func() int {
					if rc, ok := h.Panel(panelregistry.Provider).(panelregistry.RowCounter); ok {
						return rc.RowCount()
					}
					return 0
				},
			}
		},
	},

	// Viewer panels
	{
		Kind: panelregistry.Usage,
		Handler: func(h RegistryHost) panelregistry.Handler {
			return panelregistry.FuncHandler{
				OnLines:      h.PanelUsageLines,
				OnViewerSpec: func() (tuiviewer.Spec, bool) { return h.UsageViewerSpec(), true },
			}
		},
	},
	{
		Kind: panelregistry.Changelog,
		Handler: func(h RegistryHost) panelregistry.Handler {
			return panelregistry.FuncHandler{
				OnLines:      h.PanelChangelogLines,
				OnViewerSpec: func() (tuiviewer.Spec, bool) { return h.ChangelogViewerSpec(), true },
			}
		},
	},
	{
		Kind: panelregistry.FeatDiff,
		Handler: func(h RegistryHost) panelregistry.Handler {
			return panelregistry.FuncHandler{
				OnLines:      featdiff.PanelLines,
				OnViewerSpec: func() (tuiviewer.Spec, bool) { return featdiff.Spec(h.TerminalHeight()), true },
			}
		},
	},
}

// BuildRegistry creates and registers all panel handlers with the host seams.
func BuildRegistry(h RegistryHost) *panelregistry.Registry {
	reg := panelregistry.New()
	for _, spec := range PanelSpecs {
		if spec.Handler != nil {
			reg.Register(spec.Kind, spec.Handler(h))
		}
	}
	return reg
}
