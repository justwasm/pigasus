package agentserver

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
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

// stopSessionTasks stops running subagents and background tasks for a session.
// Upstream 1.5.10: canceling or closing a session stops them, even when no prompt is running.
func stopSessionTasks(sessID string) {
	if sessID == "" {
		return
	}
	_ = shell.KillConversationTasks(sessID)

	if reg := tools.SharedTaskRegistry(); reg != nil {
		prefix := sessID + "/"
		for _, t := range reg.List() {
			if (strings.HasPrefix(t.ID, prefix) || t.OwnerID == sessID) && t.State == task.StateRunning {
				reg.Kill(t.ID)
			}
		}
	}
}

func stopSessionWork(sessID string, sess *Session) {
	stopSessionTasks(sessID)
	if sess != nil {
		if sess.ConversationID != "" && sess.ConversationID != sessID {
			stopSessionTasks(sess.ConversationID)
		}
		if sess.SubagentRegistry != nil {
			for _, c := range sess.SubagentRegistry.List() {
				if c.IsRunning() {
					c.Kill()
				}
			}
		}
	}
}

// hasNonDaemonBackgroundWork reports whether the session has any active
// non-daemon background tasks (commands or timers) or running subagents.
// Daemon tasks do not keep an ACP prompt turn open.
func hasNonDaemonBackgroundWork(sess *Session) bool {
	if sess == nil {
		return false
	}
	if sess.SubagentRegistry != nil && sess.SubagentRegistry.Busy() {
		return true
	}
	if sess.ConversationID != "" {
		for _, snap := range shell.RunningTaskSnapshots(sess.ConversationID) {
			if !snap.IsDaemon {
				return true
			}
		}
	}
	return false
}
