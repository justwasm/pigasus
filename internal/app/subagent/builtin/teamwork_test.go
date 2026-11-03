package builtin

import (
	"strings"
	"testing"
)

func TestAllTeamworkRoles(t *testing.T) {
	roles := AllTeamworkRoles()
	if len(roles) < 25 {
		t.Fatalf("expected at least 25 Teamwork roles, got %d", len(roles))
	}

	essentialRoles := []string{
		"problem_decomposer",
		"strategy_explorer",
		"subproblem_falser",
		"subproblem_solver",
		"subproblem_aggregator",
		"exploration_falser",
		"explore_aggregator",
		"decompose_aggregator",
		"section_verifier",
		"surgical_reviser",
		"revise_aggregator",
		"verify_aggregator",
		"revision_decomposer",
		"swarm_orchestrator",
		"swarm_follower",
		"swarm_synthesizer",
		"swarm_blackboard_updater",
		"pipeline_conductor",
		"pipeline_sentinel",
		"pipeline_blackboard_updater",
		"layer_orchestrator",
		"orchestrator_pure",
		"stellar_orchestrator",
		"project_sentinel",
		"interviewer",
		"dependency_auditor",
		"victory_auditor",
		"document_victory_auditor",
		"forensic_auditor",
		"gate_reviewer",
		"gate_aggregator",
		"reviewer_critic",
		"empirical_challenger",
		"test_writer",
		"spec_miner",
		"knowledge_curator",
		"colosseum_proof_verifier",
		"armed_critic",
		"armed_worker",
		"swe_light_orchestrator",
		"swe_light_implementer",
		"swe_light_reviewer",
		"document_ingestor",
		"critic_orchestrator",
		"document_review_orchestrator",
		"document_review_synthesizer",
	}

	for _, name := range essentialRoles {
		role, ok := LookupTeamworkRole(name)
		if !ok || role == nil {
			t.Errorf("essential Teamwork role %q not found in registry", name)
			continue
		}
		if role.Name != name {
			t.Errorf("role.Name = %q, want %q", role.Name, name)
		}
		if role.Role == "" {
			t.Errorf("role %q has empty Role title", name)
		}
		if role.Description == "" {
			t.Errorf("role %q has empty Description", name)
		}
		if !strings.Contains(role.SystemPrompt, "You are a") && !strings.Contains(role.SystemPrompt, "orchestrator") {
			t.Errorf("role %q has invalid system prompt lead:\n%s", name, role.SystemPrompt)
		}
	}
}

func TestLookupTeamworkRole_AliasesAndPrefixes(t *testing.T) {
	cases := []struct {
		query    string
		wantName string
	}{
		{"problem_decomposer", "problem_decomposer"},
		{"teamwork_problem_decomposer", "problem_decomposer"},
		{"teamwork_decomposer", "problem_decomposer"},
		{"teamwork_preview_problem_decomposer", "problem_decomposer"},
		{"PROBLEM_DECOMPOSER", "problem_decomposer"},
		{"  strategy_explorer  ", "strategy_explorer"},
		{"teamwork_strategy_explorer", "strategy_explorer"},
		{"subproblem_falser", "subproblem_falser"},
		{"teamwork_solve_falser", "subproblem_falser"},
		{"swarm_orchestrator", "swarm_orchestrator"},
		{"teamwork_swarm_orchestrator", "swarm_orchestrator"},
		{"teamwork_preview_orchestrator", "swarm_orchestrator"},
		{"pipeline_conductor", "pipeline_conductor"},
		{"teamwork_conductor", "pipeline_conductor"},
		{"teamwork_swe", "swe_light_orchestrator"},
		{"teamwork_document", "document_review_orchestrator"},
	}

	for _, tc := range cases {
		r, ok := LookupTeamworkRole(tc.query)
		if !ok || r == nil {
			t.Errorf("LookupTeamworkRole(%q) returned not found", tc.query)
			continue
		}
		if r.Name != tc.wantName {
			t.Errorf("LookupTeamworkRole(%q) = %q, want %q", tc.query, r.Name, tc.wantName)
		}
	}
}

func TestTeamworkCatalog(t *testing.T) {
	cat := TeamworkCatalog()
	if !strings.Contains(cat, "problem_decomposer:") {
		t.Errorf("TeamworkCatalog missing problem_decomposer:\n%s", cat)
	}
	if !strings.Contains(cat, "swarm_orchestrator:") {
		t.Errorf("TeamworkCatalog missing swarm_orchestrator:\n%s", cat)
	}
	if !strings.Contains(cat, "pipeline_conductor:") {
		t.Errorf("TeamworkCatalog missing pipeline_conductor:\n%s", cat)
	}
}
