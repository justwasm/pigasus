// Package remotecontrol —
// top_level_dispatcher.go: the per-method JSON-RPC
// handlers for the three "top-level" methods promoted
// in cycle rc-top-level-rpc.
//
// Why a dedicated file (not another switch arm in
// dispatch.go): the handlers share the (*Dispatcher)
// AllSinks set + the RemoteControlOnMessageAllowList
// gate + the topLevelStatus snapshot builder, and
// the cycle introduces a per-connID set the
// dispatcher tracks. Putting the handlers here, next
// to the type definitions (top_level.go), keeps the
// per-method bodies focused on the JSON-RPC envelope
// shape rather than the dispatcher's switch table.
//
// Sentinel map:
//
//	missing / non-string fields    → -32602 "Invalid params"
//	gatekeeper rejection           → -32603 "Internal error"
//	(internal)                     → -32603 (any other)
//
// remoteControlOnMessage has its OWN return code
// vocabulary from the brief:
//
//	empty / non-string message / missing method
//	                              → -32602
//	method not in allowlist       → -32603
//	(internal)                    → -32603
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"strings"
	"time"
)

// ----- AllSinks set (dispatcher-level) -----
//
// The cycle's per-connID set lives on the dispatcher
// (not on a transport) so the in-process dispatcher
// tests can exercise it without spinning a transport.
// The seam mirrors the (*Dispatcher).notify /
// (*Dispatcher).broadcaster pattern: the dispatcher
// owns the state, the handlers wrap it in OnOutput /
// future-fanout methods.

// AddAllSink registers the given connID in the
// dispatcher's AllSinks set. A subsequent
// rc-all-sinks-fanout cycle's notification path will
// push every dispatcher notification to all connIDs
// in this set. Idempotent — re-registering an
// existing connID is a no-op.
//
// Nil-safe: a nil receiver short-circuits the
// registration. The dispatcher tests inject a
// non-nil dispatcher; the helper is exported so the
// in-process test suite can verify registration
// without going through the wire.
func (d *Dispatcher) AddAllSink(connID string) {
	if d == nil || connID == "" {
		return
	}
	d.allSinksMu.Lock()
	defer d.allSinksMu.Unlock()
	if d.allSinks == nil {
		d.allSinks = make(map[string]struct{})
	}
	d.allSinks[connID] = struct{}{}
}

// RemoveAllSink drops the given connID from the
// dispatcher's AllSinks set. Idempotent — removing
// an absent connID is a no-op. The transport's
// accept-loop teardown is the typical caller (when
// a ws conn closes, the transport calls RemoveAllSink
// so the sink doesn't accumulate).
//
// Nil-safe. The transport hooks will be added by a
// future rc-all-sinks-fanout cycle; this cycle ships
// the helper so the seam exists.
func (d *Dispatcher) RemoveAllSink(connID string) {
	if d == nil || connID == "" {
		return
	}
	d.allSinksMu.Lock()
	defer d.allSinksMu.Unlock()
	if d.allSinks == nil {
		return
	}
	delete(d.allSinks, connID)
}

// IsAllSink reports whether the given connID is in
// the dispatcher's AllSinks set. The future
// rc-all-sinks-fanout cycle's fan-out helper uses
// this to decide whether to push a notification to
// the connID.
func (d *Dispatcher) IsAllSink(connID string) bool {
	if d == nil || connID == "" {
		return false
	}
	d.allSinksMu.RLock()
	defer d.allSinksMu.RUnlock()
	_, ok := d.allSinks[connID]
	return ok
}

