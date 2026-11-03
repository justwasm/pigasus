// Package plugins — route.go: the domain's own routing table.
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
// The plugins domain serves two protocols, and this table is only
// the JSON-RPC half of it. The language-server half — the
// marketplace, the customization and the plugin-auth methods —
// lives in the host's language-server surface, because those
// handlers answer proto-JSON with the reference's own member
// names and share the host's plugin catalogue.
package plugins

import (
	"encoding/json"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// The JSON-RPC method names the plugins domain owns.
const (
	// MethodList lists the connected plugins.
	MethodList = "ListPlugins"
	// MethodDisconnect disconnects a plugin.
	MethodDisconnect = "DisconnectPlugin"

	// Legacy aliases kept for backward compatibility with early dashboard callers.
	LegacyMethodList       = "ListRemoteAgentPlugins"
	LegacyMethodDisconnect = "DisconnectRemoteAgentPlugin"
)

// Route dispatches the plugins domain's JSON-RPC method table.
//
// It answers (nil, nil, false) for every method the domain does
// not own, and (result, err, true) for the ones it does.
func Route(s Seams, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case MethodList, LegacyMethodList:
		r, e := HandleList(s, params)
		return r, e, true
	case MethodDisconnect, LegacyMethodDisconnect:
		r, e := HandleDisconnect(s, params)
		return r, e, true
	}
	return nil, nil, false
}
