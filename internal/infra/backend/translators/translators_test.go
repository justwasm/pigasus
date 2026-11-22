package translators_test

import (
	"encoding/json"
	"testing"

	"github.com/justwasm/pigasus/internal/infra/backend/translators"
)

func TestPigasus1514NativeTools_CountAndNames(t *testing.T) {
	decls, err := translators.GetPigasus1514Declarations(true)
	if err != nil {
		t.Fatalf("failed to load 1.5.14 declarations: %v", err)
	}

	if len(decls) != 12 {
		t.Fatalf("expected exactly 12 native tools, got %d", len(decls))
	}

	expectedNames := []string{
		"read_file",
		"run_command",
		"manage_task",
		"send_message",
		"schedule",
		"invoke_subagent",
		"define_subagent",
		"manage_subagents",
		"write_file",
		"edit_file",
		"search_web",
		"ask_question",
	}

	nameMap := make(map[string]bool)
	for _, d := range decls {
		nameMap[d.Name] = true
	}

	for _, want := range expectedNames {
		if !nameMap[want] {
			t.Errorf("missing expected 1.5.14 tool: %s", want)
		}
	}
}

func TestOpenAIToolTranslator_12Tools_Strict(t *testing.T) {
	decls, err := translators.GetPigasus1514Declarations(true)
	if err != nil {
		t.Fatalf("failed to load declarations: %v", err)
	}

	tr := translators.NewOpenAIToolTranslator()
	tools, err := tr.TranslateAll(decls, translators.WithStrict(true), translators.WithSandbox(true))
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	if len(tools) != 12 {
		t.Fatalf("expected 12 OpenAI tools, got %d", len(tools))
	}

	for _, tool := range tools {
		if tool.Type != "function" {
			t.Errorf("tool %s has type %q, want 'function'", tool.Function.Name, tool.Type)
		}
		if tool.Function.Strict == nil || !*tool.Function.Strict {
			t.Errorf("tool %s missing strict: true", tool.Function.Name)
		}
		params := tool.Function.Parameters
		if params["type"] != "object" {
			t.Errorf("tool %s parameter type = %v, want 'object'", tool.Function.Name, params["type"])
		}
		if params["additionalProperties"] != false {
			t.Errorf("tool %s missing additionalProperties: false", tool.Function.Name)
		}

		// Verify strict rule: all property keys are in required
		if props, ok := params["properties"].(map[string]any); ok {
			reqSlice, hasReq := params["required"].([]any)
			if !hasReq {
				t.Errorf("tool %s has properties but missing required array", tool.Function.Name)
				continue
			}
			reqSet := make(map[string]bool)
			for _, r := range reqSlice {
				if s, ok := r.(string); ok {
					reqSet[s] = true
				}
			}
			for pName := range props {
				if !reqSet[pName] {
					t.Errorf("tool %s property %q not in required in strict mode", tool.Function.Name, pName)
				}
			}
		}
	}
}

func TestOpenAIToolTranslator_BypassSandboxGating(t *testing.T) {
	decls, err := translators.GetPigasus1514Declarations(true)
	if err != nil {
		t.Fatalf("failed to load declarations: %v", err)
	}

	tr := translators.NewOpenAIToolTranslator()

	// 1. Sandboxed = true: run_command carries BypassSandbox
	toolsSandboxed, err := tr.TranslateAll(decls, translators.WithSandbox(true))
	if err != nil {
		t.Fatalf("sandboxed translation failed: %v", err)
	}
	var rcSandboxed *translators.OpenAITool
	for i := range toolsSandboxed {
		if toolsSandboxed[i].Function.Name == "run_command" {
			rcSandboxed = &toolsSandboxed[i]
			break
		}
	}
	if rcSandboxed == nil {
		t.Fatal("run_command not found in sandboxed tools")
	}
	propsSandboxed := rcSandboxed.Function.Parameters["properties"].(map[string]any)
	if _, ok := propsSandboxed["BypassSandbox"]; !ok {
		t.Errorf("sandboxed run_command MUST carry BypassSandbox parameter")
	}

	// 2. Sandboxed = false: run_command strictly omits BypassSandbox
	toolsUnsandboxed, err := tr.TranslateAll(decls, translators.WithSandbox(false))
	if err != nil {
		t.Fatalf("unsandboxed translation failed: %v", err)
	}
	var rcUnsandboxed *translators.OpenAITool
	for i := range toolsUnsandboxed {
		if toolsUnsandboxed[i].Function.Name == "run_command" {
			rcUnsandboxed = &toolsUnsandboxed[i]
			break
		}
	}
	if rcUnsandboxed == nil {
		t.Fatal("run_command not found in unsandboxed tools")
	}
	propsUnsandboxed := rcUnsandboxed.Function.Parameters["properties"].(map[string]any)
	if _, ok := propsUnsandboxed["BypassSandbox"]; ok {
		t.Errorf("unsandboxed run_command MUST NOT carry BypassSandbox parameter")
	}
}

