package translators

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/justwasm/pigasus/geminicode"
)

// OpenAIToolTranslator performs lossless bi-directional translation between
// Pigasus/Gemini FunctionDeclarations and OpenAI tools schema.
type OpenAIToolTranslator struct{}

// NewOpenAIToolTranslator creates a new OpenAIToolTranslator.
func NewOpenAIToolTranslator() *OpenAIToolTranslator {
	return &OpenAIToolTranslator{}
}

// Translate converts a Pigasus FunctionDeclaration to an OpenAITool.
func (t *OpenAIToolTranslator) Translate(decl geminicode.FunctionDeclaration, opts ...Option) (OpenAITool, error) {
	options := applyOptions(opts)

	params := decl.Parameters
	if params == nil {
		params = map[string]any{
			"type":       "OBJECT",
			"properties": map[string]any{},
		}
	} else {
		params = copyMap(params)
	}

	// Handle BypassSandbox stripping for run_command when unsandboxed
	if decl.Name == "run_command" && !options.Sandboxed {
		params = stripBypassSandbox(params)
	}

	convertedParams := t.convertSchemaToOpenAI(params, options.Strict)

	var strictPtr *bool
	if options.Strict {
		s := true
		strictPtr = &s
	}

	return OpenAITool{
		Type: "function",
		Function: OpenAIFunction{
			Name:        decl.Name,
			Description: decl.Description,
			Parameters:  convertedParams,
			Strict:      strictPtr,
		},
	}, nil
}

// TranslateAll converts a slice of Pigasus FunctionDeclarations to OpenAITools.
func (t *OpenAIToolTranslator) TranslateAll(decls []geminicode.FunctionDeclaration, opts ...Option) ([]OpenAITool, error) {
	result := make([]OpenAITool, 0, len(decls))
	for _, d := range decls {
		tool, err := t.Translate(d, opts...)
		if err != nil {
			return nil, err
		}
		result = append(result, tool)
	}
	return result, nil
}

// ReverseTranslate converts an OpenAITool back into a Pigasus FunctionDeclaration.
func (t *OpenAIToolTranslator) ReverseTranslate(tool OpenAITool) (geminicode.FunctionDeclaration, error) {
	if tool.Type != "" && tool.Type != "function" {
		return geminicode.FunctionDeclaration{}, fmt.Errorf("unsupported OpenAI tool type: %q", tool.Type)
	}

	convertedParams := t.convertSchemaFromOpenAI(tool.Function.Parameters)

	return geminicode.FunctionDeclaration{
		Name:        tool.Function.Name,
		Description: tool.Function.Description,
		Parameters:  convertedParams,
	}, nil
}

// TranslateToolCall parses arguments from an OpenAI tool call into a structured map.
func (t *OpenAIToolTranslator) TranslateToolCall(call OpenAIToolCall) (name string, args map[string]any, err error) {
	name = call.Function.Name
	rawArgs := strings.TrimSpace(call.Function.Arguments)
	if rawArgs == "" {
		return name, map[string]any{}, nil
	}
	args = make(map[string]any)
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return name, nil, fmt.Errorf("failed to decode tool arguments: %w", err)
	}
	return name, args, nil
}

// TranslateCallToOpenAI constructs an OpenAIToolCall from tool execution arguments.
func (t *OpenAIToolTranslator) TranslateCallToOpenAI(callID, name string, args any) (OpenAIToolCall, error) {
	var argBytes []byte
	var err error
	switch v := args.(type) {
	case string:
		argBytes = []byte(v)
	case []byte:
		argBytes = v
	case json.RawMessage:
		argBytes = v
	default:
		argBytes, err = json.Marshal(v)
		if err != nil {
			return OpenAIToolCall{}, fmt.Errorf("failed to encode tool call arguments: %w", err)
		}
	}

	return OpenAIToolCall{
		ID:   callID,
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{
			Name:      name,
			Arguments: string(argBytes),
		},
	}, nil
}

