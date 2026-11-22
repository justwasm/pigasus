package routing_test

import (
	"encoding/json"
	"testing"

	"github.com/justwasm/pigasus/internal/features/models/routing"
)

func TestSwarmRolesConfig_FullConfig(t *testing.T) {
	rawJSON := `{
		"planner": {
			"model": "o3-mini",
			"reasoning_effort": "high"
		},
		"coder": {
			"model": "claude-3-7-sonnet",
			"temperature": 0.2
		},
		"reviewer": {
			"model": "gpt-4o",
			"temperature": 0.1
		},
		"summarizer": {
			"model": "gemini-2.5-flash"
		}
	}`

	cfg, err := routing.ParseSwarmRolesJSON(rawJSON)
	if err != nil {
		t.Fatalf("ParseSwarmRolesJSON failed: %v", err)
	}

	planner := cfg.ResolveRole(routing.RolePlanner)
	if planner.Model != "o3-mini" || planner.ReasoningEffort != "high" {
		t.Errorf("unexpected planner config: %+v", planner)
	}

	coder := cfg.ResolveRole(routing.RoleCoder)
	if coder.Model != "claude-3-7-sonnet" || coder.Temperature == nil || *coder.Temperature != 0.2 {
		t.Errorf("unexpected coder config: %+v", coder)
	}

	reviewer := cfg.ResolveRole(routing.RoleReviewer)
	if reviewer.Model != "gpt-4o" || reviewer.Temperature == nil || *reviewer.Temperature != 0.1 {
		t.Errorf("unexpected reviewer config: %+v", reviewer)
	}

	summarizer := cfg.ResolveRole(routing.RoleSummarizer)
	if summarizer.Model != "gemini-2.5-flash" {
		t.Errorf("unexpected summarizer config: %+v", summarizer)
	}
}

func TestSwarmRolesConfig_FallbackToDefault(t *testing.T) {
	// Only Coder is explicitly configured, default is set to gpt-4o
	rawJSON := `{
		"coder": {
			"model": "claude-3-7-sonnet"
		},
		"default": {
			"model": "gpt-4o",
			"temperature": 0.5
		}
	}`

	cfg, err := routing.ParseSwarmRolesJSON(rawJSON)
	if err != nil {
		t.Fatalf("ParseSwarmRolesJSON failed: %v", err)
	}

	// Coder uses explicit model
	coder := cfg.ResolveRole(routing.RoleCoder)
	if coder.Model != "claude-3-7-sonnet" {
		t.Errorf("expected coder claude-3-7-sonnet, got %s", coder.Model)
	}

	// Planner, Reviewer, Summarizer must fall back to default
	for _, r := range []routing.RoleName{routing.RolePlanner, routing.RoleReviewer, routing.RoleSummarizer} {
		res := cfg.ResolveRole(r)
		if res.Model != "gpt-4o" {
			t.Errorf("expected role %s to fall back to default gpt-4o, got %s", r, res.Model)
		}
		if res.Temperature == nil || *res.Temperature != 0.5 {
			t.Errorf("expected role %s to inherit temperature 0.5, got %v", r, res.Temperature)
		}
	}
}

func TestSwarmRolesConfig_FallbackToSystemBaselines(t *testing.T) {
	// Completely empty config
	cfg := routing.NewSwarmRolesConfig()

	baselines := routing.DefaultBaselineRoles()

	planner := cfg.ResolveRole(routing.RolePlanner)
	if planner.Model != baselines[routing.RolePlanner].Model {
		t.Errorf("expected planner %s, got %s", baselines[routing.RolePlanner].Model, planner.Model)
	}

	coder := cfg.ResolveRole(routing.RoleCoder)
	if coder.Model != baselines[routing.RoleCoder].Model {
		t.Errorf("expected coder %s, got %s", baselines[routing.RoleCoder].Model, coder.Model)
	}

	reviewer := cfg.ResolveRole(routing.RoleReviewer)
	if reviewer.Model != baselines[routing.RoleReviewer].Model {
		t.Errorf("expected reviewer %s, got %s", baselines[routing.RoleReviewer].Model, reviewer.Model)
	}

	summarizer := cfg.ResolveRole(routing.RoleSummarizer)
	if summarizer.Model != baselines[routing.RoleSummarizer].Model {
		t.Errorf("expected summarizer %s, got %s", baselines[routing.RoleSummarizer].Model, summarizer.Model)
	}
}

