// parentcontext.go — what a subagent must not lose when its own context
// is compacted.
//
// 【一手】 agy 1.4.10, the vendor's own release notes:
//
//	Fixed subagents losing the task their parent agent gave them, and the
//	parent's ID, after their context was compacted, including with
//	`/compact`; a subagent's summary now keeps the parent agent's 10 most
//	recent messages.
//
// A subagent's conversation is two conversations in one: the parent's half
// (the task it was given, the messages it was sent afterwards) and the
// child's own half (the turns it worked through). A compaction rewrites
// the child's half into a summary — and a summary that only knows about
// the child's own turns leaves the child without the task it was given
// and without the id it has to answer on.
//
// The clone therefore keeps the parent's half itself: the ParentContext
// below is the record the clone maintains of what the parent said, and
// Lines is what its checkpoints carry.
//
// 【一手】 agy 1.5.17 keeps the two halves strictly apart rather than
// composing them:
//
//	utils.CaptureUserRequests / utils.FormatCheckpointUserRequests
//	utils.CaptureParentMessages / utils.FormatCheckpointParentMessages
//
// both render into repeated CapturedUserRequest fields of
// CortexStepCheckpoint (captured_user_requests = 30, captured_parent_messages
// = 31), and ResumeBlock emits one *or* the other — ParentMessages replaces
// the "# User Requests" section. The child's own half is the model-written
// session_summary, never a Go-side digest of its tool results.
//
// So there is nothing here that combines "the parent's half plus my own
// work": the parent's half travels through Lines into
// checkpoint.Snapshot.ParentMessages, and the child's half is summarized by
// checkpoint.Policy. A SubagentSummary/CompactionPreparation pair that
// modelled the combined shape was removed — see this file's history.
package subagenttools

import (
	"strings"
	"time"

	agentmessage "github.com/justwasm/pigasus/internal/core/msg"
)

// MaxParentMessages is the number of the parent agent's most recent
// messages a subagent's checkpoint keeps.
//
// 【一手】 the 1.4.10 note: "a subagent's summary now keeps the parent
// agent's 10 most recent messages".
const MaxParentMessages = 10

// ParentMessage is one message the parent agent sent to the subagent.
type ParentMessage struct {
	// Content is the message's own text.
	Content string
	// At is when the parent sent it.
	At time.Time
}

// ParentContext is the parent's half of a subagent's conversation: the
// task the parent agent gave the subagent, the parent's own id, and the
// messages the parent sent afterwards.
//
// The zero value is usable: an empty context renders no lines.
type ParentContext struct {
	// ID is the parent agent's conversation id — the id the subagent
	// answers on, and the sender the parent's messages carry.
	ID string
	// Task is the task the parent gave the subagent.
	Task string
	// TaskAt is when the task was given.
	TaskAt time.Time
	// Messages are the parent's most recent messages, oldest first. At
	// most MaxParentMessages of them are kept.
	Messages []ParentMessage

	// truncated records that Record dropped an older message.
	truncated bool
}

// Record notes a message the parent agent sent.
//
// Only the most recent MaxParentMessages are kept, exactly as the
// reference's summary does. This is the only place the cap is applied: the
// task is not a message the parent "sent afterwards", and it must never be
// one of the entries the cap drops (see Lines).
func (p *ParentContext) Record(content string, at time.Time) {
	if p == nil || strings.TrimSpace(content) == "" {
		return
	}
	p.Messages = append(p.Messages, ParentMessage{Content: content, At: at})
	if len(p.Messages) > MaxParentMessages {
		p.Messages = p.Messages[len(p.Messages)-MaxParentMessages:]
		p.truncated = true
	}
}

// parentContextOf builds the parent context of one delegation.
//
// The task is the invocation's own prompt — the text the parent agent
// gave the subagent — and the id is the parent's conversation id: the
// id the subagent answers on.
func parentContextOf(deps Deps, inv Invocation) *ParentContext {
	return &ParentContext{
		ID:     parentIDOf(deps),
		Task:   inv.Prompt,
		TaskAt: time.Now(),
	}
}

// parentIDOf returns the parent agent's conversation id.
//
// The registry knows it when the conversation runs subagents; the
// dependency's own id is the fallback for a delegation made without a
// registry.
func parentIDOf(deps Deps) string {
	if deps.Registry != nil {
		if id := deps.Registry.ParentID(); id != "" {
			return id
		}
	}
	return deps.ConversationID
}

// Lines renders the parent's half in the checkpoint's form: the task first,
// then the messages the parent sent since, one agent-message line each.
//
// 【一手】 agy 1.5.3, conversation a9756e99-…, checkpoint at step 350. The
// minimum verbatim shape is:
//
//	# Parent Agent Messages
//	The following were messages from your parent agent in the truncated conversation in chronological order:
//	1. [Message] timestamp=… sender=… priority=MESSAGE_PRIORITY_HIGH content=…
//
// The bool is true when the cap hid older messages, which selects the
// reference's "most recent messages" preamble.
//
// The task is never capped. It is the delegation itself, not one of the
// parent's "most recent messages", and a checkpoint that dropped it would
// recreate the exact defect agy 1.4.10 fixed. Only the messages the parent
// sent afterwards are capped, and Record applies that cap — one place, so
// the retained count and the retained lines cannot disagree.
func (p *ParentContext) Lines() ([]string, bool) {
	if p == nil {
		return nil, false
	}
	var out []string
	if strings.TrimSpace(p.Task) != "" {
		out = append(out, agentmessage.RenderLine(p.ID, p.Task, p.TaskAt, agentmessage.PriorityHigh))
	}
	for _, msg := range p.Messages {
		if strings.TrimSpace(msg.Content) == strings.TrimSpace(p.Task) {
			continue
		}
		out = append(out, agentmessage.RenderLine(p.ID, msg.Content, msg.At, agentmessage.PriorityHigh))
	}
	return out, p.truncated
}
