package browserbridge

import (
	"encoding/json"
)

// coerceArgs is the bridge's own argument coercion.
//
// The project's runtime coerces the arguments before the call
// (`coerceArgs` in the project's `host/tool-runtime.js`): a `tabId`
// that arrived as a string becomes a number, and the three
// array-shaped arguments — `coordinate`, `start_coordinate`,
// `region` — become arrays when they arrive as a JSON string.
//
// The reason is the wire, not the model: the values come straight off
// a JSON-RPC call, where a number written as a string is still a
// string. The extension compares them against real numbers and real
// arrays, so without the coercion a perfectly reasonable call
// ("tabId": "590534703") is answered as an unknown tab.
//
// A value that does not parse is left as it arrived: the extension
// then reports the real problem, which beats a coercion that guesses.
func coerceArgs(args json.RawMessage) json.RawMessage {
	if len(args) == 0 {
		return args
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return args
	}

	changed := false
	if raw, ok := fields["tabId"]; ok {
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			// json.Number keeps the digits as they arrived: a tab id
			// is an integer, and a float would print it differently.
			var num json.Number
			if err := json.Unmarshal([]byte(text), &num); err == nil {
				fields["tabId"] = json.RawMessage(num.String())
				changed = true
			}
		}
	}
	for _, name := range []string{"coordinate", "start_coordinate", "region"} {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			continue // not a string: nothing to coerce
		}
		var parsed []any
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			continue // not a JSON array: leave it as it arrived
		}
		b, err := json.Marshal(parsed)
		if err != nil {
			continue
		}
		fields[name] = b
		changed = true
	}
	if !changed {
		return args
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return args
	}
	return out
}
