// default_effort_test.go — the default-thinking-level chain.
//
// Pins AGY 1.3.4 changelog line 26:
//
//	"Changed --model and /model to accept a base model name without --effort:
//	 instead of failing with an error asking for --effort, the CLI now picks
//	 the model's default thinking level (the server's default for that model,
//	 otherwise medium, otherwise the lowest available)."
//
// Cycle 346: these four cases lived in internal/cliapp/model_test.go,
// driving the cliapp-local `resolveDefaultEffort` wrapper. The wrapper is
// gone (the call sites name internal/modelcatalog directly) and the
// cases moved here, with the code they cover.
package catalog

import "testing"

// TestDefaultEffortServerDefault pins the chain's first tier: when
// spec.def is set and present in spec.levels, the chain returns it
// unchanged. This is the canonical gemini-3.8-flash path — the server
// default is "high", which is in [low, medium, high].
//
// Recovered: --model gemini-3.8-flash -> gemini-3.8-flash-high.
func TestDefaultEffortServerDefault(t *testing.T) {
	got := ResolveDefaultEffort(BaseModelSpec{Levels: []string{"low", "medium", "high"}, Def: "high"})
	if got != "high" {
		t.Errorf("resolveDefaultEffort(server-default high) = %q, want %q", got, "high")
	}

	// gpt-oss-120b's server default is also its only option.
	got = ResolveDefaultEffort(BaseModelSpec{Levels: []string{"medium"}, Def: "medium"})
	if got != "medium" {
		t.Errorf("resolveDefaultEffort(server-default medium) = %q, want %q", got, "medium")
	}
}

// TestDefaultEffortMediumFallback pins the chain's second tier: when
// spec.def is empty (or refers to a tier the model does not expose), the
// chain picks "medium" if the model exposes it. This is the future
// "baseModelEfforts entry with def unset" path.
func TestDefaultEffortMediumFallback(t *testing.T) {
	// def == "" — server has no preference, chain falls to medium.
	got := ResolveDefaultEffort(BaseModelSpec{Levels: []string{"low", "medium", "high"}, Def: ""})
	if got != "medium" {
		t.Errorf("resolveDefaultEffort(empty def + levels w/ medium) = %q, want %q", got, "medium")
	}

	// def set to a tier not in levels — chain ignores the bad def and falls to medium.
	got = ResolveDefaultEffort(BaseModelSpec{Levels: []string{"low", "medium", "high"}, Def: "xhigh"})
	if got != "medium" {
		t.Errorf("resolveDefaultEffort(invalid def + levels w/ medium) = %q, want %q (xhigh -> medium)", got, "medium")
	}
}

// TestDefaultEffortLowestFallback pins the chain's third tier: when
// neither the server default nor medium applies, the chain returns the
// first element of spec.levels. The reference's levels slice is in
// ascending order, so the first element is the lowest available.
func TestDefaultEffortLowestFallback(t *testing.T) {
	// def empty, medium absent — chain returns the lowest available.
	got := ResolveDefaultEffort(BaseModelSpec{Levels: []string{"low", "high"}, Def: ""})
	if got != "low" {
		t.Errorf("resolveDefaultEffort(no def, no medium, [low,high]) = %q, want %q", got, "low")
	}

	// def set to a tier absent from levels, medium also absent — chain
	// ignores the bad def and returns the lowest available.
	got = ResolveDefaultEffort(BaseModelSpec{Levels: []string{"low", "high"}, Def: "xhigh"})
	if got != "low" {
		t.Errorf("resolveDefaultEffort(bad def, no medium, [low,high]) = %q, want %q", got, "low")
	}
}

// TestDefaultEffortEmptyLevels: an empty spec.levels means the base
// model has no thinking tier to pick (e.g. baked-in "thinking" models
// that ResolveEffectiveModel handles separately). The helper returns
// "" and ResolveModelSelection falls through to the catalogue lookup.
func TestDefaultEffortEmptyLevels(t *testing.T) {
	got := ResolveDefaultEffort(BaseModelSpec{Levels: nil, Def: "high"})
	if got != "" {
		t.Errorf("resolveDefaultEffort(nil levels, def=high) = %q, want %q", got, "")
	}
	got = ResolveDefaultEffort(BaseModelSpec{Levels: []string{}, Def: ""})
	if got != "" {
		t.Errorf("resolveDefaultEffort([] levels, def=empty) = %q, want %q", got, "")
	}
}
