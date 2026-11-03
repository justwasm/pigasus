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

	"github.com/justwasm/pigasus/internal/remotecontrol/convconfig"
	"github.com/justwasm/pigasus/internal/remotecontrol/fastpush"
	"github.com/justwasm/pigasus/internal/remotecontrol/files"
	"github.com/justwasm/pigasus/internal/remotecontrol/origins"
	"github.com/justwasm/pigasus/internal/remotecontrol/permissions"
	"github.com/justwasm/pigasus/internal/remotecontrol/plugins"
	"github.com/justwasm/pigasus/internal/remotecontrol/projects"
	"github.com/justwasm/pigasus/internal/remotecontrol/resources"
	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
	"github.com/justwasm/pigasus/internal/remotecontrol/terminals"
	"github.com/justwasm/pigasus/internal/remotecontrol/triggers"
	"github.com/justwasm/pigasus/originpolicy"
)

// routeMCPTools handles the MCP handshake + tool catalogue
// methods: initialize / tools/list / tools/call / ping /
// notifications/initialized.
func (d *Dispatcher) routeMCPTools(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
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
func (d *Dispatcher) routeCascadeLifecycle(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case "StartCascade", "StartRemoteAgentCascade":
		// Cycle rc-cascade-start: allocates a *Cascade
		// and registers it with the dispatcher's
		// registry.
		r, e := d.handleStartRemoteAgentCascade(params)
		return r, e, true
	case "SendUserCascadeMessage", "SendRemoteAgentCascadeMessage":
		// Cycle rc-cascade-message: synchronous
		// end-to-end turn.
		r, e := d.handleSendRemoteAgentCascadeMessage(ctx, params)
		return r, e, true
	case "CancelCascade", "CancelRemoteAgentCascade":
		// Cycle rc-cancel-cascade: cancels an
		// in-flight cascade turn.
		r, e := d.handleCancelRemoteAgentCascade(ctx, params)
		return r, e, true
	case "HandleCascadeUserInteraction", "HandleRemoteAgentCascadeUserInteraction":
		// Cycle rc-handle-user-interaction:
		// bi-directional RPC.
		r, e := d.handleRemoteAgentCascadeUserInteraction(ctx, params)
		return r, e, true
	}
	return nil, nil, false
}

// routeReadOnlyLists handles the four read-only "List…" methods.
func (d *Dispatcher) routeReadOnlyLists(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case "ListAvailableModels", "ListRemoteAgentModels",
		"ListSlashCommands", "ListRemoteAgentSlashCommands",
		"ListConversations", "ListRemoteAgentConversations",
		"ListEnvironments", "ListRemoteAgentEnvironments":
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
//
// The table itself lives in the files domain package; this is the
// seam that hands it the host's dependencies.
func (d *Dispatcher) routeFileMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	return files.Route(d.filesSeams(), method, params)
}

// filesSeams assembles the files domain's dependencies from the
// dispatcher: the filesystem provider the host installed.
func (d *Dispatcher) filesSeams() files.Seams {
	return files.Seams{
		Provider: d.cfg.FS,
	}
}

// routeProjectMethods handles the project-CRUD methods
// (List/Create/Update + Delete).
//
// The table itself lives in the projects domain package; this is
// the seam that hands it the host's dependencies.
func (d *Dispatcher) routeProjectMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	return projects.Route(d.projectsSeams(), method, params)
}

// projectsSeams assembles the projects domain's dependencies
// from the dispatcher: the project store the host installed.
//
// A nil store is not a no-op: the domain answers -32603 ("no
// project store installed"), because there is nothing to read
// through.
func (d *Dispatcher) projectsSeams() projects.Seams {
	return projects.Seams{
		Store: d.cfg.Projects,
	}
}

// routePermissionMethods handles the four permission-mutation
// methods. Cycle rc-permissions-grants.
//
// The table itself lives in the permissions domain package; this
// is the seam that hands it the host's dependencies.
func (d *Dispatcher) routePermissionMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	return permissions.Route(d.permissionsSeams(), method, params)
}

