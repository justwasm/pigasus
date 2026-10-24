package subagenttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
)

// sendMessageTool answers send_message. Delegations in this clone are
// synchronous, so by the time the parent can message a recipient that
// recipient's turn has already ended and its session is closed — there is
// nothing to deliver to. The reference reports an unknown conversation with
// the same sentence, returned as a normal (stamped, not IsError) result:
//
//	Encountered error in tool execution: recipient "abc123" not found
//
// 【一手】 agy 1.4.4, `internal/tools/testdata/result_texts_1.4.4.txt`.
//
// The reference's *success* texts are known but deliberately NOT emitted
// here, because emitting them would claim a delivery that did not happen:
//
//	Message sent to %q.
//	Message sent to %d subagents: %s.
//	Message sent to %d of %d subagents: %s.
//
// The first is now captured from the wire rather than decompiled (一手, live
// 1.4.4: `Message sent to "<child-conv-id>".` — see
// docs/cycles/subagent-async-framing-capture/deliverable.md, which also
// records the <SYSTEM_MESSAGE> envelope a delivery arrives in). It is only
// emitted when a recipient is live, which requires an asynchronous transport;
// the clone does not have one.
type sendMessageTool struct{}

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
	// Delegations in this clone are synchronous: by the time the parent can
	// send a message the subagent has already finished, so there is no live
	// recipient. The reference reports an unknown conversation the same way —
	// 【一手】 1.4.4: `Encountered error in tool execution: recipient "abc123"
	// not found`, returned as a normal result.
	text := resultStamp(time.Now()) + fmt.Sprintf("Encountered error in tool execution: recipient %q not found", subID)
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: text}},
	}, nil
}
