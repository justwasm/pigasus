// Package remotecontrol — dispatch_init.go: the MCP handshake
// handlers (initialize / tools/list / tools/call). Static
// catalogue; no per-cycle evolution. Pure file split; each
// handler body was previously inline in dispatch.go.
package remotecontrol

import (
	"context"
	"encoding/json"
)

// handleInitialize responds to MCP's handshake. We accept any
// protocolVersion and echo it back so dashboards that pin a newer
// version than ours still see "negotiation succeeded". The
// serverInfo.name / .version are the constants declared in daemon.go.
func (d *Dispatcher) handleInitialize(params json.RawMessage) (any, *rpcErrorBody) {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
		ClientInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}
	_ = json.Unmarshal(params, &p) // params is best-effort
	if p.ProtocolVersion == "" {
		p.ProtocolVersion = ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": p.ProtocolVersion,
		"serverInfo": map[string]any{
			"name":    ServerName,
			"version": ServerVersion,
		},
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
	}, nil
}

// handleToolsList returns the static list of three tools the
// changelog mentions: status, list_agents, get_output. Each
// tool's inputSchema is intentionally minimal — the dispatcher
// does not validate inputs strictly, but a strict dashboard
// wants to know what shape to send.
func (d *Dispatcher) handleToolsList() (any, *rpcErrorBody) {
	return map[string]any{
		"tools": []map[string]any{
			{
				"name":        "status",
				"description": "Return live session state: pid, version, model, workspace, connected dashboard count, uptime.",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			{
				"name":        "list_agents",
				"description": "Return the list of known agent names (built-in default and any custom agents configured for this user).",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			{
				"name":        "get_output",
				"description": "Return the most recent turn output for a conversation id, or for the host's currently-running conversation when no id is provided.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"conversation_id": map[string]any{
							"type":        "string",
							"description": "Conversation id to look up. Defaults to the host's currently-running conversation.",
						},
					},
				},
			},
		},
	}, nil
}

// handleToolsCall dispatches one tool invocation to its handler.
// Per MCP, the params carry {"name": "<tool>", "arguments": {…}}.
// The result follows MCP's envelope:
//
//	{ "content": [ { "type": "text", "text": "…" } ], "isError": false }
//
// On failure the same envelope carries isError=true and the text
// explains why; we never propagate a JSON-RPC -32603 error from
// a tool because the dashboard protocol expects tools/call to
// always return a 200-equivalent.
func (d *Dispatcher) handleToolsCall(ctx context.Context, params json.RawMessage) (any, *rpcErrorBody) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}
	}
	if p.Name == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "name is required"}
	}
	var text string
	var isErr bool
	switch p.Name {
	case "status":
		text, isErr = d.callStatus()
	case "list_agents":
		text, isErr = d.callListAgents()
	case "get_output":
		text, isErr = d.callGetOutput(ctx, p.Arguments)
	default:
		return nil, &rpcErrorBody{Code: -32602, Message: "Unknown tool", Data: p.Name}
	}
	env := map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isErr,
	}
	return env, nil
}
