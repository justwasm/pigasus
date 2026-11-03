// Package triggers — route.go: the domain's own routing table.
//
// Every domain package exposes a Route function with the same
// shape: the host's dispatcher calls it with the method name and
// the raw params, and it answers (result, error, handled). The
// third value is false when the method is not the domain's —
// the dispatcher then tries the next domain.
//
// The shape is deliberate: the dispatcher keeps the fan-out, the
// domain keeps the table. A domain that owned the fan-out would
// own every other domain's method names too.
package triggers

import (
	"encoding/json"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// The JSON-RPC method names the triggers domain owns.
const (
	// MethodList lists the triggers.
	MethodList = "ListTriggers"
	// MethodListOptions lists the trigger options (the kinds and
	// their spec schemas).
	MethodListOptions = "ListTriggerOptions"
	// MethodCreate creates a trigger.
	MethodCreate = "CreateTrigger"
	// MethodUpdate updates a trigger.
	MethodUpdate = "UpdateTrigger"
	// MethodDelete deletes a trigger.
	MethodDelete = "DeleteTrigger"
	// MethodRun runs a trigger.
	MethodRun = "RunTrigger"

	// Legacy aliases kept for backward compatibility with early dashboard callers.
	LegacyMethodList        = "ListRemoteAgentTriggers"
	LegacyMethodListOptions = "ListRemoteAgentTriggerOptions"
	LegacyMethodCreate      = "CreateRemoteAgentTrigger"
	LegacyMethodUpdate      = "UpdateRemoteAgentTrigger"
	LegacyMethodDelete      = "DeleteRemoteAgentTrigger"
	LegacyMethodRun         = "RunRemoteAgentTrigger"
)

// Route dispatches the triggers domain's JSON-RPC method table.
//
// It answers (nil, nil, false) for every method the domain does
// not own, and (result, err, true) for the ones it does.
func Route(s Seams, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case MethodList, LegacyMethodList, "listTriggers":
		r, e := HandleList(s, params)
		return r, e, true
	case MethodListOptions, LegacyMethodListOptions, "listTriggerOptions":
		r, e := HandleListOptions(s, params)
		return r, e, true
	case MethodCreate, LegacyMethodCreate, "createTrigger":
		r, e := HandleCreate(s, params)
		return r, e, true
	case MethodUpdate, LegacyMethodUpdate, "updateTrigger":
		r, e := HandleUpdate(s, params)
		return r, e, true
	case MethodDelete, LegacyMethodDelete, "deleteTrigger":
		r, e := HandleDelete(s, params)
		return r, e, true
	case MethodRun, LegacyMethodRun, "runTrigger":
		r, e := HandleRun(s, params)
		return r, e, true
	}
	return nil, nil, false
}
