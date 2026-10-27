package browserbridge

import (
	"encoding/json"
	"testing"
)

// TestCoerceArgsTurnsAStringTabIdIntoANumber pins the wire coercion.
//
// The values arrive straight off a JSON-RPC call, where a number
// written as a string is still a string. The extension compares them
// against real numbers, so a call with "tabId": "590534703" is
// answered as an unknown tab unless the bridge coerces it first.
func TestCoerceArgsTurnsAStringTabIdIntoANumber(t *testing.T) {
	got := coerceArgs(json.RawMessage(`{"tabId":"590534703"}`))
	var fields map[string]any
	if err := json.Unmarshal(got, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tabID, ok := fields["tabId"].(float64)
	if !ok {
		t.Fatalf("tabId = %v (%T), want a number", fields["tabId"], fields["tabId"])
	}
	if tabID != 590534703 {
		t.Fatalf("tabId = %v, want 590534703", tabID)
	}
}

// TestCoerceArgsParsesTheArrayShapedArguments pins the second half of
// the coercion: the coordinate and the region arrive as JSON strings.
func TestCoerceArgsParsesTheArrayShapedArguments(t *testing.T) {
	got := coerceArgs(json.RawMessage(`{"coordinate":"[10, 20]","start_coordinate":"[1, 2]","region":"[0, 0, 100, 50]"}`))
	var fields map[string]any
	if err := json.Unmarshal(got, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, name := range []string{"coordinate", "start_coordinate", "region"} {
		arr, ok := fields[name].([]any)
		if !ok {
			t.Fatalf("%s = %v (%T), want an array", name, fields[name], fields[name])
		}
		if len(arr) == 0 {
			t.Fatalf("%s is an empty array", name)
		}
	}
}

// TestCoerceArgsLeavesWhatItCannotParse pins the conservative half:
// a value that does not parse is left as it arrived, so the extension
// reports the real problem instead of the bridge's guess.
func TestCoerceArgsLeavesWhatItCannotParse(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"a tab id that is not a number", `{"tabId":"not-a-number"}`},
		{"a coordinate that is not JSON", `{"coordinate":"down and to the left"}`},
		{"a coordinate that is JSON but not an array", `{"coordinate":"12"}`},
		{"a region that is not JSON", `{"region":"the whole page"}`},
		{"a value that is already a number", `{"tabId":7}`},
		{"a coordinate that is already an array", `{"coordinate":[10,20]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(coerceArgs(json.RawMessage(tc.in)))
			var want, have any
			if err := json.Unmarshal([]byte(tc.in), &want); err != nil {
				t.Fatalf("unmarshal the input: %v", err)
			}
			if err := json.Unmarshal([]byte(got), &have); err != nil {
				t.Fatalf("unmarshal the output: %v", err)
			}
			if !sameJSON(want, have) {
				t.Fatalf("coerceArgs(%s) = %s, want it unchanged", tc.in, got)
			}
		})
	}
}

// TestCoerceArgsLeavesOtherArgumentsAlone pins that the coercion is
// narrow: only the four arguments the wire needs are touched.
func TestCoerceArgsLeavesOtherArgumentsAlone(t *testing.T) {
	got := string(coerceArgs(json.RawMessage(`{"text":"tabId is a string here","url":"https://example.com"}`)))
	if got != `{"text":"tabId is a string here","url":"https://example.com"}` {
		t.Fatalf("coerceArgs changed an unrelated call: %s", got)
	}
}

// TestCoerceArgsSurvivesRubbish pins that a call whose arguments are
// not an object at all is passed through untouched — the bridge must
// not turn a malformed call into a different one.
func TestCoerceArgsSurvivesRubbish(t *testing.T) {
	for _, in := range []string{`[]`, `"a string"`, `42`, `{`, ``} {
		if got := string(coerceArgs(json.RawMessage(in))); got != in {
			t.Fatalf("coerceArgs(%q) = %q, want it unchanged", in, got)
		}
	}
}

// sameJSON compares two decoded JSON values by their rendered form.
func sameJSON(a, b any) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return string(ra) == string(rb)
}
