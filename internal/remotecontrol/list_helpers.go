// Package remotecontrol — list_helpers.go: small stdlib wrappers the
// list dispatcher uses. They exist in a single file (rather than
// each file declaring its own) so the dispatcher's per-handler
// bodies stay focused on protocol shape, not stdlib plumbing.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"encoding/json"
	"strings"
	"time"
)

// jsonUnmarshal is a thin wrapper over encoding/json.Unmarshal. The
// wrapper exists so the per-handler error sites don't pull
// encoding/json into the imports — the same pattern
// dispatch.go's handler bodies use.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// stringsTrimSpace is a thin wrapper over strings.TrimSpace. The
// list dispatcher is hot-path code; the wrapper keeps the import
// list narrow and matches dispatch.go's bytesTrimSpace style.
func stringsTrimSpace(s string) string {
	return strings.TrimSpace(s)
}

// timeParse is a thin wrapper over time.Parse. The Conversations
// handler tries RFC3339Nano first and falls back to RFC3339; both
// go through this so the parse-error mapping is in one place.
func timeParse(layout, value string) (time.Time, error) {
	return time.Parse(layout, value)
}
