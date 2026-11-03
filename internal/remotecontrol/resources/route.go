// Package resources — route.go: the domain's own routing table.
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
//
// This table carries a context, because the bind method writes
// through the store and fans a notification out, and both want
// the caller's context. The permissions domain's language-server
// table carries one for the same reason.
package resources

import (
	"context"
	"encoding/json"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// The JSON-RPC method names the resources domain owns.
const (
	// MethodListProviders lists the registered providers.
	MethodListProviders = "ListResourceProviders"
	// MethodListResources lists the addressable resources.
	MethodListResources = "ListAvailableResources"
	// MethodUpdateProjectResources binds resources to a project.
	MethodUpdateProjectResources = "UpdateProjectResources"

	// Legacy aliases kept for backward compatibility with early dashboard callers.
	LegacyMethodListProviders          = "ListRemoteAgentResourceProviders"
	LegacyMethodListResources          = "ListRemoteAgentAvailableResources"
	LegacyMethodUpdateProjectResources = "UpdateRemoteAgentProjectResources"
)

// Route dispatches the resources domain's JSON-RPC method table.
//
// It answers (nil, nil, false) for every method the domain does
// not own, and (result, err, true) for the ones it does.
func Route(s Seams, ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case MethodListProviders, LegacyMethodListProviders:
		r, e := HandleListProviders(s, params)
		return r, e, true
	case MethodListResources, LegacyMethodListResources:
		r, e := HandleListResources(s, params)
		return r, e, true
	case MethodUpdateProjectResources, LegacyMethodUpdateProjectResources:
		r, e := HandleUpdateProjectResources(s, ctx, params)
		return r, e, true
	}
	return nil, nil, false
}
