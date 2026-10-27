package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/agystep"
)

// CommandStatus — Source: tools.command_status.
type CommandStatus struct{}

func (c *CommandStatus) Name() string { return "command_status" }
func (c *CommandStatus) Schema() json.RawMessage {
	return obj(`"id":{"type":"string","description":"Identifier returned by run_command when background was set."}`, "id")
}
func (c *CommandStatus) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	t, ok := backgroundTasks.get(p.ID)
	if !ok {
		return nil, fmt.Errorf("command_status: unknown command id %q", p.ID)
	}
	return t.status(), nil
}

// SendCommandInput — Source: tools.send_command_input. Writes a line to the
// stdin of a background command.
//
// The result text is the reference's own: the step string that
// step_strings/send_command_input.tmpl renders. That template is the only
// source of the wording in the reference's binary — "Input sent
// successfully.", "The command is still running." and "The command exited
// with code N." each appear exactly once in `strings -a ~/.local/bin/agy`,
// inside that template file — so this is the reference's rendering, not a
// guess. See internal/agystep.
type SendCommandInput struct{}

func (s *SendCommandInput) Name() string { return "send_command_input" }
func (s *SendCommandInput) Schema() json.RawMessage {
	return obj(`"id":{"type":"string"},`+
		`"input":{"type":"string","description":"Line written to the command's stdin."}`,
		"id", "input")
}
func (s *SendCommandInput) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		ID    string `json:"id"`
		Input string `json:"input"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	t, ok := backgroundTasks.get(p.ID)
	if !ok {
		return nil, fmt.Errorf("send_command_input: unknown command id %q", p.ID)
	}
	if err := t.writeStdin(p.Input + "\n"); err != nil {
		return nil, err
	}
	output, running, exitCode := t.stepState()
	step := agystep.NewStep(agystep.StepOptions{
		Status: agystep.StatusDone,
		Source: agystep.SourceModel,
		SendCommandInput: agystep.NewSendCommandInput(agystep.SendCommandInputOptions{
			Output:   output,
			Running:  running,
			ExitCode: exitCode,
		}),
	})
	// The template's rendering starts with a newline — its own whitespace
	// handling, which the reference's step strings carry between a result's
	// stamp and the text. This result has no stamp, so the newline goes.
	return strings.TrimPrefix(agystep.SendCommandInputString(step), "\n"), nil
}

// ManageTask — Source: tools.manage_task. Lists or drives the background
// tasks of the conversation.
//
// 【一手】 The reference's own declaration (the captured 1.4.6 wire
// declaration and the CLI-side description recovered from the 1.4.8
// binary) advertises four actions:
//
//	- 'list': List all currently running background tasks
//	- 'kill': Cancel the task's execution
//	- 'status': Check the task's current status and log file location
//	- 'send_input': Send input to a running task
//
// The clone implements all four. The tasks it manages are the
// conversation's background commands; the schedules a conversation owns
// are listed by the /tasks panel (internal/cliapp), which is where the
// reference puts them too.
type ManageTask struct{}

func (m *ManageTask) Name() string { return "manage_task" }
func (m *ManageTask) Schema() json.RawMessage {
	return obj(`"action":{"type":"string","description":"One of list, kill, status or send_input."},`+
		`"id":{"type":"string","description":"Task id, required for kill, status and send_input."},`+
		`"input":{"type":"string","description":"Input sent to the task's stdin, for send_input."}`,
		"action")
}
func (m *ManageTask) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		Action string `json:"action"`
		ID     string `json:"id"`
		Input  string `json:"input"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	switch p.Action {
	case "list":
		all := backgroundTasks.list()
		if len(all) == 0 {
			// 【一手】 1.4.4 (standard and custom-agent turns):
			// "No background tasks are currently running."
			return resultStamp(start) + "No background tasks are currently running.", nil
		}
		var b strings.Builder
		for _, s := range all {
			b.WriteString(s)
			b.WriteByte('\n')
		}
		return resultStamp(start) + strings.TrimRight(b.String(), "\n"), nil
	case "kill":
		if p.ID == "" {
			return nil, fmt.Errorf("manage_task: id is required for kill")
		}
		t, ok := backgroundTasks.get(p.ID)
		if !ok {
			return nil, fmt.Errorf("manage_task: unknown task id %q", p.ID)
		}
		out, err := t.kill()
		if err != nil {
			return out, err
		}
		return resultStamp(start) + fmt.Sprint(out), nil
	case "status":
		if p.ID == "" {
			return nil, fmt.Errorf("manage_task: id is required for status")
		}
		t, ok := backgroundTasks.get(p.ID)
		if !ok {
			return nil, fmt.Errorf("manage_task: unknown task id %q", p.ID)
		}
		return resultStamp(start) + t.status(), nil
	case "send_input":
		if p.ID == "" {
			return nil, fmt.Errorf("manage_task: id is required for send_input")
		}
		t, ok := backgroundTasks.get(p.ID)
		if !ok {
			return nil, fmt.Errorf("manage_task: unknown task id %q", p.ID)
		}
		if err := t.writeStdin(p.Input + "\n"); err != nil {
			return nil, err
		}
		return resultStamp(start) + "Input sent successfully.", nil
	}
	return nil, fmt.Errorf("manage_task: unknown action %q (want list, kill, status or send_input)", p.Action)
}

