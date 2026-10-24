package tools

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/taskregistry"
)

// The metadata keys the rehydrators write and the appliers read.
//
// 【反编译】 the reference's `rehydrateManageTask` (LS 0x100f3e1a0) and
// `rehydrateSchedule` (LS 0x100f3f1b0) build a `map[string]string` and
// attach it to the step they rebuild (`rehydratedGenericWithMetadata`,
// LS 0x100f3dfd0). The keys are `action`, `input`, `task_id`, `task_ids`
// for manage_task and `mode`, `outcome`, `cancelled_by`, `iterations`
// for schedule.
const (
	metaTaskAction = "action"
	metaTaskInput  = "input"
	metaTaskID     = "task_id"
	metaTaskIDs    = "task_ids"

	metaScheduleMode       = "mode"
	metaScheduleOutcome    = "outcome"
	metaScheduleCancelled  = "cancelled_by"
	metaScheduleIterations = "iterations"

	// The schedule's own fields travel with the delta under the names
	// the reference stores them under.
	//
	// 【一手】 the reference stores a schedule call as
	// {"DurationSeconds","Prompt","TimerCondition"}.
	metaSchedulePrompt          = "Prompt"
	metaScheduleDurationSeconds = "DurationSeconds"
	metaScheduleCronExpression  = "CronExpression"
	metaScheduleTimerCondition  = "TimerCondition"
	metaScheduleIsDaemon        = "IsDaemon"
	metaScheduleMaxIterations   = "MaxIterations"
	metaScheduleExactTiming     = "ExactTiming"
)

// manageTaskArgs is the argument set of a manage_task call.
//
// The reference's declaration is capitalised — `Action`, `Input`,
// `TaskId` (【一手】 internal/tools/model_tools_1.4.6.json) — and that
// is also the spelling the reference stores (【一手】 the stored calls
// read {"Action":"status","TaskId":"<conv-id>/task-34"}). The clone's
// own declaration uses the lower-case names, so both are accepted.
type manageTaskArgs struct {
	Action    string `json:"action"`
	ActionCap string `json:"Action,omitempty"`
	Input     string `json:"input,omitempty"`
	InputCap  string `json:"Input,omitempty"`
	ID        string `json:"id,omitempty"`
	IDCap     string `json:"Id,omitempty"`
	TaskID    string `json:"TaskId,omitempty"`
}

