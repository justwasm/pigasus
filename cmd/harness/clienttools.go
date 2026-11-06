// clienttools.go — the tools the *client* declared.
//
// # The split
//
// `HarnessConfig.tools` are the tools the client implements. The model
// may call them, but the harness cannot run them: it has to ask the
// client to do it and wait for the answer.
//
// `HarnessConfig.harness_side_tools` are the tools the harness runs itself
// — the clone's own catalogue, in this implementation.
//
// The reference distinguishes the two the same way: the harness-side tools
// are executed in-process, and the client-side ones travel over the wire
// as a `ToolCall` in an `OutputEvent`, answered by a `ToolResponse` in an
// `InputEvent`.
//
// # The round trip
//
//	harness  --[OutputEvent{tool_call}]-->  client
//	harness  <--[InputEvent{tool_response}]-  client
//
// The correlation id is the tool call id PiG handed the tool, so the
// client can match the answer to the call even when several are in
// flight.
//
// # Limitations
//
//   - The client's `response_json` is handed to the model verbatim; a
//     structured `response` is re-encoded as JSON first.
//   - `supplemental_media` is ignored: PiG's tool-result content has no
//     media slot this bridge can fill.
//   - The client cannot ask for a *confirmation* through this path. The
//     protocol has a `ToolConfirmation` event, but the reference's
//     confirmation flow belongs to the permission system, and the clone's
//     permission policy answers locally.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/lifecyclehooks"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// clientTools returns the tools the client declared, as actor tools.
//
// The slice is nil when the client declared none: an empty tool list and
// a nil one are the same, and a nil slice keeps the declaration
// list free of empty entries.
func (c *conversation) clientTools() []actor.Tool {
	decls := c.cfg.GetTools()
	if len(decls) == 0 {
		return nil
	}
	out := make([]actor.Tool, 0, len(decls))
	for _, decl := range decls {
		if decl.GetName() == "" {
			continue
		}
		out = append(out, &clientTool{conv: c, decl: decl})
	}
	return out
}

// clientTool is one tool the client implements, seen from the harness.
type clientTool struct {
	conv *conversation
	decl *pb.Tool
}

// Name is the tool's name as the model knows it.
func (t *clientTool) Name() string { return t.decl.GetName() }

// Description is the tool's description.
func (t *clientTool) Description() string { return t.decl.GetDescription() }

// RequiresApproval returns false for client-side tools.
func (t *clientTool) RequiresApproval(string) bool { return false }

// Definition returns the tool definition for the model.
func (t *clientTool) Definition() actor.ToolDefinition {
	var params map[string]any
	if raw := t.decl.GetParametersJsonSchema(); raw != "" {
		_ = json.Unmarshal([]byte(raw), &params)
	}
	return actor.ToolDefinition{
		Name:        t.decl.GetName(),
		Description: t.decl.GetDescription(),
		Parameters:  params,
	}
}

// Execute asks the client to run the tool and waits for its answer.
func (t *clientTool) Execute(ctx context.Context, input string) (actor.ToolResult, error) {
	toolCallID := actor.ToolCallIDFromContext(ctx)
	if toolCallID == "" {
		toolCallID = "call-" + randomID()
	}
	call := &pb.ToolCall{}
	call.SetId(toolCallID)
	call.SetName(t.decl.GetName())
	call.SetArgumentsJson(input)
	call.SetTrajectoryId(t.conv.trajectoryID)

	ev := &pb.OutputEvent{}
	ev.SetToolCall(call)
	answer, err := t.conv.await(ctx, toolCallID, func() error {
		return t.conv.send(ctx, ev)
	})
	if err != nil {
		return actor.ToolResult{ToolCallID: toolCallID}, err
	}
	resp := answer.GetToolResponse()
	if resp == nil {
		return actor.ToolResult{ToolCallID: toolCallID}, fmt.Errorf("harness: the client answered the %s tool call with no response", t.decl.GetName())
	}
	if message := resp.GetErrorMessage(); message != "" {
		return actor.ToolResult{ToolCallID: toolCallID, Error: message}, errors.New(message)
	}
	text := resp.GetResponseJson()
	if text == "" {
		if structured := resp.GetResponse(); structured != nil {
			if fields := lifecyclehooks.StructToMap(structured); len(fields) > 0 {
				if encoded, err := json.Marshal(fields); err == nil {
					text = string(encoded)
				}
			}
		}
	}
	if text == "" {
		text = "{}"
	}
	return actor.ToolResult{
		ToolCallID: toolCallID,
		Output:     text,
	}, nil
}
