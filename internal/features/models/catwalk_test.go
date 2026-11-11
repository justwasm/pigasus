package models

import (
	"strings"
	"testing"
)

func TestCatwalkProviders_LoadsEmbeddedPresets(t *testing.T) {
	all := CatwalkProviders()
	if len(all) == 0 {
		t.Fatal("CatwalkProviders returned empty slice")
	}

	// Should contain standard known providers
	foundDeepseek := false
	foundAnthropic := false
	foundOpenrouter := false

	for _, p := range all {
		switch string(p.ID) {
		case "deepseek":
			foundDeepseek = true
		case "anthropic":
			foundAnthropic = true
		case "openrouter":
			foundOpenrouter = true
		}
	}

	if !foundDeepseek {
		t.Error("Catwalk missing deepseek provider")
	}
	if !foundAnthropic {
		t.Error("Catwalk missing anthropic provider")
	}
	if !foundOpenrouter {
		t.Error("Catwalk missing openrouter provider")
	}
}

func TestPresetProviderConfig_MapsDeepSeek(t *testing.T) {
	p, ok := PresetProviderConfig("deepseek")
	if !ok {
		t.Fatal("PresetProviderConfig(deepseek) returned false")
	}
	if p.ID != "deepseek" {
		t.Errorf("ID = %q, want deepseek", p.ID)
	}
	if !strings.Contains(p.BaseURL, "deepseek.com") && !strings.Contains(p.APIURL, "deepseek.com") {
		t.Errorf("expected deepseek.com in endpoint, got BaseURL=%q, APIURL=%q", p.BaseURL, p.APIURL)
	}
	if len(p.Models) == 0 {
		t.Fatal("expected deepseek to have preset models")
	}
	foundModel := false
	for _, m := range p.Models {
		if strings.HasPrefix(m.ID, "deepseek-") {
			foundModel = true
			break
		}
	}
	if !foundModel {
		t.Errorf("expected deepseek to have deepseek-* models, got models: %+v", p.Models)
	}
}

func TestPresetProviderConfig_MapsAnthropic(t *testing.T) {
	p, ok := PresetProviderConfig("anthropic")
	if !ok {
		t.Fatal("PresetProviderConfig(anthropic) returned false")
	}
	if p.Type != ProviderTypeAnthropic {
		t.Errorf("expected Anthropic wire type, got %q", p.Type)
	}
	if len(p.Models) == 0 {
		t.Fatal("expected anthropic to have models")
	}
}

func TestCatwalkAuthMethod(t *testing.T) {
	if got := CatwalkAuthMethod("copilot"); got != "browser" {
		t.Errorf("CatwalkAuthMethod(copilot) = %q, want browser", got)
	}
	if got := CatwalkAuthMethod("hyper"); got != "browser" {
		t.Errorf("CatwalkAuthMethod(hyper) = %q, want browser", got)
	}
	if got := CatwalkAuthMethod("google-antigravity"); got != "browser" {
		t.Errorf("CatwalkAuthMethod(google-antigravity) = %q, want browser", got)
	}
	if got := CatwalkAuthMethod("openai"); got != "both" {
		t.Errorf("CatwalkAuthMethod(openai) = %q, want both", got)
	}
	if got := CatwalkAuthMethod("deepseek"); got != "api_key" {
		t.Errorf("CatwalkAuthMethod(deepseek) = %q, want api_key", got)
	}
	if got := CatwalkAuthMethod("gaterouter"); got != "api_key" {
		t.Errorf("CatwalkAuthMethod(gaterouter) = %q, want api_key", got)
	}
	if got := CatwalkAuthMethod("stepfun"); got != "api_key" {
		t.Errorf("CatwalkAuthMethod(stepfun) = %q, want api_key", got)
	}
}

