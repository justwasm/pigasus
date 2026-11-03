// schema.go — the JSON-schema literal helper every tool declaration uses.
//
// Cycle 379: this helper used to live in the catalogue
// (internal/app/tools/surface/catalog.go), so every capability that
// declared a schema had to import the catalogue to reach it — and the
// catalogue imports the capabilities. A helper that four packages need
// is not the catalogue's; it belongs to the contract.

package tools

import (
	"encoding/json"
	"fmt"
)

// Obj is a tiny helper for building JSON-schema object literals.
func Obj(props string, required ...string) json.RawMessage {
	req := ""
	for i, r := range required {
		if i > 0 {
			req += ","
		}
		req += fmt.Sprintf("%q", r)
	}
	return json.RawMessage(fmt.Sprintf(
		`{"type":"object","properties":{%s},"required":[%s]}`, props, req))
}
