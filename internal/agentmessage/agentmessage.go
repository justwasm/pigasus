// Package agentmessage renders the reference's <SYSTEM_MESSAGE> envelope.
//
// The reference delivers an agent-to-agent message — a subagent's answer, a
// background task's completion, a peer's question — to a model as a
// user-role content part that is wrapped in a fixed envelope. The envelope
// tells the model that the text was not typed by the user, and it carries
// the message's metadata in one `[Message]` line.
//
// 【反编译】 agy 1.4.7's binary holds the three format strings the envelope
// is built from, one hit each:
//
//	"The following is a "
//	"<SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.\n\n<SYSTEM_MESSAGE>\n%s\n</SYSTEM_MESSAGE>"
//	"[Message] timestamp=%s sender=%s priority=%s content=%s"
//
// 【一手】 the same bytes on the wire, from a live 1.4.4 capture
// (docs/cycles/subagent-async-framing-capture/deliverable.md):
//
//	The following is a <SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.
//
//	<SYSTEM_MESSAGE>
//	[Message] timestamp=2026-10-22T03:24:39Z sender=73ab3617-5a99-4e26-b1f1-c88e712efe39 priority=MESSAGE_PRIORITY_HIGH content=PONG
//	</SYSTEM_MESSAGE>
//
// The package exists so that the two producers of an envelope — the
// background-task notifier (internal/agent) and the subagent mailbox
// (internal/subagenttools) — render the same bytes. The second one cannot
// import the first: internal/agent reaches internal/tools, and internal/tools
// reaches internal/subagenttools, so the dependency runs the other way.
package agentmessage

import (
	"fmt"
	"time"
)

const (
	// Preamble is the reference's own lead-in: it tells the model that
	// what follows was not sent by the user.
	//
	// The 141-byte sentence plus the blank line that separates it from
	// the tag below.
	Preamble = "The following is a <SYSTEM_MESSAGE> not actually sent by the user. " +
		"It is provided by the system as important information to pay attention to."

	// Wrapper wraps the rendered message in the reference's tags.
	Wrapper = "<SYSTEM_MESSAGE>\n%s\n</SYSTEM_MESSAGE>"

	// LineFormat is the reference's own rendering of one agent message:
	// the timestamp, the sender, the priority and the content.
	LineFormat = "[Message] timestamp=%s sender=%s priority=%s content=%s"

	// PriorityHigh is the reference's spelling of the priority a
	// delivery carries ("MESSAGE_PRIORITY_HIGH").
	PriorityHigh = "MESSAGE_PRIORITY_HIGH"
)

// Render renders one agent message into the reference's envelope.
//
// The timestamp is UTC — every other time in the reference's requests is
// local, but the message line is not (【一手】 the 1.4.4 capture: the
// envelope's `timestamp=2026-10-22T03:24:39Z` against the request's
// `Created At: 2026-10-22T11:24:33+08:00`).
//
// The content is not quoted: a multi-line message continues to the end of
// the block, which is the reference's own behaviour (the capture's
// background-task notification carries a command's whole output in one
// message).
//
// An empty priority means PriorityHigh: a delivered message is information
// the model has to pay attention to.
func Render(sender, content string, at time.Time, priority string) string {
	if priority == "" {
		priority = PriorityHigh
	}
	if at.IsZero() {
		at = time.Now()
	}
	msg := fmt.Sprintf(LineFormat, at.UTC().Format(time.RFC3339), sender, priority, content)
	return Preamble + "\n\n" + fmt.Sprintf(Wrapper, msg)
}
