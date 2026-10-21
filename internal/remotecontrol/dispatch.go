// Package remotecontrol — dispatch.go: the transport-agnostic
// JSON-RPC engine that every transport (Unix socket, TCP+WebSocket, …)
// drives.
//
// Why split: daemon.go used to mix "what to do with a JSON-RPC
// frame" (parser, method table, tool catalogue) with "how to read
// frames off a net.Conn" (bufio.Scanner, accept loop, file-mode on
// a Unix socket). Cycle rc-ws-transport splits the two halves so a
// future WebSocket transport (and any other transport we add later)
// can share the same dispatch logic without touching the
// wire-format details.
//
// Cycle rc-filesize-internal-remotecontrol further splits the
// dispatcher itself into per-handler dispatch_*.go files. The
// Dispatcher struct + constructor + Dispatch entry point live in
// this file; the per-method handlers live in:
//
//   - dispatch_cascade.go     Start / Send / Cancel / UserInteraction
//   - dispatch_deep_link.go   remoteControlDeepLink (3 verbs)
//   - dispatch_helpers.go     rpcError / rpcResult / nonStringFieldInJSON
//     / firstNonEmpty / timeFormat / formatUptime
//     / formatSeconds / callStatus / callListAgents
//     / callGetOutput
//   - dispatch_init.go        initialize / tools/list / tools/call
//   - dispatch_status.go      RemoteAgentErrorStatus /
//     SetRemoteControlSession /
//     remoteControlVersion + richStatusSnapshot
//
// Public surface:
//
//   - Dispatcher — holds the per-instance Config + counters; one
//     Dispatcher is shared across all transports a single binary
//     brings up (the Unix-socket one and the TCP+WS one). Each
//     transport keeps its own accept loop and per-connection
//     goroutine; each per-connection goroutine calls
//     Dispatcher.Dispatch for every frame it pulls off the wire.
//
//   - NewDispatcher applies the same defaults Start used to apply
//     (Permissions=0o600, Agents=DefaultAgentLister, Reader=
//     DefaultOutputReader{}) so transport adapters don't have to
//     repeat that boilerplate.
//
// Wire format (unchanged from the cycle-183 daemon — the existing
// 613 lines of daemon_test.go pin this byte-for-byte):
//
//	{ "jsonrpc": "2.0", "id": <any>, "method": "<name>", "params": {…} }
//
// Responses follow JSON-RPC 2.0 (result or error). tools/call
// results carry an MCP envelope:
//
//	{ "content": [ { "type": "text", "text": "…" } ], "isError": false }
//
// The split does NOT change the response envelopes — only which
// file the parsing + handler dispatch lives in.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/justwasm/pigasus/internal/originpolicy"
)

