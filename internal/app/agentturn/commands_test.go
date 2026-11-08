package agentturn

import (
	"testing"
)

func TestACPRemote_AvailableCommands(t *testing.T) {
	r := &ACPRemote{}
	if len(r.AvailableCommands()) != 0 {
		t.Errorf("expected initial ACPRemote commands to be empty")
	}
	if caps := r.Capabilities(); caps != CapNone {
		t.Errorf("expected CapNone for generic ACP remote, got: %v", caps)
	}

	r.mu.Lock()
	r.availableCmds = []CommandInfo{
		{Name: "deploy", Description: "Deploy service", InputHint: "<target>"},
		{Name: "/tasks", Description: "View background tasks"},
		{Name: "/agents", Description: "List available custom agents"},
		{Name: "model", Description: "Switch model"},
	}
	r.mu.Unlock()

	cmds := r.AvailableCommands()
	if len(cmds) != 4 || cmds[0].Name != "deploy" {
		t.Fatalf("expected 4 commands, got: %+v", cmds)
	}

	caps := r.Capabilities()
	if caps&CapTasks == 0 {
		t.Errorf("expected CapTasks in capabilities, got: %v", caps)
	}
	if caps&CapAgents == 0 {
		t.Errorf("expected CapAgents in capabilities, got: %v", caps)
	}
	if caps&CapModelSwitch == 0 {
		t.Errorf("expected CapModelSwitch in capabilities, got: %v", caps)
	}
}

func TestLocal_ExecuteAction(t *testing.T) {
	loc := NewLocal(nil)

	// Test steer action
	res, err := loc.ExecuteAction(nil, "steer", "guide model")
	if err != nil || !res.Success {
		t.Fatalf("steer action failed: res=%+v, err=%v", res, err)
	}
	steerPending := loc.PendingSteer()
	if len(steerPending) != 1 || steerPending[0] != "guide model" {
		t.Errorf("pending steer = %+v, want ['guide model']", steerPending)
	}

	// Test model action
	res, err = loc.ExecuteAction(nil, "model", "gemini-3.8-flash-high")
	if err != nil || !res.Success {
		t.Fatalf("model action failed: res=%+v, err=%v", res, err)
	}
	loc.mu.Lock()
	ov := loc.overrideModel
	loc.mu.Unlock()
	if ov != "gemini-3.8-flash-high" {
		t.Errorf("overrideModel = %q, want gemini-3.8-flash-high", ov)
	}

	// Test cancel action when no active turn
	res, err = loc.ExecuteAction(nil, "cancel", "")
	if err != nil || !res.Success {
		t.Fatalf("cancel action failed: res=%+v, err=%v", res, err)
	}
}
