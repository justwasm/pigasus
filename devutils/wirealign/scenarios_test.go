package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
)

func TestAllScenarios_RenderFromTemplates(t *testing.T) {
	for _, sc := range AllScenarios {
		t.Run(sc.Name, func(t *testing.T) {
			refWire := BuildReferenceWire(sc)
			if refWire == nil || refWire.SystemInstruction == "" {
				t.Fatalf("scenario %s failed to render reference wire from templates", sc.Name)
			}

			pDec := DeconstructPrompt(refWire.SystemInstruction)
			tDec := DeconstructTools(refWire.Tools)

			// 1. Verify section ordering and framing
			if len(pDec.Sections) == 0 {
				t.Fatalf("no XML sections found in prompt for %s", sc.Name)
			}

			// 2. Specific scenario assertions
			switch sc.Name {
			case "standard_turn":
				if pDec.IsSandboxed {
					t.Errorf("standard turn must not have <terminal_sandbox>")
				}
				if tDec.RunCommandHasBypass {
					t.Errorf("unsandboxed run_command must not have BypassSandbox")
				}
				if !pDec.IsGenericPrompt {
					t.Errorf("standard turn must enable generic-system-prompt: true")
				}
				if pDec.UserInfoWording != "Files relating to..." {
					t.Errorf("wording = %q, want 'Files relating to...'", pDec.UserInfoWording)
				}
				if tDec.InvokeSubagentAppended {
					t.Errorf("invoke_subagent must not carry appended built-in list")
				}

			case "sandboxed_turn":
				if !pDec.IsSandboxed {
					t.Errorf("sandboxed turn missing <terminal_sandbox>")
				}
				if !tDec.RunCommandHasBypass {
					t.Errorf("sandboxed run_command missing BypassSandbox")
				}

			case "clean_no_rules":
				if _, hasRules := pDec.SectionMap["user_rules"]; hasRules {
					t.Errorf("clean_no_rules must omit <user_rules>")
				}

			case "custom_user_rules":
				if _, hasRules := pDec.SectionMap["user_rules"]; !hasRules {
					t.Errorf("custom_user_rules missing <user_rules>")
				}
				// Verify ordering: skills precedes user_rules
				skillsIdx := -1
				rulesIdx := -1
				for i, tag := range pDec.SectionOrder {
					if tag == "skills" {
						skillsIdx = i
					}
					if tag == "user_rules" {
						rulesIdx = i
					}
				}
				if skillsIdx == -1 || rulesIdx == -1 || skillsIdx > rulesIdx {
					t.Errorf("section order invalid: skillsIdx=%d, rulesIdx=%d (want skills before user_rules)", skillsIdx, rulesIdx)
				}

			case "planning_mode":
				artSec := pDec.SectionMap["artifacts"]
				if !strings.Contains(artSec.Content, "task.md") || !strings.Contains(artSec.Content, "implementation_plan.md") {
					t.Errorf("planning_mode artifacts missing planning mode artifacts template output")
				}

			case "autonomous_mode":
				idSec := pDec.SectionMap["identity"]
				if !strings.Contains(idSec.Content, "working for") {
					t.Errorf("autonomous mode identity must use 'working for', got %q", idSec.Content)
				}
				commSec := pDec.SectionMap["communication_style"]
				if !strings.Contains(commSec.Content, "make your best judgement") {
					t.Errorf("autonomous mode communication_style missing autonomous guidance")
				}

			case "subagent_turn":
				if _, hasPeerMsg := pDec.SectionMap["messaging"]; !hasPeerMsg {
					t.Errorf("subagent turn missing messaging section")
				}
				msgSec := pDec.SectionMap["messaging"]
				if strings.Contains(msgSec.Content, "peer") {
					t.Errorf("subagent messaging section must omit peer agents")
				}

			case "compaction_turn":
				if tDec.Count != 12 {
					t.Errorf("compaction turn must expose full 12 tools, got %d", tDec.Count)
				}
				if !pDec.IsGenericPrompt {
					t.Errorf("compaction turn must preserve generic system prompt")
				}
				if len(refWire.Contents) != 1 {
					t.Fatalf("compaction turn contents count = %d, want 1", len(refWire.Contents))
				}
				if !strings.Contains(refWire.Contents[0].Parts[0].Text, "<CONTEXT_SUMMARY>") {
					t.Errorf("compaction turn must include <CONTEXT_SUMMARY> in resuming turn")
				}
				if !strings.Contains(refWire.Contents[0].Parts[0].Text, "</CONTEXT_SUMMARY>") {
					t.Errorf("compaction turn must close with </CONTEXT_SUMMARY>")
				}
				if !strings.Contains(refWire.Contents[0].Parts[0].Text, "# User Requests") {
					t.Errorf("compaction turn must include '# User Requests'")
				}

			case "compaction_request":
				if tDec.Count != 12 {
					t.Errorf("compaction request must expose full 12 tools, got %d", tDec.Count)
				}
				if !pDec.IsGenericPrompt {
					t.Errorf("compaction request must preserve generic system prompt")
				}
				if len(refWire.Contents) < 1 {
					t.Fatalf("compaction request must have conversation contents")
				}
				lastTurn := refWire.Contents[len(refWire.Contents)-1]
				if lastTurn.Role != "user" {
					t.Errorf("compaction request last turn role = %s, want user", lastTurn.Role)
				}
				lastText := lastTurn.Parts[0].Text
				requiredSections := []string{
					"Task Overview",
					"Progress",
					"Key Findings",
					"Errors & Fixes",
					"Active Context",
					"Next Steps",
					"Commitments & Constraints",
					"ON-DEMAND COMPACTION:",
				}
				for _, sec := range requiredSections {
					if !strings.Contains(lastText, sec) {
						t.Errorf("compaction request prompt missing section %q", sec)
					}
				}

			case "swarm_coordinator_turn":
				swarmTool, ok := tDec.ToolMap["swarm"]
				if !ok {
					t.Fatalf("swarm_coordinator_turn missing swarm tool declaration")
				}
				if !strings.Contains(swarmTool.Description, "Manage the swarm of agents working with you") {
					t.Errorf("unexpected swarm tool description: %s", swarmTool.Description)
				}
				props, ok := swarmTool.Parameters["properties"].(map[string]any)
				if !ok {
					t.Fatalf("swarm tool parameters missing properties")
				}
				actionProp, ok := props["action"].(map[string]any)
				if !ok {
					t.Fatalf("swarm tool properties missing action")
				}
				enums, ok := actionProp["enum"].([]any)
				if !ok || len(enums) != 6 {
					t.Errorf("swarm action enum count = %d, want 6", len(enums))
				}
				skillsSec, hasSkills := pDec.SectionMap["skills"]
				if !hasSkills || !strings.Contains(skillsSec.Content, "native_swarm/SKILL.md") || !strings.Contains(skillsSec.Content, "swarm_monitor/SKILL.md") {
					t.Errorf("swarm coordinator turn skills missing swarm/swarm_monitor: %s", skillsSec.Content)
				}
			}
		})
	}
}

