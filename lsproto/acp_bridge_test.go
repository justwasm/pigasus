package lsproto

import (
	"context"
	"testing"
	"time"
)

func TestACPBridge_Initialize(t *testing.T) {
	codec, err := New()
	if err != nil {
		t.Fatalf("New codec: %v", err)
	}

	bridge, err := NewACPBridge(codec)
	if err != nil {
		t.Fatalf("NewACPBridge: %v", err)
	}

	ctx := context.Background()

	// 1. Rejects invalid version
	_, err = bridge.Initialize(ctx, ACPInitializeRequest{ProtocolVersion: 0})
	if err == nil {
		t.Fatal("expected error on protocol version 0, got nil")
	}

	// 2. Accepts valid version 1
	resp, err := bridge.Initialize(ctx, ACPInitializeRequest{
		ProtocolVersion: 1,
		ClientInfo: ACPImplementation{
			Name:    "zed",
			Version: "0.180.0",
		},
		Capabilities: ACPClientCapabilities{
			Streaming: true,
		},
	})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	if resp.ProtocolVersion != 1 {
		t.Errorf("expected ProtocolVersion=1, got %d", resp.ProtocolVersion)
	}
	if resp.AgentInfo.Name != "pigasus-lsproto-acp" {
		t.Errorf("expected agent name pigasus-lsproto-acp, got %s", resp.AgentInfo.Name)
	}
	if !resp.AgentCapabilities.PromptSession || !resp.AgentCapabilities.StreamingEvents {
		t.Errorf("expected PromptSession and StreamingEvents capabilities to be true")
	}

	hasStreamAgentState := false
	for _, m := range resp.AgentCapabilities.LSPMethods {
		if m == "StreamAgentStateUpdates" {
			hasStreamAgentState = true
			break
		}
	}
	if !hasStreamAgentState {
		t.Errorf("expected StreamAgentStateUpdates in LSPMethods, got %v", resp.AgentCapabilities.LSPMethods)
	}
}

func TestACPBridge_SessionAndPromptFlow(t *testing.T) {
	bridge, err := NewACPBridge(nil)
	if err != nil {
		t.Fatalf("NewACPBridge: %v", err)
	}

	ctx := context.Background()

	// 1. Create new session
	sessResp, err := bridge.NewSession(ctx, ACPNewSessionRequest{
		SessionID: "sess-ide-001",
		Workspace: "/test/workspace",
		Model:     "gemini-3.8-flash-high",
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sessResp.SessionID != "sess-ide-001" {
		t.Errorf("expected sessionId sess-ide-001, got %s", sessResp.SessionID)
	}

	// 2. Subscribe to streaming events
	events, unsub, err := bridge.Subscribe(sessResp.SessionID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsub()

	// 3. Prompt session
	promptResp, err := bridge.Prompt(ctx, ACPPromptRequest{
		SessionID: sessResp.SessionID,
		Prompt:    "Inspect architecture charter",
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if promptResp.Status != "started" {
		t.Errorf("expected status started, got %s", promptResp.Status)
	}

	// 4. Verify initial state change event was received
	select {
	case ev := <-events:
		if ev.Type != ACPEventStateChange || ev.State != "working" {
			t.Errorf("expected state_change to working, got %+v", ev)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for state_change event")
	}

	// 5. Emit custom text and tool call events
	bridge.EmitEvent(sessResp.SessionID, ACPEvent{
		TurnID: promptResp.TurnID,
		Type:   ACPEventTextDelta,
		Text:   "Checking AGENTS.md...",
	})

	select {
	case ev := <-events:
		if ev.Type != ACPEventTextDelta || ev.Text != "Checking AGENTS.md..." {
			t.Errorf("expected text_delta event, got %+v", ev)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for text_delta event")
	}

	// 6. Test proto-JSON serialization
	jsonBytes, err := bridge.FormatSessionUpdateProtoJSON(ACPEvent{
		SessionID: sessResp.SessionID,
		TurnID:    promptResp.TurnID,
		Type:      ACPEventToolCall,
		ToolCall: &ACPToolCall{
			Name:   "read_file",
			Args:   `{"path":"/test/workspace/AGENTS.md"}`,
			Status: "completed",
		},
	})
	if err != nil {
		t.Fatalf("FormatSessionUpdateProtoJSON: %v", err)
	}
	if len(jsonBytes) == 0 {
		t.Fatal("expected non-empty proto-JSON bytes")
	}
}
