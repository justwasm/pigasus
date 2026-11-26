package tui

import (
	"strings"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/tui/panels"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/taskspanel"
)

// Tasks
func (m *RootModel) TaskRegistry() *taskregistry.Registry  { return m.tasks.reg }
func (m *RootModel) KillSelectedTask(t *taskregistry.Task) { m.killSelectedTask(t) }
func (m *RootModel) TerminalSize() (int, int)              { return m.viewport.width, m.viewport.height }
func (m *RootModel) ClosePanel() tea.Cmd                   { return m.closePanel() }
func (m *RootModel) newTasksPanel() *taskspanel.Panel      { return panels.NewTasksPanel(m) }
func (m *RootModel) tasksPanel() *taskspanel.Panel {
	if m.overlays.panel.tp == nil {
		m.overlays.panel.tp = m.newTasksPanel()
	}
	return m.overlays.panel.tp
}

func (m *RootModel) killSelectedTask(t *taskregistry.Task) {
	if t == nil {
		return
	}
	mgr := m.tasks.subagentManager
	if mgr == nil {
		mgr = subagent.NewManager()
	}
	taskregistry.KillByID(m.tasks.reg, t.ID, mgr)
}

func (m *RootModel) openTasksDetail(id string, isSubagent bool) tea.Cmd {
	if m.overlays.panel.kind != panelregistry.Tasks || m.overlays.panel.tp == nil {
		m.overlays.panel = slashPanel{kind: panelregistry.Tasks, cmd: ""}
		m.initOpenedSlashPanel(panelregistry.Tasks)
	}
	if m.overlays.panel.tp != nil {
		m.overlays.panel.tp.OpenTaskDetail(id, isSubagent)
		m.overlays.panel.tp.SetFromActiveItems(true)
	}
	m.activeItems.Panel.SetFocus(false)
	return m.ensureTasksTick()
}

func (m *RootModel) tasksStatusSuffix() string {
	taskPart := ""
	reg := m.tasks.reg
	if m.tasks.ctrl != nil && m.tasks.ctrl.Registry() != nil {
		reg = m.tasks.ctrl.Registry()
	}
	if reg != nil {
		taskPart = taskregistry.StatusSuffix(reg.CountRunning())
	}
	planPart := m.enginePlan()
	if planPart != "" {
		if taskPart != "" {
			return taskPart + " · " + planPart
		}
		return " · " + planPart
	}
	return taskPart
}

func (m *RootModel) shouldTickTasks() bool {
	reg := m.tasks.reg
	if m.tasks.ctrl != nil && m.tasks.ctrl.Registry() != nil {
		reg = m.tasks.ctrl.Registry()
	}
	if reg != nil && reg.CountRunning() > 0 {
		return true
	}
	if m.overlays.panel.kind == panelregistry.Tasks {
		if m.overlays.panel.tp != nil && m.overlays.panel.tp.Detail() {
			t := m.overlays.panel.tp.DetailTask()
			return t != nil && t.State == taskregistry.StateRunning
		}
		return reg != nil && reg.CountRunning() > 0
	}
	if m.overlays.panel.kind == panelregistry.Agents {
		if m.overlays.panel.ag != nil && m.overlays.panel.ag.DetailMode() {
			sa := m.overlays.panel.ag.DetailSubagent()
			return sa != nil && strings.ToLower(sa.State) == "running"
		}
	}
	return false
}

func (m *RootModel) startTasksSubscription() {
	if m.tasks.ctrl != nil {
		m.tasks.ctrl.SetOnChange(func() {
			if m.turn.prog != nil {
				m.sendTasksChanged()
			}
		})
		m.tasks.ctrl.StartSubscription()
		return
	}
	if m.tasks.reg == nil || m.tasks.subscribed {
		return
	}
	ch, cancel := m.tasks.reg.Subscribe()
	m.tasks.cancel = cancel
	m.tasks.subscribed = true
	go func() {
		for ev := range ch {
			_ = ev
			atomic.StoreInt32(&m.tasks.dirty, 1)
			if m.turn.prog != nil {
				m.sendTasksChanged()
			}
		}
	}()
}

func (m *RootModel) consumeTasksDirty() bool {
	if m.tasks.ctrl != nil {
		return m.tasks.ctrl.ConsumeDirty()
	}
	return atomic.SwapInt32(&m.tasks.dirty, 0) == 1
}
