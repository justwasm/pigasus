package shell

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

// KillBackgroundTask kills one background task by its id.
//
// The id is the reference's own task id: "<conversation-id>/task-<n>".
func KillBackgroundTask(id string) error {
	t, ok := backgroundTasks.get(id)
	if !ok {
		return fmt.Errorf("unknown task id %q", id)
	}
	_, err := t.Kill()
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

// FindBackgroundTask returns one background task by its id.
//
// It is the door the task capability knocks on: the task tools read a
// task's status, write to its stdin and stop it, and they live in
// another package (internal/app/tools/tasks), so the table itself stays
// unexported and this accessor is the only way in.
func FindBackgroundTask(id string) (*BackgroundTask, bool) {
	return backgroundTasks.get(id)
}

// ListBackgroundTasks returns the rendered status lines of the tasks that
// are still in the table.
//
// The manage_task tool's "list" action answers with these lines; the
// lines are the reference's own wording, so they are rendered here and
// not in the caller.
func ListBackgroundTasks() []string {
	return backgroundTasks.list()
}