// ---- control flow ----------------------------------------------------------

// WaitTool — Source: tools.wait. Sleeps, bounded by the workspace's context.
type WaitTool struct{}

func (w *WaitTool) Name() string { return "wait" }
func (w *WaitTool) Schema() json.RawMessage {
	return obj(`"seconds":{"type":"number","description":"Seconds to wait."}`, "seconds")
}
func (w *WaitTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Seconds float64 `json:"seconds"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.Seconds < 0 {
		return nil, fmt.Errorf("wait: seconds must not be negative")
	}
	if p.Seconds > 600 {
		return nil, fmt.Errorf("wait: seconds must not exceed 600")
	}
	timer := time.NewTimer(time.Duration(p.Seconds * float64(time.Second)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	return fmt.Sprintf("Waited %gs.", p.Seconds), nil
}

// FinishTool — Source: tools.finish. Signals that the task is complete; the loop
// treats a finish call as the terminal step.
type FinishTool struct{}

func (f *FinishTool) Name() string { return "finish" }
func (f *FinishTool) Schema() json.RawMessage {
	return obj(`"summary":{"type":"string","description":"Optional summary of what was completed."}`)
}
func (f *FinishTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Summary string `json:"summary"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.Summary == "" {
		return "Task marked complete.", nil
	}
	return "Task marked complete: " + p.Summary, nil
}

// ScheduleTool — Source: tools.schedule. The reference registers a deferred or
// recurring prompt with the session's scheduler and returns immediately with a
// background-task acknowledgement (【一手】 1.4.7; see
// testdata/result_texts_1.4.7.txt). This clone has no scheduler backend yet, so
// it mints the task, writes the log file the reference names, and returns the
// reference's text verbatim. Nothing fires when the delay elapses; that gap is
// recorded in docs/cycles/cycle-293-model-tool-surface/deliverable.md.
type ScheduleTool struct {
	// ConversationID scopes the brain/ log path. The reference uses the
	// live conversation id; when a caller cannot supply one, the tool
	// falls back to the OPENAGY_CONVERSATION_ID env var and then to a
	// process-stable id.
	ConversationID string
	// Home overrides the antigravity home directory (brain/ lives under
	// it). Empty resolves $ANTIGRAVITY_HOME then ~/.gemini/antigravity-cli.
	Home string
}

func (s *ScheduleTool) Name() string { return "schedule" }
func (s *ScheduleTool) Schema() json.RawMessage {
	return obj(`"Prompt":{"type":"string","description":"The prompt to run when the schedule fires."},` +
		`"DurationSeconds":{"type":"integer","description":"One-shot delay in seconds."},` +
		`"CronExpression":{"type":"string","description":"Recurring schedule expression."},` +
		`"IsDaemon":{"type":"boolean","description":"Keep the schedule alive across turns."},` +
		`"MaxIterations":{"type":"integer","description":"Stop after this many runs."},` +
		`"TimerCondition":{"type":"string","description":"Optional condition that must hold before firing."}`)
}

