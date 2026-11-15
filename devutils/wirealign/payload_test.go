package main

import (
	"testing"
)

func TestParseWirePayload_Shapes(t *testing.T) {
	// Shape A: Standard envelope with requestBody and request
	rawA := []byte(`{
		"ts": 1234567,
		"requestBody": {
			"project": "aicode-consumers",
			"model": "gemini-3.8-flash-high",
			"request": {
				"systemInstruction": {
					"parts": [{"text": "<identity>agentic AI assistant</identity>"}]
				},
				"tools": [
					{
						"functionDeclarations": [
							{"name": "read_file", "description": "reads a file"}
						]
					}
				],
				"contents": [
					{
						"role": "user",
						"parts": [{"text": "hello"}]
					}
				]
			}
		}
	}`)

	p, err := ParseWirePayload(rawA)
	if err != nil {
		t.Fatalf("failed to parse Shape A: %v", err)
	}
	if p.Model != "gemini-3.8-flash-high" {
		t.Errorf("model = %q, want 'gemini-3.8-flash-high'", p.Model)
	}
	if p.Project != "aicode-consumers" {
		t.Errorf("project = %q, want 'aicode-consumers'", p.Project)
	}
	if len(p.Tools) != 1 || p.Tools[0].Name != "read_file" {
		t.Errorf("unexpected tools: %v", p.Tools)
	}
	if len(p.Contents) != 1 || p.Contents[0].Role != "user" {
		t.Errorf("unexpected contents: %v", p.Contents)
	}
}

func TestDeconstructPrompt(t *testing.T) {
	prompt := `<identity>
You are Antigravity, a powerful agentic AI assistant designed by the Google Deepmind team.
You are working with a USER to solve their task.
</identity>
<artifacts>
Artifacts are special markdown documents.
</artifacts>
<skills>
Available skills:
- a11y-debugging (/path/to/skill/SKILL.md): description
- web-perf (/path/to/perf/SKILL.md): perf description
</skills>
<subagents>
Available subagents:
- self: Self clone
- research: Research agent
- image-generator: Image generator
</subagents>
<user_rules>
The following are user-defined rules:
<RULE[/path/to/AGENTS.md]>
rule text
</RULE[/path/to/AGENTS.md]>
</user_rules>
<user_information>
The USER's OS version is mac.
Command Working Directory: /workspace
Files relating to the user's requests should be written in the locations listed above.
</user_information>`

	d := DeconstructPrompt(prompt)
	expectedOrder := []string{"identity", "artifacts", "skills", "subagents", "user_rules", "user_information"}

	if len(d.SectionOrder) != len(expectedOrder) {
		t.Fatalf("sections count = %d, want %d (%v)", len(d.SectionOrder), len(expectedOrder), d.SectionOrder)
	}
	for i, tag := range expectedOrder {
		if d.SectionOrder[i] != tag {
			t.Errorf("section[%d] = %q, want %q", i, d.SectionOrder[i], tag)
		}
	}

	if !d.IsGenericPrompt {
		t.Errorf("expected IsGenericPrompt=true")
	}
	if d.IdentityRole != "agentic AI assistant" {
		t.Errorf("IdentityRole = %q, want 'agentic AI assistant'", d.IdentityRole)
	}
	if d.IsSandboxed {
		t.Errorf("expected IsSandboxed=false")
	}
	if d.SkillsCount != 2 {
		t.Errorf("skills count = %d, want 2", d.SkillsCount)
	}
	if len(d.SubagentsList) != 3 || d.SubagentsList[0] != "self" {
		t.Errorf("subagents list = %v, want [self, research, image-generator]", d.SubagentsList)
	}
	if d.UserRulesCount != 1 || d.UserRuleLabels[0] != "/path/to/AGENTS.md" {
		t.Errorf("user rules = %v, want [/path/to/AGENTS.md]", d.UserRuleLabels)
	}
	if d.UserInfoWording != "Files relating to..." {
		t.Errorf("UserInfoWording = %q, want 'Files relating to...'", d.UserInfoWording)
	}
}

func TestDeconstructTools(t *testing.T) {
	tools := []FunctionDecl{
		{
			Name:        "run_command",
			Description: "runs a command",
			Parameters: map[string]any{
				"properties": map[string]any{
					"CommandLine": map[string]any{"type": "STRING"},
				},
			},
		},
		{
			Name:        "invoke_subagent",
			Description: "Launches a subagent. Runs in background.",
		},
	}

	td := DeconstructTools(tools)
	if td.Count != 2 {
		t.Fatalf("count = %d, want 2", td.Count)
	}
	if !td.HasRunCommand {
		t.Errorf("expected HasRunCommand=true")
	}
	if td.RunCommandHasBypass {
		t.Errorf("expected RunCommandHasBypass=false")
	}
	if td.InvokeSubagentAppended {
		t.Errorf("expected InvokeSubagentAppended=false")
	}

	// Now test sandboxed run_command and dirty invoke_subagent
	tools[0].Parameters["properties"].(map[string]any)["BypassSandbox"] = map[string]any{"type": "BOOLEAN"}
	tools[1].Description += "\n\nBuilt-in subagents:\n- foo: bar"

	tdDirty := DeconstructTools(tools)
	if !tdDirty.RunCommandHasBypass {
		t.Errorf("expected RunCommandHasBypass=true")
	}
	if !tdDirty.InvokeSubagentAppended {
		t.Errorf("expected InvokeSubagentAppended=true")
	}
}
