// Package remotecontrol — dispatch_route.go: the per-family
// JSON-RPC routing tables. Cycle rc-filesize-internal-remotecontrol
// extracts them out of the 123-line (*Dispatcher).routeMethod
// switch so the body of every per-family helper stays under the
// 50-LOC ceiling the brief pins.
//
// The split keeps the same routing semantics byte-for-byte: every
// method name maps to exactly one handler. Unknown methods
// return -32601 "Method not found" via the top-level
// (*Dispatcher).Dispatch fallback.
//
// Every helper takes ctx for signature uniformity with the
// per-handler route functions that need it; the methods that
// don't use ctx simply leave the parameter unused.
package remotecontrol

import (
	"context"
	"encoding/json"
)

// routeMCPTools handles the MCP handshake + tool catalogue
// methods: initialize / tools/list / tools/call / ping /
// notifications/initialized.
func (d *Dispatcher) routeMCPTools(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "initialize":
		r, e := d.handleInitialize(params)
		return r, e, true
	case "tools/list":
		r, e := d.handleToolsList()
		return r, e, true
	case "tools/call":
		r, e := d.handleToolsCall(ctx, params)
		return r, e, true
	case "ping":
		return map[string]any{}, nil, true
	case "notifications/initialized":
		// MCP client handshake completion; always a
		// notification (no id, no response). The
		// isNotification guard in Dispatch drops the
		// reply even if a future cycle decides to return
		// a result here.
		return nil, nil, true
	}
	return nil, nil, false
}

// routeCascadeLifecycle handles the four cascade lifecycle
// methods (Start / Send / Cancel / HandleUserInteraction).
func (d *Dispatcher) routeCascadeLifecycle(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "StartRemoteAgentCascade":
		// Cycle rc-cascade-start: allocates a *Cascade
		// and registers it with the dispatcher's
		// registry.
		r, e := d.handleStartRemoteAgentCascade(params)
		return r, e, true
	case "SendRemoteAgentCascadeMessage":
		// Cycle rc-cascade-message: synchronous
		// end-to-end turn.
		r, e := d.handleSendRemoteAgentCascadeMessage(ctx, params)
		return r, e, true
	case "CancelRemoteAgentCascade":
		// Cycle rc-cancel-cascade: cancels an
		// in-flight cascade turn.
		r, e := d.handleCancelRemoteAgentCascade(ctx, params)
		return r, e, true
	case "HandleRemoteAgentCascadeUserInteraction":
		// Cycle rc-handle-user-interaction:
		// bi-directional RPC.
		r, e := d.handleRemoteAgentCascadeUserInteraction(ctx, params)
		return r, e, true
	}
	return nil, nil, false
}

// routeReadOnlyLists handles the four read-only "List…" methods.
func (d *Dispatcher) routeReadOnlyLists(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "ListRemoteAgentModels",
		"ListRemoteAgentSlashCommands",
		"ListRemoteAgentConversations",
		"ListRemoteAgentEnvironments":
		// Cycle rc-list-readonly: the four read-only
		// "List…" methods share one dispatch entry point
		// that fans out to the per-method handlers in
		// list_dispatcher.go.
		r, e := d.dispatchList(method, params)
		return r, e, true
	}
	return nil, nil, false
}

// routeFileMethods handles the two read-only filesystem probes.
func (d *Dispatcher) routeFileMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "ReadRemoteAgentFile":
		r, e := d.handleReadRemoteAgentFile(params)
		return r, e, true
	case "ReadRemoteAgentDir":
		r, e := d.handleReadRemoteAgentDir(params)
		return r, e, true
	}
	return nil, nil, false
}

// routeProjectMethods handles the project-CRUD methods
// (List/Create/Update + Delete which lives next door in
// project_dispatcher.go).
func (d *Dispatcher) routeProjectMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "ListRemoteAgentProjects":
		r, e := d.handleListRemoteAgentProjects(params)
		return r, e, true
	case "CreateRemoteAgentProject":
		r, e := d.handleCreateRemoteAgentProject(params)
		return r, e, true
	case "UpdateRemoteAgentProject":
		r, e := d.handleUpdateRemoteAgentProject(params)
		return r, e, true
	}
	return nil, nil, false
}

// routePermissionMethods handles the four permission-mutation
// methods. Cycle rc-permissions-grants.
func (d *Dispatcher) routePermissionMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "SetRemoteAgentPermissionPreset":
		r, e := d.handleSetRemoteAgentPermissionPreset(params)
		return r, e, true
	case "SetRemoteAgentArtifactReviewMode":
		r, e := d.handleSetRemoteAgentArtifactReviewMode(params)
		return r, e, true
	case "AddRemoteAgentPermissionGrants":
		r, e := d.handleAddRemoteAgentPermissionGrants(params)
		return r, e, true
	case "RemoveRemoteAgentPermissionGrants":
		r, e := d.handleRemoveRemoteAgentPermissionGrants(params)
		return r, e, true
	}
	return nil, nil, false
}

// routeDeleteMethods handles the three destructive methods
// (DeleteRemoteAgentMessage / Conversation / Project).
func (d *Dispatcher) routeDeleteMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "DeleteRemoteAgentMessage":
		r, e := d.handleDeleteRemoteAgentMessage(params)
		return r, e, true
	case "DeleteRemoteAgentConversation":
		r, e := d.handleDeleteRemoteAgentConversation(params)
		return r, e, true
	case "DeleteRemoteAgentProject":
		r, e := d.handleDeleteRemoteAgentProject(params)
		return r, e, true
	}
	return nil, nil, false
}