// convertSchemaToOpenAI transforms schema types to lowercase and handles strict rules.
func (t *OpenAIToolTranslator) convertSchemaToOpenAI(schema map[string]any, strict bool) map[string]any {
	if schema == nil {
		return map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		}
	}

	out := copyMap(schema)

	// Lowercase type
	if typeVal, ok := out["type"].(string); ok {
		out["type"] = strings.ToLower(typeVal)
	}

	// Read original required set
	origRequired := make(map[string]bool)
	if reqSlice, ok := out["required"].([]any); ok {
		for _, r := range reqSlice {
			if s, ok := r.(string); ok {
				origRequired[s] = true
			}
		}
	} else if reqSlice, ok := out["required"].([]string); ok {
		for _, s := range reqSlice {
			origRequired[s] = true
		}
	}

	// Process properties
	if props, ok := out["properties"].(map[string]any); ok {
		newProps := make(map[string]any, len(props))
		var allPropKeys []string

		for propName, propVal := range props {
			allPropKeys = append(allPropKeys, propName)
			propMap, isMap := propVal.(map[string]any)
			if !isMap {
				newProps[propName] = propVal
				continue
			}

			propOut := copyMap(propMap)

			// Normalize type
			if pType, ok := propOut["type"].(string); ok {
				propOut["type"] = strings.ToLower(pType)
			}

			// If strict and not originally required, make nullable
			if strict {
				if !origRequired[propName] {
					makePropertyNullable(propOut)
				}
			}

			// Recursively handle nested object or array schemas
			if pType, ok := propOut["type"].(string); ok && pType == "object" {
				propOut = t.convertSchemaToOpenAI(propOut, strict)
			} else if pTypeSlice, ok := propOut["type"].([]any); ok && sliceContains(pTypeSlice, "object") {
				propOut = t.convertSchemaToOpenAI(propOut, strict)
			}

			if items, ok := propOut["items"].(map[string]any); ok {
				propOut["items"] = t.convertSchemaToOpenAI(items, strict)
			}

			newProps[propName] = propOut
		}

		out["properties"] = newProps

		if strict {
			out["additionalProperties"] = false
			sort.Strings(allPropKeys)
			reqAny := make([]any, len(allPropKeys))
			for i, k := range allPropKeys {
				reqAny[i] = k
			}
			out["required"] = reqAny
		}
	} else if strict {
		out["additionalProperties"] = false
		if _, hasReq := out["required"]; !hasReq {
			out["required"] = []any{}
		}
	}

	return out
}

func makePropertyNullable(prop map[string]any) {
	if tStr, ok := prop["type"].(string); ok {
		prop["type"] = []any{tStr, "null"}
		return
	}
	if tSlice, ok := prop["type"].([]any); ok {
		for _, item := range tSlice {
			if s, ok := item.(string); ok && s == "null" {
				return
			}
		}
		prop["type"] = append(tSlice, "null")
		return
	}
	if anyOf, ok := prop["anyOf"].([]any); ok {
		hasNull := false
		for _, branch := range anyOf {
			if bMap, ok := branch.(map[string]any); ok {
				if t, ok := bMap["type"].(string); ok && t == "null" {
					hasNull = true
					break
				}
			}
		}
		if !hasNull {
			prop["anyOf"] = append(anyOf, map[string]any{"type": "null"})
		}
		return
	}
	prop["type"] = []any{"string", "null"}
}

func sliceContains(s []any, target string) bool {
	for _, item := range s {
		if str, ok := item.(string); ok && str == target {
			return true
		}
	}
	return false
}

// convertSchemaFromOpenAI converts an OpenAI schema back to Pigasus/Gemini uppercase schema format.
func (t *OpenAIToolTranslator) convertSchemaFromOpenAI(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{
			"type": "OBJECT",
		}
	}

	out := copyMap(schema)
	delete(out, "additionalProperties")

	// Uppercase top-level type
	if typeVal, ok := out["type"].(string); ok {
		out["type"] = strings.ToUpper(typeVal)
	}

	var restoredRequired []any

	if props, ok := out["properties"].(map[string]any); ok {
		newProps := make(map[string]any, len(props))
		for propName, propVal := range props {
			propMap, isMap := propVal.(map[string]any)
			if !isMap {
				newProps[propName] = propVal
				continue
			}

			propOut := copyMap(propMap)
			delete(propOut, "additionalProperties")

			wasNullable := false
			// Handle type un-nullifying
			if tSlice, ok := propOut["type"].([]any); ok {
				var nonNullTypes []string
				for _, item := range tSlice {
					if s, ok := item.(string); ok {
						if strings.ToLower(s) == "null" {
							wasNullable = true
						} else {
							nonNullTypes = append(nonNullTypes, strings.ToUpper(s))
						}
					}
				}
				if len(nonNullTypes) == 1 {
					propOut["type"] = nonNullTypes[0]
				} else if len(nonNullTypes) > 1 {
					propOut["type"] = nonNullTypes[0]
				} else {
					propOut["type"] = "STRING"
				}
			} else if tStr, ok := propOut["type"].(string); ok {
				propOut["type"] = strings.ToUpper(tStr)
			}

			// Recursively process objects and arrays
			if pType, ok := propOut["type"].(string); ok && pType == "OBJECT" {
				propOut = t.convertSchemaFromOpenAI(propOut)
			}
			if items, ok := propOut["items"].(map[string]any); ok {
				propOut["items"] = t.convertSchemaFromOpenAI(items)
			}

			newProps[propName] = propOut

			// Only keep in required if it was NOT made nullable (i.e. was originally required)
			if !wasNullable {
				restoredRequired = append(restoredRequired, propName)
			}
		}
		out["properties"] = newProps
	}

	if len(restoredRequired) > 0 {
		out["required"] = restoredRequired
	} else if _, hadReq := schema["required"]; !hadReq {
		delete(out, "required")
	}

	return out
}
