package cliapp

import (
	tea "charm.land/bubbletea/v2"

	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/artifactpanel"
	"github.com/justwasm/pigasus/internal/ui/views/contextpanel"
	featdiff "github.com/justwasm/pigasus/internal/ui/views/diff/feat"
	"github.com/justwasm/pigasus/internal/ui/views/mcppanel"
)

func (m *runMode) getPanelRegistry() *panelregistry.Registry {
	if m.panelReg == nil {
		m.panelReg = m.buildPanelRegistry()
	}
	return m.panelReg
}

func (m *runMode) buildPanelRegistry() *panelregistry.Registry {
	reg := panelregistry.New()

	reg.Register(panelregistry.MCP, panelregistry.FuncHandler{
		OnLines: func() []string { return mcppanel.Lines() },
	})
	reg.Register(panelregistry.Tasks, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.tasksPanel().Lines() },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.tasksPanel().HandleKey(key) },
	})
	reg.Register(panelregistry.Effort, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.effortPanel().Lines() },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.effortPanel().HandleKey(key) },
	})
	reg.Register(panelregistry.Hooks, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.hooksPanel().Lines() },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.hooksPanel().HandleKey(key) },
	})
	reg.Register(panelregistry.Permissions, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.permissionsPanel().Lines() },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.permissionsPanel().HandleKey(key) },
	})
	reg.Register(panelregistry.Agents, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.agentsPanel().Lines() },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.agentsPanel().HandleKey(key) },
	})
	reg.Register(panelregistry.Credits, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.creditsPanel().Lines() },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.creditsPanel().HandleKey(key) },
	})
	reg.Register(panelregistry.CodeSearch, panelregistry.FuncHandler{
		OnLines: func() []string { return m.codesearchPanel().Lines() },
	})
	reg.Register(panelregistry.Artifact, panelregistry.FuncHandler{
		OnLines: func() []string { return artifactpanel.Lines() },
	})
	reg.Register(panelregistry.Feedback, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.feedbackPanel().Lines() },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.feedbackPanel().HandleKey(key) },
	})
	reg.Register(panelregistry.Rewind, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.rewindPanel().Lines() },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.rewindPanel().HandleKey(key) },
	})
	reg.Register(panelregistry.Context, panelregistry.FuncHandler{
		OnLines: func() []string { return contextpanel.Lines(m.contextState()) },
	})
	reg.Register(panelregistry.Model, panelregistry.FuncHandler{
		OnLines: func() []string { return m.modelPanel().Lines(m.height) },
	})
	reg.Register(panelregistry.Config, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.configPanel().Lines(m.height, m.width) },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.configPanel().HandleKey(key) },
	})
	reg.Register(panelregistry.Plugin, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.pluginPanel().Lines(m.width) },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.pluginPanel().HandleKey(key) },
	})
	reg.Register(panelregistry.Skills, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.skillsPanel().Lines() },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.skillsPanel().HandleKey(key) },
	})
	reg.Register(panelregistry.Usage, panelregistry.FuncHandler{
		OnLines:      func() []string { return m.panelUsageLines() },
		OnViewerSpec: func() (tuiviewer.Spec, bool) { return m.usageViewerSpec(), true },
	})
	reg.Register(panelregistry.Changelog, panelregistry.FuncHandler{
		OnLines:      func() []string { return m.panelChangelogLines() },
		OnViewerSpec: func() (tuiviewer.Spec, bool) { return m.changelogViewerSpec(), true },
	})
	reg.Register(panelregistry.Diff, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.diffPanel().Lines() },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.diffPanel().HandleKey(key) },
	})
	reg.Register(panelregistry.AddDir, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.addDirPanel().Lines() },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.addDirPanel().HandleKey(key) },
	})
	reg.Register(panelregistry.Rename, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.renamePanel().Lines() },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.renamePanel().HandleKey(key) },
	})
	reg.Register(panelregistry.FeatDiff, panelregistry.FuncHandler{
		OnLines:      func() []string { return featdiff.PanelLines() },
		OnViewerSpec: func() (tuiviewer.Spec, bool) { return featdiff.Spec(m.height), true },
	})
	reg.Register(panelregistry.Provider, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.providerPanel().Lines(m.height, m.width) },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.providerPanel().HandleKey(key) },
	})
	reg.Register(panelregistry.Accounts, panelregistry.FuncHandler{
		OnLines:     func() []string { return m.accountsPanel().Lines() },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return m.accountsPanel().HandleKey(key) },
	})

	return reg
}
