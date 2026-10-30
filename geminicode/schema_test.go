package geminicode

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSchema_Boolean(t *testing.T) {
	out := Schema(json.RawMessage(`true`))
	if out["type"] != "OBJECT" {
		t.Errorf("Schema(true) type = %v, want OBJECT", out["type"])
	}

	propSchema := Schema(json.RawMessage(`{"type":"object","properties":{"flag":true}}`))
	props := propSchema["properties"].(map[string]any)
	flagProp := props["flag"].(map[string]any)
	if flagProp["type"] != "STRING" {
		t.Errorf("flag property type = %v, want STRING", flagProp["type"])
	}
}

func TestSchema_ConstToEnum(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {
				"const": "run"
			}
		}
	}`)
	out := Schema(raw)
	props := out["properties"].(map[string]any)
	action := props["action"].(map[string]any)
	if action["type"] != "STRING" {
		t.Errorf("action type = %v, want STRING", action["type"])
	}
	enum, ok := action["enum"].([]any)
	if !ok || len(enum) != 1 || enum[0] != "run" {
		t.Errorf("action enum = %v, want [run]", action["enum"])
	}
}

func TestSchema_ArrayRequiresItems(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "object",
		"properties": {
			"tags": {
				"type": "array"
			}
		}
	}`)
	out := Schema(raw)
	props := out["properties"].(map[string]any)
	tags := props["tags"].(map[string]any)
	if tags["type"] != "ARRAY" {
		t.Errorf("tags type = %v, want ARRAY", tags["type"])
	}
	items, ok := tags["items"].(map[string]any)
	if !ok || items["type"] != "STRING" {
		t.Errorf("tags items = %v, want {type: STRING}", tags["items"])
	}
}

func TestSchema_PruneMismatchedKeywords(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "object",
		"properties": {
			"str": {
				"type": "string",
				"items": {"type": "string"},
				"minItems": 1
			},
			"num": {
				"type": "number",
				"properties": {"foo": {"type": "string"}},
				"required": ["foo"]
			}
		}
	}`)
	out := Schema(raw)
	props := out["properties"].(map[string]any)
	str := props["str"].(map[string]any)
	if _, hasItems := str["items"]; hasItems {
		t.Errorf("str should not have items")
	}
	if _, hasMinItems := str["minItems"]; hasMinItems {
		t.Errorf("str should not have minItems")
	}

	num := props["num"].(map[string]any)
	if _, hasProps := num["properties"]; hasProps {
		t.Errorf("num should not have properties")
	}
	if _, hasReq := num["required"]; hasReq {
		t.Errorf("num should not have required")
	}
}

func TestSchema_RequiredPropertiesSanitized(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		},
		"required": ["name", "non_existent"]
	}`)
	out := Schema(raw)
	req, ok := out["required"].([]any)
	if !ok {
		t.Fatalf("required not a slice: %v", out["required"])
	}
	if !reflect.DeepEqual(req, []any{"name"}) {
		t.Errorf("required = %v, want [name]", req)
	}

	// When no declared property matches required, required should be removed
	rawNoMatch := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		},
		"required": ["other"]
	}`)
	outNoMatch := Schema(rawNoMatch)
	if _, hasReq := outNoMatch["required"]; hasReq {
		t.Errorf("outNoMatch should have deleted required, got %v", outNoMatch["required"])
	}
}

func TestSchema_AnyOfUnionFolding(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "object",
		"properties": {
			"filter": {
				"anyOf": [
					{"type": "string"},
					{"type": "null"}
				],
				"description": "an optional string filter"
			},
			"count": {
				"oneOf": [
					{"type": "integer"},
					{"type": "null"}
				]
			}
		}
	}`)
	out := Schema(raw)
	props := out["properties"].(map[string]any)

	filter := props["filter"].(map[string]any)
	if filter["type"] != "STRING" {
		t.Errorf("filter type = %v, want STRING", filter["type"])
	}
	if filter["nullable"] != true {
		t.Errorf("filter nullable = %v, want true", filter["nullable"])
	}
	if filter["description"] != "an optional string filter" {
		t.Errorf("filter description = %v", filter["description"])
	}

	count := props["count"].(map[string]any)
	if count["type"] != "INTEGER" {
		t.Errorf("count type = %v, want INTEGER", count["type"])
	}
	if count["nullable"] != true {
		t.Errorf("count nullable = %v, want true", count["nullable"])
	}
}

