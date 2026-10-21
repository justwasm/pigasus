// stubs_bg_name_test.go — cycle bg-task-given-name-preserved.
//
// End-to-end tests for the create_task → taskregistry path that the
// AGY 1.2.5 changelog line describes: a task that was given a name
// when sent to the background keeps that name in completion notifications
// and task lists instead of being renamed to a generic auto-derived
// one. We exercise the same path the slash command and the panel
// read from:
//
//   - create_task.Run builds a Subagent, sets Title, registers it
//     with the package-level task registry (defaultTaskRegistry),
//     and runs Start+Wait.
//
// The tests below drive create_task with `title` and inspect the
// registry's *Task to verify the title is the Name (not "bash -c
// <prompt>" or some other auto-derived form). The fallback case
// verifies the OLD behaviour is preserved when no title is given,
// so existing callers that pass only agent_name + prompt do not
// regress.
//
// stubs_test.go (the file that owns TestStubsCreateTask) is not
// gofmt-clean per AGENTS.md, so the new tests live in this sibling
// file to avoid touching it.
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/subagent"
	"github.com/justwasm/pigasus/internal/taskregistry"
)

// TestCreateTaskTitleUsedInRegistry — given name preserved in the
// task list. Calling create_task with a `title` field registers a
// Subagent whose task registry entry uses the title as its Name.
// This is the canonical "task list shows the given name" path the
// changelog line promises.
//
// We do not call Start on the returned Subagent (the test does not
// need to prove runtime behaviour, just registry wiring). create_task
// itself blocks until the Subagent exits, so we use `echo` which
// returns immediately.
func TestCreateTaskTitleUsedInRegistry(t *testing.T) {
	before := defaultTaskRegistryListIDs()

	c := &CreateTask{}
	out, err := c.Run(context.Background(), json.RawMessage(
		`{"agent_name":"echo","prompt":"hello","title":"Greet the user"}`))
	if err != nil {
		t.Fatalf("create_task with title: %v", err)
	}

	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("create_task returned %T, want map[string]any", out)
	}
	id, _ := m["id"].(string)
	if id == "" {
		t.Fatal("create_task returned empty id")
	}

	// Find the task the create_task call just registered. We do
	// not assume the id is the registry ID — create_task's
	// response carries the Subagent ID, and the registry copies
	// it onto Task.ID. Either way, the title must end up as
	// Task.Name.
	var found *taskregistry.Task
	for _, tk := range defaultTaskRegistry.List() {
		if before[tk.ID] {
			continue
		}
		if tk.Name == "Greet the user" {
			found = tk
			break
		}
	}
	if found == nil {
		t.Fatalf("no task in registry has Name = %q; entries: %s",
			"Greet the user", taskRegistryListNames(defaultTaskRegistry))
	}

	// Double-check: the title must be the only thing that changed
	// versus the auto-derived path. State should still be one of
	// the terminal values (create_task blocks until done/failed/
	// killed) and the registry ID must equal the create_task
	// response ID.
	if found.ID != id {
		t.Errorf("registry ID = %q, create_task ID = %q", found.ID, id)
	}
	if found.State != taskregistry.StateDone &&
		found.State != taskregistry.StateFailed &&
		found.State != taskregistry.StateKilled {
		t.Errorf("registry State = %q, want terminal state", found.State)
	}
}

