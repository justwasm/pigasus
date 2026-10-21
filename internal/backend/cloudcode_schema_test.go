package backend

import (
	"encoding/json"
	"testing"
)

// TestGeminiToolSchemaProtoIntStrings pins the round trip the tools package
// performs for PiG: minLength/minItems are numbers in PiG's copy (draft
// 2020-12 requires integers) and protojson strings on the wire — the
// reference sends `"minLength":"1"`.
func TestGeminiToolSchemaProtoIntStrings(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"Message":{"type":"string","minLength":1}},"required":["Message"],"minItems":2}`)
	got := geminiToolSchema(raw)
	if got["type"] != "OBJECT" {
		t.Errorf("type = %v, want OBJECT", got["type"])
	}
	if got["minItems"] != "2" {
		t.Errorf("minItems = %#v, want the protojson string %q", got["minItems"], "2")
	}
	props, _ := got["properties"].(map[string]any)
	msg, _ := props["Message"].(map[string]any)
	if msg["minLength"] != "1" {
		t.Errorf("minLength = %#v, want the protojson string %q", msg["minLength"], "1")
	}
}