func TestSchema_TypeSliceWithNull(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "object",
		"properties": {
			"active": {
				"type": ["boolean", "null"]
			}
		}
	}`)
	out := Schema(raw)
	props := out["properties"].(map[string]any)
	active := props["active"].(map[string]any)
	if active["type"] != "BOOLEAN" {
		t.Errorf("active type = %v, want BOOLEAN", active["type"])
	}
	if active["nullable"] != true {
		t.Errorf("active nullable = %v, want true", active["nullable"])
	}
}

func TestNormalizeToolSchema(t *testing.T) {
	input := map[string]any{
		"type": "OBJECT",
		"properties": map[string]any{
			"flag": true,
			"emptyProp": map[string]any{},
			"tags": map[string]any{
				"type":  "ARRAY",
				"items": true,
			},
			"nested": map[string]any{
				"properties": map[string]any{
					"sub": map[string]any{},
				},
			},
		},
	}

	norm, ok := NormalizeToolSchema(input, true).(map[string]any)
	if !ok {
		t.Fatalf("NormalizeToolSchema returned non-map: %T", norm)
	}
	if norm["type"] != "object" {
		t.Errorf("expected root type 'object', got %v", norm["type"])
	}
	props, ok := norm["properties"].(map[string]any)
	if !ok {
		t.Fatalf("missing properties map")
	}

	// flag boolean schema -> string
	flag := props["flag"].(map[string]any)
	if flag["type"] != "string" {
		t.Errorf("flag expected type 'string', got %v", flag["type"])
	}

	// emptyProp -> string
	emptyProp := props["emptyProp"].(map[string]any)
	if emptyProp["type"] != "string" {
		t.Errorf("emptyProp expected type 'string', got %v", emptyProp["type"])
	}

	// tags -> array with items: string
	tags := props["tags"].(map[string]any)
	if tags["type"] != "array" {
		t.Errorf("tags expected type 'array', got %v", tags["type"])
	}
	items := tags["items"].(map[string]any)
	if items["type"] != "string" {
		t.Errorf("tags.items expected type 'string', got %v", items["type"])
	}

	// nested without type has properties -> type inferred as 'object'
	nested := props["nested"].(map[string]any)
	if nested["type"] != "object" {
		t.Errorf("nested expected inferred type 'object', got %v", nested["type"])
	}
	subProps := nested["properties"].(map[string]any)
	sub := subProps["sub"].(map[string]any)
	if sub["type"] != "string" {
		t.Errorf("sub expected type 'string', got %v", sub["type"])
	}
}

func TestNormalizeToolSchema_Advanced(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "OBJECT",
		"properties": {
			"action": {
				"const": "execute"
			},
			"count": {
				"enum": [1, 2, 3]
			},
			"param": {
				"anyOf": [
					{"type": "string"},
					{"type": "null"}
				]
			},
			"name": {
				"type": "string"
			}
		},
		"required": ["name", "ghost_param"]
	}`)

	norm, ok := NormalizeToolSchema(raw, true).(map[string]any)
	if !ok {
		t.Fatalf("NormalizeToolSchema returned non-map: %T", norm)
	}

	props := norm["properties"].(map[string]any)

	// const -> enum
	action := props["action"].(map[string]any)
	if action["type"] != "string" {
		t.Errorf("action type = %v, want string", action["type"])
	}
	enumVals, ok := action["enum"].([]any)
	if !ok || len(enumVals) != 1 || enumVals[0] != "execute" {
		t.Errorf("action enum = %v, want ['execute']", action["enum"])
	}

	// count with number enum -> number type
	count := props["count"].(map[string]any)
	if count["type"] != "number" {
		t.Errorf("count type = %v, want number", count["type"])
	}

	// anyOf string + null -> string + nullable
	param := props["param"].(map[string]any)
	if param["type"] != "string" {
		t.Errorf("param type = %v, want string", param["type"])
	}
	if param["nullable"] != true {
		t.Errorf("param nullable = %v, want true", param["nullable"])
	}

	// required must prune ghost_param
	req, ok := norm["required"].([]any)
	if !ok || len(req) != 1 || req[0] != "name" {
		t.Errorf("required = %v, want ['name']", norm["required"])
	}
}

func TestNormalizeToolSchema_StringSliceTypes(t *testing.T) {
	input := map[string]any{
		"type": []string{"object"},
		"properties": map[string]any{
			"opt": map[string]any{
				"type": []string{"integer", "null"},
			},
			"choice": map[string]any{
				"enum": []string{"alpha", "beta"},
			},
		},
	}

	norm, ok := NormalizeToolSchema(input, true).(map[string]any)
	if !ok {
		t.Fatalf("NormalizeToolSchema returned non-map: %T", norm)
	}
	if norm["type"] != "object" {
		t.Errorf("expected root type 'object', got %v", norm["type"])
	}

	props := norm["properties"].(map[string]any)
	opt := props["opt"].(map[string]any)
	if opt["type"] != "integer" {
		t.Errorf("opt type = %v, want integer", opt["type"])
	}
	if opt["nullable"] != true {
		t.Errorf("opt nullable = %v, want true", opt["nullable"])
	}

	choice := props["choice"].(map[string]any)
	if choice["type"] != "string" {
		t.Errorf("choice type = %v, want string", choice["type"])
	}
}