func TestAnthropicToolTranslator_12Tools(t *testing.T) {
	decls, err := translators.GetPigasus1514Declarations(true)
	if err != nil {
		t.Fatalf("failed to load declarations: %v", err)
	}

	tr := translators.NewAnthropicToolTranslator()
	tools, err := tr.TranslateAll(decls, translators.WithSandbox(true))
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	if len(tools) != 12 {
		t.Fatalf("expected 12 Anthropic tools, got %d", len(tools))
	}

	for _, tool := range tools {
		if tool.Name == "" {
			t.Errorf("empty name in Anthropic tool")
		}
		schema := tool.InputSchema
		if schema["type"] != "object" {
			t.Errorf("tool %s input_schema type = %v, want 'object'", tool.Name, schema["type"])
		}
		if props, ok := schema["properties"].(map[string]any); ok {
			for pName, pVal := range props {
				pMap, isMap := pVal.(map[string]any)
				if !isMap {
					t.Errorf("tool %s property %s is not a map", tool.Name, pName)
					continue
				}
				// Verify lowercase type
				if pType, ok := pMap["type"].(string); ok {
					if pType != "" && pType != "object" && pType != "string" && pType != "integer" &&
						pType != "boolean" && pType != "array" && pType != "number" {
						t.Errorf("tool %s property %s has non-standard type %q", tool.Name, pName, pType)
					}
				}
			}
		}
	}
}

func TestAnthropicToolTranslator_BypassSandboxGating(t *testing.T) {
	decls, err := translators.GetPigasus1514Declarations(true)
	if err != nil {
		t.Fatalf("failed to load declarations: %v", err)
	}

	tr := translators.NewAnthropicToolTranslator()

	// 1. Sandboxed = true
	toolsSandboxed, err := tr.TranslateAll(decls, translators.WithSandbox(true))
	if err != nil {
		t.Fatalf("sandboxed translation failed: %v", err)
	}
	var rcSandboxed *translators.AnthropicTool
	for i := range toolsSandboxed {
		if toolsSandboxed[i].Name == "run_command" {
			rcSandboxed = &toolsSandboxed[i]
			break
		}
	}
	propsSandboxed := rcSandboxed.InputSchema["properties"].(map[string]any)
	if _, ok := propsSandboxed["BypassSandbox"]; !ok {
		t.Errorf("sandboxed Anthropic run_command MUST carry BypassSandbox")
	}

	// 2. Sandboxed = false
	toolsUnsandboxed, err := tr.TranslateAll(decls, translators.WithSandbox(false))
	if err != nil {
		t.Fatalf("unsandboxed translation failed: %v", err)
	}
	var rcUnsandboxed *translators.AnthropicTool
	for i := range toolsUnsandboxed {
		if toolsUnsandboxed[i].Name == "run_command" {
			rcUnsandboxed = &toolsUnsandboxed[i]
			break
		}
	}
	propsUnsandboxed := rcUnsandboxed.InputSchema["properties"].(map[string]any)
	if _, ok := propsUnsandboxed["BypassSandbox"]; ok {
		t.Errorf("unsandboxed Anthropic run_command MUST NOT carry BypassSandbox")
	}
}

