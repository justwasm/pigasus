package geminicode

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

// Schema converts one provider-neutral JSON Schema (lowercase types,
// possibly additionalProperties) into the Gemini Schema shape the reference
// sends. The reference's captured declarations use `"type":"OBJECT"` and a
// plain properties/required tree.
//
// Untyped schemas and boolean schemas (e.g. items: true or empty property
// definitions), which Google Vertex AI and Gemini turn away with 400 errors,
// are normalized and given appropriate schema types (inspired by magpie).
//
// The count keywords come out as JSON *strings* — see protoIntString — so
// the declaration stays byte-identical to the reference's.
func Schema(raw json.RawMessage) map[string]any {
	var schema any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil
	}
	out, _ := convertGeminiSchema(schema, true).(map[string]any)
	if out == nil {
		return nil
	}
	if _, ok := out["type"]; !ok {
		out["type"] = "OBJECT"
	}
	return out
}

func convertGeminiSchema(v any, isRoot bool) any {
	switch node := v.(type) {
	case bool:
		if node {
			if isRoot {
				return map[string]any{"type": "OBJECT"}
			}
			return map[string]any{"type": "STRING"}
		}
		return nil
	case map[string]any:
		out := make(map[string]any, len(node))

		// Handle const -> enum (JSON Schema const is not supported by Gemini OpenAPI)
		if c, ok := node["const"]; ok {
			out["enum"] = []any{c}
		}

		// Handle anyOf / oneOf unions: fold them into the schema.
		// If any branch is {"type": "null"}, mark nullable: true.
		// Extract properties and type from the first non-null branch.
		for _, unionKey := range []string{"anyOf", "oneOf"} {
			if alts, ok := node[unionKey].([]any); ok && len(alts) > 0 {
				for _, alt := range alts {
					if m, ok := alt.(map[string]any); ok {
						if t, ok := m["type"].(string); ok && strings.ToLower(t) == "null" {
							out["nullable"] = true
						} else {
							// Merge fields from the first non-null branch if not already defined
							for k, val := range m {
								if k == "type" || k == "properties" || k == "items" || k == "enum" || k == "format" {
									if _, exists := node[k]; !exists {
										if _, inOut := out[k]; !inOut {
											if k == "type" {
												if s, ok := val.(string); ok {
													out["type"] = strings.ToUpper(s)
												}
											} else if cp := convertGeminiSchema(val, false); cp != nil {
												out[k] = cp
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}

		for k, value := range node {
			if !geminiSchemaKeys[k] {
				continue
			}
			switch k {
			case "type":
				switch t := value.(type) {
				case string:
					out[k] = strings.ToUpper(t)
				case []any:
					// e.g. ["string", "null"] in JSON Schema
					for _, item := range t {
						if s, ok := item.(string); ok {
							if strings.ToLower(s) == "null" {
								out["nullable"] = true
							} else if _, hasType := out[k]; !hasType {
								out[k] = strings.ToUpper(s)
							}
						}
					}
				case []string:
					for _, s := range t {
						if strings.ToLower(s) == "null" {
							out["nullable"] = true
						} else if _, hasType := out[k]; !hasType {
							out[k] = strings.ToUpper(s)
						}
					}
				default:
					out[k] = value
				}
			case "properties":
				props, ok := value.(map[string]any)
				if !ok {
					continue
				}
				converted := make(map[string]any, len(props))
				for name, prop := range props {
					if cp := convertGeminiSchema(prop, false); cp != nil {
						converted[name] = cp
					}
				}
				out[k] = converted
			case "items":
				if ci := convertGeminiSchema(value, false); ci != nil {
					out[k] = ci
				}
			case "minLength", "maxLength", "minItems", "maxItems":
				out[k] = protoIntString(value)
			default:
				out[k] = value
			}
		}

		// Infer missing type
		if _, ok := out["type"]; !ok {
			if isRoot {
				out["type"] = "OBJECT"
			} else if _, hasProps := out["properties"]; hasProps {
				out["type"] = "OBJECT"
			} else if _, hasItems := out["items"]; hasItems {
				out["type"] = "ARRAY"
			} else if enumVals, ok := out["enum"].([]any); ok && len(enumVals) > 0 {
				out["type"] = inferEnumType(enumVals)
			} else {
				out["type"] = "STRING"
			}
		}

		// Array items requirement: Google Schema requires items on every ARRAY
		if out["type"] == "ARRAY" && out["items"] == nil {
			out["items"] = map[string]any{"type": "STRING"}
		}

		// Clean up type-mismatched fields (Google returns 400 if ARRAY fields appear
		// on non-ARRAY, or OBJECT fields on non-OBJECT)
		if t, ok := out["type"].(string); ok {
			if t != "ARRAY" {
				delete(out, "items")
				if !isRoot {
					delete(out, "minItems")
					delete(out, "maxItems")
				}
			}
			if t != "OBJECT" {
				delete(out, "properties")
				delete(out, "required")
			}
		}

		// Sanitize required: required must only list properties that are declared in properties
		if req, ok := out["required"]; ok {
			props, _ := out["properties"].(map[string]any)
			var keep []any
			switch r := req.(type) {
			case []any:
				for _, name := range r {
					if s, ok := name.(string); ok && props != nil && props[s] != nil {
						keep = append(keep, s)
					}
				}
			case []string:
				for _, s := range r {
					if props != nil && props[s] != nil {
						keep = append(keep, s)
					}
				}
			}
			if len(keep) > 0 {
				out["required"] = keep
			} else {
				delete(out, "required")
			}
		}

		return out
	case []any:
		out := make([]any, 0, len(node))
		for _, item := range node {
			if ci := convertGeminiSchema(item, false); ci != nil {
				out = append(out, ci)
			}
		}
		return out
	default:
		return v
	}
}

func inferEnumType(vals []any) string {
	for _, v := range vals {
		switch v.(type) {
		case string:
			return "STRING"
		case float64, int, int64, json.Number:
			return "NUMBER"
		case bool:
			return "BOOLEAN"
		}
	}
	return "STRING"
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

// NormalizeToolSchema normalizes a JSON Schema for third-party providers
// (OpenAI, Anthropic, Ollama, DeepSeek). It handles:
// 1. Boolean schemas (e.g. items: true -> items: {"type": "string"}).
// 2. Empty property definitions (e.g. "prop": {} -> "prop": {"type": "string"}).
// 3. Lowercasing uppercase Gemini types ("OBJECT" -> "object", "STRING" -> "string").
// 4. Inferring missing "type" when "properties" (type: "object"), "items" (type: "array"), or "enum" are present.
// 5. Ensuring arrays have an items schema (defaults to {"type": "string"}).
// 6. Converting "const" keywords to single-item "enum" lists.
// 7. Folding "anyOf" / "oneOf" unions into the non-null schema and marking nullable: true.
// 8. Sanitizing "required" arrays to only reference declared properties.
// 9. Accepting json.RawMessage, []byte, and string inputs gracefully.
func NormalizeToolSchema(v any, isRoot bool) any {
	if raw, ok := v.(json.RawMessage); ok {
		var parsed any
		if err := json.Unmarshal(raw, &parsed); err == nil {
			v = parsed
		}
	} else if b, ok := v.([]byte); ok {
		var parsed any
		if err := json.Unmarshal(b, &parsed); err == nil {
			v = parsed
		}
	}

	switch node := v.(type) {
	case bool:
		if node {
			if isRoot {
				return map[string]any{"type": "object"}
			}
			return map[string]any{"type": "string"}
		}
		return nil
	case map[string]any:
		out := make(map[string]any, len(node))

		// Handle const -> enum (JSON Schema const is not supported by OpenAI/many providers)
		if c, ok := node["const"]; ok {
			out["enum"] = []any{c}
		}

		// Handle anyOf / oneOf unions: fold first non-null branch into the schema
		for _, unionKey := range []string{"anyOf", "oneOf"} {
			if alts, ok := node[unionKey].([]any); ok && len(alts) > 0 {
				for _, alt := range alts {
					if m, ok := alt.(map[string]any); ok {
						if t, ok := m["type"].(string); ok && strings.ToLower(t) == "null" {
							out["nullable"] = true
						} else {
							for k, val := range m {
								if k == "type" || k == "properties" || k == "items" || k == "enum" || k == "format" {
									if _, exists := node[k]; !exists {
										if _, inOut := out[k]; !inOut {
											if k == "type" {
												if s, ok := val.(string); ok {
													out["type"] = strings.ToLower(s)
												}
											} else if norm := NormalizeToolSchema(val, false); norm != nil {
												out[k] = norm
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}

		for k, val := range node {
			if k == "anyOf" || k == "oneOf" || k == "const" {
				continue
			}
			switch k {
			case "type":
				switch t := val.(type) {
				case string:
					out[k] = strings.ToLower(t)
				case []any:
					var types []string
					for _, item := range t {
						if s, ok := item.(string); ok {
							if strings.ToLower(s) == "null" {
								out["nullable"] = true
							} else {
								types = append(types, strings.ToLower(s))
							}
						}
					}
					if len(types) > 0 {
						out[k] = types[0]
					}
				case []string:
					var types []string
					for _, s := range t {
						if strings.ToLower(s) == "null" {
							out["nullable"] = true
						} else {
							types = append(types, strings.ToLower(s))
						}
					}
					if len(types) > 0 {
						out[k] = types[0]
					}
				default:
					out[k] = val
				}
			case "properties":
				props, ok := val.(map[string]any)
				if !ok {
					continue
				}
				converted := make(map[string]any, len(props))
				for name, prop := range props {
					norm := NormalizeToolSchema(prop, false)
					if m, ok := norm.(map[string]any); ok {
						if _, hasType := m["type"]; !hasType {
							m["type"] = "string"
						}
						converted[name] = m
					} else if norm != nil {
						converted[name] = norm
					} else {
						converted[name] = map[string]any{"type": "string"}
					}
				}
				out[k] = converted
			case "items":
				if b, ok := val.(bool); ok {
					if b {
						out[k] = map[string]any{"type": "string"}
					}
				} else {
					norm := NormalizeToolSchema(val, false)
					if m, ok := norm.(map[string]any); ok {
						if _, hasType := m["type"]; !hasType {
							m["type"] = "string"
						}
						out[k] = m
					} else if norm != nil {
						out[k] = norm
					} else {
						out[k] = map[string]any{"type": "string"}
					}
				}
			default:
				out[k] = NormalizeToolSchema(val, false)
			}
		}

		// Infer missing type
		if _, ok := out["type"]; !ok {
			if isRoot {
				out["type"] = "object"
			} else if _, hasProps := out["properties"]; hasProps {
				out["type"] = "object"
			} else if _, hasItems := out["items"]; hasItems {
				out["type"] = "array"
			} else if enumVals, ok := out["enum"].([]any); ok && len(enumVals) > 0 {
				out["type"] = inferEnumLowerType(enumVals)
			} else if enumStrs, ok := out["enum"].([]string); ok && len(enumStrs) > 0 {
				out["type"] = "string"
			} else {
				out["type"] = "string"
			}
		}

		if out["type"] == "array" && out["items"] == nil {
			out["items"] = map[string]any{"type": "string"}
		}

		// Sanitize required: required must only list properties that are declared in properties
		if req, ok := out["required"]; ok {
			props, _ := out["properties"].(map[string]any)
			var keep []any
			switch r := req.(type) {
			case []any:
				for _, name := range r {
					if s, ok := name.(string); ok && props != nil && props[s] != nil {
						keep = append(keep, s)
					}
				}
			case []string:
				for _, s := range r {
					if props != nil && props[s] != nil {
						keep = append(keep, s)
					}
				}
			}
			if len(keep) > 0 {
				out["required"] = keep
			} else {
				delete(out, "required")
			}
		}

		return out
	case []any:
		out := make([]any, 0, len(node))
		for _, item := range node {
			if norm := NormalizeToolSchema(item, false); norm != nil {
				out = append(out, norm)
			}
		}
		return out
	default:
		return v
	}
}

func inferEnumLowerType(vals []any) string {
	for _, v := range vals {
		switch v.(type) {
		case string:
			return "string"
		case float64, float32, int, int32, int64, uint, uint32, uint64, json.Number:
			return "number"
		case bool:
			return "boolean"
		}
	}
	return "string"
}