// taskID returns the task the call names.
func (a manageTaskArgs) taskID() string {
	for _, v := range []string{a.TaskID, a.IDCap, a.ID} {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// taskAction returns the action the call names.
func (a manageTaskArgs) taskAction() string {
	for _, v := range []string{a.ActionCap, a.Action} {
		if s := strings.TrimSpace(v); s != "" {
			return strings.ToLower(s)
		}
	}
	return ""
}

// taskInput returns the input the call delivers.
func (a manageTaskArgs) taskInput() string {
	for _, v := range []string{a.InputCap, a.Input} {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// RehydrateManageTask rebuilds the state delta of a manage_task call
// from its stored arguments.
//
// The delta carries the action, the input the action delivers and the
// task the action names.
//
// 【反编译】 the reference's `rehydrateManageTask` (LS 0x100f3e1a0)
// writes `action`, `input`, `task_id` and `task_ids`; the task ids are
// joined with a comma (`strings.Join`, the one-byte separator at
// 0x103510350).
func RehydrateManageTask(args json.RawMessage) (map[string]string, bool) {
	var a manageTaskArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, false
	}
	action := a.taskAction()
	if action == "" {
		return nil, false
	}
	meta := map[string]string{metaTaskAction: action}
	if input := a.taskInput(); input != "" {
		meta[metaTaskInput] = input
	}
	if id := a.taskID(); id != "" {
		meta[metaTaskID] = id
		meta[metaTaskIDs] = id
	}
	return meta, true
}

// RehydrateSchedule rebuilds the state delta of a schedule call from
// its stored arguments.
//
// The delta carries the schedule's runtime state — the reference's
// `mode`, `outcome`, `iterations` and `cancelled_by` — plus the
// schedule's own fields, so the conversation's schedule table can be
// rebuilt.
//
// 【反编译】 the reference's `rehydrateSchedule` (LS 0x100f3f1b0) writes
// exactly those four keys. The reference reads the runtime state from
// the native payload's schedule fields; the clone has no scheduler, so
// its schedules never ran: the mode is derived from the schedule's own
// shape, and the outcome, the iteration count and the canceller are
// empty.
//
// The arguments are parsed with the same parser the live handler uses
// (`parseScheduleParams`), so a rehydrated schedule and a live one
// agree by construction.
func RehydrateSchedule(args json.RawMessage) (map[string]string, bool) {
	p, err := parseScheduleParams(args)
	if err != nil {
		return nil, false
	}
	mode := "timer"
	if p.CronExpression != "" {
		mode = "cron"
	}
	meta := map[string]string{
		metaScheduleMode:       mode,
		metaScheduleOutcome:    "",
		metaScheduleCancelled:  "",
		metaScheduleIterations: "0",
		metaSchedulePrompt:     p.Prompt,
	}
	if p.DurationSeconds != 0 {
		meta[metaScheduleDurationSeconds] = strconv.Itoa(p.DurationSeconds)
	}
	if p.CronExpression != "" {
		meta[metaScheduleCronExpression] = p.CronExpression
	}
	if p.TimerCondition != "" {
		meta[metaScheduleTimerCondition] = p.TimerCondition
	}
	if p.IsDaemon {
		meta[metaScheduleIsDaemon] = "true"
	}
	if p.MaxIterations != 0 {
		meta[metaScheduleMaxIterations] = strconv.Itoa(p.MaxIterations)
	}
	if p.ExactTiming {
		meta[metaScheduleExactTiming] = "true"
	}
	return meta, true
}

// RestoreManageTask writes a rehydrated manage_task delta back into the
// task registry.
//
// The reference's task list is logical: a task is a scheduled trigger
// or a subagent, and it survives a restart. The clone's registry holds
// the work that is running in this process, so a task the trajectory
// names is restored as a finished record — the conversation did run it,
// and it is not running any more.
//
// A delta that names no task (a `list` action) restores nothing.
func RestoreManageTask(reg *taskregistry.Registry, meta map[string]string) bool {
	if reg == nil {
		return false
	}
	id := strings.TrimSpace(meta[metaTaskID])
	if id == "" {
		return false
	}
	if existing := reg.Get(id); existing != nil {
		return false
	}
	task := &taskregistry.Task{
		ID:   id,
		Name: id,
		Kind: taskregistry.KindOther,
		// The work is gone: a resumed conversation cannot revive a
		// process that died with the previous one.
		State: taskregistry.StateDone,
	}
	reg.Register(task)
	return true
}

// RestoreSchedule writes a rehydrated schedule delta back into the
// conversation's schedule table.
//
// The table mints the task id, so a resumed conversation continues its
// numbering: the third schedule of a conversation is task-3 whether the
// conversation was resumed or not.
func RestoreSchedule(table *ScheduleTable, meta map[string]string) bool {
	if table == nil {
		return false
	}
	prompt := meta[metaSchedulePrompt]
	if prompt == "" {
		return false
	}
	taskID := table.nextTaskID()
	entry := ScheduleEntry{
		ID:             table.ConversationID() + "/" + taskID,
		Prompt:         prompt,
		TimerCondition: meta[metaScheduleTimerCondition],
	}
	if v := meta[metaScheduleDurationSeconds]; v != "" {
		entry.DurationSeconds, _ = strconv.Atoi(v)
	}
	entry.CronExpression = meta[metaScheduleCronExpression]
	if v := meta[metaScheduleIsDaemon]; v == "true" {
		entry.IsDaemon = true
	}
	if v := meta[metaScheduleMaxIterations]; v != "" {
		entry.MaxIterations, _ = strconv.Atoi(v)
	}
	table.add(entry)
	return true
}