func TestNewPresets_GateRouterAndStepFun(t *testing.T) {
	// GateRouter
	gp, ok := FindCatwalkProvider("gaterouter")
	if !ok {
		t.Fatal("FindCatwalkProvider(gaterouter) returned false")
	}
	if gp.Name != "GateRouter" {
		t.Errorf("Name = %q, want GateRouter", gp.Name)
	}
	gcfg, ok := PresetProviderConfig("gaterouter")
	if !ok {
		t.Fatal("PresetProviderConfig(gaterouter) returned false")
	}
	if gcfg.BaseURL != "https://api.gaterouter.ai/openai/v1" {
		t.Errorf("gaterouter BaseURL = %q, want https://api.gaterouter.ai/openai/v1", gcfg.BaseURL)
	}
	if len(gcfg.Models) < 40 {
		t.Errorf("gaterouter models count = %d, want >= 40", len(gcfg.Models))
	}

	// StepFun
	sp, ok := FindCatwalkProvider("stepfun")
	if !ok {
		t.Fatal("FindCatwalkProvider(stepfun) returned false")
	}
	if sp.Name != "StepFun" {
		t.Errorf("Name = %q, want StepFun", sp.Name)
	}
	scfg, ok := PresetProviderConfig("stepfun")
	if !ok {
		t.Fatal("PresetProviderConfig(stepfun) returned false")
	}
	if scfg.BaseURL != "https://api.stepfun.com/step_plan/v1" {
		t.Errorf("stepfun BaseURL = %q, want https://api.stepfun.com/step_plan/v1", scfg.BaseURL)
	}
	if len(scfg.Models) < 4 {
		t.Errorf("stepfun models count = %d, want >= 4", len(scfg.Models))
	}

	// Google Antigravity
	ap, ok := FindCatwalkProvider("google-antigravity")
	if !ok {
		t.Fatal("FindCatwalkProvider(google-antigravity) returned false")
	}
	if ap.Name != "Google Antigravity" {
		t.Errorf("Name = %q, want Google Antigravity", ap.Name)
	}
	acfg, ok := PresetProviderConfig("google-antigravity")
	if !ok {
		t.Fatal("PresetProviderConfig(google-antigravity) returned false")
	}
	if acfg.Type != "cloudcode" {
		t.Errorf("google-antigravity Type = %q, want cloudcode", acfg.Type)
	}
	if len(acfg.Models) != 8 {
		t.Errorf("google-antigravity models count = %d, want 8", len(acfg.Models))
	}

	// Also test "antigravity" alias
	acfg2, ok := PresetProviderConfig("antigravity")
	if !ok {
		t.Fatal("PresetProviderConfig(antigravity) returned false")
	}
	if len(acfg2.Models) != 8 {
		t.Errorf("antigravity models count = %d, want 8", len(acfg2.Models))
	}
}

func TestHyper_PresetAndConfig(t *testing.T) {
	// Hyper should be found in Catwalk presets
	p, ok := FindCatwalkProvider("hyper")
	if !ok {
		t.Fatal("FindCatwalkProvider(hyper) returned false")
	}
	if p.Name != "Charm Hyper" {
		t.Errorf("Name = %q, want Charm Hyper", p.Name)
	}

	cfg, ok := PresetProviderConfig("hyper")
	if !ok {
		t.Fatal("PresetProviderConfig(hyper) returned false")
	}
	if cfg.Type != ProviderTypeOpenAI {
		t.Errorf("Type = %q, want %q", cfg.Type, ProviderTypeOpenAI)
	}
	if cfg.BaseURL != "https://hyper.charm.land/v1" {
		t.Errorf("BaseURL = %q, want https://hyper.charm.land/v1", cfg.BaseURL)
	}
	if len(cfg.Models) == 0 {
		t.Fatal("expected hyper to have models")
	}

	// Should contain prism router model
	m, ok := FindCatwalkModel("hyper", "prism")
	if !ok {
		t.Fatal("FindCatwalkModel(hyper, prism) returned false")
	}
	if m.ID != "prism" {
		t.Errorf("prism model ID = %q, want prism", m.ID)
	}
}

func TestFindCatwalkModel(t *testing.T) {
	m, ok := FindCatwalkModel("deepseek", "deepseek-v4-flash")
	if !ok {
		t.Fatal("FindCatwalkModel(deepseek, deepseek-v4-flash) not found")
	}
	if m.ID != "deepseek-v4-flash" {
		t.Errorf("expected deepseek-v4-flash, got %q", m.ID)
	}
	if m.ContextWindow <= 0 {
		t.Errorf("expected positive context window, got %d", m.ContextWindow)
	}
}