func TestSwarmRolesConfig_CaseInsensitivity(t *testing.T) {
	cfg := routing.NewSwarmRolesConfig()
	cfg.Planner = &routing.RoleModelConfig{Model: "my-planner"}
	cfg.Coder = &routing.RoleModelConfig{Model: "my-coder"}

	if got := cfg.ResolveRole("PLANNER").Model; got != "my-planner" {
		t.Errorf("expected PLANNER -> my-planner, got %s", got)
	}
	if got := cfg.ResolveRole("Planner").Model; got != "my-planner" {
		t.Errorf("expected Planner -> my-planner, got %s", got)
	}
	if got := cfg.ResolveRole("  coder  ").Model; got != "my-coder" {
		t.Errorf("expected '  coder  ' -> my-coder, got %s", got)
	}
}

func TestSwarmRolesConfig_CustomRole(t *testing.T) {
	cfg := routing.NewSwarmRolesConfig()
	cfg.CustomRoles["researcher"] = &routing.RoleModelConfig{
		Model: "sonar-deep-research",
	}

	res := cfg.ResolveRole("researcher")
	if res.Model != "sonar-deep-research" {
		t.Errorf("expected custom role researcher to be sonar-deep-research, got %s", res.Model)
	}
}

func TestSwarmRolesConfig_Validation(t *testing.T) {
	cfg := routing.NewSwarmRolesConfig()
	cfg.Planner = &routing.RoleModelConfig{Model: "   "}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error on empty model name in validation")
	}

	cfg.Planner.Model = "valid-model"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected validation success, got: %v", err)
	}
}

func TestSwarmRolesConfig_CloneImmutability(t *testing.T) {
	cfg := routing.NewSwarmRolesConfig()
	temp := float32(0.7)
	cfg.Planner = &routing.RoleModelConfig{
		Model:       "base-planner",
		Temperature: &temp,
		Extra:       map[string]any{"key": "val1"},
	}

	resolved := cfg.ResolveRole(routing.RolePlanner)
	newTemp := float32(0.1)
	resolved.Temperature = &newTemp
	resolved.Extra["key"] = "modified"

	// Original should not be mutated
	if *cfg.Planner.Temperature != float32(0.7) {
		t.Errorf("original temperature was mutated")
	}
	if cfg.Planner.Extra["key"] != "val1" {
		t.Errorf("original extra map was mutated")
	}
}

func TestSwarmRolesConfig_ToMap(t *testing.T) {
	cfg := routing.NewSwarmRolesConfig()
	cfg.Coder = &routing.RoleModelConfig{Model: "custom-coder"}

	m := cfg.ToMap()
	if len(m) != 4 {
		t.Fatalf("expected 4 roles in ToMap, got %d", len(m))
	}
	if m[routing.RoleCoder].Model != "custom-coder" {
		t.Errorf("expected custom-coder, got %s", m[routing.RoleCoder].Model)
	}
	if m[routing.RolePlanner].Model == "" {
		t.Errorf("expected resolved planner model, got empty")
	}
}

func TestSwarmRolesConfig_JSONRoundTrip(t *testing.T) {
	cfg := routing.NewSwarmRolesConfig()
	temp := float32(0.3)
	cfg.Planner = &routing.RoleModelConfig{Model: "model-p", Temperature: &temp}
	cfg.Default = &routing.RoleModelConfig{Model: "model-d"}

	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json marshal failed: %v", err)
	}

	parsed, err := routing.ParseSwarmRoles(b)
	if err != nil {
		t.Fatalf("ParseSwarmRoles failed: %v", err)
	}

	if parsed.Planner.Model != "model-p" || *parsed.Planner.Temperature != 0.3 {
		t.Errorf("mismatch in deserialized planner: %+v", parsed.Planner)
	}
	if parsed.Default.Model != "model-d" {
		t.Errorf("mismatch in deserialized default: %+v", parsed.Default)
	}
}