// permissionsSeams assembles the permissions domain's
// dependencies from the dispatcher: the store the host installed
// and the broadcaster the notification goes out through.
func (d *Dispatcher) permissionsSeams() permissions.Seams {
	return permissions.Seams{
		Store:     d.cfg.PermissionsStore,
		Broadcast: d.BroadcastNotify,
	}
}

// routeDeleteMethods handles the two destructive conversation
// methods (DeleteRemoteAgentMessage / Conversation).
//
// DeleteRemoteAgentProject is the projects domain's method, not
// this table's: it lives in the projects package and is reached
// through routeProjectMethods.
func (d *Dispatcher) routeDeleteMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case "DeleteAgentMessage", "DeleteRemoteAgentMessage":
		r, e := d.handleDeleteRemoteAgentMessage(params)
		return r, e, true
	case "DeleteConversation", "DeleteRemoteAgentConversation":
		r, e := d.handleDeleteRemoteAgentConversation(params)
		return r, e, true
	}
	return nil, nil, false
}

// routeStatusMethods handles the four rc-status-tool methods
// (RemoteAgentErrorStatus / SetRemoteControlSession /
// remoteControlVersion / remoteControlDeepLink).
func (d *Dispatcher) routeStatusMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
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
//
// The table itself lives in the plugins domain package; this is
// the seam that hands it the host's dependencies.
func (d *Dispatcher) routePluginMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	return plugins.Route(d.pluginsSeams(), method, params)
}

// pluginsSeams assembles the plugins domain's dependencies from
// the dispatcher: the store the host installed and the broadcaster
// the notification goes out through.
func (d *Dispatcher) pluginsSeams() plugins.Seams {
	return plugins.Seams{
		Store:     d.cfg.PluginsStore,
		Broadcast: d.BroadcastNotify,
	}
}

// routeTriggerMethods handles the five trigger methods
// (List / ListOptions / Create / Update / Delete). Cycle
// rc-triggers.
//
// The table itself lives in the triggers domain package; this is
// the seam that hands it the host's dependencies.
func (d *Dispatcher) routeTriggerMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	return triggers.Route(d.triggersSeams(), method, params)
}

// triggersSeams assembles the triggers domain's dependencies
// from the dispatcher: the store the host installed and the
// broadcaster the notification goes out through.
func (d *Dispatcher) triggersSeams() triggers.Seams {
	return triggers.Seams{
		Store:     d.cfg.TriggersStore,
		Broadcast: d.BroadcastNotify,
	}
}

// routeFastpushMethods handles the single fastpush toggle
// method. Cycle rc-fastpush-toggle.
//
// The table itself lives in the fastpush domain package; this
// is the seam that hands it the host's dependencies.
func (d *Dispatcher) routeFastpushMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	return fastpush.Route(d.fastpushSeams(), method, params)
}

// fastpushSeams assembles the fastpush domain's dependencies
// from the dispatcher: the store the host installed, the
// narrow cascade view the toggle flips, and the broadcaster
// the notification goes out through.
func (d *Dispatcher) fastpushSeams() fastpush.Seams {
	return fastpush.Seams{
		Store:     d.cfg.FastpushStore,
		Cascades:  d.lookupFastpushCascade,
		Broadcast: d.BroadcastNotify,
	}
}

// lookupFastpushCascade resolves a cascade id to the narrow
// view the fastpush domain needs — the toggle it flips.
//
// The adapter is nil-safe in both directions: an unknown id
// answers (nil, false), and a registry entry that carries no
// cascade answers (nil, false) too, so the domain never sees a
// typed-nil interface.
func (d *Dispatcher) lookupFastpushCascade(id string) (fastpush.Cascade, bool) {
	if d == nil || d.cascades == nil {
		return nil, false
	}
	c, ok := d.cascades.Get(id)
	if !ok || c == nil {
		return nil, false
	}
	return c, true
}

// routeResourceMethods handles the three resource-management
// methods. Cycle rc-resources.
//
// The table itself lives in the resources domain package; this is
// the seam that hands it the host's dependencies.
func (d *Dispatcher) routeResourceMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	return resources.Route(d.resourcesSeams(), ctx, method, params)
}

