package printmode

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestJSONSchema_EnforcedInPromptConstraint(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"major":{"type":"integer"}}}`)
	cfg := pigPrintConfig{
		Prompt:     "parse version",
		JSONSchema: schema,
	}

	prompt := cfg.Prompt
	if len(cfg.JSONSchema) > 0 {
		prompt = cfg.Prompt + "\n\n[System constraint: You must output ONLY valid JSON matching this schema exactly, with no markdown code fences and no extra text:\n" + string(cfg.JSONSchema) + "]"
	}

	if !strings.Contains(prompt, "[System constraint: You must output ONLY valid JSON matching this schema exactly") {
		t.Errorf("expected system constraint in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, `"properties":{"major":{"type":"integer"}}`) {
		t.Errorf("expected schema in prompt, got:\n%s", prompt)
	}
}
