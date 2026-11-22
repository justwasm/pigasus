package translators

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/geminicode"
)

// AnthropicToolTranslator performs lossless bi-directional translation between
// Pigasus/Gemini FunctionDeclarations and Anthropic tools schema.
type AnthropicToolTranslator struct{}

// NewAnthropicToolTranslator creates a new AnthropicToolTranslator.
func NewAnthropicToolTranslator() *AnthropicToolTranslator {
	return &AnthropicToolTranslator{}
}

// Translate converts a Pigasus FunctionDeclaration to an AnthropicTool.
func (t *AnthropicToolTranslator) Translate(decl geminicode.FunctionDeclaration, opts ...Option) (AnthropicTool, error) {
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

	convertedSchema := t.convertSchemaToAnthropic(params)

	return AnthropicTool{
		Name:        decl.Name,
		Description: decl.Description,
		InputSchema: convertedSchema,
	}, nil
}

// TranslateAll converts a slice of Pigasus FunctionDeclarations to AnthropicTools.
func (t *AnthropicToolTranslator) TranslateAll(decls []geminicode.FunctionDeclaration, opts ...Option) ([]AnthropicTool, error) {
	result := make([]AnthropicTool, 0, len(decls))
	for _, d := range decls {
		tool, err := t.Translate(d, opts...)
		if err != nil {
			return nil, err
		}
		result = append(result, tool)
	}
	return result, nil
}

// ReverseTranslate converts an AnthropicTool back into a Pigasus FunctionDeclaration.
func (t *AnthropicToolTranslator) ReverseTranslate(tool AnthropicTool) (geminicode.FunctionDeclaration, error) {
	convertedParams := t.convertSchemaFromAnthropic(tool.InputSchema)

	return geminicode.FunctionDeclaration{
		Name:        tool.Name,
		Description: tool.Description,
		Parameters:  convertedParams,
	}, nil
}

// TranslateToolUse extracts tool invocation details from an AnthropicToolUse block.
func (t *AnthropicToolTranslator) TranslateToolUse(toolUse AnthropicToolUse) (name string, args map[string]any, err error) {
	if toolUse.Type != "" && toolUse.Type != "tool_use" {
		return "", nil, fmt.Errorf("unexpected content block type %q, expected 'tool_use'", toolUse.Type)
	}
	name = toolUse.Name
	if toolUse.Input == nil {
		return name, map[string]any{}, nil
	}
	return name, copyMap(toolUse.Input), nil
}

// TranslateCallToAnthropic constructs an AnthropicToolUse content block.
func (t *AnthropicToolTranslator) TranslateCallToAnthropic(callID, name string, args any) (AnthropicToolUse, error) {
	var inputMap map[string]any
	switch v := args.(type) {
	case map[string]any:
		inputMap = copyMap(v)
	case []byte:
		inputMap = make(map[string]any)
		if len(v) > 0 {
			if err := json.Unmarshal(v, &inputMap); err != nil {
				return AnthropicToolUse{}, fmt.Errorf("failed to decode args JSON into map: %w", err)
			}
		}
	case string:
		inputMap = make(map[string]any)
		if strings.TrimSpace(v) != "" {
			if err := json.Unmarshal([]byte(v), &inputMap); err != nil {
				return AnthropicToolUse{}, fmt.Errorf("failed to decode args string into map: %w", err)
			}
		}
	case json.RawMessage:
		inputMap = make(map[string]any)
		if len(v) > 0 {
			if err := json.Unmarshal(v, &inputMap); err != nil {
				return AnthropicToolUse{}, fmt.Errorf("failed to decode args RawMessage into map: %w", err)
			}
		}
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return AnthropicToolUse{}, fmt.Errorf("failed to marshal args: %w", err)
		}
		inputMap = make(map[string]any)
		if err := json.Unmarshal(b, &inputMap); err != nil {
			return AnthropicToolUse{}, fmt.Errorf("failed to decode marshaled args into map: %w", err)
		}
	}

	return AnthropicToolUse{
		ID:    callID,
		Type:  "tool_use",
		Name:  name,
		Input: inputMap,
	}, nil
}

// TranslateResultToAnthropic creates a tool_result block for Anthropic.
func (t *AnthropicToolTranslator) TranslateResultToAnthropic(callID string, content string, isError bool) AnthropicToolResult {
	return AnthropicToolResult{
		Type:      "tool_result",
		ToolUseID: callID,
		Content:   content,
		IsError:   isError,
	}
}

// convertSchemaToAnthropic converts Gemini uppercase types to standard lowercase JSON schema.
func (t *AnthropicToolTranslator) convertSchemaToAnthropic(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}

	out := copyMap(schema)

	// Lowercase type
	if typeVal, ok := out["type"].(string); ok {
		out["type"] = strings.ToLower(typeVal)
	}

	// Process properties
	if props, ok := out["properties"].(map[string]any); ok {
		newProps := make(map[string]any, len(props))
		for propName, propVal := range props {
			propMap, isMap := propVal.(map[string]any)
			if !isMap {
				newProps[propName] = propVal
				continue
			}

			propOut := copyMap(propMap)
			if pType, ok := propOut["type"].(string); ok {
				propOut["type"] = strings.ToLower(pType)
			}

			// Recursively handle nested object or array schemas
			if pType, ok := propOut["type"].(string); ok && pType == "object" {
				propOut = t.convertSchemaToAnthropic(propOut)
			}
			if items, ok := propOut["items"].(map[string]any); ok {
				propOut["items"] = t.convertSchemaToAnthropic(items)
			}

			newProps[propName] = propOut
		}
		out["properties"] = newProps
	}

	return out
}

// convertSchemaFromAnthropic converts Anthropic lowercase schema back to Gemini uppercase format.
func (t *AnthropicToolTranslator) convertSchemaFromAnthropic(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{
			"type": "OBJECT",
		}
	}

	out := copyMap(schema)

	// Uppercase type
	if typeVal, ok := out["type"].(string); ok {
		out["type"] = strings.ToUpper(typeVal)
	}

	if props, ok := out["properties"].(map[string]any); ok {
		newProps := make(map[string]any, len(props))
		for propName, propVal := range props {
			propMap, isMap := propVal.(map[string]any)
			if !isMap {
				newProps[propName] = propVal
				continue
			}

			propOut := copyMap(propMap)
			if pType, ok := propOut["type"].(string); ok {
				propOut["type"] = strings.ToUpper(pType)
			}

			// Recursively process objects and arrays
			if pType, ok := propOut["type"].(string); ok && pType == "OBJECT" {
				propOut = t.convertSchemaFromAnthropic(propOut)
			}
			if items, ok := propOut["items"].(map[string]any); ok {
				propOut["items"] = t.convertSchemaFromAnthropic(items)
			}

			newProps[propName] = propOut
		}
		out["properties"] = newProps
	}

	return out
}