// resourcesSeams assembles the resources domain's dependencies
// from the dispatcher: the store the host installed and the
// broadcaster the notification goes out through.
func (d *Dispatcher) resourcesSeams() resources.Seams {
	return resources.Seams{
		Store:     d.cfg.ResourceStore,
		Broadcast: d.BroadcastNotify,
	}
}

// routeConversationConfigMethods handles the single per-conv-config
// write method. Cycle rc-conversation-config.
//
// The table itself lives in the convconfig domain package; this
// is the seam that hands it the host's dependencies.
func (d *Dispatcher) routeConversationConfigMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	return convconfig.Route(d.convconfigSeams(), method, params)
}

// convconfigSeams assembles the conversation-config domain's
// dependencies from the dispatcher: the store the host
// installed and the broadcaster the notification goes out
// through.
func (d *Dispatcher) convconfigSeams() convconfig.Seams {
	return convconfig.Seams{
		Store:     d.cfg.ConversationConfigStore,
		Broadcast: d.BroadcastNotify,
	}
}

// routeTerminalMethods handles the five RemoteAgentTerminal*
// methods. Cycle rc-terminals.
//
// The table itself lives in the terminals domain package; this is
// the seam that hands it the host's dependencies.
func (d *Dispatcher) routeTerminalMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	return terminals.Route(d.terminalsSeams(), method, params)
}

// terminalsSeams assembles the terminals domain's dependencies
// from the dispatcher: the registry the host installed, the
// narrow cascade view the Create path checks and attaches to, and
// the broadcaster the notifications go out through.
func (d *Dispatcher) terminalsSeams() terminals.Seams {
	return terminals.Seams{
		Store:     d.cfg.TerminalsRegistry,
		Cascades:  d.lookupTerminalsCascade,
		Broadcast: d.BroadcastNotify,
	}
}

// lookupTerminalsCascade resolves a cascade id to the narrow
// view the terminals domain needs — the cascade's output chan.
//
// The adapter is nil-safe in both directions: an unknown id
// answers (nil, false), and a registry entry that carries no
// cascade answers (nil, false) too, so the domain never sees a
// typed-nil interface.
func (d *Dispatcher) lookupTerminalsCascade(id string) (terminals.Cascade, bool) {
	if d == nil || d.cascades == nil {
		return nil, false
	}
	c, ok := d.cascades.Get(id)
	if !ok || c == nil {
		return nil, false
	}
	return c, true
}

// routeTopLevelMethods handles the three rc-top-level-rpc methods
// (RemoteControlStatus / RemoteControlOnOutput /
// remoteControlOnMessage).
func (d *Dispatcher) routeTopLevelMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
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
//
// The table itself lives in the origins domain package; this is
// the seam that hands it the host's dependencies.
func (d *Dispatcher) routeOriginPolicyMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	return origins.Route(d.originsSeams(), method, params)
}

// originsSeams assembles the origin-policy domain's
// dependencies from the dispatcher: the store the host
// installed, the broadcaster the notification goes out through,
// and the setter that mirrors the accepted patterns into the
// transport's live snapshot.
func (d *Dispatcher) originsSeams() origins.Seams {
	return origins.Seams{
		Store:     d.cfg.OriginPolicyStore,
		Broadcast: d.BroadcastNotify,
		SetPolicy: d.originPolicySetterCallback(),
	}
}

// originPolicySetterCallback returns a callback that invokes
// the setter the host registered, or nil when it registered
// none.
//
// The dispatcher's mutex guards the swap, not the call: the
// callback is read under the lock and then invoked without it,
// so a slow transport never blocks SetOriginPolicySetter.
func (d *Dispatcher) originPolicySetterCallback() func(originpolicy.OriginPolicy) {
	if d == nil {
		return nil
	}
	d.originPolicySetterMu.RLock()
	setter := d.originPolicySetter
	d.originPolicySetterMu.RUnlock()
	if setter == nil {
		return nil
	}
	return setter
}
