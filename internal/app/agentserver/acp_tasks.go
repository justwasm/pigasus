package agentserver

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/tools"
)

// formatTasksSummary formats the background tasks in the shared registry into a readable Markdown report.
func formatTasksSummary() string {
	reg := tools.SharedTaskRegistry()
	if reg == nil {
		return "No background task registry available."
	}
	tasks := reg.List()
	if len(tasks) == 0 {
		return "No background tasks currently running or completed."
	}

	var running, done int
	for _, t := range tasks {
		if t.State == task.StateRunning {
			running++
		} else {
			done++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### Background Tasks (%d running, %d finished)\n\n", running, done))
	sb.WriteString("| Task ID | State | Command / Name | Started | Exit Code |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- | :--- |\n")
	for _, t := range tasks {
		cmd := t.Command
		if cmd == "" {
			cmd = t.Name
		}
		if cmd == "" {
			cmd = t.Prompt
		}
		cmd = strings.ReplaceAll(cmd, "\n", " ")
		if len(cmd) > 50 {
			cmd = cmd[:47] + "..."
		}
		exitStr := "-"
		if t.State != task.StateRunning {
			exitStr = fmt.Sprintf("%d", t.ExitCode)
		}
		startedStr := "-"
		if !t.StartedAt.IsZero() {
			startedStr = t.StartedAt.Local().Format("15:04:05")
		}
		sb.WriteString(fmt.Sprintf("| `%s` | %s | `%s` | %s | %s |\n", t.ID, t.State, cmd, startedStr, exitStr))
	}
	return sb.String()
}