// AllSinkConnIDs returns the sorted list of connIDs
// in the dispatcher's AllSinks set. Used by the
// future rc-all-sinks-fanout cycle's fan-out helper;
// this cycle ships the method so the seam is
// in-place.
func (d *Dispatcher) AllSinkConnIDs() []string {
	if d == nil {
		return nil
	}
	d.allSinksMu.RLock()
	defer d.allSinksMu.RUnlock()
	if len(d.allSinks) == 0 {
		return nil
	}
	out := make([]string, 0, len(d.allSinks))
	for id := range d.allSinks {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// ----- Handlers -----

// handleRemoteControlStatus is the JSON-RPC handler
// for "RemoteControlStatus". Pure read; no params
// parsed, no dispatcher state mutated. See
// top_level.go for the envelope definition.
//
// Sources (see (*Dispatcher).topLevelStatus for the
// full list):
//
//   - version: SessionState.Version (or ServerVersion
//     fallback).
//   - uptime_seconds: time.Since(SessionState.StartedAt).
//   - started_at: RFC3339Nano UTC.
//   - connected_clients: SessionState.Connected().
//   - open_cascades: (*CascadeRegistry).Len().
//   - dispatch_methods: sortedDefaultDispatchMethods() copy.
//   - server_fingerprint: serverFingerprint(d, version).
//
// Nil-safe: a dispatcher with bare-bones Config
// (no Session, no cascades) returns a populated
// envelope with zero values rather than panicking.
func (d *Dispatcher) handleRemoteControlStatus() (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	snap := d.topLevelStatus()
	return snap, nil
}

// handleRemoteControlOnOutput is the JSON-RPC
// handler for "RemoteControlOnOutput". Subscribes
// the calling ws connection to ALL dispatcher
// notifications via the AllSinks set.
//
// Param validation:
//
//   - sink_id: optional. Absent / non-string →
//     default to the connID threaded via the
//     per-request context (connIDFromContext).
//     Whitespace-trimmed; empty after trim → same
//     default. A dashboard that supplies a custom
//     sink_id (e.g. a routing handle distinct from
//     the ws connID) gets that value echoed in
//     the response.
//
// Wire envelope (see RemoteControlOnOutputResult
// in top_level.go):
//
//	{
//	  "ok":            true,
//	  "sink_id":       "<resolved sink id>",
//	  "subscribed_at": "<RFC3339Nano UTC>",
//	  "filter":        [<sorted topics>]
//	}
//
// Server-side effects: AddAllSink(connID) so a
// future rc-all-sinks-fanout cycle's notification
// fan-out helper sees this conn as a sink.
//
// Error envelopes:
//
//   - non-string sink_id → -32602
func (d *Dispatcher) handleRemoteControlOnOutput(ctx context.Context, params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	var p struct {
		SinkID string `json:"sink_id"`
	}
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if nonStringFieldInJSON(params, "sink_id", p.SinkID) {
		return nil, invalidParams("sink_id must be a string")
	}
	sinkID := strings.TrimSpace(p.SinkID)
	if sinkID == "" {
		// Default to the calling ws conn's connID
		// so a dashboard that omits sink_id still
		// gets a stable, identifiable handle.
		sinkID = connIDFromContext(ctx)
	}

	// Register the sink with the dispatcher's set.
	// Idempotency is the helper's contract.
	d.AddAllSink(sinkID)

	now := time.Now().UTC()
	return RemoteControlOnOutputResult{
		OK:           true,
		SinkID:       sinkID,
		SubscribedAt: now.Format(time.RFC3339Nano),
		Filter:       sortedDefaultNotificationFilter(),
	}, nil
}

// handleRemoteControlOnMessage is the JSON-RPC
// handler for "remoteControlOnMessage". Gates an
// inbound JSON-RPC message on the
// RemoteControlOnMessageAllowList closed set and
// re-dispatches the allowed message through the
// dispatcher's normal handler table.
//
// Param validation:
//
//   - message: required, non-null object. Absent /
//     null / non-object → -32602 (the brief pins
//     this as -32602 "Invalid params" — see the
//     "BadParams" test case).
//   - message.method: required, non-empty string.
//     Empty / whitespace / non-string → -32602.
//   - message.jsonrpc: optional. Accepts any value
//     (the dispatcher ignores the field on the
//     re-dispatched frame — the inner handler is
//     reached via (*Dispatcher).Dispatch, which
//     doesn't depend on jsonrpc being a specific
//     value).
//   - message.params: optional. Re-marshalled into
//     the inner frame so a permitted `{"X":1}` is
//     faithfully forwarded.
//
// Gatekeeper:
//
//   - method not in RemoteControlOnMessageAllowList
//     → -32603 with data="rejected by gatekeeper:
//     <method>".
//   - every attempt (allowed + denied) is logged via
//     the standard log package so an operator can
//     audit who tried to side-channel what through
//     the daemon.
//
// On the accepted path the handler re-dispatches
// the inner message through (*Dispatcher).Dispatch;
// the inner response is intentionally DISCARDED
// (not nested into the outer envelope) — the brief
// pins the outer envelope as a flat
// {ok:true, accepted:true, replayed_at} confirmation.
//
// Error envelopes:
//
//   - empty / non-object message → -32602
//   - empty / non-string method → -32602
//   - method not in allowlist → -32603
//   - marshal failure on the inner frame → -32603
//     (defensive; encoding/json can't fail on the
//     shapes this handler accepts in practice).
func (d *Dispatcher) handleRemoteControlOnMessage(ctx context.Context, params json.RawMessage) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	method, innerParams, perr := decodeRemoteControlOnMessage(params)
	if perr != nil {
		return nil, perr
	}
	// Gatekeeper. The brief pins -32603 for rejected
	// methods. We log every attempt (allowed +
	// rejected) so an operator can audit which
	// dashboards tried to use the surface for what.
	if !isRemoteControlOnMessageAllowed(method) {
		log.Printf("rc-top-level-rpc: remoteControlOnMessage rejected method=%q conn=%q", method, connIDFromContext(ctx))
		return nil, &rpcErrorBody{
			Code:    -32603,
			Message: "Internal error",
			Data:    "rejected by gatekeeper: " + method,
		}
	}
	// Re-dispatch through the dispatcher's normal
	// handler table. The inner frame is a JSON-RPC
	// 2.0 request with a fresh numeric id (the
	// outer caller's id is dropped — the inner
	// response is intentionally discarded; the
	// outer envelope is just the confirmation).
	innerRaw, err := buildRemoteControlInnerFrame(method, innerParams)
	if err != nil {
		log.Printf("rc-top-level-rpc: remoteControlOnMessage marshal failure method=%q: %v", method, err)
		return nil, internalError("inner marshal failure: " + err.Error())
	}
	// The inner Dispatch writes its response frame
	// to whatever transport the outer caller came
	// in on (we use the outer ctx so any connID /
	// notifier hooks flow through). We discard the
	// returned frame — the outer envelope is the
	// confirmation. Side effects of the inner
	// dispatch (mutations, notifications, etc.)
	// already happened; that's the point.
	_ = d.Dispatch(ctx, innerRaw)

	log.Printf("rc-top-level-rpc: remoteControlOnMessage dispatched method=%q conn=%q", method, connIDFromContext(ctx))

	now := time.Now().UTC()
	return RemoteControlOnMessageResult{
		OK:         true,
		Accepted:   true,
		ReplayedAt: now.Format(time.RFC3339Nano),
	}, nil
}
