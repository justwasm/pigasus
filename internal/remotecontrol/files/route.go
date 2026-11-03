// Package files — route.go: the domain's own routing table.
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
package files

import (
	"encoding/json"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// The JSON-RPC method names the files domain owns.
const (
	// MethodReadFile reads a file's content (with a size cap).
	MethodReadFile = "ReadFile"
	// MethodReadDir lists the entries of a directory.
	MethodReadDir = "ReadDir"

	// LegacyMethodReadFile is the clone's historical guessed method name.
	LegacyMethodReadFile = "ReadRemoteAgentFile"
	// LegacyMethodReadDir is the clone's historical guessed method name.
	LegacyMethodReadDir = "ReadRemoteAgentDir"
)

// Route dispatches the files domain's JSON-RPC method table.
//
// It answers (nil, nil, false) for every method the domain does
// not own, and (result, err, true) for the ones it does.
func Route(s Seams, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case MethodReadFile, LegacyMethodReadFile:
		r, e := HandleReadFile(s, params)
		return r, e, true
	case MethodReadDir, LegacyMethodReadDir:
		r, e := HandleReadDir(s, params)
		return r, e, true
	}
	return nil, nil, false
}
