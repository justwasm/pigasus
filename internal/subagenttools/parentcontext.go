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
// it is what the subagent's compaction summary carries (see
// compaction.go).
package subagenttools

import (
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/agentmessage"
)

// MaxParentMessages is the number of the parent agent's most recent
// messages a subagent's summary keeps.
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
// The zero value is usable: an empty context renders an empty summary.
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
}

// Record notes a message the parent agent sent.
//
// Only the most recent MaxParentMessages are kept: the older ones are
// what the summary drops, exactly as the reference's does.
func (p *ParentContext) Record(content string, at time.Time) {
	if p == nil || strings.TrimSpace(content) == "" {
		return
	}
	p.Messages = append(p.Messages, ParentMessage{Content: content, At: at})
	if len(p.Messages) > MaxParentMessages {
		p.Messages = p.Messages[len(p.Messages)-MaxParentMessages:]
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

// Summary renders the parent's half for the subagent's compaction
// summary.
//
// The three things the 1.4.10 note names are all here: the task, the
// parent's id, and the parent's most recent messages. The task is
// rendered in the reference's own envelope — it is the very message the
// parent sent, so the child recognises it — while the later messages are
// rendered in the reference's own one-line message format, which is
// compact enough for a summary to carry ten of them.
func (p *ParentContext) Summary() string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("# The task your parent agent gave you\n\n")
	if strings.TrimSpace(p.Task) != "" {
		b.WriteString(agentmessage.Render(p.ID, p.Task, p.TaskAt, agentmessage.PriorityHigh))
		b.WriteString("\n\n")
	}
	b.WriteString("# Your parent agent\n\n")
	b.WriteString("id: ")
	b.WriteString(p.ID)
	b.WriteString("\n")
	if len(p.Messages) == 0 {
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString("\n# The most recent messages from your parent agent\n\n")
	for i, msg := range p.Messages {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, agentmessage.RenderLine(p.ID, msg.Content, msg.At, agentmessage.PriorityHigh)))
	}
	return strings.TrimRight(b.String(), "\n")
}