// Dispatcher is the JSON-RPC protocol engine. It owns the Config
// (so the same handlers are used by every transport) and exposes
// a Dispatch method that takes a single JSON-RPC request frame
// and returns a JSON-RPC response frame (or nil for
// notifications).
type Dispatcher struct {
	cfg Config
	// accepted counts the cumulative number of connections since
	// NewDispatcher. The accept loop on each transport calls
	// MarkAccepted once per accepted connection; the value is
	// surfaced through Dispatcher.Accepted() and from there
	// through the per-transport facade (so e.g. Daemon.Accepted
	// still works for the existing daemon_test.go).
	accepted atomic.Uint64

	// notifyMu guards notify. RWMutex so the hot-path (Notify)
	// takes a read lock while SetNotifier takes a write lock.
	notifyMu sync.RWMutex
	// notify is the callback Notify uses to push frames to a
	// specific connection. nil means "no notifier registered" —
	// Notify becomes a no-op. Set by the transport (TCPServer
	// sets it in NewTCPServer; UnixServer does not, because the
	// Unix-socket transport is request/response only).
	notify Notifier

	// cascades is the per-process registry of cascades currently
	// alive on this dispatcher. Cycle rc-cascade-start adds it:
	// StartRemoteAgentCascade allocates one Cascade value here
	// so later-cycle methods (SendRemoteAgentCascadeMessage,
	// StreamRemoteAgentStateUpdates, …) can look it up by id.
	// Owned by the dispatcher because every cascade the dashboard
	// holds needs the same dispatcher's tool / status / reader
	// surface; the brief's "registry lives per-Daemon" rule means
	// cycles reset cascades across restarts.
	cascades *CascadeRegistry

	// broadcasterMu guards broadcaster. RWMutex so the
	// hot-path (BroadcastNotify) takes a read lock while
	// SetBroadcaster takes a write lock. Mirrors
	// notifyMu / notify on the per-connID path.
	broadcasterMu sync.RWMutex
	// broadcaster is the callback BroadcastNotify uses to fan a
	// frame out to every open connection on the registered
	// transport. nil means "no broadcaster installed" —
	// BroadcastNotify becomes a no-op (the typical case for
	// in-process dispatchers and the Unix-socket transport).
	// Set by the transport's constructor via SetBroadcaster;
	// rc-permissions-grants adds the seam, tcpserver's
	// NewTCPServer is the first caller.
	broadcaster ConnBroadcaster

	// allSinksMu guards allSinks. RWMutex so the hot-path
	// (IsAllSink / AllSinkConnIDs) takes a read lock while
	// AddAllSink / RemoveAllSink take a write lock.
	//
	// Cycle rc-top-level-rpc adds this seam. The
	// RemoteControlOnOutput handler adds the calling ws conn's
	// connID here. Today the per-cascade notification paths
	// (cycle-5) and the broadcast-to-all paths (cycle-13's
	// BroadcastNotify) do NOT fan into AllSinks — that's the
	// forward-compat `rc-all-sinks-fanout` cycle. This cycle
	// ships the registration surface so future wiring has
	// nothing to migrate.
	allSinksMu sync.RWMutex
	// allSinks is the set of ws connection IDs registered to
	// receive ALL dispatcher notifications (subject to the
	// future rc-all-sinks-fanout integration). Empty / nil
	// means "no sinks registered" — the helper methods are
	// no-ops in that case.
	allSinks map[string]struct{}

	// originPolicySetterMu guards originPolicySetter. RWMutex so
	// the hot-path read in handleSetRemoteControlOriginPatterns
	// takes a read lock while SetOriginPolicySetter takes a
	// write lock. Mirrors notifyMu / notify + broadcasterMu /
	// broadcaster on the per-connID / cross-conn paths.
	//
	// Cycle rc-auth-tokens adds this.
	originPolicySetterMu sync.RWMutex
	// originPolicySetter is the callback the
	// SetRemoteControlOriginPatterns handler invokes after a
	// successful store mutation, so an immediately-following WS
	// Accept sees the new patterns. nil means "no setter
	// registered" — the handler still completes the store
	// mutation + emits the origin_policy/changed notification
	// but skips the in-process push to the server.
	//
	// Set by the transport's constructor (or the host bridge)
	// via SetOriginPolicySetter.
	originPolicySetter func(originpolicy.OriginPolicy)
}

// NewDispatcher returns a Dispatcher wired with cfg. The cfg's
// defaults are applied here (Permissions/Agents/Reader/Cascades
// nil checks) so callers don't have to repeat them per transport.
//
// Cycle rc-cascade-start adds the Cascades default: a fresh
// CascadeRegistry with CascadeRegistryDefaultMax capacity. Host
// wiring that wants a different cap can pass a pre-built
// registry via Config.Cascades — useful for tests that need to
// exercise the over-cap path without spinning 256 cascades.
//
// Cycle rc-permissions-grants adds the Permissions default
// pointer: a fresh MemPermissionStore seeded with
// DefaultPermissionPreset + DefaultArtifactReviewMode + empty
// grants. Production wiring always replaces this with an
// FSPermissionStore rooted at $ANTIGRAVITY_HOME/permissions.json
// via cmd/wagyu/remote_control.go's buildHostPermissionStore.
func NewDispatcher(cfg Config) (*Dispatcher, error) {
	if cfg.Session == nil {
		return nil, errors.New("remotecontrol: Config.Session is required")
	}
	if err := applyDispatcherDefaults(&cfg); err != nil {
		return nil, err
	}
	return &Dispatcher{cfg: cfg, cascades: cfg.Cascades}, nil
}

