package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/core/compaction"
)

// ScenarioSpec defines a test scenario where the Single Source of Truth is
// the template engine (internal/app/prompt/agytemplates) and model tools surface.
type ScenarioSpec struct {
	Name        string
	Description string
	Options     agyprompt.Options
	Sandboxed   bool
	Subagent    bool
	AgentBody   string
	Swarm       bool
}

// AllScenarios lists the standard common benchmark scenarios.
var AllScenarios = []ScenarioSpec{
	{
		Name:        "standard_turn",
		Description: "Standard 1st turn (templates: identity.tmpl with generic-system-prompt, guidelines.tmpl, communication_style.tmpl, etc.; unsandboxed 12 tools)",
		Options: agyprompt.DefaultOptions(
			"/tmp/workspace",
			"/tmp/appdata",
			"00000000-0000-0000-0000-000000000000",
		),
		Sandboxed: false,
	},
	{
		Name:        "sandboxed_turn",
		Description: "Sandboxed mode (--sandbox): terminal_sandbox.txt injected, run_command has BypassSandbox",
		Options: func() agyprompt.Options {
			o := agyprompt.DefaultOptions("/tmp/workspace", "/tmp/appdata", "00000000-0000-0000-0000-000000000000")
			o.Sandboxed = true
			return o
		}(),
		Sandboxed: true,
	},
	{
		Name:        "clean_no_rules",
		Description: "Clean workspace without AGENTS.md: user_rules omitted, skills immediately precedes user_information",
		Options: func() agyprompt.Options {
			o := agyprompt.DefaultOptions("/tmp/workspace", "/tmp/appdata", "00000000-0000-0000-0000-000000000000")
			o.UserRules = nil
			return o
		}(),
		Sandboxed: false,
	},
	{
		Name:        "custom_user_rules",
		Description: "Workspace with AGENTS.md user rules: <user_rules> rendered after <skills>, before <user_information>",
		Options: func() agyprompt.Options {
			o := agyprompt.DefaultOptions("/tmp/workspace", "/tmp/appdata", "00000000-0000-0000-0000-000000000000")
			o.UserRules = []agyprompt.UserRule{
				{
					Path:    "/tmp/workspace/AGENTS.md",
					Content: "# Project Rules\nStrictly follow architecture tiers.",
				},
			}
			return o
		}(),
		Sandboxed: false,
	},
	{
		Name:        "planning_mode",
		Description: "Planning mode (/plan): planning_mode_artifacts.tmpl appended to <artifacts>",
		Options: func() agyprompt.Options {
			o := agyprompt.DefaultOptions("/tmp/workspace", "/tmp/appdata", "00000000-0000-0000-0000-000000000000")
			o.PlanningMode = true
			return o
		}(),
		Sandboxed: false,
	},
	{
		Name:        "autonomous_mode",
		Description: "Autonomous mode: autonomous branches active in identity.tmpl and communication_style.tmpl",
		Options: func() agyprompt.Options {
			o := agyprompt.DefaultOptions("/tmp/workspace", "/tmp/appdata", "00000000-0000-0000-0000-000000000000")
			o.Autonomous = true
			return o
		}(),
		Sandboxed: false,
	},
	{
		Name:        "subagent_turn",
		Description: "Delegated subagent turn: agentBlocks splitting, isolated agent_messaging.txt, restricted tools",
		Options: agyprompt.DefaultOptions(
			"/tmp/workspace",
			"/tmp/appdata",
			"00000000-0000-0000-0000-000000000000",
		),
		Sandboxed: false,
		Subagent:  true,
		AgentBody: "# Role: Weather Researcher\nInvestigate weather in Shanghai.",
	},
	{
		Name:        "compaction_turn",
		Description: "Turn resuming after compaction: <CONTEXT_SUMMARY> and # Resuming from a compaction replace prior messages",
		Options: agyprompt.DefaultOptions(
			"/tmp/workspace",
			"/tmp/appdata",
			"00000000-0000-0000-0000-000000000000",
		),
		Sandboxed: false,
	},
	{
		Name:        "compaction_request",
		Description: "The summarization request sent to the model during compaction: keeps Antigravity instructions and appends 7-section continuation summary as last turn",
		Options: agyprompt.DefaultOptions(
			"/tmp/workspace",
			"/tmp/appdata",
			"00000000-0000-0000-0000-000000000000",
		),
		Sandboxed: false,
	},
	{
		Name:        "swarm_coordinator_turn",
		Description: "Swarm coordinator turn (/swarm <task>): native_swarm skill and swarm monitor skills, plus swarm tool surface with 6 actions",
		Options: func() agyprompt.Options {
			o := agyprompt.DefaultOptions("/tmp/workspace", "/tmp/appdata", "00000000-0000-0000-0000-000000000000")
			o.Skills = []agyprompt.Skill{
				{
					Name:        "swarm",
					Path:        "/Users/gear/.gemini/antigravity-cli/builtin/skills/native_swarm/SKILL.md",
					Description: "(Preview & Quota Heavy) Run a swarm: several agents working in parallel on one task, each in its own conversation, collaborating through a shared message board. Use when the work splits into pieces that can be done at the same time, or when a second pair of eyes should check the first pair's work.",
				},
				{
					Name:        "swarm_monitor",
					Path:        "/Users/gear/.gemini/antigravity-cli/builtin/skills/swarm_monitor/SKILL.md",
					Description: "ONLY for the native-swarm worker whose brief says it is the monitor. On a timer, it checks whether the whole swarm has stalled and calls on the one member who can unstick it. Coordinators and other workers: do not load this.",
				},
			}
			return o
		}(),
		Sandboxed: false,
		Swarm:     true,
	},
}

