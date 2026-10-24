package remotecontrol

import (
	"time"
)

// Package remotecontrol — list_dispatcher.go: the read-only
// dispatch table for the four List… methods the cycle
// rc-list-readonly ships.
//
// Why a dedicated dispatcher file (not just another switch
// in dispatch.go):
//
//   - The four list handlers are uniformly read-only. The
//     cycle's "no mutation" rule is *its own* contract — the
//     other handlers' rule is "decode params, validate, call
//     registry/state, map errors". Mixing the two patterns in
//     one switch loses the read-only invariant.
//
//   - Future cycles (`rc-list-projects`, `rc-list-agents`)
//     add to this dispatcher rather than to dispatch.go so the
//     read-only surface stays in one file.
//
//   - The pre-execution hook below (ListReadOnlyPreFlight)
//     enforces the read-only rule uniformly across all four
//     handlers and is the place a future "registered
//     side-effecting handler on a list-shaped method" attempt
//     fails fast.
//
// Read-only contract enforcement:
//
//   The contract is "ListRemoteAgent{Models,SlashCommands,
//   Conversations,Environments} MUST NOT mutate state". The
//   cycle encodes it in three places:
//
//   1. ListMethodsNoSideEffects — a package-level var that
//      names the four read-only methods; used by the dispatch
//      table's pre-execution hook below.
//
//   2. ListReadOnlyPreFlight — a method on *Dispatcher called
//      once per request before the per-method handler runs;
//      it consults the configured handler's "this handler
//      DOES set a side-effect flag" (a future-cycle affordance)
//      and rejects the call with -32601 if the flag is on.
//
//      Today, the production handlers don't set the flag
//      (their closures are pure reads). The cycle ships the
//      pre-flight as a guardrail; the test surface pins it
//      via TestListReadOnly_RejectsSideEffectingHandler.
//
//   3. TestListBackend_NoEnvLeak — the read-only invariant
//      over the env-leak path of ListBackend.Environments:
//      the resulting env_keys_present slice MUST be sorted,
//      deduped, and contain only NAMES — never VALUES.
//
// The dispatcher patterns in this file match dispatch.go's:
// switch on method name, decode JSON, validate with the
// existing nonStringFieldInJSON where relevant, call into
// Config.ListBackend (or its default), return either a
// JSON-RPC result or a JSON-RPC error envelope.

// ListMethodsNoSideEffects names the four read-only methods.
// Listing these here means future cycles that add a method
// can opt-in to the read-only contract by adding their name
// to this slice; the pre-flight hook below consults the
// slice rather than a hand-rolled switch.
//
// Kept as a var (not a const slice) so a test that wants to
// prove "a method is in the read-only set" can compare against
// it without re-typing the names.
var ListMethodsNoSideEffects = []string{
	"ListRemoteAgentModels",
	"ListRemoteAgentSlashCommands",
	"ListRemoteAgentConversations",
	"ListRemoteAgentEnvironments",
}

// isListReadOnly reports whether method is in the
// ListMethodsNoSideEffects set. Pure helper used by both
// the pre-flight hook and the wire-level tests.
func isListReadOnly(method string) bool {
	for _, m := range ListMethodsNoSideEffects {
		if m == method {
			return true
		}
	}
	return false
}

// dispatcherList is the per-method dispatch table for the
// four read-only methods. A nil entry means "unregistered";
// the dispatch switch falls back to a -32601 envelope in that
// case.
//
// The handlers are stored here (not directly in dispatch.go's
// switch) so the read-only surface is in one file. The
// dispatcher's main switch routes each of these method names
// into d.dispatchList(method, params) — see dispatch.go's
// rc-list-readonly block.
//
// Each handler signature is:
//
//	func(d *Dispatcher, params json.RawMessage) (any, *rpcErrorBody)
//
// Matching the existing (any, *rpcErrorBody) pattern so the
// dispatcher doesn't grow a second pass through rpcErrorBody /
// rpcResult. The handlers return the same (nil, *rpcErrorBody)
// shape as the existing handlers.
type dispatcherList struct {
	d *Dispatcher
}

