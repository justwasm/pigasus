// Running-task snapshots for the checkpoint's resume block.
//
// 【一手】 agy 1.5.3, conversation 43966145-…, the checkpoints from step
// 23467 onward carry a "# Running Background Tasks" section whose entries are
// protojson TaskSnapshot objects (taskId, toolName, toolSummary, description,
// startTime, stepIndex, logUri, isDaemon). The table below is the only owner
// of that state, so the snapshot is rendered here and the hosts convert it.

package shell

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/core/compaction"
)

// ConversationTaskRef pairs a running background task snapshot with the
// conversation ID that owns it.
type ConversationTaskRef struct {
	ConversationID string
	Task           compaction.TaskRef
}

// AllRunningTaskSnapshots returns all still-running background tasks across
// every conversation, oldest first.
func AllRunningTaskSnapshots() []ConversationTaskRef {
	backgroundTasks.mu.Lock()
	tasks := make([]*BackgroundTask, 0, len(backgroundTasks.order))
	for _, id := range backgroundTasks.order {
		if t, ok := backgroundTasks.tasks[id]; ok && t != nil {
			tasks = append(tasks, t)
		}
	}
	backgroundTasks.mu.Unlock()

	out := make([]ConversationTaskRef, 0, len(tasks))
	for _, t := range tasks {
		t.mu.Lock()
		done := t.completed || t.killed
		t.mu.Unlock()
		if done {
			continue
		}
		convID, _, _ := strings.Cut(t.id, "/")
		out = append(out, ConversationTaskRef{
			ConversationID: convID,
			Task: compaction.TaskRef{
				TaskID:      t.id,
				ToolName:    t.toolName,
				ToolSummary: t.toolSummary,
				Description: t.command,
				StartTime:   protoJSONTimestamp(t.started),
				StepIndex:   uint32(t.stepIndex),
				LogURI:      fileURI(t.logPath),
				IsDaemon:    t.isDaemon,
			},
		})
	}
	return out
}

// RunningTaskSnapshots returns the conversation's still-running tasks in the
// checkpoint's shape, oldest first.
//
// A task that already exited is not "running at the time this checkpoint was
// created", so completed and killed tasks are skipped. A task whose id does
// not belong to the conversation is skipped too: the table is process-wide,
// and the reference's checkpoint only reports the conversation's own tasks.
func RunningTaskSnapshots(conversationID string) []compaction.TaskRef {
	if conversationID == "" {
		return nil
	}
	prefix := conversationID + "/"
	backgroundTasks.mu.Lock()
	ids := make([]string, 0, len(backgroundTasks.order))
	for _, id := range backgroundTasks.order {
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		if _, ok := backgroundTasks.tasks[id]; ok {
			ids = append(ids, id)
		}
	}
	tasks := make([]*BackgroundTask, 0, len(ids))
	for _, id := range ids {
		tasks = append(tasks, backgroundTasks.tasks[id])
	}
	backgroundTasks.mu.Unlock()

	out := make([]compaction.TaskRef, 0, len(tasks))
	for _, t := range tasks {
		if t == nil {
			continue
		}
		t.mu.Lock()
		done := t.completed || t.killed
		t.mu.Unlock()
		if done {
			continue
		}
		out = append(out, compaction.TaskRef{
			TaskID:      t.id,
			ToolName:    t.toolName,
			ToolSummary: t.toolSummary,
			Description: t.command,
			StartTime:   protoJSONTimestamp(t.started),
			StepIndex:   uint32(t.stepIndex),
			LogURI:      fileURI(t.logPath),
			IsDaemon:    t.isDaemon,
		})
	}
	return out
}

// fileURI renders a local path the way the reference does.
func fileURI(path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		return ""
	}
	return "file://" + path
}

// protoJSONTimestamp renders a timestamp the way protojson does: RFC 3339 in
// UTC with 0, 3, 6 or 9 fractional digits.
func protoJSONTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.UTC()
	layout := "2006-01-02T15:04:05"
	switch n := t.Nanosecond(); {
	case n == 0:
	case n%1_000_000 == 0:
		layout += ".000"
	case n%1_000 == 0:
		layout += ".000000"
	default:
		layout += ".000000000"
	}
	return t.Format(layout) + "Z"
}
