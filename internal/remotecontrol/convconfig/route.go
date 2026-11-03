// Package convconfig — route.go: the domain's own routing table.
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
package convconfig

import (
	"encoding/json"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// MethodUpsert is the JSON-RPC method name the
// conversation-config domain owns.
const MethodUpsert = "UpsertRemoteAgentConversationConfig"

// Route dispatches the conversation-config domain's method table.
//
// It answers (nil, nil, false) for every method the domain does
// not own, and (result, err, true) for the one it does.
func Route(s Seams, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	if method != MethodUpsert {
		return nil, nil, false
	}
	r, e := HandleUpsert(s, params)
	return r, e, true
}
