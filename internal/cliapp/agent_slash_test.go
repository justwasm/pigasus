package cliapp

import (
	"context"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

type mockAgentWithCommands struct {
	commands []agentturn.CommandInfo
}

func (m *mockAgentWithCommands) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	ch := make(chan agentlink.Frame)
	close(ch)
	return ch, nil
}

func (m *mockAgentWithCommands) Close() error {
	return nil
}

func (m *mockAgentWithCommands) AvailableCommands() []agentturn.CommandInfo {
	return m.commands
}

func (m *mockAgentWithCommands) EngineName() string {
	return "codex"
}

func (m *mockAgentWithCommands) Capabilities() agentturn.Capability {
	return agentturn.CapModelSwitch | agentturn.CapReasoningTier
}

func TestEngineSlashCommands_Discovery(t *testing.T) {
	mockEng := &mockAgentWithCommands{
		commands: []agentturn.CommandInfo{
			{Name: "review", Description: "Review code changes", InputHint: "[branch]"},
			{Name: "deploy", Description: "Deploy application", InputHint: "<env>"},
		},
	}

	m := &runMode{
		engine: mockEng,
	}
	initRunModeInput(m)
	m.picker = m.newSlashPicker()

	cmds := m.engineSlashCommands()
	if len(cmds) != 2 {
		t.Fatalf("expected 2 dynamic commands, got %d", len(cmds))
	}

	if cmds[0].Name != "/review" || cmds[0].Source != slashcommands.SourceAgent || cmds[0].Badge != "agent" {
		t.Errorf("unexpected cmds[0]: %+v", cmds[0])
	}
	if !strings.Contains(cmds[0].Description, "[branch]") {
		t.Errorf("expected input hint [branch] in description, got: %q", cmds[0].Description)
	}

	if cmds[1].Name != "/deploy" || cmds[1].Source != slashcommands.SourceAgent || cmds[1].Badge != "agent" {
		t.Errorf("unexpected cmds[1]: %+v", cmds[1])
	}
}

func TestHandleSlashCommand_AgentCommandPassThrough(t *testing.T) {
	mockEng := &mockAgentWithCommands{
		commands: []agentturn.CommandInfo{
			{Name: "review", Description: "Review code changes"},
		},
	}

	m := &runMode{
		engine: mockEng,
	}
	initRunModeInput(m)
	m.picker = m.newSlashPicker()
	initRunModeInput(m)
	m.picker = m.newSlashPicker()

	// 1. Direct invocation of dynamic agent command /review
	handled, _ := m.handleSlashCommand("/review main")
	if !handled {
		t.Errorf("expected /review main to be handled")
	}
	// Verify that it sent as prompt rather than responding with "Unknown command"
	for _, line := range m.response {
		if strings.Contains(line, "Unknown command") {
			t.Errorf("expected /review not to produce Unknown command, got response: %v", m.response)
		}
	}
	if len(m.response) == 0 || !strings.Contains(m.response[0], "> /review main") {
		t.Errorf("expected response to record prompt '> /review main', got: %v", m.response)
	}

	// 2. /agent namespaced invocation
	m.response = nil
	handled, _ = m.handleSlashCommand("/agent deploy staging")
	if !handled {
		t.Errorf("expected /agent deploy staging to be handled")
	}
	for _, line := range m.response {
		if strings.Contains(line, "Unknown command") {
			t.Errorf("expected /agent deploy staging not to produce Unknown command, got response: %v", m.response)
		}
	}
	if len(m.response) == 0 || !strings.Contains(m.response[0], "> /deploy staging") {
		t.Errorf("expected response to record prompt '> /deploy staging', got: %v", m.response)
	}
}

func TestAgentCommands_QueueWhileStreaming(t *testing.T) {
	mockEng := &mockAgentWithCommands{
		commands: []agentturn.CommandInfo{
			{Name: "review", Description: "Review code changes"},
		},
	}

	m := &runMode{
		engine: mockEng,
	}
	initRunModeInput(m)
	m.picker = m.newSlashPicker()
	initRunModeInput(m)
	m.picker = m.newSlashPicker()

	// 1. Check isEngineSlashCommand and slashQueuesWhileStreaming
	if !m.isEngineSlashCommand("review") {
		t.Errorf("expected review to be recognized as engine slash command")
	}
	if !m.isEngineSlashCommand("agent") {
		t.Errorf("expected agent to be recognized as engine slash command")
	}
	if !m.slashQueuesWhileStreaming("/review main") {
		t.Errorf("expected /review main to queue while streaming")
	}
	if !m.slashQueuesWhileStreaming("/agent deploy staging") {
		t.Errorf("expected /agent deploy staging to queue while streaming")
	}

	// 2. Submit while streaming -> should be queued into StreamQueue
	m.streaming = true
	m.input.SetValue("/review main")
	cmd := m.submitPrompt()
	if cmd != nil {
		t.Errorf("expected submitPrompt to return nil cmd when queued, got %v", cmd)
	}
	if len(m.store.StreamQueue) != 1 || m.store.StreamQueue[0] != "/review main" {
		t.Errorf("expected /review main to be in StreamQueue, got %v", m.store.StreamQueue)
	}
}

func TestSlashCommand_BackendScopedRejection(t *testing.T) {
	mockEng := &mockAgentWithCommands{
		commands: []agentturn.CommandInfo{
			{Name: "review", Description: "Review code changes"},
		},
	}

	m := &runMode{
		engine: mockEng,
	}
	initRunModeInput(m)
	m.picker = m.newSlashPicker()

	// 1. Invoking a pigasus-exclusive command (/accounts) on codex engine should be rejected
	handled, _ := m.handleSlashCommand("/accounts")
	if !handled {
		t.Fatalf("expected /accounts to be intercepted and handled")
	}

	// Verify rejection notice in response
	foundNotice := false
	for _, row := range m.response {
		if strings.Contains(row, "/accounts is not supported by the codex backend") {
			foundNotice = true
			break
		}
	}
	if !foundNotice {
		t.Errorf("expected rejection notice in response, got: %v", m.response)
	}

	// 2. Invoking a pigasus-exclusive skill (/science:uv) on codex engine should be rejected
	m.response = nil
	handled, _ = m.handleSlashCommand("/science:uv")
	if !handled {
		t.Fatalf("expected /science:uv to be intercepted and handled")
	}
	foundNotice = false
	for _, row := range m.response {
		if strings.Contains(row, "not supported by the codex backend") {
			foundNotice = true
			break
		}
	}
	if !foundNotice {
		t.Errorf("expected skill rejection notice in response, got: %v", m.response)
	}
}
