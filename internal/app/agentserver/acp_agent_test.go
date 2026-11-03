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
	"github.com/ironpark/acp-go/router"
	"github.com/justwasm/pigasus/internal/core/backend"
)

type dummyV1Client struct {
	acp1.UnimplementedClient
	mu      sync.Mutex
	updates []*acp1.SessionNotification
}

func (c *dummyV1Client) SessionUpdate(ctx context.Context, n *acp1.SessionNotification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = append(c.updates, n)
	return nil
}

func (c *dummyV1Client) waitForUpdate(tag string, timeout time.Duration) *acp1.SessionNotification {
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

type dummyV2Client struct {
	acp2.UnimplementedClient
	mu      sync.Mutex
	updates []*acp2.UpdateSessionNotification
}

func (c *dummyV2Client) SessionUpdate(ctx context.Context, n *acp2.UpdateSessionNotification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = append(c.updates, n)
	return nil
}

func (c *dummyV2Client) waitForUpdate(tag string, timeout time.Duration) *acp2.UpdateSessionNotification {
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

func TestACPAgent_V1_InitializeAndSession(t *testing.T) {
	srv, err := New(Config{
		Model:      "test-model",
		Workspace:  t.TempDir(),
		BackendFor: func(string) backend.Client { return backend.NewMock() },
	})
	if err != nil {
		t.Fatalf("New server failed: %v", err)
	}

	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := &dummyV1Client{}
	agentConn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return NewACPV1Agent(srv, c)
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	clientConn := acp1.NewClientSideConnection(func(c *acp1.ClientSideConnection) acp1.Client {
		return client
	}, acp.NewStdioTransport(clientR, clientW))
	go func() { _ = clientConn.Start(ctx) }()

	initResp, err := clientConn.Initialize(ctx, &acp1.InitializeRequest{
		ProtocolVersion: 1,
		ClientInfo: &acp1.Implementation{
			Name:    "test-v1-client",
			Version: "1.0.0",
		},
	})
	if err != nil {
		t.Fatalf("v1 Initialize failed: %v", err)
	}
	if initResp.ProtocolVersion != 1 {
		t.Errorf("ProtocolVersion = %d, want 1", initResp.ProtocolVersion)
	}
	if initResp.AgentInfo == nil || (initResp.AgentInfo.Name != "Google Antigravity" && initResp.AgentInfo.Name != "antigravity-acp") {
		t.Errorf("unexpected agent info: %+v", initResp.AgentInfo)
	}
	if initResp.AgentCapabilities == nil || initResp.AgentCapabilities.LoadSession == nil || !*initResp.AgentCapabilities.LoadSession {
		t.Errorf("expected LoadSession capability")
	}
	if len(initResp.AuthMethods) == 0 {
		t.Errorf("expected non-empty AuthMethods in InitializeResponse")
	}

	sessResp, err := clientConn.NewSession(ctx, &acp1.NewSessionRequest{
		Cwd: srv.cfg.Workspace,
	})
	if err != nil {
		t.Fatalf("v1 NewSession failed: %v", err)
	}
	if sessResp.SessionID == "" {
		t.Fatalf("expected non-empty SessionID")
	}
	if len(sessResp.ConfigOptions) == 0 {
		t.Errorf("expected ConfigOptions in v1 NewSessionResponse")
	}
	if sessResp.Modes == nil || len(sessResp.Modes.AvailableModes) == 0 {
		t.Errorf("expected Modes in v1 NewSessionResponse")
	}

	// Test SetSessionConfigOption (e.g. Zed / IntelliJ model / thinking_level switch)
	cfgChangeReq := acp1.NewSetSessionConfigOptionRequest(acp1.SetSessionConfigOptionRequestUntagged{
		SessionID: sessResp.SessionID,
		ConfigID:  "thinking_level",
		Value:     "low",
	})
	cfgResp, err := clientConn.SetSessionConfigOption(ctx, &cfgChangeReq)
	if err != nil {
		t.Fatalf("SetSessionConfigOption failed: %v", err)
	}
	if len(cfgResp.ConfigOptions) == 0 {
		t.Errorf("expected updated ConfigOptions in response")
	}

	// Test SetSessionMode
	modeResp, err := clientConn.SetSessionMode(ctx, &acp1.SetSessionModeRequest{
		SessionID: sessResp.SessionID,
		ModeID:    "auto_edit",
	})
	if err != nil {
		t.Fatalf("SetSessionMode failed: %v", err)
	}
	_ = modeResp

	// 1. Assert available_commands update received during session creation
	cmdUpdate := client.waitForUpdate("available_commands_update", 3*time.Second)
	if cmdUpdate == nil {
		t.Fatalf("expected available_commands_update notification in v1 NewSession, got none")
	}
	cmds, ok := cmdUpdate.Update.As[acp1.SessionUpdateAvailableCommandsUpdate]()
	if !ok || len(cmds.AvailableCommands) == 0 {
		t.Fatalf("expected non-empty available commands, got: %+v", cmds)
	}

	// 2. Assert usage_update received with non-zero tokens when prompt turn completes
	promptResp, err := clientConn.Prompt(ctx, &acp1.PromptRequest{
		SessionID: sessResp.SessionID,
		Prompt: []acp1.ContentBlock{
			acp1.TextBlock("Hello ACP v1!"),
		},
	})
	if err != nil {
		t.Fatalf("v1 Prompt failed: %v", err)
	}
	if promptResp.Usage == nil || promptResp.Usage.TotalTokens == 0 {
		t.Errorf("expected non-zero promptResp.Usage tokens, got: %+v", promptResp.Usage)
	}

	usageUpdate := client.waitForUpdate("usage_update", 3*time.Second)
	if usageUpdate == nil {
		t.Fatalf("expected usage_update notification in v1 Prompt, got none")
	}
	usage, ok := usageUpdate.Update.As[acp1.SessionUpdateUsageUpdate]()
	if !ok || usage.Used == 0 {
		t.Errorf("expected non-zero Used tokens in usage_update, got: %+v", usage)
	}

	err = clientConn.CancelSession(ctx, &acp1.CancelNotification{
		SessionID: sessResp.SessionID,
	})
	if err != nil {
		t.Fatalf("v1 CancelSession failed: %v", err)
	}
}

func TestACPAgent_V2_InitializeAndSession(t *testing.T) {
	srv, err := New(Config{
		Model:      "test-model",
		Workspace:  t.TempDir(),
		BackendFor: func(string) backend.Client { return backend.NewMock() },
	})
	if err != nil {
		t.Fatalf("New server failed: %v", err)
	}

	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := &dummyV2Client{}
	agentConn := acp2.NewAgentSideConnection(func(c *acp2.AgentSideConnection) acp2.Agent {
		return NewACPV2Agent(srv, c)
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	clientConn := acp2.NewClientSideConnection(func(c *acp2.ClientSideConnection) acp2.Client {
		return client
	}, acp.NewStdioTransport(clientR, clientW))
	go func() { _ = clientConn.Start(ctx) }()

	initResp, err := clientConn.Initialize(ctx, &acp2.InitializeRequest{
		ProtocolVersion: 2,
		Info: acp2.Implementation{
			Name:    "test-v2-client",
			Version: "2.0.0",
		},
	})
	if err != nil {
		t.Fatalf("v2 Initialize failed: %v", err)
	}
	if initResp.ProtocolVersion != 2 {
		t.Errorf("ProtocolVersion = %d, want 2", initResp.ProtocolVersion)
	}
	if initResp.Info.Name != "Google Antigravity" && initResp.Info.Name != "antigravity-acp" {
		t.Errorf("unexpected agent info name: %s", initResp.Info.Name)
	}

	sessResp, err := clientConn.NewSession(ctx, &acp2.NewSessionRequest{
		Cwd: acp2.AbsolutePath(srv.cfg.Workspace),
	})
	if err != nil {
		t.Fatalf("v2 NewSession failed: %v", err)
	}
	if sessResp.SessionID == "" {
		t.Fatalf("expected non-empty SessionID")
	}

	// 1. Assert available_commands update received during session creation
	cmdUpdate := client.waitForUpdate("available_commands_update", 3*time.Second)
	if cmdUpdate == nil {
		t.Fatalf("expected available_commands_update notification in v2 NewSession, got none")
	}
	cmds, ok := cmdUpdate.Update.As[acp2.SessionUpdateAvailableCommandsUpdate]()
	if !ok || len(cmds.AvailableCommands) == 0 {
		t.Fatalf("expected non-empty available commands in notification, got: %+v", cmds)
	}
	if len(sessResp.AvailableCommands) == 0 {
		t.Fatalf("expected non-empty AvailableCommands in NewSessionResponse, got none")
	}

	// 2. Test prompt: In v2, returns PromptResponse immediately with MessageID
	promptResp, err := clientConn.Prompt(ctx, &acp2.PromptRequest{
		SessionID: sessResp.SessionID,
		Prompt: []acp2.ContentBlock{
			acp2.TextBlock("Hello ACP v2!"),
		},
	})
	if err != nil {
		t.Fatalf("v2 Prompt failed: %v", err)
	}
	if promptResp.MessageID == "" {
		t.Fatalf("expected non-empty message ID from v2 prompt acknowledgment")
	}

	// 3. Assert usage_update received with non-zero tokens when turn completes
	usageUpdate := client.waitForUpdate("usage_update", 5*time.Second)
	if usageUpdate == nil {
		t.Fatalf("expected usage_update notification in v2 Prompt, got none")
	}
	usage, ok := usageUpdate.Update.As[acp2.SessionUpdateUsageUpdate]()
	if !ok || usage.Used == 0 {
		t.Errorf("expected non-zero Used tokens in v2 usage_update, got: %+v", usage)
	}

	err = clientConn.CancelSession(ctx, &acp2.CancelSessionNotification{
		SessionID: sessResp.SessionID,
	})
	if err != nil {
		t.Fatalf("v2 CancelSession failed: %v", err)
	}
}

func TestACPAgent_RouterNegotiation(t *testing.T) {
	srv, err := New(Config{
		Model:      "test-model",
		Workspace:  t.TempDir(),
		ACPVersion: "auto",
	})
	if err != nil {
		t.Fatalf("New server failed: %v", err)
	}

	// Connect client and router over memory pipes
	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = srv.ServeACP(ctx, agentW, agentR)
	}()

	client := &dummyV2Client{}
	agent, err := router.NewClient().
		WithV2(func(c *acp2.ClientSideConnection) acp2.Client {
			return client
		}, &acp2.InitializeRequest{
			Info: acp2.Implementation{Name: "v2-negotiator", Version: "1.0.0"},
		}).
		Connect(ctx, func(context.Context) (acp.Transport, error) {
			return acp.NewStdioTransport(clientR, clientW), nil
		})
	if err != nil {
		t.Fatalf("router negotiation failed: %v", err)
	}
	defer agent.Close()

	if agent.V2 == nil {
		t.Fatalf("expected negotiated V2 agent")
	}
	if agent.V2Init.ProtocolVersion != 2 {
		t.Errorf("expected ProtocolVersion = 2, got %d", agent.V2Init.ProtocolVersion)
	}
}