type scheduleParams struct {
	Prompt          string `json:"prompt"`
	DurationSeconds int    `json:"duration_seconds"`
	CronExpression  string `json:"cron_expression"`
	IsDaemon        bool   `json:"is_daemon"`
	MaxIterations   int    `json:"max_iterations"`
	TimerCondition  string `json:"timer_condition"`
	// ExactTiming is the 1.4.5 addition to the reference's schedule
	// declaration: "Set ExactTiming only when a cron must fire at the exact
	// wall-clock time without jitter" (【一手】, live 1.4.5 request body). The
	// clone has no scheduler, so the flag is carried but nothing acts on it.
	ExactTiming  bool   `json:"exact_timing"`
	Command      string `json:"command"`
	DelaySeconds int    `json:"delay_seconds"`
	Cron         string `json:"cron"`
}

func parseScheduleParams(args json.RawMessage) (*scheduleParams, error) {
	var p scheduleParams
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.Prompt == "" {
		p.Prompt = p.Command
	}
	if p.DurationSeconds == 0 {
		p.DurationSeconds = p.DelaySeconds
	}
	if p.CronExpression == "" {
		p.CronExpression = p.Cron
	}
	if p.Prompt == "" {
		return nil, fmt.Errorf("schedule: Prompt is required")
	}
	if p.DurationSeconds == 0 && p.CronExpression == "" {
		return nil, fmt.Errorf("schedule: set DurationSeconds or CronExpression")
	}
	return &p, nil
}

func writeScheduleTaskLog(logPath string, p *scheduleParams) error {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return fmt.Errorf("schedule: create task dir: %w", err)
	}
	header := fmt.Sprintf("prompt: %s\n", p.Prompt)
	if p.CronExpression != "" {
		header += "cron: " + p.CronExpression + "\n"
	} else {
		header += fmt.Sprintf("delay_seconds: %d\n", p.DurationSeconds)
	}
	if err := os.WriteFile(logPath, []byte(header), 0o600); err != nil {
		return fmt.Errorf("schedule: write task log: %w", err)
	}
	return nil
}

func (s *ScheduleTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	p, err := parseScheduleParams(args)
	if err != nil {
		return nil, err
	}

	conversationID := s.ConversationID
	if conversationID == "" {
		conversationID = os.Getenv("OPENAGY_CONVERSATION_ID")
	}
	if conversationID == "" {
		conversationID = processConversationID()
	}
	taskID := ScheduleTableFor(conversationID).nextTaskID()
	logPath := filepath.Join(scheduleHome(s.Home), "brain", conversationID,
		".system_generated", "tasks", taskID+".log")
	if err := writeScheduleTaskLog(logPath, p); err != nil {
		return nil, err
	}
	// The conversation now owns the schedule: the table is what the
	// rehydration pass rebuilds when the conversation is resumed, and
	// what the task-id counter is restored from.
	ScheduleTableFor(conversationID).add(ScheduleEntry{
		ID:              conversationID + "/" + taskID,
		Prompt:          p.Prompt,
		DurationSeconds: p.DurationSeconds,
		CronExpression:  p.CronExpression,
		TimerCondition:  p.TimerCondition,
		IsDaemon:        p.IsDaemon,
		MaxIterations:   p.MaxIterations,
		LogPath:         logPath,
	})

	// The reference has no Completed At on this result: the command was
	// backgrounded. The text is the reference's launch acknowledgement
	// (background_task.go) — the same text a backgrounded run_command
	// returns, because in the reference both are tasks.
	return taskLaunchResult(start, conversationID, taskID, logPath), nil
}

var (
	procConvID   string
	procConvOnce sync.Once
)

// processConversationID is a last-resort stable id so the acknowledged log path stays inside one dir.
func processConversationID() string {
	procConvOnce.Do(func() {
		procConvID = fmt.Sprintf("schedule-%d", os.Getpid())
	})
	return procConvID
}

// scheduleHome resolves the antigravity home the reference writes brain/ under.
func scheduleHome(override string) string {
	if override != "" {
		return override
	}
	if h := os.Getenv("ANTIGRAVITY_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Join(h, ".gemini", "antigravity-cli")
	}
	return filepath.Join(os.TempDir(), ".gemini", "antigravity-cli")
}
