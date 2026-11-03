// Package projects — route.go: the domain's own routing table.
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
package projects

import (
	"encoding/json"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// The JSON-RPC method names the projects domain owns.
const (
	// MethodList lists the projects.
	MethodList = "ListProjects"
	// MethodCreate creates a project.
	MethodCreate = "CreateProject"
	// MethodUpdate updates a project.
	MethodUpdate = "UpdateProject"
	// MethodDelete deletes a project.
	MethodDelete = "DeleteProject"

	// Legacy aliases kept for backward compatibility with early dashboard callers.
	LegacyMethodList   = "ListRemoteAgentProjects"
	LegacyMethodCreate = "CreateRemoteAgentProject"
	LegacyMethodUpdate = "UpdateRemoteAgentProject"
	LegacyMethodDelete = "DeleteRemoteAgentProject"
)

// Route dispatches the projects domain's JSON-RPC method table.
//
// It answers (nil, nil, false) for every method the domain does
// not own, and (result, err, true) for the ones it does.
func Route(s Seams, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case MethodList, LegacyMethodList:
		r, e := HandleList(s, params)
		return r, e, true
	case MethodCreate, LegacyMethodCreate:
		r, e := HandleCreate(s, params)
		return r, e, true
	case MethodUpdate, LegacyMethodUpdate:
		r, e := HandleUpdate(s, params)
		return r, e, true
	case MethodDelete, LegacyMethodDelete:
		r, e := HandleDelete(s, params)
		return r, e, true
	}
	return nil, nil, false
}
