package lsproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestMarshalAnswerUsesTheDeclarationOrder checks the thing the
// reference's own answer shows: protojson writes a message's fields in
// the order the descriptor declares them, not alphabetically.
//
// 【一手】 the reference's captured GetMendelFlags answer carries its
// experiment entries as keyString, disabled, string, json, csv,
// source — the declaration order of
// exa.codeium_common_pb.ExperimentWithVariant.
func TestMarshalAnswerUsesTheDeclarationOrder(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result := map[string]any{
		// Alphabetically, "experimentConfig" comes before
		// "webrtcMeshReady" as well — so the outer order is not the
		// tell. The inner one is.
		"webrtcMeshReady": true,
		"experimentConfig": map[string]any{
			"experiments": []any{
				map[string]any{
					"source":    "EXPERIMENT_SOURCE_API_SERVER",
					"csv":       "a,b",
					"json":      "{}",
					"string":    "60",
					"disabled":  true,
					"keyString": "remote-control-version",
				},
			},
		},
	}
	out, err := c.MarshalAnswer("GetMendelFlags", result)
	if err != nil {
		t.Fatalf("MarshalAnswer: %v", err)
	}
	got := string(out)
	want := `{"experimentConfig":{"experiments":[{"keyString":"remote-control-version",` +
		`"disabled":true,"string":"60","json":"{}","csv":"a,b",` +
		`"source":"EXPERIMENT_SOURCE_API_SERVER"}]},"webrtcMeshReady":true}`
	if got != want {
		t.Fatalf("answer order:\n got %s\nwant %s", got, want)
	}
}

// TestMarshalAnswerKeepsTheUnknownKeysLast checks the second half of
// the rule: a key the schema does not know follows the keys it does,
// in alphabetical order.
func TestMarshalAnswerKeepsTheUnknownKeysLast(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result := map[string]any{
		"zebra":           1,
		"webrtcMeshReady": true,
		"alpha":           2,
	}
	out, err := c.MarshalAnswer("GetMendelFlags", result)
	if err != nil {
		t.Fatalf("MarshalAnswer: %v", err)
	}
	got := string(out)
	want := `{"webrtcMeshReady":true,"alpha":2,"zebra":1}`
	if got != want {
		t.Fatalf("answer order:\n got %s\nwant %s", got, want)
	}
}

// TestMarshalAnswerLeavesAnUnknownMethodAlone checks that a method the
// schema does not know keeps the ordinary encoding: the schema is not
// the authority on what the clone serves.
func TestMarshalAnswerLeavesAnUnknownMethodAlone(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result := map[string]any{"b": 2, "a": 1}
	out, err := c.MarshalAnswer("CloneOwnMethod", result)
	if err != nil {
		t.Fatalf("MarshalAnswer: %v", err)
	}
	if got := string(out); got != `{"a":1,"b":2}` {
		t.Fatalf("answer: %s", got)
	}
}

// TestMarshalAnswerKeepsTheValues checks that the reordering touches
// the order of the keys and nothing else: the numbers keep their own
// text, and the strings keep their own escaping.
func TestMarshalAnswerKeepsTheValues(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result := map[string]any{
		"experimentConfig": map[string]any{
			"experiments": []any{
				map[string]any{"keyString": "a\"b", "string": "60"},
				map[string]any{"keyString": "c"},
			},
		},
	}
	out, err := c.MarshalAnswer("GetMendelFlags", result)
	if err != nil {
		t.Fatalf("MarshalAnswer: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("the answer does not parse: %v", err)
	}
	experiments, ok := decoded["experimentConfig"].(map[string]any)["experiments"].([]any)
	if !ok || len(experiments) != 2 {
		t.Fatalf("the experiments did not survive: %s", out)
	}
	first := experiments[0].(map[string]any)
	if first["keyString"] != `a"b` {
		t.Fatalf("the string did not survive: %s", out)
	}
	if first["string"] != "60" {
		t.Fatalf("the value did not survive: %s", out)
	}
}

// TestMarshalAnswerKeepsAListOfScalars checks the repeated field of a
// scalar type: the elements keep their order, and the field keeps its
// place.
func TestMarshalAnswerKeepsAListOfScalars(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result := map[string]any{
		"experimentConfig": map[string]any{
			"forceEnableExperimentStrings": []any{"b", "a"},
			"experiments":                  []any{},
		},
	}
	out, err := c.MarshalAnswer("GetMendelFlags", result)
	if err != nil {
		t.Fatalf("MarshalAnswer: %v", err)
	}
	got := string(out)
	want := `{"experimentConfig":{"experiments":[],"forceEnableExperimentStrings":["b","a"]}}`
	if got != want {
		t.Fatalf("answer:\n got %s\nwant %s", got, want)
	}
}

// TestMarshalAnswerAnswersAMapField checks that a map field's own keys
// stay the map's own: the schema does not order a map's keys.
func TestMarshalAnswerAnswersAMapField(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result := map[string]any{
		"experimentConfig": map[string]any{
			"experiments": []any{},
			"devMode":     false,
		},
	}
	out, err := c.MarshalAnswer("GetMendelFlags", result)
	if err != nil {
		t.Fatalf("MarshalAnswer: %v", err)
	}
	if got := string(out); !strings.Contains(got, `"devMode":false`) {
		t.Fatalf("the value did not survive: %s", got)
	}
}
