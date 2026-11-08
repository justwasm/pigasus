package tui

// tasks_model.go — the background-tasks sub-model.
//
// TasksModel owns the session's background tasks: the registry the rows
// come from, the controller that tracks changes and delivers notifications,
// the subagent manager tasks may kill through, and the subscription state.
// The /tasks panel itself lives in internal/ui/views/taskspanel and is built
// by the slash-panel registry; the model owns the data behind it.
//
// Upstream: google3/third_party/jetski/cli/model/model.TasksModel.

import (
	"github.com/justwasm/pigasus/internal/app/agentturn"
	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
)

// TasksModel owns the task registry, its change controller and the
// subscription that feeds the TUI.
type TasksModel struct {
	reg             *taskregistry.Registry
	ctrl            *taskregistry.Controller
	subagentManager *subagent.Manager
	inbox           *agentturn.TaskInbox
	subscribed      bool
	cancel          func()
	dirty           int32
}
