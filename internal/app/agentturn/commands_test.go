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
