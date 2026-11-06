package tui

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
		engine: engineState{engine: mockEng},
	}
	initRunModeInput(m)
	m.composer.picker = m.newSlashPicker()

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

	// When engine is ACP, badge is "acp"
	mockACPEng := &mockACPAgentWithCommands{
		mockAgentWithCommands: mockAgentWithCommands{
			commands: []agentturn.CommandInfo{
				{Name: "test", Description: "Run tests"},
			},
		},
	}
	m.engine.engine = mockACPEng
	acpCmds := m.engineSlashCommands()
	if len(acpCmds) != 1 || acpCmds[0].Badge != "acp" {
		t.Errorf("expected acp badge for ACP engine, got: %+v", acpCmds)
	}
}

type mockACPAgentWithCommands struct {
	mockAgentWithCommands
}

func (m *mockACPAgentWithCommands) EngineName() string {
	return "acp"
}

func TestHandleSlashCommand_AgentCommandPassThrough(t *testing.T) {
	mockEng := &mockAgentWithCommands{
		commands: []agentturn.CommandInfo{
			{Name: "review", Description: "Review code changes"},
		},
	}

	m := &runMode{
		engine: engineState{engine: mockEng},
	}
	initRunModeInput(m)
	m.composer.picker = m.newSlashPicker()
	initRunModeInput(m)
	m.composer.picker = m.newSlashPicker()

	// 1. Direct invocation of dynamic agent command /review
	handled, _ := m.handleSlashCommand("/review main")
	if !handled {
		t.Errorf("expected /review main to be handled")
	}
	// Verify that it sent as prompt rather than responding with "Unknown command"
	for _, line := range m.engine.response {
		if strings.Contains(line, "Unknown command") {
			t.Errorf("expected /review not to produce Unknown command, got response: %v", m.engine.response)
		}
	}
	if len(m.engine.response) == 0 || !strings.Contains(m.engine.response[0], "> /review main") {
		t.Errorf("expected response to record prompt '> /review main', got: %v", m.engine.response)
	}

	// 2. /agent namespaced invocation
	m.engine.response = nil
	handled, _ = m.handleSlashCommand("/agent deploy staging")
	if !handled {
		t.Errorf("expected /agent deploy staging to be handled")
	}
	for _, line := range m.engine.response {
		if strings.Contains(line, "Unknown command") {
			t.Errorf("expected /agent deploy staging not to produce Unknown command, got response: %v", m.engine.response)
		}
	}
	if len(m.engine.response) == 0 || !strings.Contains(m.engine.response[0], "> /deploy staging") {
		t.Errorf("expected response to record prompt '> /deploy staging', got: %v", m.engine.response)
	}
}

func TestAgentCommands_QueueWhileStreaming(t *testing.T) {
	mockEng := &mockAgentWithCommands{
		commands: []agentturn.CommandInfo{
			{Name: "review", Description: "Review code changes"},
		},
	}

	m := &runMode{
		engine: engineState{engine: mockEng},
	}
	initRunModeInput(m)
	m.composer.picker = m.newSlashPicker()
	initRunModeInput(m)
	m.composer.picker = m.newSlashPicker()

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
	m.engine.streaming = true
	m.composer.input.SetValue("/review main")
	cmd := m.submitPrompt()
	if cmd != nil {
		t.Errorf("expected submitPrompt to return nil cmd when queued, got %v", cmd)
	}
	if len(m.session.store.StreamQueue) != 1 || m.session.store.StreamQueue[0] != "/review main" {
		t.Errorf("expected /review main to be in StreamQueue, got %v", m.session.store.StreamQueue)
	}
}

func TestSlashCommand_BackendScopedRejection(t *testing.T) {
	mockEng := &mockAgentWithCommands{
		commands: []agentturn.CommandInfo{
			{Name: "review", Description: "Review code changes"},
		},
	}

	m := &runMode{
		engine: engineState{engine: mockEng},
	}
	initRunModeInput(m)
	m.composer.picker = m.newSlashPicker()

	// 1. Invoking a pigasus-exclusive command (/accounts) on codex engine should be rejected
	handled, _ := m.handleSlashCommand("/accounts")
	if !handled {
		t.Fatalf("expected /accounts to be intercepted and handled")
	}

	// Verify rejection notice in response
	foundNotice := false
	for _, row := range m.engine.response {
		if strings.Contains(row, "/accounts is not supported by the codex backend") {
			foundNotice = true
			break
		}
	}
	if !foundNotice {
		t.Errorf("expected rejection notice in response, got: %v", m.engine.response)
	}

	// 2. Invoking a pigasus-exclusive skill (/science:uv) on codex engine should be rejected
	m.engine.response = nil
	handled, _ = m.handleSlashCommand("/science:uv")
	if !handled {
		t.Fatalf("expected /science:uv to be intercepted and handled")
	}
	foundNotice = false
	for _, row := range m.engine.response {
		if strings.Contains(row, "not supported by the codex backend") {
			foundNotice = true
			break
		}
	}
	if !foundNotice {
		t.Errorf("expected skill rejection notice in response, got: %v", m.engine.response)
	}
}

type mockRemoteEngine struct {
	name     string
	caps     agentturn.Capability
	commands []agentturn.CommandInfo
}

func (m *mockRemoteEngine) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	ch := make(chan agentlink.Frame)
	close(ch)
	return ch, nil
}
func (m *mockRemoteEngine) Close() error { return nil }
func (m *mockRemoteEngine) AvailableCommands() []agentturn.CommandInfo {
	return m.commands
}
func (m *mockRemoteEngine) EngineName() string {
	return m.name
}
func (m *mockRemoteEngine) Capabilities() agentturn.Capability {
	return m.caps
}

func TestSlashTasksAndAgents_OverRemoteACP(t *testing.T) {
	remoteEng := &mockRemoteEngine{
		name: "acp",
		caps: agentturn.CapTasks | agentturn.CapAgents,
		commands: []agentturn.CommandInfo{
			{Name: "/tasks", Description: "View background tasks"},
			{Name: "/agents", Description: "List available custom agents"},
		},
	}

	m := &runMode{
		engine: engineState{engine: remoteEng},
	}
	initRunModeInput(m)
	m.composer.picker = m.newSlashPicker()

	// 1. /tasks over ACP
	handled, _ := m.dispatchSlashLine("/tasks")
	if !handled {
		t.Fatalf("expected /tasks to be handled")
	}
	if m.overlays.panel.kind != 0 {
		t.Errorf("expected no local panel to open, got panel kind %v", m.overlays.panel.kind)
	}
	if len(m.engine.response) == 0 || !strings.Contains(m.engine.response[0], "> /tasks") {
		t.Errorf("expected prompt '> /tasks' recorded in response, got: %v", m.engine.response)
	}

	// 2. /agents over ACP
	m.engine.response = nil
	handled, _ = m.dispatchSlashLine("/agents")
	if !handled {
		t.Fatalf("expected /agents to be handled")
	}
	if m.overlays.panel.kind != 0 {
		t.Errorf("expected no local panel to open, got panel kind %v", m.overlays.panel.kind)
	}
	if len(m.engine.response) == 0 || !strings.Contains(m.engine.response[0], "> /agents") {
		t.Errorf("expected prompt '> /agents' recorded in response, got: %v", m.engine.response)
	}
}
