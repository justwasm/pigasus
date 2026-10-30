// Package taskregistry is a backward-compatible facade over internal/app/task.
package taskregistry

import (
	"github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/subagent"
)

type Kind = task.Kind

const (
	KindShell     = task.KindShell
	KindSubagent  = task.KindSubagent
	KindTimer     = task.KindTimer
	KindRecurring = task.KindRecurring
	KindOther     = task.KindOther
)

type State = task.State

const (
	StatePending = task.StatePending
	StateRunning = task.StateRunning
	StateDone    = task.StateDone
	StateFailed  = task.StateFailed
	StateKilled  = task.StateKilled
	StateStopped = task.StateStopped
	StateRemoved = task.StateRemoved
)

const ExitCodeUnset = task.ExitCodeUnset
const TasksPanelLimit = task.TasksPanelLimit
const TaskDetailTitle = task.TaskDetailTitle

type Task = task.Task
type Registry = task.Registry
type TaskEvent = task.TaskEvent
type PanelRows = task.PanelRows
type SubagentHeaderContext = task.SubagentHeaderContext

var (
	New              = task.New
	NewPanelRows     = task.NewPanelRows
	ErrClosed        = task.ErrClosed
	SetClock         = task.SetClock
	KillByID         = task.KillByID
	TaskDetailBody   = task.TaskDetailBody
	SubagentDetailHeader = task.SubagentDetailHeader
	SubagentStateWord    = task.SubagentStateWord
	SubagentStateColor   = task.SubagentStateColor
	SubagentDetailBody   = task.SubagentDetailBody
	StateWord            = task.StateWord
	TaskStatus           = task.TaskStatus
	StateStyle           = task.StateStyle
	StateClose           = task.StateClose
	Elapsed              = task.Elapsed
	RowLine              = task.RowLine
	ActiveRowLine        = task.ActiveRowLine
	ActiveSubagentRowLine = task.ActiveSubagentRowLine
	StatusSuffix         = task.StatusSuffix
)

// Ensure KillByID signature matches
func _() {
	var _ func(r *Registry, id string, m *subagent.Manager) bool = KillByID
}
