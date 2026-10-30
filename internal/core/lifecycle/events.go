package lifecycle

// ---- CascadeEvent (cycle 45 Issue #7 integration) -----------------------
//
// The 1.3.5 binary's decompiled `cascader.Run` loop emits a
// sequence of step events that the recovered TUI subscribes to
// in order to render the live task list (cli_printmode.tui.tasksPanel).
// Cycle 45 wires the recovered `cascade.RealLifecycle` to that same
// event vocabulary: a real emit surface that any consumer
// (notably internal/tui.TasksPanel.OnCascadeEvent) can hook into.

// CascadeEventType is the event discriminator emitted by
// RealLifecycle to consumers (TasksPanel, etc).
type CascadeEventType string

const (
	// EventStepStart — a logical step just began. RealLifecycle
	// emits this when the cascader transitions from one of
	// {CascaderInit, CascaderReady, CascaderExecute, CascaderStream}
	// into CascaderExecute / CascaderStream / CascaderSave.
	EventStepStart CascadeEventType = "start"
	// EventStepEnd — a logical step just completed. RealLifecycle
	// emits this when the cascader leaves one of
	// {CascaderExecute, CascaderStream, CascaderSave, CascaderDone}
	// for CascaderSave / CascaderDone / CascaderError.
	EventStepEnd CascadeEventType = "end"
)

// CascadeEvent is the recovered 1.3.5 step event. It carries
// just enough information for a consumer (e.g. the TUI's
// TasksPanel) to render the live step list without depending
// on the cascade package's internal State.
//
// StepID is a stable identifier for the step (currently
// one of "init", "execute", "stream", "save"); consumers use
// it to match end events to start events. Title is the
// human-readable label. Error is non-empty only for end
// events when the step failed.
type CascadeEvent struct {
	Type   CascadeEventType
	StepID string
	Title  string
	Error  string
}

// SetEventEmitter wires an optional event callback. The
// callback is invoked synchronously from CascaderRun at every
// step boundary; pass nil to silence. The cycle 45
// integration test (internal/tui/tasks_integration_test.go)
// uses this hook to drive a TasksPanel without a full
// backend.Backend round-trip.
func (r *RealLifecycle) SetEventEmitter(fn func(CascadeEvent)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onEvent = fn
}

// emitEvent fires the optional event callback. Safe to call
// when no callback is wired.
func (r *RealLifecycle) emitEvent(ev CascadeEvent) {
	r.mu.Lock()
	fn := r.onEvent
	r.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// stepTitle is the human-readable label for a known step.
// Keeps the event-emission code (CascaderRun) free of magic
// strings.
func stepTitle(stepID string) string {
	switch stepID {
	case "init":
		return "Read user request"
	case "execute":
		return "Execute tools"
	case "stream":
		return "Stream response"
	case "save":
		return "Save session"
	default:
		return stepID
	}
}

// stepIDForState maps a CascaderState transition to the step
// it represents. Returns "" if the transition is not a step
// boundary (e.g. init -> ready, or save -> done).
func stepIDForState(to CascaderState) string {
	switch to {
	case CascaderExecute:
		return "execute"
	case CascaderStream:
		return "stream"
	case CascaderSave:
		return "save"
	case CascaderDone:
		return "save"
	default:
		return ""
	}
}