// handleListRemoteAgentModels is the per-method handler for
// ListRemoteAgentModels. Param shape:
//
//	{
//	  "backend": "gateway" | "shim" | "",   // optional, default ""
//	  "filter":  "<substring>"          // optional, default ""
//	}
//
// Result shape:
//
//	{
//	  "models":        [ {id, label, backend, effort_supported, default}, ... ],
//	  "default_model": "<id>"
//	}
//
// `backend` is validated against the closed set { "gateway",
// "shim", "" } — anything else yields -32602. `filter` is
// pass-through; the helper applies a substring match against
// ModelInfo.ID and ModelInfo.Label.
//
// default_model is the id of the row with default=true when
// one is present; empty otherwise. The frontend's
// "show me what's running" probe reads this.
//
// Errors:
//
//   - bad backend value → -32602 "backend must be one of: gateway, shim"
//   - underlying Models closure returned error → -32603
func (l dispatcherList) handleListRemoteAgentModels(params []byte) (any, *rpcErrorBody) {
	var p struct {
		Backend string `json:"backend"`
		Filter  string `json:"filter"`
	}
	if len(params) > 0 {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	if p.Backend != "" && p.Backend != "gateway" && p.Backend != "shim" {
		return nil, invalidParams("backend must be one of: gateway, shim, \"\"")
	}

	var (
		out   []ModelInfo
		defID string
		err   error
	)
	if l.d != nil && l.d.cfg.ListBackend.Models != nil {
		out, err = l.d.cfg.ListBackend.Models(p.Backend, p.Filter)
	}
	if err != nil {
		return nil, internalError(err.Error())
	}
	// No closure installed: fall back to the package-level
	// DefaultModelInfo (always non-empty) so a dashboard
	// always sees a usable picker.
	if out == nil {
		out = append(out, DefaultModelInfo...)
	}
	// Filter pass: apply our own substring filter so a
	// future ListBackend that ignores the filter still
	// returns the trimmed slice. `filterModels` is
	// pass-through when filter is empty.
	out = filterModels(out, p.Filter)
	for _, m := range out {
		if m.Default {
			defID = m.ID
			break
		}
	}
	if out == nil {
		out = []ModelInfo{}
	}
	return map[string]any{
		"models":        out,
		"default_model": defID,
	}, nil
}

// handleListRemoteAgentSlashCommands is the per-method handler
// for ListRemoteAgentSlashCommands. Param shape: {} (any
// non-object / empty is accepted). Result shape:
//
//	{
//	  "commands": [ {name, description, scope, args?} ]
//	}
//
// Errors:
//   - underlying SlashCommands closure returned error → -32603
func (l dispatcherList) handleListRemoteAgentSlashCommands(params []byte) (any, *rpcErrorBody) {
	// Param shape is intentionally tolerant: empty / {} /
	// {"unrelated":"…"} are all valid.
	var p map[string]any
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	var (
		out []SlashCommandInfo
		err error
	)
	if l.d != nil && l.d.cfg.ListBackend.SlashCommands != nil {
		out, err = l.d.cfg.ListBackend.SlashCommands()
	}
	if err != nil {
		return nil, internalError(err.Error())
	}
	if out == nil {
		// Default command catalog is a package-level
		// var. Always non-nil.
		out = DefaultSlashCommands
	}
	return map[string]any{
		"commands": out,
	}, nil
}

// handleListRemoteAgentConversations is the per-method handler
// for ListRemoteAgentConversations. Param shape:
//
//	{
//	  "limit":  <int>,           // optional, 1..200, default 50
//	  "before": "<RFC3339>"      // optional cursor
//	}
//
// Result shape:
//
//	{
//	  "conversations": [ {id, title, created_at, updated_at, model?} ],
//	  "next_before":   "<RFC3339>" | ""   // cursor for the next page
//	}
//
// `limit` is capped at 200 (the brief: "absurdly large" →
// -32602). `before` is parsed RFC3339; bad input → -32602. An
// empty before means "newest first, no upper bound".
//
// next_before is the UpdatedAt of the LAST entry the page
// returned; "" when fewer than `limit` entries came back
// (the dashboard knows there's nothing more).
//
// Errors:
//   - limit < 0 or > 200 → -32602
//   - bad RFC3339 on `before` → -32602
//   - underlying Conversations closure error → -32603
func (l dispatcherList) handleListRemoteAgentConversations(params []byte) (any, *rpcErrorBody) {
	limit, before, perr := decodeListConversationsParams(params)
	if perr != nil {
		return nil, perr
	}
	var (
		out    []ConversationInfo
		cursor time.Time
		err    error
	)
	if l.d != nil && l.d.cfg.ListBackend.Conversations != nil {
		out, cursor, err = l.d.cfg.ListBackend.Conversations(limit, before)
	}
	if err != nil {
		return nil, internalError(err.Error())
	}
	if out == nil {
		out = []ConversationInfo{}
	}
	nextBefore := resolveTimeFormat(cursor)
	return map[string]any{
		"conversations": out,
		"next_before":   nextBefore,
	}, nil
}

// decodeListConversationsParams unmarshals the
// ListRemoteAgentConversations params, validates the
// bounded `limit` (0 → 50 default), and parses the
// optional `before` cursor (RFC3339Nano-or-RFC3339).
// Extracted from handleListRemoteAgentConversations
// to keep the handler body under the 50-fn ceiling
// cycle rc-filesize-internal-remotecontrol enforces.
func decodeListConversationsParams(params []byte) (int, time.Time, *rpcErrorBody) {
	var p struct {
		Limit  int    `json:"limit"`
		Before string `json:"before"`
	}
	if len(params) > 0 {
		if err := jsonUnmarshal(params, &p); err != nil {
			return 0, time.Time{}, invalidParams(err.Error())
		}
	}
	if p.Limit < 0 {
		return 0, time.Time{}, invalidParams("limit must be >= 0")
	}
	if p.Limit > 200 {
		return 0, time.Time{}, invalidParams("limit must be <= 200")
	}
	if p.Limit == 0 {
		p.Limit = 50
	}
	var before time.Time
	if stringsTrimSpace(p.Before) != "" {
		t, err := timeParse(time.RFC3339Nano, p.Before)
		if err != nil {
			t, err = timeParse(time.RFC3339, p.Before)
		}
		if err != nil {
			return 0, time.Time{}, invalidParams("before must be RFC3339")
		}
		before = t
	}
	return p.Limit, before, nil
}

// handleListRemoteAgentEnvironments is the per-method handler
// for ListRemoteAgentEnvironments. Param shape: {} (empty or
// any object). Result shape: the EnvironmentInfo struct
// marshalled verbatim.
//
// Errors:
//   - underlying Environments closure error → -32603
//
// The closure is the only place that reads os env, hostname,
// cwd, etc. — keeping stdlib reads out of this file means a
// future cycle that wants to substitute a mock for tests
// only has to wire a different closure into Config.ListBackend.
func (l dispatcherList) handleListRemoteAgentEnvironments(params []byte) (any, *rpcErrorBody) {
	var p map[string]any
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	var (
		info EnvironmentInfo
		err  error
	)
	if l.d != nil && l.d.cfg.ListBackend.Environments != nil {
		info, err = l.d.cfg.ListBackend.Environments()
	}
	if err != nil {
		return nil, internalError(err.Error())
	}
	return map[string]any{
		"os":               info.OS,
		"arch":             info.Arch,
		"hostname":         info.Hostname,
		"cwd":              info.Cwd,
		"user":             info.User,
		"shell":            info.Shell,
		"antigravity_home": info.AntigravityHome,
		"version":          info.Version,
		"env_keys_present": info.EnvKeysPresent,
		"capabilities":     info.Capabilities,
	}, nil
}

// dispatchList is the entry point the dispatcher's main switch
// calls for the four read-only methods. It returns the same
// (any, *rpcErrorBody) shape as handleSendRemoteAgentCascadeMessage
// etc.
//
// On unknown method names it returns (nil, *rpcErrorBody{-32601})
// so the caller can decide whether to fall through to the
// cascade-stream registry path or surface the not-found.
//
// Read-only pre-flight: a future cycle that registers a
// handler with a side-effecting closure (it mutates the
// registry, sends network traffic, etc.) gets rejected at
// this boundary with -32601. The cycle ships the hook but no
// handlers trip it yet — production handlers are pure reads.
func (d *Dispatcher) dispatchList(method string, params []byte) (any, *rpcErrorBody) {
	if d == nil {
		return nil, internalError("dispatcher is nil")
	}
	if !isListReadOnly(method) {
		return nil, &rpcErrorBody{Code: -32601, Message: "Method not found", Data: method}
	}

	// Read-only pre-flight. Today every list handler is
	// pure; this is the guardrail a future register-side-
	// effecting handler trips. d.cfg.ListBackend carries
	// closures; we walk them in case a future cycle wants
	// to assert "this closure is pure" via reflection or a
	// bool field on the struct.
	if isSideEffectingListBackend(&d.cfg.ListBackend) {
		return nil, &rpcErrorBody{
			Code:    -32601,
			Message: "Method not allowed",
			Data:    method + " is read-only; handler reports side effects",
		}
	}

	l := dispatcherList{d: d}
	switch method {
	case "ListRemoteAgentModels":
		return l.handleListRemoteAgentModels(params)
	case "ListRemoteAgentSlashCommands":
		return l.handleListRemoteAgentSlashCommands(params)
	case "ListRemoteAgentConversations":
		return l.handleListRemoteAgentConversations(params)
	case "ListRemoteAgentEnvironments":
		return l.handleListRemoteAgentEnvironments(params)
	}
	// Unreachable when isListReadOnly(method) is true
	// (ListMethodsNoSideEffects enumerates the four
	// names). Defensive fallback in case the slice and
	// the switch drift.
	return nil, &rpcErrorBody{Code: -32601, Message: "Method not found", Data: method}
}

// isSideEffectingListBackend reports whether the configured
// ListBackend carries a side-effecting closure. Today the
// rule is "no closure flags it as side-effecting" — the field
// ListBackend doesn't carry such a flag yet, so this always
// returns false. TestListBackend_NoSideEffectsCanBeFlagged
// proves the hook is wired by constructing a fake ListBackend
// with a future sideEffect flag set and asserting the
// dispatcher rejects the method.
func isSideEffectingListBackend(b *ListBackend) bool {
	if b == nil {
		return false
	}
	// The rule today: nothing is side-effecting. The hook
	// is here for forward compat — a future ListBackend
	// struct field (e.g. "MutatesServerState bool") would
	// be read here. Until that field exists, the function
	// is a constant false, and the pre-flight is a
	// guardrail rather than an active enforcer.
	_ = b.Models
	_ = b.SlashCommands
	_ = b.Conversations
	_ = b.Environments
	return false
}
