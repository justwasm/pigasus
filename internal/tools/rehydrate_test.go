package tools

import (
	"encoding/json"
	"testing"

	"github.com/justwasm/pigasus/internal/taskregistry"
)

// TestRehydrateManageTask pins the delta of a manage_task call.
//
// 【一手】 the reference stores it as {"Action":"status","TaskId":"<conv>/task-34"}
// — the capitalised native-payload names.
func TestRehydrateManageTask(t *testing.T) {
	meta, ok := RehydrateManageTask(json.RawMessage(
		`{"Action":"status","TaskId":"0d6f85b5-d661-4705-8a75-1387ab8a9446/task-34"}`))
	if !ok {
		t.Fatalf("RehydrateManageTask reported false")
	}
	if meta["action"] != "status" {
		t.Fatalf("action = %q, want status", meta["action"])
	}
	if meta["task_id"] != "0d6f85b5-d661-4705-8a75-1387ab8a9446/task-34" {
		t.Fatalf("task_id = %q", meta["task_id"])
	}
	if meta["task_ids"] != "0d6f85b5-d661-4705-8a75-1387ab8a9446/task-34" {
		t.Fatalf("task_ids = %q", meta["task_ids"])
	}
	if _, ok := meta["input"]; ok {
		t.Fatalf("an unset input must not appear in the delta: %v", meta)
	}
	// The clone's own spelling is accepted too.
	meta, ok = RehydrateManageTask(json.RawMessage(`{"action":"kill","id":"task-1","input":"stop"}`))
	if !ok {
		t.Fatalf("RehydrateManageTask(lower case) reported false")
	}
	if meta["action"] != "kill" || meta["task_id"] != "task-1" || meta["input"] != "stop" {
		t.Fatalf("delta = %v", meta)
	}
	if _, ok := RehydrateManageTask(json.RawMessage(`{}`)); ok {
		t.Fatalf("RehydrateManageTask without an action reported ok, want false")
	}
}

// TestRestoreManageTask pins the restore: the task the trajectory names
// comes back as a finished record — the work is gone, the record is not.
func TestRestoreManageTask(t *testing.T) {
	reg := taskregistry.New()
	meta, ok := RehydrateManageTask(json.RawMessage(`{"Action":"kill","TaskId":"conv/task-3"}`))
	if !ok {
		t.Fatalf("RehydrateManageTask reported false")
	}
	if !RestoreManageTask(reg, meta) {
		t.Fatalf("RestoreManageTask reported false")
	}
	task := reg.Get("conv/task-3")
	if task == nil {
		t.Fatalf("the task was not restored: %v", reg.List())
	}
	if task.State != taskregistry.StateDone {
		t.Fatalf("state = %v, want %v", task.State, taskregistry.StateDone)
	}
	// A second restore of the same task changes nothing.
	if RestoreManageTask(reg, meta) {
		t.Fatalf("RestoreManageTask of a known task reported true, want false")
	}
	// A list action names no task.
	if RestoreManageTask(reg, map[string]string{"action": "list"}) {
		t.Fatalf("RestoreManageTask of a list action reported true, want false")
	}
}

// TestRehydrateSchedule pins the delta of a schedule call.
//
// 【一手】 the reference stores it as
// {"DurationSeconds":90,"Prompt":"…","TimerCondition":"…"}.
//
// 【反编译】 the reference's `rehydrateSchedule` (LS 0x100f3f1b0) writes
// `mode`, `outcome`, `cancelled_by` and `iterations`; the clone's
// schedules never ran, so the outcome and the canceller are empty and
// the iteration count is zero.
func TestRehydrateSchedule(t *testing.T) {
	meta, ok := RehydrateSchedule(json.RawMessage(
		`{"DurationSeconds":90,"Prompt":"check the build","TimerCondition":"build done"}`))
	if !ok {
		t.Fatalf("RehydrateSchedule reported false")
	}
	if meta["mode"] != "timer" {
		t.Fatalf("mode = %q, want timer", meta["mode"])
	}
	if meta["iterations"] != "0" {
		t.Fatalf("iterations = %q, want 0", meta["iterations"])
	}
	if meta["outcome"] != "" || meta["cancelled_by"] != "" {
		t.Fatalf("the clone's schedules never ran: %v", meta)
	}
	if meta["Prompt"] != "check the build" {
		t.Fatalf("Prompt = %q", meta["Prompt"])
	}
	if meta["DurationSeconds"] != "90" {
		t.Fatalf("DurationSeconds = %q, want 90", meta["DurationSeconds"])
	}
	if meta["TimerCondition"] != "build done" {
		t.Fatalf("TimerCondition = %q", meta["TimerCondition"])
	}
	// A cron schedule.
	meta, ok = RehydrateSchedule(json.RawMessage(
		`{"CronExpression":"*/5 * * * *","Prompt":"poll","IsDaemon":true,"MaxIterations":3}`))
	if !ok {
		t.Fatalf("RehydrateSchedule(cron) reported false")
	}
	if meta["mode"] != "cron" {
		t.Fatalf("mode = %q, want cron", meta["mode"])
	}
	if meta["IsDaemon"] != "true" || meta["MaxIterations"] != "3" {
		t.Fatalf("delta = %v", meta)
	}
}

