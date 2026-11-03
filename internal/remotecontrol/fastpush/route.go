// Package fastpush — route.go: the domain's own routing table.
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
package fastpush

import (
	"encoding/json"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// MethodSetEnabled is the JSON-RPC method name the fastpush
// domain owns.
const MethodSetEnabled = "SetRemoteAgentFastpushEnabled"

// Route dispatches the fastpush domain's method table.
//
// It answers (nil, nil, false) for every method the domain does
// not own, and (result, err, true) for the one it does.
func Route(s Seams, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	if method != MethodSetEnabled {
		return nil, nil, false
	}
	r, e := HandleSetEnabled(s, params)
	return r, e, true
}
