package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The reference's background-task subsystem.
//
// 【一手】 In the reference every backgrounded unit of work is a *task*:
// a shell command that did not finish inside WaitMsBeforeAsync, a
// schedule, a recurring job. A task has
//
//   - an id of the form "<conversation-id>/task-<n>" — the reference
//     formats it with "%s/task-%d" (a format string in the 1.4.7 binary
//     at offset 54548670) and its own transcripts carry ids like
//     "45fde9e1-76d5-4b87-bd23-1b2c626bbf26/task-56";
//   - a log file under "brain/<conversation-id>/.system_generated/tasks/",
//     named after the task;
//   - a launch acknowledgement, and
//   - a completion notification.
//
// The launch acknowledgement, from the reference's own conversation store
// (1.4.7, the `schedule` tool) and from its own transcript (a backgrounded
// `run_command`):
//
//	Created At: 2026-10-25T03:43:15+08:00
//	Tool is running as a background task with task id: <conv>/task-4
//	Task logs are available at: file://<home>/brain/<conv>/.system_generated/tasks/task-4.log
//	YOU MUST TAKE ONE OF THE FOLLOWING TWO ACTIONS: A) either proceed to other relevant work (if any) or, B) simply update the user with a short message (that you have launched the command and will wait for it to finish) and end the turn.
//	 DO NOTHING ELSE.
//
// The completion notification, from the reference's own store (the function
// response of a backgrounded `go test`) and from its transcript (the
// SYSTEM_MESSAGE that carries it to the model):
//
//	Task id "<conv>/task-56" finished with result:
//	Command exited with code 0.
//
//	Command output:
//	...
//
//	Log: file://<home>/brain/<conv>/.system_generated/tasks/task-56.log
//
// The reference's step *string* (the server-side rendering of the step,
// step_strings/run_command.tmpl) has a third shape for a backgrounded
// command — "Background command ID: <id>" plus an output snapshot — but it
// never reaches the model: the string "Background command ID" does not
// occur once in the reference's 500 conversation stores, while the launch
// acknowledgement above occurs in both the store and the transcript. The
// clone therefore sends the launch acknowledgement as the tool result.

// taskLaunchResult renders the reference's launch acknowledgement for a
// background task.
//
// started is the moment the task was created; convID and taskID are the
// task's identity ("<conv>/task-<n>", so taskID is the bare "task-<n>");
// logPath is the absolute path of the task log.
func taskLaunchResult(started time.Time, convID, taskID, logPath string) string {
	return "Created At: " + started.Format(time.RFC3339) + "\n" +
		"Tool is running as a background task with task id: " + convID + "/" + taskID + "\n" +
		"Task logs are available at: file://" + logPath + "\n" +
		"YOU MUST TAKE ONE OF THE FOLLOWING TWO ACTIONS: A) either proceed to other relevant work (if any) or, B) simply update the user with a short message (that you have launched the command and will wait for it to finish) and end the turn.\n" +
		" DO NOTHING ELSE."
}

// taskCompletionNotification renders the reference's completion
// notification: the text the model receives when a background task
// finishes.
//
// 【一手】 The reference formats it "Task id %q %s%s" and appends
// "\nLog: %s": "Task id \"<id>\" finished with result:\n<result>" and then
// the task log's file:// URL. The blank line before "Log:" in the
// reference's own text is the result's own trailing newline, not a
// separator of its own.
//
// result is the CLI's own formatter text for the finished step ("Command
// exited with code N." + "Command output:" + the command's output),
// logPath the task log.
func taskCompletionNotification(taskID, result, logPath string) string {
	return taskNotification(taskID, "finished", " with result:\n"+result, logPath)
}

// taskNotification renders the reference's task notification:
//
//	Task id "<id>" <status><suffix>
//	Log: file://<task log>
//
// The status word is the reference's own: "finished", "was canceled" or
// "failed" (【一手】 the three words in the 1.4.7 binary). The suffix
// carries the result for a finished task; the reference's suffix for a
// canceled or failed task was not observed (存疑).
func taskNotification(taskID, status, suffix, logPath string) string {
	return "Task id \"" + taskID + "\" " + status + suffix + "\nLog: file://" + logPath
}

// taskLogPath returns the reference's task log path for a task of a
// conversation: "<home>/brain/<conv>/.system_generated/tasks/<task>.log".
//
// The reference writes the file when the task is created and appends the
// task's output to it while the task runs; see the task log files under
// ~/.gemini/antigravity-cli/brain/<conv>/.system_generated/tasks/.
func taskLogPath(home, convID, taskID string) string {
	return filepath.Join(home, "brain", convID, ".system_generated", "tasks", taskID+".log")
}

// createTaskLog creates the reference's task log file for a task.
//
// The reference creates the file when the task starts: the log of a task
// that is still running exists and is empty (【一手】 the reference's own
// task-114.log, the log of a `sleep 10` that had not finished). It makes
// the directory 0755 and the file 0644 (【一手】 the reference's own
// initLogFile).
func createTaskLog(logPath string) error {
	if logPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("create task log dir: %w", err)
	}
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		return fmt.Errorf("create task log: %w", err)
	}
	return nil
}

// nextTaskID mints the next task id of a conversation.
//
// The reference's task number is the step index the task was created at
// (its transcripts carry "task-4" for step 4, "task-56" for step 56), so
// the numbers are sparse and tied to the conversation's history. The clone
// has no step index at the tool layer, so it keeps a per-conversation
// counter instead — the id *shape* ("<conv>/task-<n>") is what the model
// and the reference's own tools address a task by.
//
// The counter is shared with the schedule table, so a conversation has one
// task-number space and a schedule and a backgrounded command never mint
// the same id.
func nextTaskID(convID string) string {
	return ScheduleTableFor(convID).nextTaskID()
}