// routeStatusMethods handles the four rc-status-tool methods
// (RemoteAgentErrorStatus / SetRemoteControlSession /
// remoteControlVersion / remoteControlDeepLink).
func (d *Dispatcher) routeStatusMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "RemoteAgentErrorStatus":
		r, e := d.handleRemoteAgentErrorStatus(params)
		return r, e, true
	case "SetRemoteControlSession":
		r, e := d.handleSetRemoteControlSession(params)
		return r, e, true
	case "remoteControlVersion":
		r, e := d.handleRemoteControlVersion()
		return r, e, true
	case "remoteControlDeepLink":
		r, e := d.handleRemoteControlDeepLink(ctx, params)
		return r, e, true
	}
	return nil, nil, false
}

// routePluginMethods handles the two plugin methods
// (List / Disconnect). Cycle rc-plugins.
func (d *Dispatcher) routePluginMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "ListRemoteAgentPlugins":
		r, e := d.handleListRemoteAgentPlugins(params)
		return r, e, true
	case "DisconnectRemoteAgentPlugin":
		r, e := d.handleDisconnectRemoteAgentPlugin(params)
		return r, e, true
	}
	return nil, nil, false
}

// routeTriggerMethods handles the five trigger methods
// (List / ListOptions / Create / Update / Delete). Cycle
// rc-triggers.
func (d *Dispatcher) routeTriggerMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "ListRemoteAgentTriggers":
		r, e := d.handleListRemoteAgentTriggers(params)
		return r, e, true
	case "ListRemoteAgentTriggerOptions":
		r, e := d.handleListRemoteAgentTriggerOptions(params)
		return r, e, true
	case "CreateRemoteAgentTrigger":
		r, e := d.handleCreateRemoteAgentTrigger(params)
		return r, e, true
	case "UpdateRemoteAgentTrigger":
		r, e := d.handleUpdateRemoteAgentTrigger(params)
		return r, e, true
	case "DeleteRemoteAgentTrigger":
		r, e := d.handleDeleteRemoteAgentTrigger(params)
		return r, e, true
	}
	return nil, nil, false
}

// routeFastpushMethods handles the single fastpush toggle
// method. Cycle rc-fastpush-toggle.
func (d *Dispatcher) routeFastpushMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	if method != "SetRemoteAgentFastpushEnabled" {
		return nil, nil, false
	}
	// Cycle rc-fastpush-toggle: one fast-push toggle
	// method. Push delivery is "deferred" today; the
	// future `rc-fastpush-channel` cycle replaces
	// the sentinel with a real transport.
	r, e := d.handleSetRemoteAgentFastpushEnabled(params)
	return r, e, true
}

// routeResourceMethods handles the three resource-management
// methods. Cycle rc-resources.
func (d *Dispatcher) routeResourceMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "ListRemoteAgentResourceProviders":
		r, e := d.handleListRemoteAgentResourceProviders(params)
		return r, e, true
	case "ListRemoteAgentAvailableResources":
		r, e := d.handleListRemoteAgentAvailableResources(params)
		return r, e, true
	case "UpdateRemoteAgentProjectResources":
		r, e := d.handleUpdateRemoteAgentProjectResources(ctx, params)
		return r, e, true
	}
	return nil, nil, false
}

// routeConversationConfigMethods handles the single per-conv-config
// write method. Cycle rc-conversation-config.
func (d *Dispatcher) routeConversationConfigMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	if method != "UpsertRemoteAgentConversationConfig" {
		return nil, nil, false
	}
	r, e := d.handleUpsertRemoteAgentConversationConfig(params)
	return r, e, true
}

// routeTerminalMethods handles the five RemoteAgentTerminal*
// methods. Cycle rc-terminals.
func (d *Dispatcher) routeTerminalMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "CreateRemoteAgentTerminal":
		r, e := d.handleCreateRemoteAgentTerminal(params)
		return r, e, true
	case "ListRemoteAgentTerminals":
		r, e := d.handleListRemoteAgentTerminals(params)
		return r, e, true
	case "SendRemoteAgentTerminalInput":
		r, e := d.handleSendRemoteAgentTerminalInput(params)
		return r, e, true
	case "ResizeRemoteAgentTerminal":
		r, e := d.handleResizeRemoteAgentTerminal(params)
		return r, e, true
	case "CloseRemoteAgentTerminal":
		r, e := d.handleCloseRemoteAgentTerminal(params)
		return r, e, true
	}
	return nil, nil, false
}

// routeTopLevelMethods handles the three rc-top-level-rpc methods
// (RemoteControlStatus / RemoteControlOnOutput /
// remoteControlOnMessage).
func (d *Dispatcher) routeTopLevelMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "RemoteControlStatus":
		r, e := d.handleRemoteControlStatus()
		return r, e, true
	case "RemoteControlOnOutput":
		r, e := d.handleRemoteControlOnOutput(ctx, params)
		return r, e, true
	case "remoteControlOnMessage":
		r, e := d.handleRemoteControlOnMessage(ctx, params)
		return r, e, true
	}
	return nil, nil, false
}

// routeOriginPolicyMethods handles the single origin-policy
// mutation method. Cycle rc-auth-tokens.
func (d *Dispatcher) routeOriginPolicyMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	if method != "SetRemoteControlOriginPatterns" {
		return nil, nil, false
	}
	r, e := d.handleSetRemoteControlOriginPatterns(params)
	return r, e, true
}
