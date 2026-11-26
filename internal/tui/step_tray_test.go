package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/app/tools/tasks"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
)

func TestStepTray_SummaryAndAutoHide(t *testing.T) {
	var tray stepTray
	if tray.visible() {
		t.Fatal("empty tray should not be visible")
	}

	tray.sync([]tasks.StepItem{
		{Title: "Inspect files", Status: tasks.StepStatusDone},
		{Title: "Edit code", Status: tasks.StepStatusInProgress},
		{Title: "Skip old path", Status: tasks.StepStatusSkipped},
		{Title: "Check lint", Status: tasks.StepStatusFailed},
		{Title: "Run tests", Status: tasks.StepStatusPending},
	})
	if !tray.visible() {
		t.Fatal("tray with pending/in_progress steps should be visible")
	}
	if got, want := tray.statusSummary(), "1/4 steps · 1 failed · 1 skipped"; got != want {
		t.Fatalf("statusSummary() = %q, want %q", got, want)
	}

	// Once all steps finish (done/skipped/failed), tray hides automatically.
	tray.sync([]tasks.StepItem{
		{Title: "Inspect files", Status: tasks.StepStatusDone},
		{Title: "Edit code", Status: tasks.StepStatusDone},
		{Title: "Skip old path", Status: tasks.StepStatusSkipped},
		{Title: "Check lint", Status: tasks.StepStatusFailed},
		{Title: "Run tests", Status: tasks.StepStatusDone},
	})
	if tray.visible() {
		t.Fatal("tray should hide automatically once all steps finish")
	}
	if rows := tray.renderTreeRows(80); len(rows) != 0 {
		t.Fatalf("expected no tree rows when finished, got %v", rows)
	}
}

func TestStepTray_WindowingOverSevenSteps(t *testing.T) {
	var tray stepTray
	items := make([]tasks.StepItem, 10)
	for i := range items {
		status := tasks.StepStatusPending
		if i < 5 {
			status = tasks.StepStatusDone
		} else if i == 5 {
			status = tasks.StepStatusInProgress
		}
		items[i] = tasks.StepItem{
			Title:  "Step " + string(rune('A'+i)),
			Status: status,
		}
	}
	tray.sync(items)

	start, end := tray.windowRange(7)
	if end-start != 7 {
		t.Fatalf("window size = %d, want 7 (%d..%d)", end-start, start, end)
	}
	if start <= 0 || end >= len(items) {
		t.Fatalf("expected middle window with both earlier and more, got %d..%d", start, end)
	}

	rows := tray.renderTreeRows(80)
	joined := ansi.Strip(strings.Join(rows, "\n"))
	if !strings.Contains(joined, "⋮ 2 earlier") {
		t.Fatalf("expected '⋮ 2 earlier' in rows:\n%s", joined)
	}
	if !strings.Contains(joined, "⋮ 1 more") {
		t.Fatalf("expected '⋮ 1 more' in rows:\n%s", joined)
	}
	if !strings.Contains(joined, "● Step F") {
		t.Fatalf("expected active step '● Step F' in rows:\n%s", joined)
	}
}

func TestRootModel_UpdateStepsLiveRowsAndRegistration(t *testing.T) {
	m := newSlashModel(t)
	m.viewport.width = 80
	m.viewport.height = 24
	m.conversation.streaming = true

	catalog := surface.RuntimeCatalog(t.TempDir())
	decls := m.registerUpdateStepsTool(catalog, toolrunner.AdvertisedToolDecls("", catalog))
	foundDecl := false
	for _, d := range decls {
		if d.Name == tasks.UpdateStepsToolName {
			foundDecl = true
			break
		}
	}
	if !foundDecl {
		t.Fatal("expected update_steps in advertised declarations")
	}
	if _, ok := catalog.Get(tasks.UpdateStepsToolName); !ok {
		t.Fatal("expected update_steps in runtime catalog")
	}

	args := json.RawMessage(`{"steps":[{"title":"Read config","status":"done"},{"title":"Patch handler","status":"in_progress"},{"title":"Verify tests","status":"pending"}]}`)
	md := "- [x] Read config\n- [>] Patch handler\n- [ ] Verify tests"

	m.Update(toolsteps.ToolRunningMsg{Step: toolsteps.NewRunningStep(tasks.UpdateStepsToolName, args)})
	m.Update(toolsteps.ToolStepMsg{Step: toolsteps.NewToolStep(tasks.UpdateStepsToolName, args, md, false)})

	if len(m.conversation.response) != 0 {
		t.Fatalf("update_steps should not pollute transcript, got %v", m.conversation.response)
	}

	live := ansi.Strip(strings.Join(m.liveRows(80), "\n"))
	if !strings.Contains(live, "1/3 steps") {
		t.Fatalf("expected '1/3 steps' in liveRows:\n%s", live)
	}
	if !strings.Contains(live, "✓ Read config") || !strings.Contains(live, "● Patch handler") || !strings.Contains(live, "○ Verify tests") {
		t.Fatalf("expected checklist items in liveRows:\n%s", live)
	}

	// Mark all done -> checklist hides automatically.
	doneMD := "- [x] Read config\n- [x] Patch handler\n- [x] Verify tests"
	m.Update(toolsteps.ToolStepMsg{Step: toolsteps.NewToolStep(tasks.UpdateStepsToolName, args, doneMD, false)})
	liveAfter := ansi.Strip(strings.Join(m.liveRows(80), "\n"))
	if strings.Contains(liveAfter, "steps") || strings.Contains(liveAfter, "Read config") {
		t.Fatalf("expected checklist to hide once all steps finish, got:\n%s", liveAfter)
	}
}