// applyDispatcherDefaults walks every Config field with a
// nil-guard default and fabricates a fresh in-memory seam when
// the host didn't install one. Each nil-guard mirrors the
// previous-cycle comment block; we extract them out of the body
// so the constructor stays ≤ 50 LOC.
func applyDispatcherDefaults(cfg *Config) error {
	fabricateCoreDispatchFields(cfg)
	fabricateStoreSeams(cfg)
	return nil
}

// fabricateCoreDispatchFields sets the four pre-cycle seams
// every dispatcher needs: Permissions / Agents / Reader /
// Cascades.
func fabricateCoreDispatchFields(cfg *Config) {
	if cfg.Permissions == 0 {
		cfg.Permissions = DefaultPermissions
	}
	if cfg.Agents == nil {
		cfg.Agents = DefaultAgentLister
	}
	if cfg.Reader == nil {
		cfg.Reader = DefaultOutputReader{}
	}
	if cfg.Cascades == nil {
		cfg.Cascades = NewCascadeRegistry(CascadeRegistryDefaultMax)
	}
	// Cycle rc-deep-link: fresh deep-link state holder
	// when the host didn't install one.
	if cfg.LastDeepLink == nil {
		cfg.LastDeepLink = &DeepLinkState{}
	}
}

// fabricateStoreSeams installs a fresh in-memory seam for every
// per-cycle store. Each guard mirrors the previous-cycle
// comment block; the helpers stay focused so the constructor
// can call them all in one place.
func fabricateStoreSeams(cfg *Config) {
	if cfg.PermissionsStore == nil {
		cfg.PermissionsStore = NewMemPermissionStore(nil)
	}
	if cfg.PluginsStore == nil {
		cfg.PluginsStore = NewMemPluginStore(nil)
	}
	if cfg.TriggersStore == nil {
		cfg.TriggersStore = NewMemTriggerStore(nil)
	}
	if cfg.FastpushStore == nil {
		cfg.FastpushStore = NewMemFastpushStore(nil, nil)
	}
	if cfg.ResourceStore == nil {
		cfg.ResourceStore = NewMemResourceStore(nil, nil)
	}
	if cfg.ConversationConfigStore == nil {
		cfg.ConversationConfigStore = NewMemConversationConfigStore(nil)
	}
	if cfg.TerminalsRegistry == nil {
		cfg.TerminalsRegistry = NewTerminalsRegistry()
	}
	if cfg.OriginPolicyStore == nil {
		cfg.OriginPolicyStore = NewMemOriginPolicyStore(nil)
	}
}

// SetOriginPolicySetter registers the callback the
// SetRemoteControlOriginPatterns handler invokes after a
// successful store mutation so an immediately-following WS Accept
// sees the new patterns. The signature matches
// (*remotetransport.TCPServer).SetOriginPolicy, which the host
// wiring installs at NewTCPServer time. nil clears the setter
// (the typical "host didn't wire a server" path the Unix-socket
// transport exercises).
//
// Safe to call concurrently with the dispatcher's hot path; the
// originPolicySetterMu write lock is held only across the swap.
// In-flight SetRemoteControlOriginPatterns calls use the callback
// that was registered when they started, matching the
// SetNotifier / SetBroadcaster contract.
//
// Cycle rc-auth-tokens adds this.
func (d *Dispatcher) SetOriginPolicySetter(setter func(originpolicy.OriginPolicy)) {
	if d == nil {
		return
	}
	d.originPolicySetterMu.Lock()
	d.originPolicySetter = setter
	d.originPolicySetterMu.Unlock()
}

// Cascades returns the dispatcher's cascade registry. Tests use
// this to assert that StartRemoteAgentCascade actually inserted
// the cascade (without spinning a model turn). Future cycle
// rc-status-tool will surface the registry's Len() through the
// status tool's "live cascades" counter.
func (d *Dispatcher) Cascades() *CascadeRegistry {
	if d == nil {
		return nil
	}
	return d.cascades
}