// BuildReferenceWire generates the ground-truth wire payload directly by executing
// the authentic templates in internal/app/prompt/agytemplates and model declarations.
func BuildReferenceWire(sc ScenarioSpec) *WirePayload {
	var promptText string
	if sc.Subagent {
		promptText = agyprompt.BuildAgent(sc.AgentBody, sc.Options)
	} else {
		promptText = agyprompt.Build(sc.Options)
	}

	modelDecls := surface.ModelDeclarationsFor(sc.Sandboxed)
	if sc.Swarm {
		modelDecls = append(modelDecls, surface.SwarmDeclaration())
	}
	var tools []FunctionDecl
	for _, md := range modelDecls {
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

	var contents []ContentTurn
	if sc.Name == "compaction_request" {
		contents = []ContentTurn{
			{
				Role: "user",
				Parts: []ContentPart{
					{Text: "Please analyze the code structure."},
				},
			},
			{
				Role: "model",
				Parts: []ContentPart{
					{Text: "I will read the source files now."},
				},
			},
			{
				Role: "user",
				Parts: []ContentPart{
					{Text: compaction.SummarizePrompt + compaction.OnDemandCompactionSuffix},
				},
			},
		}
	} else if sc.Name == "compaction_turn" {
		resumeText := compaction.ResumingBlock([]string{"Please analyze the code structure."}, "<summary>\n## 1. Task Overview\nAnalyzed code structure.\n</summary>")
		contents = []ContentTurn{
			{
				Role: "user",
				Parts: []ContentPart{
					{Text: compaction.FormatContextSummary(resumeText, time.Now()) + "\n\nContinue with the next steps."},
				},
			},
		}
	}

	return &WirePayload{
		Model:             "gemini-3.8-flash-high",
		Project:           "aicode-consumers",
		RequestType:       "agent",
		SystemInstruction: promptText,
		Tools:             tools,
		Contents:          contents,
	}
}

// GetScenario returns a scenario spec by name.
func GetScenario(name string) (*ScenarioSpec, error) {
	for _, sc := range AllScenarios {
		if sc.Name == name {
			return &sc, nil
		}
	}
	return nil, fmt.Errorf("unknown scenario %q (available: standard_turn, sandboxed_turn, clean_no_rules, custom_user_rules, planning_mode, autonomous_mode, subagent_turn)", name)
}
