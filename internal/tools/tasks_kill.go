package tools

import (
	"fmt"
	"strings"
)

// The background-task registry's exported surface.
//
// The registry itself stays unexported: the tools own it, and this file
// is the one door the rest of the clone knocks on.
//
// The remote-control surface needs it: the Web UI's Stop button asks
// for the background tasks of the conversation to die with the turn
// (`CancelCascadeInvocation{killBackgroundTasks: true}`), and the
// reference kills them with the turn.

// BackgroundTaskIDs returns the ids of the background tasks that are
// still in the table, oldest first.
func BackgroundTaskIDs() []string {
	return backgroundTasks.ids()
}

// KillBackgroundTask kills one background task by its id.
//
// The id is the reference's own task id: "<conversation-id>/task-<n>".
func KillBackgroundTask(id string) error {
	t, ok := backgroundTasks.get(id)
	if !ok {
		return fmt.Errorf("unknown task id %q", id)
	}
	_, err := t.kill()
	return err
}

// KillConversationTasks kills every background task that belongs to the
// conversation and returns the ids it killed.
//
// The reference's task id is "<conversation-id>/task-<n>", so the
// conversation owns the tasks whose id starts with it.
func KillConversationTasks(conversationID string) []string {
	if conversationID == "" {
		return nil
	}
	prefix := conversationID + "/"
	var killed []string
	for _, id := range backgroundTasks.ids() {
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		if err := KillBackgroundTask(id); err == nil {
			killed = append(killed, id)
		}
	}
	return killed
}

// ids returns the ids of the tasks in the table, oldest first.
//
// The order slice keeps the ids of the tasks that are gone, so the
// table itself is the truth about what is still there.
func (b *bgTable) ids() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.order))
	for _, id := range b.order {
		if _, ok := b.tasks[id]; ok {
			out = append(out, id)
		}
	}
	return out
}
