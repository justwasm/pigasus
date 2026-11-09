package agentturn

import (
	"context"
	"encoding/json/jsontext"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	"github.com/ironpark/acp-go/acp2"
	schemav1 "github.com/ironpark/acp-go/schema/v1"
	schemav2 "github.com/ironpark/acp-go/schema/v2"
	"github.com/justwasm/pigasus/agentlink"
)

type mockV1Agent struct {
	client     acp1.Client
	promptFunc func(ctx context.Context, p *acp1.PromptRequest) (*acp1.PromptResponse, error)
}

func (m *mockV1Agent) Initialize(ctx context.Context, p *acp1.InitializeRequest) (*acp1.InitializeResponse, error) {
	return &acp1.InitializeResponse{
		ProtocolVersion: 1,
		AgentInfo: &acp1.Implementation{
			Name:    "mock-v1-agent",
			Version: "1.0.0",
		},
	}, nil
}

func (m *mockV1Agent) NewSession(ctx context.Context, p *acp1.NewSessionRequest) (*acp1.NewSessionResponse, error) {
	return &acp1.NewSessionResponse{SessionID: "sess_v1_123"}, nil
}

func (m *mockV1Agent) Prompt(ctx context.Context, p *acp1.PromptRequest) (*acp1.PromptResponse, error) {
	if m.promptFunc != nil {
		return m.promptFunc(ctx, p)
	}
	stream := acp1.NewSessionStream(m.client, p.SessionID)
	_ = stream.SendText(ctx, "Hello from ACP v1!")
	_ = stream.StartToolCall(ctx, "call_1", "read_file", acp1.ToolKindExecute, acp1.WithRawInput(jsontext.Value(`{"path":"test.txt"}`)))
	_ = stream.CompleteToolCall(ctx, "call_1", acp1.WithRawOutput(jsontext.Value(`{"result":"ok"}`)))
	return &acp1.PromptResponse{
		StopReason: acp1.StopReasonEndTurn,
		Usage: &acp1.Usage{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	}, nil
}

func (m *mockV1Agent) CancelSession(ctx context.Context, p *acp1.CancelNotification) error {
	return nil
}

type mockV2Agent struct {
	client     acp2.Client
	promptFunc func(ctx context.Context, p *acp2.PromptRequest) (*acp2.PromptResponse, error)
}

func (m *mockV2Agent) Initialize(ctx context.Context, p *acp2.InitializeRequest) (*acp2.InitializeResponse, error) {
	return &acp2.InitializeResponse{
		ProtocolVersion: 2,
		Info: acp2.Implementation{
			Name:    "mock-v2-agent",
			Version: "2.0.0",
		},
	}, nil
}

func (m *mockV2Agent) NewSession(ctx context.Context, p *acp2.NewSessionRequest) (*acp2.NewSessionResponse, error) {
	return &acp2.NewSessionResponse{SessionID: "sess_v2_456"}, nil
}

func (m *mockV2Agent) ListSessions(ctx context.Context, p *acp2.ListSessionsRequest) (*acp2.ListSessionsResponse, error) {
	return &acp2.ListSessionsResponse{}, nil
}

func (m *mockV2Agent) ResumeSession(ctx context.Context, p *acp2.ResumeSessionRequest) (*acp2.ResumeSessionResponse, error) {
	return &acp2.ResumeSessionResponse{}, nil
}

func (m *mockV2Agent) CloseSession(ctx context.Context, p *acp2.CloseSessionRequest) (*acp2.CloseSessionResponse, error) {
	return &acp2.CloseSessionResponse{}, nil
}

func (m *mockV2Agent) CancelSession(ctx context.Context, p *acp2.CancelSessionNotification) error {
	return nil
}

func (m *mockV2Agent) Prompt(ctx context.Context, p *acp2.PromptRequest) (*acp2.PromptResponse, error) {
	if m.promptFunc != nil {
		return m.promptFunc(ctx, p)
	}
	msgID := acp2.GenerateMessageID()
	stream := acp2.NewSessionStream(m.client, p.SessionID)
	_ = stream.SendUserMessage(ctx, msgID, p.Prompt...)
	_ = stream.Running(ctx)
	agentMsgID := acp2.GenerateMessageID()
	_ = stream.SendText(ctx, agentMsgID, "Hello from ACP v2!")
	_ = stream.StartToolCall(ctx, "call_2", "write_file", acp2.ToolKindExecute, acp2.WithRawInput(jsontext.Value(`{"path":"out.txt"}`)))
	_ = stream.CompleteToolCall(ctx, "call_2", acp2.WithRawOutput(jsontext.Value(`{"written":true}`)))
	_ = stream.Idle(ctx, acp2.StopReasonEndTurn)
	return &acp2.PromptResponse{MessageID: msgID}, nil
}

func TestACPRemote_V1_EndToEndTurn(t *testing.T) {
	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	agentConn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return &mockV1Agent{client: c}
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	remote, err := NewACPRemote(ctx, clientW, clientR, nil, t.TempDir(), "1")
	if err != nil {
		t.Fatalf("NewACPRemote failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Hello v1"},
		},
	})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}

	var textReceived string
	var sawToolStart, sawToolEnd, sawResult bool
	for f := range frames {
		switch f.Kind {
		case agentlink.KindText:
			textReceived += f.Text
		case agentlink.KindTool:
			if f.Tool.Phase == agentlink.ToolStart {
				sawToolStart = true
			} else if f.Tool.Phase == agentlink.ToolEnd {
				sawToolEnd = true
			}
		case agentlink.KindResult:
			sawResult = true
			if f.Usage == nil || f.Usage.TotalTokens != 30 {
				t.Errorf("expected usage total 30, got %+v", f.Usage)
			}
		}
	}

	if textReceived != "Hello from ACP v1!" {
		t.Errorf("textReceived = %q, want 'Hello from ACP v1!'", textReceived)
	}
	if !sawToolStart || !sawToolEnd {
		t.Errorf("expected tool start and end, got start=%v end=%v", sawToolStart, sawToolEnd)
	}
	if !sawResult {
		t.Errorf("expected result frame")
	}
}