func TestBidirectionalRoundtrip_OpenAI(t *testing.T) {
	decls, err := translators.GetPigasus1514Declarations(true)
	if err != nil {
		t.Fatalf("failed to load declarations: %v", err)
	}

	tr := translators.NewOpenAIToolTranslator()

	for _, original := range decls {
		// Non-strict roundtrip preserving original required fields
		openAITool, err := tr.Translate(original, translators.WithStrict(false), translators.WithSandbox(true))
		if err != nil {
			t.Fatalf("tool %s translation failed: %v", original.Name, err)
		}

		reversed, err := tr.ReverseTranslate(openAITool)
		if err != nil {
			t.Fatalf("tool %s reverse translation failed: %v", original.Name, err)
		}

		if reversed.Name != original.Name {
			t.Errorf("name mismatch: got %q, want %q", reversed.Name, original.Name)
		}
		if reversed.Description != original.Description {
			t.Errorf("tool %s description mismatch", original.Name)
		}

		origProps := original.Parameters["properties"].(map[string]any)
		revProps := reversed.Parameters["properties"].(map[string]any)
		if len(origProps) != len(revProps) {
			t.Errorf("tool %s properties count mismatch: orig %d vs rev %d", original.Name, len(origProps), len(revProps))
		}
		for k := range origProps {
			if _, ok := revProps[k]; !ok {
				t.Errorf("tool %s missing reversed property: %s", original.Name, k)
			}
		}
	}
}

func TestBidirectionalRoundtrip_Anthropic(t *testing.T) {
	decls, err := translators.GetPigasus1514Declarations(true)
	if err != nil {
		t.Fatalf("failed to load declarations: %v", err)
	}

	tr := translators.NewAnthropicToolTranslator()

	for _, original := range decls {
		anthropicTool, err := tr.Translate(original, translators.WithSandbox(true))
		if err != nil {
			t.Fatalf("tool %s translation failed: %v", original.Name, err)
		}

		reversed, err := tr.ReverseTranslate(anthropicTool)
		if err != nil {
			t.Fatalf("tool %s reverse translation failed: %v", original.Name, err)
		}

		if reversed.Name != original.Name {
			t.Errorf("name mismatch: got %q, want %q", reversed.Name, original.Name)
		}
		if reversed.Description != original.Description {
			t.Errorf("tool %s description mismatch", original.Name)
		}

		origProps := original.Parameters["properties"].(map[string]any)
		revProps := reversed.Parameters["properties"].(map[string]any)
		if len(origProps) != len(revProps) {
			t.Errorf("tool %s properties count mismatch: orig %d vs rev %d", original.Name, len(origProps), len(revProps))
		}
	}
}

func TestToolCall_Translation(t *testing.T) {
	trOpenAI := translators.NewOpenAIToolTranslator()

	// 1. OpenAI Call Translation
	call, err := trOpenAI.TranslateCallToOpenAI("call_123", "read_file", map[string]any{
		"Path": "/tmp/test.go",
	})
	if err != nil {
		t.Fatalf("TranslateCallToOpenAI failed: %v", err)
	}
	if call.ID != "call_123" || call.Function.Name != "read_file" {
		t.Fatalf("unexpected call format: %+v", call)
	}

	name, args, err := trOpenAI.TranslateToolCall(call)
	if err != nil {
		t.Fatalf("TranslateToolCall failed: %v", err)
	}
	if name != "read_file" || args["Path"] != "/tmp/test.go" {
		t.Fatalf("unexpected parsed call: name=%s, args=%v", name, args)
	}

	// 2. Anthropic Call Translation
	trAnthropic := translators.NewAnthropicToolTranslator()
	use, err := trAnthropic.TranslateCallToAnthropic("toolu_456", "write_file", map[string]any{
		"Path":    "/tmp/test.go",
		"Content": "package main",
	})
	if err != nil {
		t.Fatalf("TranslateCallToAnthropic failed: %v", err)
	}
	if use.ID != "toolu_456" || use.Name != "write_file" || use.Type != "tool_use" {
		t.Fatalf("unexpected tool_use block: %+v", use)
	}

	uName, uArgs, err := trAnthropic.TranslateToolUse(use)
	if err != nil {
		t.Fatalf("TranslateToolUse failed: %v", err)
	}
	if uName != "write_file" || uArgs["Path"] != "/tmp/test.go" || uArgs["Content"] != "package main" {
		t.Fatalf("unexpected parsed tool use: name=%s, args=%v", uName, uArgs)
	}

	// 3. Anthropic Result Translation
	res := trAnthropic.TranslateResultToAnthropic("toolu_456", "Success", false)
	if res.Type != "tool_result" || res.ToolUseID != "toolu_456" || res.Content != "Success" || res.IsError {
		t.Fatalf("unexpected tool result: %+v", res)
	}

	resBytes, _ := json.Marshal(res)
	if string(resBytes) == "" {
		t.Fatal("empty serialized tool result")
	}
}
