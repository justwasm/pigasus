package cliapp

import (
	"testing"

	"github.com/justwasm/pigasus/internal/core/policy/permissions"
)

func TestPrependSlashPlanIfNeeded(t *testing.T) {
	// ModeAcceptEdits or ModeAuto should not prepend /plan
	if got := PrependSlashPlanIfNeeded("hello world", permissions.ModeAcceptEdits); got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
	if got := PrependSlashPlanIfNeeded("hello world", permissions.ModeAuto); got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}

	// ModePlan with regular prompt should prepend /plan
	if got := PrependSlashPlanIfNeeded("hello world", permissions.ModePlan); got != "/plan hello world" {
		t.Errorf("expected '/plan hello world', got %q", got)
	}

	// ModePlan with existing slash command should not prepend /plan
	slashPrompts := []string{
		"/plan hello",
		"/grill-me",
		"/goal finish task",
		"/help",
		"/custom-cmd args",
	}
	for _, p := range slashPrompts {
		if got := PrependSlashPlanIfNeeded(p, permissions.ModePlan); got != p {
			t.Errorf("PrependSlashPlanIfNeeded(%q, ModePlan) = %q, want %q", p, got, p)
		}
	}
}

func TestSlashPlanModeSwitchAndStacking(t *testing.T) {
	m := newRunMode(nil, "", "Gemini 3.8 Flash (High)", "", t.TempDir(), true)
	m.mode = permissions.ModeAuto

	// Bare /plan switches mode to ModePlan and sends planning prompt
	handled, cmd := m.slashPlan("")
	if !handled || cmd == nil {
		t.Errorf("bare /plan: handled=%v, cmd=%v", handled, cmd)
	}
	if m.mode != permissions.ModePlan {
		t.Errorf("m.mode = %q, want %q", m.mode, permissions.ModePlan)
	}
	if m.input.Placeholder != permissions.ModeBanner(permissions.ModePlan) {
		t.Errorf("placeholder = %q, want %q", m.input.Placeholder, permissions.ModeBanner(permissions.ModePlan))
	}

	// /plan with prompt arguments starts model turn
	handled, cmd = m.slashPlan("architect system")
	if !handled || cmd == nil {
		t.Errorf("/plan prompt: handled=%v, cmd=%v", handled, cmd)
	}

	// Verify promptOptions sets PlanningMode
	opts := m.promptOptions()
	if !opts.PlanningMode {
		t.Errorf("expected opts.PlanningMode=true when m.mode==ModePlan")
	}

	// ModeAuto should set PlanningMode=false
	m.mode = permissions.ModeAuto
	optsAuto := m.promptOptions()
	if optsAuto.PlanningMode {
		t.Errorf("expected opts.PlanningMode=false when m.mode==ModeAuto")
	}
}