func TestACPRemote_V2_EndToEndTurn(t *testing.T) {
	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	agentConn := acp2.NewAgentSideConnection(func(c *acp2.AgentSideConnection) acp2.Agent {
		return &mockV2Agent{client: c}
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	remote, err := NewACPRemote(ctx, clientW, clientR, nil, t.TempDir(), "2")
	if err != nil {
		t.Fatalf("NewACPRemote (v2) failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Hello v2"},
		},
	})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}

	var textReceived string
	var sawToolStart, sawToolEnd, sawResult bool
	for f := range frames {
		switch f.Kind {
		case agentlink.KindText:
			textReceived += f.Text
		case agentlink.KindTool:
			if f.Tool.Phase == agentlink.ToolStart {
				sawToolStart = true
			} else if f.Tool.Phase == agentlink.ToolEnd {
				sawToolEnd = true
			}
		case agentlink.KindResult:
			sawResult = true
		}
	}

	if textReceived != "Hello from ACP v2!" {
		t.Errorf("textReceived = %q, want 'Hello from ACP v2!'", textReceived)
	}
	if !sawToolStart || !sawToolEnd {
		t.Errorf("expected tool start and end, got start=%v end=%v", sawToolStart, sawToolEnd)
	}
	if !sawResult {
		t.Errorf("expected result frame")
	}
}

