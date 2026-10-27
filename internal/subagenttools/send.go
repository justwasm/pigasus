package subagenttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
)

// sendMessageTool answers send_message: "Send a message to another agent,
// such as a subagent or a peer. Not a channel to the user."
//
// The reference's own texts are the reference's:
//
//	Encountered error in tool execution: recipient "abc123" not found
//	Message sent to %q.
//
// 【一手】 agy 1.4.7, `internal/tools/testdata/result_texts_1.4.7.txt` for
// the first; 【一手】 the live 1.4.4 capture for the second —
// `Message sent to "df83f9c4-ac60-4bce-86f8-9ddad4103f23".`
// (docs/cycles/subagent-async-framing-capture/deliverable.md)
//
// The success text is only truthful when a recipient is live, which is
// what the asynchronous transport buys: a subagent that is still running
// can be messaged, and a subagent that is talking to its parent reaches it
// through the same call.
type sendMessageTool struct {
	// registry is the conversation's live-subagent registry.
	registry *Registry
	// owner is the conversation id of the agent that runs this tool. A
	// subagent's copy carries the subagent's own id, so its messages go
	// to the parent.
	owner string
}

func (t *sendMessageTool) Name() string  { return "send_message" }
func (t *sendMessageTool) Label() string { return "Send Message" }
func (t *sendMessageTool) ExecutionMode() piagent.ToolExecutionMode {
	return piagent.ToolModeSequential
}

func (t *sendMessageTool) Schema() pi.ToolSchema {
	// Mirrors the captured 1.4.4 declaration (Recipient/Message, both
	// minLength 1). The executor also accepts the older snake_case spellings
	// so direct callers keep working; on the PiG path the captured declaration
	// is what reaches the model (piagentbridge.SurfaceTools).
	return pi.ToolSchema{
		Name:        "send_message",
		Description: sendMessageDescription,
		Parameters: pi.JsonObject{
			"type": "object",
			"properties": pi.JsonObject{
				"Recipient": pi.JsonObject{
					"type":        "string",
					"description": "Conversation ID of the agent to message.",
					"minLength":   1,
				},
				"Message": pi.JsonObject{
					"type":        "string",
					"description": "Content of the message.",
					"minLength":   1,
				},
			},
			"required": []string{"Recipient", "Message"},
		},
	}
}

const sendMessageDescription = "Send a message to another agent, such as a subagent or a peer. " +
	"Not a channel to the user."

type sendMessageArgs struct {
	SubagentID   string `json:"subagent_id"`
	Recipient    string `json:"recipient,omitempty"`
	RecipientCap string `json:"Recipient,omitempty"`
	Message      string `json:"message"`
	MessageCap   string `json:"Message,omitempty"`
}

func (t *sendMessageTool) Execute(_ context.Context, _ string, params json.RawMessage, _ piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	var args sendMessageArgs
	if err := json.Unmarshal(params, &args); err != nil {
		return piagent.AgentToolResult{}, fmt.Errorf("send_message: parse args: %w", err)
	}
	subID := firstNonEmpty(args.SubagentID, args.Recipient, args.RecipientCap)
	msg := firstNonEmpty(args.Message, args.MessageCap)
	if subID == "" || msg == "" {
		return piagent.AgentToolResult{}, errors.New("send_message: Recipient and Message are required")
	}
	if subID == childrenRecipient {
		return t.broadcast(msg), nil
	}
	if t.deliver(subID, msg) {
		text := resultStamp(time.Now()) + fmt.Sprintf("Message sent to %q.", subID)
		return piagent.AgentToolResult{
			Content: []pi.ToolResultMessageContent{pi.TextContent{Text: text}},
		}, nil
	}
	// The reference reports an unknown conversation the same way —
	// 【一手】 1.4.4: `Encountered error in tool execution: recipient
	// "abc123" not found`, returned as a normal result.
	text := resultStamp(time.Now()) + fmt.Sprintf("Encountered error in tool execution: recipient %q not found", subID)
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: text}},
	}, nil
}

// childrenRecipient is the recipient form that broadcasts to every active
// direct subagent.
//
// 【strings】 the reference's own second tool-text variant names it:
// "Send a message to another agent, such as a subagent or a peer, or pass
// Recipient='@children' to broadcast to all active direct subagents. Not a
// channel to the user." (internal/tools/testdata/tool_variants_1.4.7.json).
const childrenRecipient = "@children"

// broadcast sends the message to every live direct subagent.
//
// 【反编译】 agy 1.4.8's `sendmessage.(*Delivery).Summary` (0x10155c580)
// writes one of three sentences:
//
//	Message sent to %q.
//	Message sent to %d subagents: %s.
//	Message sent to %d of %d subagents: %s.
//
// and appends " Failed to deliver to %q: %v." for every recipient that
// failed. The ids are joined with ", ".
func (t *sendMessageTool) broadcast(message string) piagent.AgentToolResult {
	delivered := make([]string, 0, 4)
	failures := make([]string, 0, 2)
	if t.registry != nil {
		for _, child := range t.registry.List() {
			if child.ConversationID == t.owner {
				continue
			}
			if child.Deliver(Message{From: t.owner, Content: message, At: time.Now()}) {
				delivered = append(delivered, child.ConversationID)
				continue
			}
			failures = append(failures, fmt.Sprintf(" Failed to deliver to %q: %v.", child.ConversationID, errRecipientGone))
		}
	}
	var body string
	switch {
	case len(failures) == 0:
		body = fmt.Sprintf("Message sent to %d subagents: %s.", len(delivered), strings.Join(delivered, ", "))
	default:
		body = fmt.Sprintf("Message sent to %d of %d subagents: %s.",
			len(delivered), len(delivered)+len(failures), strings.Join(delivered, ", "))
		body += strings.Join(failures, "")
	}
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: resultStamp(time.Now()) + body}},
	}
}

// errRecipientGone is what a failed delivery reports.
//
// 【存疑】 the reference's own wording for a recipient that cannot be
// reached is not recovered; the sentence it wraps is the reference's.
var errRecipientGone = errors.New("the recipient is no longer reachable")


// deliver hands the message to the recipient.
//
// Three cases, in the order the reference's own transport tries them:
//
//   - a live subagent of this conversation — the message wakes it up;
//   - the parent conversation, when the sender is a subagent — the
//     message reaches the parent's next turn;
//   - anything else — the recipient does not exist.
func (t *sendMessageTool) deliver(recipient, message string) bool {
	if t.registry == nil || recipient == "" {
		return false
	}
	if recipient == t.owner {
		// The reference refuses to deliver to the sending conversation
		// itself ("the sending conversation is unavailable").
		return false
	}
	if child, ok := t.registry.Get(recipient); ok {
		if !child.Deliver(Message{From: t.owner, Content: message, At: time.Now()}) {
			return false
		}
		return true
	}
	if t.registry.ParentID() == recipient {
		// The sender is a subagent and the recipient is its parent. The
		// parent's transport owns the delivery: the TUI puts the message
		// in the session's inbox, the print mode hands it to the next
		// turn.
		if !t.registry.NotifyParent(t.owner, message) {
			return false
		}
		if child, ok := t.registry.Get(t.owner); ok {
			child.MarkReported()
		}
		return true
	}
	return false
}