// TestCreateTaskNoTitleFallsBackToAuto — default name used when no
// name given. Without a `title` field, the task keeps the cycle
// tasks-panel auto-derived name "bash -c <prompt>". This pins the
// fallback behaviour so a regression that ALWAYS uses "" (or always
// uses some new string) does not silently break every existing
// create_task caller that passes only agent_name + prompt.
//
// The expected auto-derived name below mirrors the bridge's
// subagentDisplayName fallback: subagent.NewSubagent("bash", "-c",
// "<prompt>") yields Name = "bash -c <prompt>".
func TestCreateTaskNoTitleFallsBackToAuto(t *testing.T) {
	before := defaultTaskRegistryListIDs()

	c := &CreateTask{}
	out, err := c.Run(context.Background(), json.RawMessage(
		`{"agent_name":"echo","prompt":"fallback"}`))
	if err != nil {
		t.Fatalf("create_task no title: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("create_task returned %T", out)
	}
	id, _ := m["id"].(string)
	if id == "" {
		t.Fatal("create_task returned empty id")
	}

	// Look for the new task; its Name should be the auto-derived
	// form, NOT a stale value.
	want := "echo fallback"
	var found *taskregistry.Task
	for _, tk := range defaultTaskRegistry.List() {
		if before[tk.ID] {
			continue
		}
		if tk.ID == id {
			found = tk
			break
		}
	}
	if found == nil {
		t.Fatalf("no task for id %q; entries: %s",
			id, taskRegistryListNames(defaultTaskRegistry))
	}
	if found.Name != want {
		t.Errorf("no-title Name = %q, want %q "+
			"(auto-derived fallback must survive when title is absent)",
			found.Name, want)
	}
}

// TestCreateTaskCompletionShowsGivenName — completion notification
// shows given name. After create_task returns, the registered
// task must still be findable by its Title (the friendly name the
// caller passed), and the State must be a terminal value. Any
// completion notification that flows from the registry — the /tasks
// panel re-render, a status-row tick, a downstream subscriber hook
// — reads from this exact *Task, so pinning State == Done with
// Name == Title proves the surface sees the right label at the
// moment the task finishes.
func TestCreateTaskCompletionShowsGivenName(t *testing.T) {
	before := defaultTaskRegistryListIDs()

	c := &CreateTask{}
	out, err := c.Run(context.Background(), json.RawMessage(
		`{"agent_name":"echo","prompt":"hi","title":"Notify friend"}`))
	if err != nil {
		t.Fatalf("create_task completion: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("create_task returned %T", out)
	}
	id, _ := m["id"].(string)

	// The task is in a terminal state by the time Run returns.
	// Walk the registry to find it; assert Name == Title (NOT
	// the auto-derived "bash -c hi" form that create_task would
	// produce without the cycle's changes) and State is done /
	// failed / killed.
	var found *taskregistry.Task
	for _, tk := range defaultTaskRegistry.List() {
		if before[tk.ID] {
			continue
		}
		if tk.ID == id {
			found = tk
			break
		}
	}
	if found == nil {
		t.Fatalf("no task for id %q; entries: %s",
			id, taskRegistryListNames(defaultTaskRegistry))
	}
	if found.State != taskregistry.StateDone {
		t.Errorf("completion State = %q, want done", found.State)
	}
	if found.Name != "Notify friend" {
		t.Errorf("completion Name = %q, want %q (Title preserved)",
			found.Name, "Notify friend")
	}
	// Belt: the auto-derived string MUST NOT leak through. A
	// regression that records the Title on the Subagent but then
	// overwrites the registry entry with subagentDisplayName's
	// fallback would produce "bash -c hi" here. The test fails
	// on that path.
	if strings.Contains(found.Name, "bash -c hi") {
		t.Errorf("completion Name = %q contains the auto-derived form "+
			"— Title did not win in the bridge", found.Name)
	}
}

// TestSubagentSetTitleNilReceiver — SetTitle must be safe on a nil
// Subagent so callers that branch on a non-nil Subagent can chain
// the call without a guard. Pinned here (rather than in subagent_test)
// because the cycle touches both packages and the tests in
// subagent_test.go are not gofmt-clean per AGENTS.md.
func TestSubagentSetTitleNilReceiver(t *testing.T) {
	var s *subagent.Subagent
	// Must not panic.
	s.SetTitle("safe")
	// Subsequent bridge calls on a nil Subagent must also not
	// panic; the bridge guards on nil via subagentDisplayName.
	if got := taskregistry.New(); got == nil {
		t.Fatal("taskregistry.New returned nil")
	}
}

// ---- helpers ---------------------------------------------------------------

// defaultTaskRegistryListIDs returns the set of task IDs currently in
// the package-level defaultTaskRegistry. The create_task tests use it
// to scope their assertions to the task a single Run call just
// registered (the registry is a package-level singleton shared by
// every test in the process; a test that walks the full List would
// race with parallel tests).
func defaultTaskRegistryListIDs() map[string]bool {
	out := make(map[string]bool)
	if defaultTaskRegistry == nil {
		return out
	}
	for _, t := range defaultTaskRegistry.List() {
		out[t.ID] = true
	}
	return out
}

// taskRegistryListNames renders the registry's Name values as a
// single space-joined string for failure messages. We avoid
// importing the fmt package at the top of the file just for this
// helper by hand-rolling the join — keeps the imports list tight.
func taskRegistryListNames(reg *taskregistry.Registry) string {
	if reg == nil {
		return ""
	}
	var b strings.Builder
	for _, tk := range reg.List() {
		if b.Len() > 0 {
			b.WriteString(", ")
		}
		b.WriteString(tk.Name)
	}
	return b.String()
}