// TestScheduleTableReset pins that a table can be emptied: the
// rehydration pass replaces the conversation's state, so a second pass
// over the same trajectory must not double the schedules.
func TestScheduleTableReset(t *testing.T) {
	table := NewScheduleTable("conv-reset")
	meta, ok := RehydrateSchedule(json.RawMessage(`{"DurationSeconds":10,"Prompt":"p"}`))
	if !ok {
		t.Fatalf("RehydrateSchedule reported false")
	}
	if !RestoreSchedule(table, meta) {
		t.Fatalf("RestoreSchedule reported false")
	}
	if table.Len() != 1 {
		t.Fatalf("table length = %d, want 1", table.Len())
	}
	table.Reset()
	if table.Len() != 0 {
		t.Fatalf("table length = %d after the reset, want 0", table.Len())
	}
	if id := table.nextTaskID(); id != "task-1" {
		t.Fatalf("next task id = %q after the reset, want task-1", id)
	}
}

// TestRestoreScheduleKeepsTaskIDs pins the restore: the task ids a
// resumed conversation mints continue the conversation's numbering, so a
// schedule created after the resume does not collide with the ones the
// conversation already used.
func TestRestoreScheduleKeepsTaskIDs(t *testing.T) {
	table := NewScheduleTable("conv-1")
	for i := 0; i < 3; i++ {
		meta, ok := RehydrateSchedule(json.RawMessage(`{"DurationSeconds":10,"Prompt":"p"}`))
		if !ok {
			t.Fatalf("RehydrateSchedule reported false")
		}
		if !RestoreSchedule(table, meta) {
			t.Fatalf("RestoreSchedule reported false")
		}
	}
	schedules := table.Schedules()
	if len(schedules) != 3 {
		t.Fatalf("schedules = %d, want 3", len(schedules))
	}
	for i, s := range schedules {
		want := "conv-1/task-" + string(rune('1'+i))
		if s.ID != want {
			t.Fatalf("schedule %d id = %q, want %q", i, s.ID, want)
		}
	}
	// The next live schedule continues the numbering.
	taskID := table.nextTaskID()
	if taskID != "task-4" {
		t.Fatalf("next task id = %q, want task-4", taskID)
	}
}

// TestScheduleToolRecordsInTable pins that the live schedule tool
// records its schedule in the conversation's table — the table the
// rehydration pass rebuilds.
func TestScheduleToolRecordsInTable(t *testing.T) {
	tool := &ScheduleTool{Home: t.TempDir(), ConversationID: "conv-schedule-test"}
	table := ScheduleTableFor("conv-schedule-test")
	before := table.Len()
	res, err := tool.Run(t.Context(), json.RawMessage(`{"Prompt":"later","DurationSeconds":5}`))
	if err != nil {
		t.Fatalf("Run returned err: %v", err)
	}
	if res == nil {
		t.Fatalf("Run returned no result")
	}
	if got := table.Len(); got != before+1 {
		t.Fatalf("table length = %d, want %d", got, before+1)
	}
	entry := table.Schedules()[before]
	if entry.Prompt != "later" || entry.DurationSeconds != 5 {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.ID == "" {
		t.Fatalf("the schedule has no task id: %+v", entry)
	}
}
