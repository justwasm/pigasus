package backend

import (
	"encoding/json"
	"strconv"
	"strings"
)

// geminiSchemaKeys is the subset of JSON Schema the CloudCode endpoint's
// Schema proto accepts. Everything else (notably additionalProperties and
// $schema) is dropped rather than risking a 400.
var geminiSchemaKeys = map[string]bool{
	"type":        true,
	"description": true,
	"properties":  true,
	"required":    true,
	"items":       true,
	"enum":        true,
	"nullable":    true,
	"format":      true,
	"minimum":     true,
	"maximum":     true,
	"minItems":    true,
	"maxItems":    true,
	"minLength":   true,
	"maxLength":   true,
	"pattern":     true,
}

// geminiToolSchema converts one provider-neutral JSON Schema (lowercase types,
// possibly additionalProperties) into the Gemini Schema shape the reference
// sends. The reference's captured declarations use `"type":"OBJECT"` and a
// plain properties/required tree.
func geminiToolSchema(raw json.RawMessage) map[string]any {
	var schema any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil
	}
	out, _ := convertGeminiSchema(schema).(map[string]any)
	if out == nil {
		return nil
	}
	if _, ok := out["type"]; !ok {
		if _, hasProps := out["properties"]; hasProps {
			out["type"] = "OBJECT"
		}
	}
	return out
}

func convertGeminiSchema(v any) any {
	switch node := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(node))
		for k, value := range node {
			if !geminiSchemaKeys[k] {
				continue
			}
			switch k {
			case "type":
				if s, ok := value.(string); ok {
					out[k] = strings.ToUpper(s)
				} else {
					out[k] = value
				}
			case "properties":
				props, ok := value.(map[string]any)
				if !ok {
					continue
				}
				converted := make(map[string]any, len(props))
				for name, prop := range props {
					converted[name] = convertGeminiSchema(prop)
				}
				out[k] = converted
			case "items":
				out[k] = convertGeminiSchema(value)
			case "minLength", "maxLength", "minItems", "maxItems":
				// The Schema proto's count fields are int64, which protojson
				// encodes as a JSON *string* ("minLength":"1"). The tools
				// package lowercases them to numbers so PiG's metaschema
				// validator accepts the schema; put the wire's original
				// spelling back here so the declaration stays byte-identical
				// to the reference's.
				out[k] = protoIntString(value)
			default:
				out[k] = value
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(node))
		for _, item := range node {
			out = append(out, convertGeminiSchema(item))
		}
		return out
	default:
		return v
	}
}

// protoIntString renders an int64 schema keyword the way protojson does: as a
// JSON string. The reference's captured declarations spell minLength/minItems
// as strings ("minItems":"2"), and the model-facing wire must match.
func protoIntString(v any) any {
	switch n := v.(type) {
	case float64:
		return strconv.FormatInt(int64(n), 10)
	case int:
		return strconv.Itoa(n)
	case int64:
		return strconv.FormatInt(n, 10)
	case json.Number:
		return n.String()
	default:
		return v
	}
}
