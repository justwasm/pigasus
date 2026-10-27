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

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/tools"
	"github.com/justwasm/pigasus/lifecyclehooks"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// clientTools returns the tools the client declared, as PiG tools.
//
// The slice is nil when the client declared none: an empty tool list and
// a nil one are the same to PiG, and a nil slice keeps the declaration
// list free of empty entries.
func (c *conversation) clientTools() []piagent.AgentTool {
	decls := c.cfg.GetTools()
	if len(decls) == 0 {
		return nil
	}
	out := make([]piagent.AgentTool, 0, len(decls))
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

// Label is the display name of the tool header.
func (t *clientTool) Label() string { return t.decl.GetName() }

// Schema is the tool's parameter schema.
//
// The client sends JSON Schema; the shared translator lowers the type
// spellings to what PiG's metaschema validator expects, exactly as it
// does for the clone's own declarations.
func (t *clientTool) Schema() pi.ToolSchema {
	return pi.ToolSchema{
		Name:        t.decl.GetName(),
		Description: t.decl.GetDescription(),
		Parameters:  tools.SchemaParams(json.RawMessage(t.decl.GetParametersJsonSchema())),
	}
}

// ExecutionMode is sequential: a client-side tool may have side effects
// the client does not want interleaved.
func (t *clientTool) ExecutionMode() piagent.ToolExecutionMode {
	return piagent.ToolModeSequential
}

// Execute asks the client to run the tool and waits for its answer.
func (t *clientTool) Execute(ctx context.Context, toolCallID string, params json.RawMessage, onUpdate piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	call := &pb.ToolCall{}
	call.SetId(toolCallID)
	call.SetName(t.decl.GetName())
	call.SetArgumentsJson(string(params))
	call.SetTrajectoryId(t.conv.trajectoryID)

	ev := &pb.OutputEvent{}
	ev.SetToolCall(call)
	answer, err := t.conv.await(ctx, toolCallID, func() error {
		return t.conv.send(ctx, ev)
	})
	if err != nil {
		return piagent.AgentToolResult{}, err
	}
	resp := answer.GetToolResponse()
	if resp == nil {
		return piagent.AgentToolResult{}, fmt.Errorf("harness: the client answered the %s tool call with no response", t.decl.GetName())
	}
	if message := resp.GetErrorMessage(); message != "" {
		return piagent.AgentToolResult{}, errors.New(message)
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
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: text}},
	}, nil
}