func TestACPRemote_V1_UsageUpdate(t *testing.T) {
	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	agentConn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return &mockV1Agent{
			client: c,
			promptFunc: func(ctx context.Context, p *acp1.PromptRequest) (*acp1.PromptResponse, error) {
				stream := acp1.NewSessionStream(c, p.SessionID)
				_ = stream.SendText(ctx, "Processing...")
				_ = stream.SendUsage(ctx, 42, 1000, nil)
				return &acp1.PromptResponse{
					StopReason: acp1.StopReasonEndTurn,
					Usage:      nil,
				}, nil
			},
		}
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	remote, err := NewACPRemote(ctx, clientW, clientR, nil, t.TempDir(), "1")
	if err != nil {
		t.Fatalf("NewACPRemote failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, Turn{History: []agentlink.Message{{Role: "user", Content: "Run"}}})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}

	var sawResult bool
	for f := range frames {
		if f.Kind == agentlink.KindResult {
			sawResult = true
			if f.Usage == nil {
				t.Fatalf("expected usage on result frame, got nil")
			}
			if f.Usage.PromptTokens != 42 || f.Usage.TotalTokens != 42 {
				t.Errorf("expected usage 42 tokens, got %+v", f.Usage)
			}
		}
	}
	if !sawResult {
		t.Fatalf("expected result frame")
	}
}

func TestACPRemote_V2_UsageUpdate(t *testing.T) {
	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	agentConn := acp2.NewAgentSideConnection(func(c *acp2.AgentSideConnection) acp2.Agent {
		return &mockV2Agent{
			client: c,
			promptFunc: func(ctx context.Context, p *acp2.PromptRequest) (*acp2.PromptResponse, error) {
				msgID := acp2.GenerateMessageID()
				stream := acp2.NewSessionStream(c, p.SessionID)
				_ = stream.SendUserMessage(ctx, msgID, p.Prompt...)
				_ = stream.Running(ctx)
				_ = stream.SendUsage(ctx, 99, 4096, nil)
				_ = stream.Idle(ctx, acp2.StopReasonEndTurn)
				return &acp2.PromptResponse{MessageID: msgID}, nil
			},
		}
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	remote, err := NewACPRemote(ctx, clientW, clientR, nil, t.TempDir(), "2")
	if err != nil {
		t.Fatalf("NewACPRemote failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, Turn{History: []agentlink.Message{{Role: "user", Content: "Run"}}})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}

	var sawResult bool
	for f := range frames {
		if f.Kind == agentlink.KindResult {
			sawResult = true
			if f.Usage == nil {
				t.Fatalf("expected usage on result frame, got nil")
			}
			if f.Usage.PromptTokens != 99 || f.Usage.TotalTokens != 99 {
				t.Errorf("expected usage 99 tokens, got %+v", f.Usage)
			}
		}
	}
	if !sawResult {
		t.Fatalf("expected result frame")
	}
}

func TestACPRemote_V1_PlanTracking(t *testing.T) {
	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	agentConn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return &mockV1Agent{
			client: c,
			promptFunc: func(ctx context.Context, p *acp1.PromptRequest) (*acp1.PromptResponse, error) {
				stream := acp1.NewSessionStream(c, p.SessionID)
				_ = stream.SendPlan(ctx, []schemav1.PlanEntry{
					{Content: "Task 1", Priority: schemav1.PlanEntryPriorityHigh, Status: schemav1.PlanEntryStatusPending},
					{Content: "Task 2", Priority: schemav1.PlanEntryPriorityMedium, Status: schemav1.PlanEntryStatusCompleted},
				})
				return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
			},
		}
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	remote, err := NewACPRemote(ctx, clientW, clientR, nil, t.TempDir(), "1")
	if err != nil {
		t.Fatalf("NewACPRemote failed: %v", err)
	}
	defer remote.Close()

	// Assert PlanLister interface
	var _ PlanLister = remote

	frames, err := remote.Turn(ctx, Turn{History: []agentlink.Message{{Role: "user", Content: "Plan"}}})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}
	for range frames {
	}

	plan := remote.Plan()
	if len(plan) != 2 {
		t.Fatalf("expected 2 plan entries, got %d", len(plan))
	}
	if plan[0].Content != "Task 1" || plan[0].Priority != "high" || plan[0].Status != "pending" {
		t.Errorf("unexpected entry 0: %+v", plan[0])
	}
	if plan[1].Content != "Task 2" || plan[1].Status != "completed" {
		t.Errorf("unexpected entry 1: %+v", plan[1])
	}
}

func TestACPRemote_V2_PlanTracking(t *testing.T) {
	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	agentConn := acp2.NewAgentSideConnection(func(c *acp2.AgentSideConnection) acp2.Agent {
		return &mockV2Agent{
			client: c,
			promptFunc: func(ctx context.Context, p *acp2.PromptRequest) (*acp2.PromptResponse, error) {
				msgID := acp2.GenerateMessageID()
				stream := acp2.NewSessionStream(c, p.SessionID)
				_ = stream.SendUserMessage(ctx, msgID, p.Prompt...)
				_ = stream.Running(ctx)
				_ = stream.SendPlan(ctx, "plan_42", []schemav2.PlanEntry{
					{Content: "Design protocol", Priority: schemav2.PlanEntryPriorityHigh, Status: schemav2.PlanEntryStatusInProgress},
				})
				_ = stream.Idle(ctx, acp2.StopReasonEndTurn)
				return &acp2.PromptResponse{MessageID: msgID}, nil
			},
		}
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	remote, err := NewACPRemote(ctx, clientW, clientR, nil, t.TempDir(), "2")
	if err != nil {
		t.Fatalf("NewACPRemote failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, Turn{History: []agentlink.Message{{Role: "user", Content: "Plan v2"}}})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}
	for range frames {
	}

	plan := remote.Plan()
	if len(plan) != 1 {
		t.Fatalf("expected 1 plan entry, got %d", len(plan))
	}
	if plan[0].Content != "Design protocol" || plan[0].Priority != "high" || plan[0].Status != "in_progress" {
		t.Errorf("unexpected entry: %+v", plan[0])
	}
}

func TestACPRemote_MarkdownPlanTracking(t *testing.T) {
	remote := &ACPRemote{}
	md := `# Execution Plan
- [x] Inspect project architecture
- [ ] Implement ACP statusline integration
* [x] Run test suite
1. [ ] Final verification and clean cache
`
	remote.setPlanMarkdown(md)
	plan := remote.Plan()
	if len(plan) != 4 {
		t.Fatalf("expected 4 plan entries from markdown checklist, got %d: %+v", len(plan), plan)
	}

	if plan[0].Content != "Inspect project architecture" || plan[0].Status != "completed" {
		t.Errorf("entry 0 mismatch: %+v", plan[0])
	}
	if plan[1].Content != "Implement ACP statusline integration" || plan[1].Status != "pending" {
		t.Errorf("entry 1 mismatch: %+v", plan[1])
	}
	if plan[2].Content != "Run test suite" || plan[2].Status != "completed" {
		t.Errorf("entry 2 mismatch: %+v", plan[2])
	}
	if plan[3].Content != "Final verification and clean cache" || plan[3].Status != "pending" {
		t.Errorf("entry 3 mismatch: %+v", plan[3])
	}
}

func TestACPRemote_V1_RequestPermission_AutoApprove(t *testing.T) {
	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var permissionAllowed bool
	var chosenOption acp1.PermissionOption
	var permErr error

	agentConn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return &mockV1Agent{
			client: c,
			promptFunc: func(ctx context.Context, p *acp1.PromptRequest) (*acp1.PromptResponse, error) {
				stream := acp1.NewSessionStream(c, p.SessionID)
				chosenOption, permissionAllowed, permErr = stream.RequestPermission(ctx, acp1.ToolCallUpdate{ToolCallID: "call_auto"}, acp1.DefaultPermissionOptions()...)
				return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
			},
		}
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	remote, err := NewACPRemote(ctx, clientW, clientR, nil, t.TempDir(), "1")
	if err != nil {
		t.Fatalf("NewACPRemote failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, Turn{History: []agentlink.Message{{Role: "user", Content: "Perm"}}})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}
	for range frames {
	}

	if permErr != nil {
		t.Fatalf("stream.RequestPermission failed: %v", permErr)
	}
	if !permissionAllowed {
		t.Errorf("expected permission to be allowed by default auto-approval")
	}
	if chosenOption.Kind != acp1.PermissionOptionKindAllowOnce && chosenOption.Kind != acp1.PermissionOptionKindAllowAlways {
		t.Errorf("expected allow option kind, got %v", chosenOption.Kind)
	}
}

func TestACPRemote_V2_RequestPermission_AutoApprove(t *testing.T) {
	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var permissionAllowed bool
	var chosenOption acp2.PermissionOption
	var permErr error

	agentConn := acp2.NewAgentSideConnection(func(c *acp2.AgentSideConnection) acp2.Agent {
		return &mockV2Agent{
			client: c,
			promptFunc: func(ctx context.Context, p *acp2.PromptRequest) (*acp2.PromptResponse, error) {
				msgID := acp2.GenerateMessageID()
				stream := acp2.NewSessionStream(c, p.SessionID)
				_ = stream.SendUserMessage(ctx, msgID, p.Prompt...)
				_ = stream.Running(ctx)
				chosenOption, permissionAllowed, permErr = stream.RequestPermission(ctx, "Execute shell script", acp2.RequestPermissionSubject{}, acp2.DefaultPermissionOptions()...)
				_ = stream.Idle(ctx, acp2.StopReasonEndTurn)
				return &acp2.PromptResponse{MessageID: msgID}, nil
			},
		}
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	remote, err := NewACPRemote(ctx, clientW, clientR, nil, t.TempDir(), "2")
	if err != nil {
		t.Fatalf("NewACPRemote failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, Turn{History: []agentlink.Message{{Role: "user", Content: "Perm v2"}}})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}
	for range frames {
	}

	if permErr != nil {
		t.Fatalf("stream.RequestPermission (v2) failed: %v", permErr)
	}
	if !permissionAllowed {
		t.Errorf("expected permission to be allowed by default auto-approval in v2")
	}
	if chosenOption.Kind != acp2.PermissionOptionKindAllowOnce && chosenOption.Kind != acp2.PermissionOptionKindAllowAlways {
		t.Errorf("expected allow option kind in v2, got %v", chosenOption.Kind)
	}
}

func TestACPRemote_RequestPermission_CustomRejectWithFeedback(t *testing.T) {
	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var permissionAllowed bool
	var chosenOption acp1.PermissionOption
	var permErr error

	agentConn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return &mockV1Agent{
			client: c,
			promptFunc: func(ctx context.Context, p *acp1.PromptRequest) (*acp1.PromptResponse, error) {
				stream := acp1.NewSessionStream(c, p.SessionID)
				chosenOption, permissionAllowed, permErr = stream.RequestPermission(ctx, acp1.ToolCallUpdate{ToolCallID: "rm_rf_tool"}, acp1.DefaultPermissionOptions()...)
				return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
			},
		}
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	remote, err := NewACPRemote(ctx, clientW, clientR, nil, t.TempDir(), "1")
	if err != nil {
		t.Fatalf("NewACPRemote failed: %v", err)
	}
	defer remote.Close()

	remote.SetPermissionHandler(func(ctx context.Context, req PermissionRequest) (PermissionDecision, error) {
		if req.ToolCallID == "rm_rf_tool" {
			return PermissionDecision{
				Approved: false,
				Feedback: "command rm -rf is prohibited by security policy",
			}, nil
		}
		return PermissionDecision{Approved: true}, nil
	})

	frames, err := remote.Turn(ctx, Turn{History: []agentlink.Message{{Role: "user", Content: "Run rm rf"}}})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}

	var sawNotice bool
	for f := range frames {
		if f.Kind == agentlink.KindNotice && f.Notice != nil {
			if strings.Contains(f.Notice.Text, "prohibited by security policy") {
				sawNotice = true
			}
		}
	}

	if permErr != nil {
		t.Fatalf("stream.RequestPermission failed: %v", permErr)
	}
	if permissionAllowed {
		t.Errorf("expected permission to be rejected")
	}
	if chosenOption.Kind != acp1.PermissionOptionKindRejectOnce && chosenOption.Kind != acp1.PermissionOptionKindRejectAlways {
		t.Errorf("expected reject option kind, got %v", chosenOption.Kind)
	}
	if !sawNotice {
		t.Errorf("expected notice frame with rejection feedback")
	}
}

func TestACPRemote_FS_ReadAndWriteFile(t *testing.T) {
	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tmpDir := t.TempDir()

	var writeErr error
	var readContent string
	var readErr error

	agentConn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return &mockV1Agent{
			client: c,
			promptFunc: func(ctx context.Context, p *acp1.PromptRequest) (*acp1.PromptResponse, error) {
				stream := acp1.NewSessionStream(c, p.SessionID)

				// Test writing file (relative path resolved against cwd)
				writeErr = stream.WriteTextFile(ctx, "sub/hello.txt", "hello from ACP client filesystem!\nsecond line\n")
				if writeErr != nil {
					return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
				}

				// Test reading file back
				readContent, readErr = stream.ReadTextFile(ctx, "sub/hello.txt")

				return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
			},
		}
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	remote, err := NewACPRemote(ctx, clientW, clientR, nil, tmpDir, "1")
	if err != nil {
		t.Fatalf("NewACPRemote failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, Turn{History: []agentlink.Message{{Role: "user", Content: "FS test"}}})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}
	for range frames {
	}

	if writeErr != nil {
		t.Fatalf("stream.WriteTextFile failed: %v", writeErr)
	}
	if readErr != nil {
		t.Fatalf("stream.ReadTextFile failed: %v", readErr)
	}
	wantContent := "hello from ACP client filesystem!\nsecond line\n"
	if readContent != wantContent {
		t.Errorf("readContent = %q, want %q", readContent, wantContent)
	}

	// Verify file was written to disk
	onDisk, err := os.ReadFile(filepath.Join(tmpDir, "sub", "hello.txt"))
	if err != nil {
		t.Fatalf("failed to read written file on disk: %v", err)
	}
	if string(onDisk) != wantContent {
		t.Errorf("onDisk = %q, want %q", string(onDisk), wantContent)
	}
}

func TestACPRemote_Terminal_CreateAndExit(t *testing.T) {
	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tmpDir := t.TempDir()

	var terminalErr error
	var capturedOutput string
	var exitCode uint32
	var hasExitCode bool

	agentConn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return &mockV1Agent{
			client: c,
			promptFunc: func(ctx context.Context, p *acp1.PromptRequest) (*acp1.PromptResponse, error) {
				stream := acp1.NewSessionStream(c, p.SessionID)

				term, err := stream.NewTerminal(ctx, acp1.CreateTerminalRequest{
					Command: `echo "hello from terminal"`,
				})
				if err != nil {
					terminalErr = err
					return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
				}

				exitResp, err := term.WaitForExit(ctx)
				if err != nil {
					terminalErr = err
					return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
				}
				if exitResp.ExitCode != nil {
					exitCode = *exitResp.ExitCode
					hasExitCode = true
				}

				outResp, err := term.CurrentOutput(ctx)
				if err != nil {
					terminalErr = err
					return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
				}
				capturedOutput = outResp.Output

				if err := term.Release(ctx); err != nil {
					terminalErr = err
				}

				return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
			},
		}
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	remote, err := NewACPRemote(ctx, clientW, clientR, nil, tmpDir, "1")
	if err != nil {
		t.Fatalf("NewACPRemote failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, Turn{History: []agentlink.Message{{Role: "user", Content: "Terminal test"}}})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}
	for range frames {
	}

	if terminalErr != nil {
		t.Fatalf("terminal operation failed: %v", terminalErr)
	}
	if !hasExitCode {
		t.Fatalf("expected non-nil exit code")
	}
	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", exitCode)
	}
	trimmed := strings.TrimSpace(capturedOutput)
	if trimmed != "hello from terminal" {
		t.Errorf("output = %q, want 'hello from terminal'", trimmed)
	}
}

func TestACPRemote_Terminal_TruncateAndKill(t *testing.T) {
	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tmpDir := t.TempDir()

	var termErr error
	var truncated bool
	var outputLen int

	agentConn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return &mockV1Agent{
			client: c,
			promptFunc: func(ctx context.Context, p *acp1.PromptRequest) (*acp1.PromptResponse, error) {
				stream := acp1.NewSessionStream(c, p.SessionID)

				limit := uint64(10)
				term, err := stream.NewTerminal(ctx, acp1.CreateTerminalRequest{
					Command:         `echo "12345678901234567890"`,
					OutputByteLimit: &limit,
				})
				if err != nil {
					termErr = err
					return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
				}

				if _, err := term.WaitForExit(ctx); err != nil {
					termErr = err
					return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
				}

				outResp, err := term.CurrentOutput(ctx)
				if err != nil {
					termErr = err
					return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
				}
				truncated = outResp.Truncated
				outputLen = len(outResp.Output)
				_ = term.Release(ctx)

				// Test Kill on a long-running process
				sleepTerm, err := stream.NewTerminal(ctx, acp1.CreateTerminalRequest{
					Command: "sleep 10",
				})
				if err != nil {
					termErr = err
					return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
				}
				time.Sleep(50 * time.Millisecond)
				if err := sleepTerm.Kill(ctx); err != nil {
					termErr = err
					return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
				}
				_, err = sleepTerm.WaitForExit(ctx)
				if err != nil {
					termErr = err
				}
				_ = sleepTerm.Release(ctx)

				return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
			},
		}
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	remote, err := NewACPRemote(ctx, clientW, clientR, nil, tmpDir, "1")
	if err != nil {
		t.Fatalf("NewACPRemote failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, Turn{History: []agentlink.Message{{Role: "user", Content: "Truncate test"}}})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}
	for range frames {
	}

	if termErr != nil {
		t.Fatalf("termErr = %v", termErr)
	}
	if !truncated {
		t.Errorf("expected output to be truncated")
	}
	if outputLen > 10 {
		t.Errorf("expected outputLen <= 10, got %d", outputLen)
	}
}
