package agentturn

import (
	"testing"
)

func TestClaudeRemote_AvailableCommands(t *testing.T) {
	r := &ClaudeRemote{}
	cmds := r.AvailableCommands()
	if len(cmds) == 0 {
		t.Fatalf("expected non-empty available commands for ClaudeRemote")
	}

	foundCost := false
	foundReview := false
	for _, c := range cmds {
		if c.Name == "cost" {
			foundCost = true
		}
		if c.Name == "review" {
			foundReview = true
			if c.InputHint != "[branch]" {
				t.Errorf("expected review InputHint to be [branch], got %q", c.InputHint)
			}
		}
	}
	if !foundCost || !foundReview {
		t.Errorf("expected cost and review in ClaudeRemote commands: %+v", cmds)
	}
}

func TestCodexRemote_AvailableCommands(t *testing.T) {
	r := &CodexRemote{}
	cmds := r.AvailableCommands()
	if len(cmds) == 0 {
		t.Fatalf("expected non-empty available commands for CodexRemote")
	}

	foundReview := false
	for _, c := range cmds {
		if c.Name == "review" {
			foundReview = true
			if c.InputHint != "[files]" {
				t.Errorf("expected review InputHint to be [files], got %q", c.InputHint)
			}
		}
	}
	if !foundReview {
		t.Errorf("expected review in CodexRemote commands: %+v", cmds)
	}
}

func TestACPRemote_AvailableCommands(t *testing.T) {
	r := &ACPRemote{}
	if len(r.AvailableCommands()) != 0 {
		t.Errorf("expected initial ACPRemote commands to be empty")
	}

	r.mu.Lock()
	r.availableCmds = []CommandInfo{
		{Name: "deploy", Description: "Deploy service", InputHint: "<target>"},
	}
	r.mu.Unlock()

	cmds := r.AvailableCommands()
	if len(cmds) != 1 || cmds[0].Name != "deploy" {
		t.Fatalf("expected 1 command 'deploy', got: %+v", cmds)
	}
}
