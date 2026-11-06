package panels

import (
	tea "charm.land/bubbletea/v2"

	coreprojects "github.com/justwasm/pigasus/internal/core/cliflags/projects"
	projectspanel "github.com/justwasm/pigasus/internal/ui/views/projects"
)

// ProjectsHost defines the host operations needed by the /projects panel.
type ProjectsHost interface {
	ClosePanel() tea.Cmd
	SwitchProject(proj coreprojects.LocalProject) tea.Cmd
	WorkspaceDir() string
	HomeDir() string
}

// NewProjectsPanel builds the /projects panel with the given host seams.
func NewProjectsPanel(h ProjectsHost) *projectspanel.Panel {
	return projectspanel.New(h.HomeDir(), h.WorkspaceDir(), projectspanel.Seams{
		Close:         h.ClosePanel,
		SwitchProject: h.SwitchProject,
	})
}
