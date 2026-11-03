package subagenttools

import (
	"context"
	"strings"
	"testing"
)

func TestTeamworkSubagentRouting(t *testing.T) {
	tool := &invokeTool{
		deps: Deps{
			Services: subagentTestServices(t),
			Model:    subagentTestModel(),
			CWD:      t.TempDir(),
		},
		defs: NewStore(),
	}

	tests := []struct {
		typeName     string
		wantRoleSub  string
		promptSubstr string
		canWrite     bool
		canSubagent  bool
	}{
		{
			typeName:     "problem_decomposer",
			wantRoleSub:  "Problem Decomposer",
			promptSubstr: "Decompose a validated proof strategy",
			canWrite:     true,
			canSubagent:  false,
		},
		{
			typeName:     "teamwork_decomposer",
			wantRoleSub:  "Problem Decomposer",
			promptSubstr: "Decompose a validated proof strategy",
			canWrite:     true,
			canSubagent:  false,
		},
		{
			typeName:     "strategy_explorer",
			wantRoleSub:  "Strategy Explorer",
			promptSubstr: "Strategy Explorer",
			canWrite:     false,
			canSubagent:  false,
		},
		{
			typeName:     "subproblem_falser",
			wantRoleSub:  "Subproblem Falser",
			promptSubstr: "Subproblem Falser",
			canWrite:     false,
			canSubagent:  false,
		},
		{
			typeName:     "subproblem_solver",
			wantRoleSub:  "Subproblem Solver",
			promptSubstr: "Subproblem Solver",
			canWrite:     true,
			canSubagent:  false,
		},
		{
			typeName:     "swarm_orchestrator",
			wantRoleSub:  "Swarm Orchestrator",
			promptSubstr: "swarm_orchestrator",
			canWrite:     true,
			canSubagent:  true,
		},
		{
			typeName:     "pipeline_conductor",
			wantRoleSub:  "Pipeline Conductor",
			promptSubstr: "pipeline_conductor",
			canWrite:     true,
			canSubagent:  true,
		},
		{
			typeName:     "swarm_follower",
			wantRoleSub:  "Swarm Follower",
			promptSubstr: "swarm_follower",
			canWrite:     true,
			canSubagent:  false,
		},
		{
			typeName:     "swarm_synthesizer",
			wantRoleSub:  "Swarm Synthesizer",
			promptSubstr: "swarm_synthesizer",
			canWrite:     true,
			canSubagent:  false,
		},
		{
			typeName:     "interviewer",
			wantRoleSub:  "Teamwork Interviewer",
			promptSubstr: "interviewer",
			canWrite:     false,
			canSubagent:  false,
		},
		{
			typeName:     "dependency_auditor",
			wantRoleSub:  "Dependency Auditor",
			promptSubstr: "dependency_auditor",
			canWrite:     true,
			canSubagent:  false,
		},
		{
			typeName:     "victory_auditor",
			wantRoleSub:  "Victory Auditor",
			promptSubstr: "victory_auditor",
			canWrite:     true,
			canSubagent:  false,
		},
		{
			typeName:     "swe_light_orchestrator",
			wantRoleSub:  "SWE Light Orchestrator",
			promptSubstr: "SWE Light",
			canWrite:     true,
			canSubagent:  true,
		},
		{
			typeName:     "teamwork_swe",
			wantRoleSub:  "SWE Light Orchestrator",
			promptSubstr: "SWE Light",
			canWrite:     true,
			canSubagent:  true,
		},
		{
			typeName:     "document_review_orchestrator",
			wantRoleSub:  "Document Review Orchestrator",
			promptSubstr: "document_review_orchestrator",
			canWrite:     true,
			canSubagent:  true,
		},
		{
			typeName:     "teamwork_document",
			wantRoleSub:  "Document Review Orchestrator",
			promptSubstr: "document_review_orchestrator",
			canWrite:     true,
			canSubagent:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.typeName, func(t *testing.T) {
			inv, handled, _, err := tool.resolve(context.Background(), invokeArgs{
				TypeName: tc.typeName,
				Prompt:   "Execute assigned objective",
			})
			if err != nil {
				t.Fatalf("resolve failed: %v", err)
			}
			if handled {
				t.Fatalf("expected delegation invocation, got handled=true")
			}
			if !strings.Contains(inv.Role, tc.wantRoleSub) {
				t.Errorf("inv.Role = %q, want substring %q", inv.Role, tc.wantRoleSub)
			}
			if !strings.Contains(inv.SystemPrompt, tc.promptSubstr) {
				t.Errorf("inv.SystemPrompt missing %q:\n%s", tc.promptSubstr, inv.SystemPrompt)
			}
			if inv.Grant.Write != tc.canWrite {
				t.Errorf("grant.Write = %v, want %v", inv.Grant.Write, tc.canWrite)
			}
			if inv.Grant.Subagent != tc.canSubagent {
				t.Errorf("grant.Subagent = %v, want %v", inv.Grant.Subagent, tc.canSubagent)
			}
		})
	}
}

func TestTeamworkRolesCannotBeShadowedOrRedefined(t *testing.T) {
	tool := &invokeTool{
		defs: NewStore(),
	}
	// Try shadowing via direct put
	tool.defs.Put("problem_decomposer", Invocation{SystemPrompt: "impostor"})
	if _, ok := tool.definition("problem_decomposer"); ok {
		t.Fatalf("problem_decomposer was shadowed by custom definition")
	}

	dt := &defineTool{defs: NewStore()}
	reservedTeamwork := []string{
		"problem_decomposer", "strategy_explorer", "subproblem_falser",
		"swarm_orchestrator", "pipeline_conductor", "teamwork_swe",
	}
	for _, name := range reservedTeamwork {
		t.Run("define_"+name, func(t *testing.T) {
			params := `{"Name":"` + name + `","SystemPrompt":"You are an impostor"}`
			_, err := dt.Execute(context.Background(), "call-1", []byte(params), nil)
			if err == nil {
				t.Fatalf("expected error when redefining teamwork role %q, got nil", name)
			}
			if !strings.Contains(err.Error(), "reserved built-in subagent name") {
				t.Fatalf("unexpected error message: %v", err)
			}
		})
	}
}