// Accepted returns the cumulative number of connections the
// Dispatcher has seen since construction. Each transport's
// accept loop calls MarkAccepted once per connection; the
// counter survives a transport's Shutdown because the
// Dispatcher outlives any single transport.
func (d *Dispatcher) Accepted() uint64 {
	if d == nil {
		return 0
	}
	return d.accepted.Load()
}

// MarkAccepted bumps the cumulative counter. Called by the
// per-transport accept loop after a successful Accept.
func (d *Dispatcher) MarkAccepted() {
	if d == nil {
		return
	}
	d.accepted.Add(1)
}

// NotifyConnected bumps the SessionState.connected counter for
// the status tool. The transport's accept loop calls this once
// per connection; NotifyDisconnected is the symmetric teardown.
func (d *Dispatcher) NotifyConnected() {
	if d == nil || d.cfg.Session == nil {
		return
	}
	d.cfg.Session.connected.Add(1)
}

// NotifyDisconnected drops the SessionState.connected counter.
func (d *Dispatcher) NotifyDisconnected() {
	if d == nil || d.cfg.Session == nil {
		return
	}
	d.cfg.Session.connected.Add(-1)
}

// Session exposes the underlying SessionState so a transport
// can print the resolved path / PID on startup without
// re-reading cfg. Returns the pointer verbatim; the dispatcher
// shares it with the host binary.
func (d *Dispatcher) Session() *SessionState {
	if d == nil {
		return nil
	}
	return d.cfg.Session
}

// Dispatch routes one JSON-RPC request to its handler. It
// returns nil for notifications (which the protocol defines as
// having no "id" field) so the transport can skip the write.
// Per-request failures are encoded in the returned JSON-RPC
// envelope — Dispatch itself never returns a Go error.
func (d *Dispatcher) Dispatch(ctx context.Context, raw []byte) map[string]any {
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id,omitempty"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return rpcError(nil, -32700, "Parse error", err.Error())
	}
	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		return rpcError(req.ID, -32600, "Invalid Request", "jsonrpc must be \"2.0\"")
	}
	if req.Method == "" {
		return rpcError(req.ID, -32600, "Invalid Request", "method is required")
	}
	isNotification := len(req.ID) == 0
	result, rpcErr := d.routeMethod(ctx, req.Method, req.Params)
	if isNotification {
		// Drop the reply — server MUST NOT reply to a
		// notification, even on handler failure. The
		// handler ran (so any side-effects landed), but
		// the wire is silent.
		return nil
	}
	if rpcErr != nil {
		return rpcError(req.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}
	return rpcResult(req.ID, result)
}

// routeMethod dispatches one JSON-RPC method name to its
// handler. The per-family routing tables live in
// dispatch_route.go; this function is a thin fan-out so the
// hot-path stays small.
func (d *Dispatcher) routeMethod(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody) {
	// The reference's gRPC method names (StartCascade, ReadFile, …) are
	// accepted as aliases for the clone's legacy JSON-RPC names; see
	// reference_surface.go.
	method = canonicalMethod(method)
	routers := []func(context.Context, string, json.RawMessage) (any, *rpcErrorBody, bool){
		d.routeMCPTools,
		d.routeCascadeLifecycle,
		d.routeReadOnlyLists,
		d.routeFileMethods,
		d.routeProjectMethods,
		d.routePermissionMethods,
		d.routeDeleteMethods,
		d.routeStatusMethods,
		d.routePluginMethods,
		d.routeTriggerMethods,
		d.routeFastpushMethods,
		d.routeResourceMethods,
		d.routeConversationConfigMethods,
		d.routeTerminalMethods,
		d.routeTopLevelMethods,
		d.routeOriginPolicyMethods,
	}
	for _, r := range routers {
		if result, rpcErr, ok := r(ctx, method, params); ok {
			return result, rpcErr
		}
	}
	return nil, &rpcErrorBody{Code: -32601, Message: "Method not found", Data: method}
}
