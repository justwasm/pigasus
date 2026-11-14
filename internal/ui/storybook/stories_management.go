package storybook

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	coreprojects "github.com/justwasm/pigasus/internal/core/cliflags/projects"
	"github.com/justwasm/pigasus/internal/ui/views/adddirpanel"
	"github.com/justwasm/pigasus/internal/ui/views/codesearchpanel"
	"github.com/justwasm/pigasus/internal/ui/views/configpanel"
	"github.com/justwasm/pigasus/internal/ui/views/hookspanel"
	"github.com/justwasm/pigasus/internal/ui/views/projects"
	"github.com/justwasm/pigasus/internal/ui/views/providerpanel"
)

// -----------------------------------------------------------------------------
// 1. ConfigPanel Story
// -----------------------------------------------------------------------------

type configPanelStory struct {
	width, height int
	stateIdx      int
	panel         *configpanel.Panel
}

func newConfigPanelStory() Story {
	return Story{
		ID:          "panel-config",
		Name:        "Config Panel (/config)",
		Category:    CategoryPanels,
		Description: "Interactive global configuration settings editor with theme selection, alt screen, and vim mode.",
		UpstreamSym: "jetski/cli/panel.ConfigPanel",
		States: []State{
			{ID: "settings-list", Name: "Settings List", Description: "Full settings catalog with sections and currently active values."},
			{ID: "editing-row", Name: "Row Walk & Values", Description: "Cursor moved onto alternate screen / vim keys row with live toggles."},
		},
		Factory: func() StoryInstance {
			s := &configPanelStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *configPanelStory) rebuild() {
	s.panel = configpanel.New(configpanel.Seams{
		ApplyTheme: func(string) tea.Cmd { return nil },
	})
	if s.stateIdx == 1 {
		// Move cursor down to toggleable settings
		s.panel.HandleKey(tea.Key{Code: tea.KeyDown})
		s.panel.HandleKey(tea.Key{Code: tea.KeyDown})
	}
}

func (s *configPanelStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *configPanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *configPanelStory) View() string {
	if s.panel == nil {
		return ""
	}
	lines := s.panel.Lines(s.height, s.width)
	return strings.Join(lines, "\n")
}

func (s *configPanelStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel == nil {
		return nil, false
	}
	handled, cmd := s.panel.HandleKey(msg.Key())
	return cmd, handled
}

// -----------------------------------------------------------------------------
// 2. CodeSearchPanel Story
// -----------------------------------------------------------------------------

type codeSearchStory struct {
	width, height int
	stateIdx      int
	panel         *codesearchpanel.Panel
}

func newCodeSearchStory() Story {
	return Story{
		ID:          "panel-codesearch",
		Name:        "Code Search Panel (/codesearch)",
		Category:    CategoryPanels,
		Description: "Streaming ripgrep code search with hit counter, file headers, and expandable context viewer.",
		UpstreamSym: "jetski/cli/panel.CodeSearchPanel",
		States: []State{
			{ID: "usage", Name: "Usage Form", Description: "Initial empty search form prompting for query syntax."},
			{ID: "results", Name: "Search Results", Description: "Rendered match hits across multiple files with context lines."},
			{ID: "file-viewer", Name: "Inline File Viewer", Description: "Result opened in the built-in file viewer with line numbering."},
		},
		Factory: func() StoryInstance {
			s := &codeSearchStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *codeSearchStory) rebuild() {
	s.panel = codesearchpanel.New(codesearchpanel.Seams{
		Root: func() string { return "." },
		Size: func() (int, int) { return s.width, s.height },
		ReadFile: func(path string) ([]string, string, bool) {
			return []string{"// File: " + path, "package main", "", "func New() *Instance {", "\treturn &Instance{}", "}"}, "", true
		},
	})

	switch s.stateIdx {
	case 0:
		s.panel.Start("")
	case 1:
		s.panel.Start("func New")
	case 2:
		s.panel.Start("func New")
		s.panel.HandleKey(tea.Key{Code: tea.KeyEnter})
	}
}

func (s *codeSearchStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *codeSearchStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *codeSearchStory) View() string {
	if s.panel == nil {
		return ""
	}
	lines := s.panel.Lines()
	return strings.Join(lines, "\n")
}

func (s *codeSearchStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel == nil {
		return nil, false
	}
	handled, cmd, _ := s.panel.HandleKey(msg.Key())
	return cmd, handled
}

// -----------------------------------------------------------------------------
// 3. ProjectsPanel Story
// -----------------------------------------------------------------------------

type projectsStory struct {
	width, height int
	stateIdx      int
	panel         *projects.Panel
}

func newProjectsStory() Story {
	return Story{
		ID:          "panel-projects",
		Name:        "Projects Panel (/projects)",
		Category:    CategoryPanels,
		Description: "Local workspace project switcher, project creator, and folder association editor.",
		UpstreamSym: "jetski/cli/panel.ProjectsPanel",
		States: []State{
			{ID: "project-list", Name: "Project List", Description: "List of configured projects with active workspace indicator."},
			{ID: "create-modal", Name: "Create Project Modal", Description: "New project name prompt dialog."},
			{ID: "edit-folders", Name: "Edit Associated Folders", Description: "Editing directory folders tied to selected project."},
		},
		Factory: func() StoryInstance {
			s := &projectsStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *projectsStory) rebuild() {
	s.panel = projects.New("/Users/demo", "/Users/demo/openagy", projects.Seams{})
	s.panel.Projects = []coreprojects.LocalProject{
		{ID: "openagy", Name: "OpenAGY Reference Agent", Folders: []string{"/Users/demo/openagy"}},
		{ID: "worker-ai", Name: "Cloudflare Agents SDK", Folders: []string{"/Users/demo/worker-ai", "/Users/demo/shared"}},
		{ID: "tui-kit", Name: "Bubbletea Components", Folders: []string{"/Users/demo/tui-kit"}},
	}
	s.panel.CurrentID = "openagy"

	switch s.stateIdx {
	case 0:
		s.panel.Mode = projects.ModeList
	case 1:
		s.panel.Mode = projects.ModeCreateName
	case 2:
		s.panel.Mode = projects.ModeEditFolders
	}
}

func (s *projectsStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *projectsStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *projectsStory) View() string {
	if s.panel == nil {
		return ""
	}
	lines := s.panel.Lines(s.width)
	return strings.Join(lines, "\n")
}

func (s *projectsStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel == nil {
		return nil, false
	}
	handled, cmd := s.panel.HandleKey(msg.Key())
	return cmd, handled
}

// -----------------------------------------------------------------------------
// 4. AddDirPanel Story
// -----------------------------------------------------------------------------

type addDirStory struct {
	width, height int
	stateIdx      int
	panel         *adddirpanel.Panel
}

func newAddDirStory() Story {
	return Story{
		ID:          "panel-adddir",
		Name:        "Add Directory Panel (/add-dir)",
		Category:    CategoryPanels,
		Description: "Path entry dialog for adding trusted workspace directories to the session.",
		UpstreamSym: "jetski/cli/panel.AddDirPanel",
		States: []State{
			{ID: "trusted-dirs", Name: "Trusted Directories", Description: "List of existing trusted workspace folders."},
			{ID: "input-path", Name: "Input New Path", Description: "Path input buffer filled with new directory candidate."},
		},
		Factory: func() StoryInstance {
			s := &addDirStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *addDirStory) rebuild() {
	s.panel = adddirpanel.New(adddirpanel.Seams{
		Trusted: func() []string {
			return []string{
				"/Users/demo/projects/openagy",
				"/Users/demo/projects/pigasus-core",
			}
		},
	})
	if s.stateIdx == 1 {
		s.panel.SetText("/Users/demo/projects/plugins/gemini-tools")
	} else {
		s.panel.SetText("")
	}
}

func (s *addDirStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *addDirStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *addDirStory) View() string {
	if s.panel == nil {
		return ""
	}
	lines := s.panel.Lines()
	return strings.Join(lines, "\n")
}

func (s *addDirStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel == nil {
		return nil, false
	}
	handled, cmd := s.panel.HandleKey(msg.Key())
	return cmd, handled
}

// -----------------------------------------------------------------------------
// 5. HooksPanel Story
// -----------------------------------------------------------------------------

type hooksStory struct {
	width, height int
	stateIdx      int
	panel         *hookspanel.Panel
}

func newHooksStory() Story {
	return Story{
		ID:          "panel-hooks",
		Name:        "Hooks Panel (/hooks)",
		Category:    CategoryPanels,
		Description: "Interactive 3-stage lifecycle and tool hooks manager with matcher inspection and rule forms.",
		UpstreamSym: "jetski/cli/panel.HooksPanel",
		States: []State{
			{ID: "hook-types", Name: "Stage 1: Hook Types", Description: "List of builtin lifecycle and tool hook event triggers."},
			{ID: "matchers", Name: "Stage 2: Matcher List", Description: "Matcher rules and active filters for selected hook type."},
			{ID: "form", Name: "Stage 3: Add Rule Form", Description: "Input form to define a new tool matcher or lifecycle handler."},
		},
		Factory: func() StoryInstance {
			s := &hooksStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *hooksStory) rebuild() {
	s.panel = hookspanel.New(hookspanel.Seams{})
	switch s.stateIdx {
	case 1:
		s.panel.HandleKey(tea.Key{Code: tea.KeyEnter})
	case 2:
		s.panel.HandleKey(tea.Key{Code: tea.KeyEnter})
		s.panel.HandleKey(tea.Key{Code: tea.KeyEnter})
	}
}

func (s *hooksStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *hooksStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *hooksStory) View() string {
	if s.panel == nil {
		return ""
	}
	lines := s.panel.Lines()
	return strings.Join(lines, "\n")
}

func (s *hooksStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel == nil {
		return nil, false
	}
	handled, cmd := s.panel.HandleKey(msg.Key())
	return cmd, handled
}

// -----------------------------------------------------------------------------
// 6. ProviderPanel Story
// -----------------------------------------------------------------------------

type providerStory struct {
	width, height int
	stateIdx      int
	panel         *providerpanel.Panel
}

func newProviderStory() Story {
	return Story{
		ID:          "panel-provider",
		Name:        "Provider Panel (/provider)",
		Category:    CategoryPanels,
		Description: "Custom AI provider manager with endpoint form, API key configuration, and OAuth authentication.",
		UpstreamSym: "jetski/cli.ProviderPanel",
		States: []State{
			{ID: "provider-list", Name: "Provider List", Description: "Configured model providers and endpoints list."},
			{ID: "search-filter", Name: "Search Filter", Description: "Filtering providers by name or model tag."},
			{ID: "edit-form", Name: "Add/Edit Endpoint Form", Description: "Interactive form for custom BaseURL, API Key, and wire format."},
		},
		Factory: func() StoryInstance {
			s := &providerStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *providerStory) rebuild() {
	s.panel = providerpanel.New(providerpanel.Seams{
		CurrentModel: func() string { return "gemini-2.5-pro" },
	})
	s.panel.Reload()

	switch s.stateIdx {
	case 1:
		// Send search character
		s.panel.HandleKey(tea.Key{Code: 'c'})
	case 2:
		// Enter form mode
		s.panel.HandleKey(tea.Key{Code: tea.KeyEnter})
	}
}

func (s *providerStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *providerStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *providerStory) View() string {
	if s.panel == nil {
		return ""
	}
	lines := s.panel.Lines(s.height, s.width)
	return strings.Join(lines, "\n")
}

func (s *providerStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel == nil {
		return nil, false
	}
	handled, cmd := s.panel.HandleKey(msg.Key())
	return cmd, handled
}

// RegisterManagementStories mounts all management and configuration stories into the catalogue.
func RegisterManagementStories(c *Catalog) {
	c.Register(newConfigPanelStory())
	c.Register(newCodeSearchStory())
	c.Register(newProjectsStory())
	c.Register(newAddDirStory())
	c.Register(newHooksStory())
	c.Register(newProviderStory())
}

