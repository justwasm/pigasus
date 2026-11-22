package agentserver

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	"github.com/ironpark/acp-go/acp2"
	schemav1 "github.com/ironpark/acp-go/schema/v1"
	schemav2 "github.com/ironpark/acp-go/schema/v2"
	"github.com/justwasm/pigasus/internal/core/backend"
)

type e2eRecordingV1Client struct {
	acp1.UnimplementedClient
	mu          sync.Mutex
	updates     []*acp1.SessionNotification
	userMsgs    []string
	assistTexts []string
	thoughts    []string
	toolStarts  []string
	usages      []uint64
}

func (c *e2eRecordingV1Client) SessionUpdate(ctx context.Context, n *acp1.SessionNotification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = append(c.updates, n)

	if msg, ok := n.Update.As[schemav1.SessionUpdateAgentMessageChunk](); ok {
		if txt, ok := msg.Content.As[schemav1.ContentBlockText](); ok {
			c.assistTexts = append(c.assistTexts, txt.Text)
		}
	}
	if th, ok := n.Update.As[schemav1.SessionUpdateAgentThoughtChunk](); ok {
		if txt, ok := th.Content.As[schemav1.ContentBlockText](); ok {
			c.thoughts = append(c.thoughts, txt.Text)
		}
	}
	if tc, ok := n.Update.As[schemav1.SessionUpdateToolCall](); ok {
		c.toolStarts = append(c.toolStarts, tc.Title)
	}
	if u, ok := n.Update.As[schemav1.SessionUpdateUsageUpdate](); ok {
		c.usages = append(c.usages, u.Used)
	}
	if um, ok := n.Update.As[schemav1.SessionUpdateUserMessageChunk](); ok {
		if txt, ok := um.Content.As[schemav1.ContentBlockText](); ok {
			c.userMsgs = append(c.userMsgs, txt.Text)
		}
	}
	return nil
}

func (c *e2eRecordingV1Client) RequestPermission(ctx context.Context, req *acp1.RequestPermissionRequest) (*acp1.RequestPermissionResponse, error) {
	// Auto-approve tool calls for end-to-end tests
	return &acp1.RequestPermissionResponse{
		Outcome: schemav1.NewRequestPermissionOutcome(schemav1.RequestPermissionOutcomeSelected{
			OptionID: "allow",
		}),
	}, nil
}

func (c *e2eRecordingV1Client) waitForUpdate(tag string, timeout time.Duration) *acp1.SessionNotification {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, u := range c.updates {
			if u.Update.Tag() == tag {
				c.mu.Unlock()
				return u
			}
		}
		c.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func (c *e2eRecordingV1Client) historySummary() (users []string, assistants []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.userMsgs...), append([]string(nil), c.assistTexts...)
}

