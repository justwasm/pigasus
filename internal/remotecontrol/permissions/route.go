// Package permissions — route.go: the domain's own routing tables.
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
// The permissions domain has two tables, because it serves two
// protocols: the four dashboard methods below are JSON-RPC, and
// the two language-server methods in ls.go are proto-JSON with
// their own method names.
package permissions

import (
	"context"
	"encoding/json"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// The JSON-RPC method names the permissions domain owns.
const (
	// MethodSetPreset sets the permission preset.
	MethodSetPreset = "SetPermissionPreset"
	// MethodSetReviewMode sets the artifact-review mode.
	MethodSetReviewMode = "SetArtifactReviewMode"
	// MethodAddGrants adds auto-allow rules.
	MethodAddGrants = "AddPermissionGrants"
	// MethodRemoveGrants removes auto-allow rules.
	MethodRemoveGrants = "RemovePermissionGrants"

	// Legacy aliases kept for backward compatibility with early dashboard callers.
	LegacyMethodSetPreset     = "SetRemoteAgentPermissionPreset"
	LegacyMethodSetReviewMode = "SetRemoteAgentArtifactReviewMode"
	LegacyMethodAddGrants     = "AddRemoteAgentPermissionGrants"
	LegacyMethodRemoveGrants  = "RemoveRemoteAgentPermissionGrants"
)

// Route dispatches the permissions domain's JSON-RPC method
// table.
//
// It answers (nil, nil, false) for every method the domain does
// not own, and (result, err, true) for the ones it does.
func Route(s Seams, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case MethodSetPreset, LegacyMethodSetPreset:
		r, e := HandleSetPreset(s, params)
		return r, e, true
	case MethodSetReviewMode, LegacyMethodSetReviewMode:
		r, e := HandleSetReviewMode(s, params)
		return r, e, true
	case MethodAddGrants, LegacyMethodAddGrants:
		r, e := HandleAddGrants(s, params)
		return r, e, true
	case MethodRemoveGrants, LegacyMethodRemoveGrants:
		r, e := HandleRemoveGrants(s, params)
		return r, e, true
	}
	return nil, nil, false
}

// The language-server method names the permissions domain owns.
//
// The language server spells a method in both its proto name and
// its JSON name ("GetPermissionSettings" /
// "getPermissionSettings"); the host's language-server table
// accepts either, so the domain's table does too.
const (
	// LSMethodGetSettings reads the permission settings.
	LSMethodGetSettings = "GetPermissionSettings"
	// LSMethodUpdateSettings writes the permission settings.
	LSMethodUpdateSettings = "UpdatePermissionSettings"
)

// LSRoute dispatches the permissions domain's language-server
// method table.
//
// It answers (nil, nil, false) for every method the domain does
// not own, and (result, err, handled) for the ones it does. The
// handled value is true even on failure: the language-server
// table distinguishes "not mine" from "mine, and it failed".
func LSRoute(s Seams, ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case LSMethodGetSettings, "getPermissionSettings":
		r, e, ok := LSGetSettings(s, ctx)
		return r, e, ok
	case LSMethodUpdateSettings, "updatePermissionSettings":
		r, e, ok := LSUpdateSettings(s, ctx, params)
		return r, e, ok
	}
	return nil, nil, false
}