func TestVerifyRequestAgainstTemplates(t *testing.T) {
	// Build a valid standard request using the prompt engine
	sc, _ := GetScenario("standard_turn")
	wire := BuildReferenceWire(*sc)

	// Validate against the template ground truth
	opts := DefaultCompareOptions()
	diff := CompareWire(wire, wire, opts)

	if !diff.Passed {
		t.Fatalf("self comparison against templates failed:\n%s", RenderTerminal(diff))
	}
	if diff.FailedChecks > 0 {
		t.Errorf("failed checks = %d, want 0", diff.FailedChecks)
	}
}

func TestTemplateSectionParity(t *testing.T) {
	opts := agyprompt.DefaultOptions("/workspace", "/appdata", "00000000-0000-0000-0000-000000000000")
	fullPrompt := agyprompt.Build(opts)
	pDec := DeconstructPrompt(fullPrompt)

	// Test that each template-rendered section matches SectionBody verbatim
	templateSections := []string{
		"identity",
		"artifacts",
		"conversation_transcript",
		"guidelines",
		"communication_style",
		"slash_commands",
		"skills",
	}

	for _, secName := range templateSections {
		expectedBody := agyprompt.SectionBody(opts, secName)
		actualSec, exists := pDec.SectionMap[secName]
		if !exists {
			t.Errorf("section %s missing from full prompt deconstruction", secName)
			continue
		}

		if actualSec.Content != expectedBody {
			t.Errorf("section %s differs from template ground truth:\n got len=%d\nwant len=%d",
				secName, len(actualSec.Content), len(expectedBody))
		}
	}
}

func TestToolsSurfaceDeclarations(t *testing.T) {
	unsandboxedDecls := surface.ModelDeclarationsFor(false)
	sandboxedDecls := surface.ModelDeclarationsFor(true)

	if len(unsandboxedDecls) != 12 {
		t.Errorf("unsandboxed tools count = %d, want 12", len(unsandboxedDecls))
	}
	if len(sandboxedDecls) != 12 {
		t.Errorf("sandboxed tools count = %d, want 12", len(sandboxedDecls))
	}

	uTD := DeconstructTools(toWireTools(unsandboxedDecls))
	sTD := DeconstructTools(toWireTools(sandboxedDecls))

	if uTD.RunCommandHasBypass {
		t.Errorf("unsandboxed run_command must not have BypassSandbox")
	}
	if !sTD.RunCommandHasBypass {
		t.Errorf("sandboxed run_command must have BypassSandbox")
	}
}

func toWireTools(decls []surface.ModelToolDecl) []FunctionDecl {
	var tools []FunctionDecl
	for _, md := range decls {
		fd := FunctionDecl{
			Name:        md.Name,
			Description: md.Description,
		}
		if len(md.Parameters) > 0 {
			var params map[string]any
			_ = json.Unmarshal(md.Parameters, &params)
			fd.Parameters = params
		}
		tools = append(tools, fd)
	}
	return tools
}