func TestACPServer_E2E_Stdio_SessionLifecycle_And_Reconnect(t *testing.T) {
	ws := t.TempDir()
	srv, err := New(Config{
		Model:      "test-model",
		Workspace:  ws,
		BackendFor: func(string) backend.Client { return backend.NewMock() },
	})
	if err != nil {
		t.Fatalf("New server failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// -------------------------------------------------------------
	// 1. First Connection (e.g. Zed / VS Code starts and connects)
	// -------------------------------------------------------------
	agentR1, clientW1 := io.Pipe()
	clientR1, agentW1 := io.Pipe()

	client1 := &e2eRecordingV1Client{}
	clientConn1 := acp1.NewClientSideConnection(func(c *acp1.ClientSideConnection) acp1.Client {
		return client1
	}, acp.NewStdioTransport(clientR1, clientW1))

	go func() {
		_ = srv.ServeACP(ctx, agentW1, agentR1)
	}()
	go func() {
		_ = clientConn1.Start(ctx)
	}()

	initResp, err := clientConn1.Initialize(ctx, &acp1.InitializeRequest{
		ProtocolVersion: 1,
		ClientInfo: &acp1.Implementation{
			Name:    "zed-editor",
			Version: "0.180.0",
		},
	})
	if err != nil {
		t.Fatalf("Initialize connection 1 failed: %v", err)
	}
	if initResp.ProtocolVersion != 1 {
		t.Fatalf("expected ProtocolVersion=1, got %d", initResp.ProtocolVersion)
	}

	// Create Session
	sessResp, err := clientConn1.NewSession(ctx, &acp1.NewSessionRequest{
		Cwd: ws,
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	sessionID := sessResp.SessionID
	if sessionID == "" {
		t.Fatalf("expected non-empty SessionID")
	}

	// Change mode to accept-edits and thinking level to medium
	_, err = clientConn1.SetSessionMode(ctx, &acp1.SetSessionModeRequest{
		SessionID: sessionID,
		ModeID:    "accept-edits",
	})
	if err != nil {
		t.Fatalf("SetSessionMode failed: %v", err)
	}

	cfgChangeReq := acp1.NewSetSessionConfigOptionRequest(acp1.SetSessionConfigOptionRequestUntagged{
		SessionID: sessionID,
		ConfigID:  "thinking_level",
		Value:     "medium",
	})
	_, err = clientConn1.SetSessionConfigOption(ctx, &cfgChangeReq)
	if err != nil {
		t.Fatalf("SetSessionConfigOption failed: %v", err)
	}

	// Send First Prompt
	promptResp, err := clientConn1.Prompt(ctx, &acp1.PromptRequest{
		SessionID: sessionID,
		Prompt: []acp1.ContentBlock{
			acp1.TextBlock("Implement hello world function in Go"),
		},
	})
	if err != nil {
		t.Fatalf("Prompt 1 failed: %v", err)
	}
	if promptResp.StopReason != acp1.StopReasonEndTurn {
		t.Errorf("StopReason = %q, want %q", promptResp.StopReason, acp1.StopReasonEndTurn)
	}

	// -------------------------------------------------------------
	// 2. Disconnection Simulation (editor process or socket closes)
	// -------------------------------------------------------------
	_ = clientW1.Close()
	_ = clientR1.Close()
	_ = agentW1.Close()
	_ = agentR1.Close()

	// Small pause simulating network/process drop
	time.Sleep(50 * time.Millisecond)

	// -------------------------------------------------------------
	// 3. Reconnection & Context Resumption (Editor reconnects with LoadSession)
	// -------------------------------------------------------------
	agentR2, clientW2 := io.Pipe()
	clientR2, agentW2 := io.Pipe()
	defer agentR2.Close()
	defer clientW2.Close()
	defer clientR2.Close()
	defer agentW2.Close()

	client2 := &e2eRecordingV1Client{}
	clientConn2 := acp1.NewClientSideConnection(func(c *acp1.ClientSideConnection) acp1.Client {
		return client2
	}, acp.NewStdioTransport(clientR2, clientW2))

	go func() {
		_ = srv.ServeACP(ctx, agentW2, agentR2)
	}()
	go func() {
		_ = clientConn2.Start(ctx)
	}()

	initResp2, err := clientConn2.Initialize(ctx, &acp1.InitializeRequest{
		ProtocolVersion: 1,
		ClientInfo: &acp1.Implementation{
			Name:    "zed-editor-reconnected",
			Version: "0.180.0",
		},
	})
	if err != nil {
		t.Fatalf("Initialize connection 2 failed: %v", err)
	}
	if initResp2.ProtocolVersion != 1 {
		t.Fatalf("expected ProtocolVersion=1, got %d", initResp2.ProtocolVersion)
	}

	// Reconnect and restore session context via LoadSession
	loadResp, err := clientConn2.LoadSession(ctx, &acp1.LoadSessionRequest{
		SessionID: sessionID,
		Cwd:       ws,
	})
	if err != nil {
		t.Fatalf("LoadSession failed on reconnected client: %v", err)
	}
	if loadResp.Modes == nil || loadResp.Modes.CurrentModeID != "accept-edits" {
		t.Errorf("expected restored mode 'accept-edits', got: %+v", loadResp.Modes)
	}

	// Verify history replay notification arrived on reconnected client
	userMsgs, assistMsgs := client2.historySummary()
	if len(userMsgs) == 0 {
		t.Errorf("expected replayed user messages on reconnected client, got none")
	} else if userMsgs[0] != "Implement hello world function in Go" {
		t.Errorf("replayed user message = %q, want %q", userMsgs[0], "Implement hello world function in Go")
	}
	if len(assistMsgs) == 0 {
		t.Errorf("expected replayed assistant messages on reconnected client, got none")
	}

	// Send Follow-up Turn on reconnected session to verify multi-turn context
	promptResp2, err := clientConn2.Prompt(ctx, &acp1.PromptRequest{
		SessionID: sessionID,
		Prompt: []acp1.ContentBlock{
			acp1.TextBlock("Add unit tests for the hello function"),
		},
	})
	if err != nil {
		t.Fatalf("Prompt 2 on reconnected session failed: %v", err)
	}
	if promptResp2.StopReason != acp1.StopReasonEndTurn {
		t.Errorf("StopReason = %q, want %q", promptResp2.StopReason, acp1.StopReasonEndTurn)
	}

	// Test CancelSession and CloseSession
	if err := clientConn2.CancelSession(ctx, &acp1.CancelNotification{SessionID: sessionID}); err != nil {
		t.Fatalf("CancelSession failed: %v", err)
	}
	if _, err := clientConn2.CloseSession(ctx, &acp1.CloseSessionRequest{SessionID: sessionID}); err != nil {
		t.Fatalf("CloseSession failed: %v", err)
	}
}

type e2eRecordingV2Client struct {
	acp2.UnimplementedClient
	mu       sync.Mutex
	updates  []*acp2.UpdateSessionNotification
	userMsgs []string
}

func (c *e2eRecordingV2Client) SessionUpdate(ctx context.Context, n *acp2.UpdateSessionNotification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = append(c.updates, n)
	if um, ok := n.Update.As[schemav2.SessionUpdateUserMessageChunk](); ok {
		if txt, ok := um.Content.As[schemav2.ContentBlockText](); ok {
			c.userMsgs = append(c.userMsgs, txt.Text)
		}
	}
	return nil
}

func (c *e2eRecordingV2Client) RequestPermission(ctx context.Context, req *acp2.RequestPermissionRequest) (*acp2.RequestPermissionResponse, error) {
	return &acp2.RequestPermissionResponse{
		Outcome: schemav2.NewRequestPermissionOutcome(schemav2.RequestPermissionOutcomeSelected{
			OptionID: "allow",
		}),
	}, nil
}

func TestACPServer_E2E_Stdio_V2_ResumeSession_And_Replay(t *testing.T) {
	ws := t.TempDir()
	srv, err := New(Config{
		Model:      "test-model",
		Workspace:  ws,
		BackendFor: func(string) backend.Client { return backend.NewMock() },
	})
	if err != nil {
		t.Fatalf("New server failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. First connection
	agentR1, clientW1 := io.Pipe()
	clientR1, agentW1 := io.Pipe()

	client1 := &e2eRecordingV2Client{}
	clientConn1 := acp2.NewClientSideConnection(func(c *acp2.ClientSideConnection) acp2.Client {
		return client1
	}, acp.NewStdioTransport(clientR1, clientW1))

	go func() {
		_ = srv.ServeACP(ctx, agentW1, agentR1)
	}()
	go func() {
		_ = clientConn1.Start(ctx)
	}()

	initResp, err := clientConn1.Initialize(ctx, &acp2.InitializeRequest{
		ProtocolVersion: 2,
		Info: acp2.Implementation{
			Name:    "test-v2-editor",
			Version: "2.0.0",
		},
	})
	if err != nil {
		t.Fatalf("v2 Initialize failed: %v", err)
	}
	if initResp.ProtocolVersion != 2 {
		t.Fatalf("expected ProtocolVersion=2, got %d", initResp.ProtocolVersion)
	}

	sessResp, err := clientConn1.NewSession(ctx, &acp2.NewSessionRequest{
		Cwd: acp2.AbsolutePath(ws),
	})
	if err != nil {
		t.Fatalf("v2 NewSession failed: %v", err)
	}

	promptResp, err := clientConn1.Prompt(ctx, &acp2.PromptRequest{
		SessionID: sessResp.SessionID,
		Prompt: []acp2.ContentBlock{
			acp2.TextBlock("Draft initial architecture overview"),
		},
	})
	if err != nil {
		t.Fatalf("v2 Prompt failed: %v", err)
	}
	if promptResp.MessageID == "" {
		t.Errorf("expected non-empty prompt MessageID")
	}

	// 2. Disconnect
	_ = clientW1.Close()
	_ = clientR1.Close()
	_ = agentW1.Close()
	_ = agentR1.Close()

	// 3. Reconnect with ResumeSession and ReplayFrom start
	agentR2, clientW2 := io.Pipe()
	clientR2, agentW2 := io.Pipe()
	defer agentR2.Close()
	defer clientW2.Close()
	defer clientR2.Close()
	defer agentW2.Close()

	client2 := &e2eRecordingV2Client{}
	clientConn2 := acp2.NewClientSideConnection(func(c *acp2.ClientSideConnection) acp2.Client {
		return client2
	}, acp.NewStdioTransport(clientR2, clientW2))

	go func() {
		_ = srv.ServeACP(ctx, agentW2, agentR2)
	}()
	go func() {
		_ = clientConn2.Start(ctx)
	}()

	_, err = clientConn2.Initialize(ctx, &acp2.InitializeRequest{
		ProtocolVersion: 2,
		Info: acp2.Implementation{
			Name:    "test-v2-editor-reconnected",
			Version: "2.0.0",
		},
	})
	if err != nil {
		t.Fatalf("v2 Reconnect Initialize failed: %v", err)
	}

	resumeResp, err := clientConn2.ResumeSession(ctx, &acp2.ResumeSessionRequest{
		SessionID:  sessResp.SessionID,
		Cwd:        acp2.AbsolutePath(ws),
		ReplayFrom: schemav2.NewReplayFrom(schemav2.ReplayFromStart{}),
	})
	if err != nil {
		t.Fatalf("v2 ResumeSession failed: %v", err)
	}
	if len(resumeResp.AvailableCommands) == 0 {
		t.Errorf("expected AvailableCommands in ResumeSession response")
	}

	// Close session
	if _, err := clientConn2.CloseSession(ctx, &acp2.CloseSessionRequest{SessionID: sessResp.SessionID}); err != nil {
		t.Fatalf("v2 CloseSession failed: %v", err)
	}
}
