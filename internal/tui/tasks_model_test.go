package tui

import (
	"testing"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
)

// TestTasksModel_OwnsRegistry pins the ownership move: the task registry
// and controller hang off TasksModel, not the engine bucket.
func TestTasksModel_OwnsRegistry(t *testing.T) {
	var empty TasksModel
	if empty.ActiveTaskCount() != 0 {
		t.Fatal("empty model reports running tasks")
	}

	reg := taskregistry.New()
	m := diffTestModel()
	m.tasks.reg = reg
	m.tasks.ctrl = taskregistry.NewController(reg)
	if m.tasks.reg != reg || m.tasks.ActiveTaskCount() != 0 {
		t.Fatal("the model did not keep the registry")
	}
}

// TestTasksModel_KeepsSubagentManager keeps the manager /tasks uses to kill
// a running subagent on the same model.
func TestTasksModel_KeepsSubagentManager(t *testing.T) {
	var tsk TasksModel
	if tsk.subagentManager != nil || tsk.inbox != nil || tsk.subscribed || tsk.cancel != nil || tsk.dirty != 0 {
		t.Fatal("zero model has subscription state")
	}
	tsk.inbox = agentturn.NewTaskInbox()
	if tsk.inbox == nil {
		t.Fatal("inbox ownership was not kept")
	}
}
