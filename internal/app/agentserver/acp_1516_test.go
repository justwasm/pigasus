package agentserver

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	"github.com/ironpark/acp-go/acp2"
	schemav2 "github.com/ironpark/acp-go/schema/v2"
	"github.com/justwasm/pigasus/internal/core/backend"
)

type subagentPermRecordingV1Client struct {
	e2eRecordingV1Client
	permMu     sync.Mutex
	permTitles []string
}

func (c *subagentPermRecordingV1Client) RequestPermission(ctx context.Context, req *acp1.RequestPermissionRequest) (*acp1.RequestPermissionResponse, error) {
	title := ""
	if req.ToolCall.Title != nil {
		title = *req.ToolCall.Title
	}
	c.permMu.Lock()
	c.permTitles = append(c.permTitles, title)
	c.permMu.Unlock()

	optID := acp1.PermissionOptionID(acp1.PermissionOptionKindAllowOnce)
	if len(req.Options) > 0 {
		optID = req.Options[0].OptionID
	}
	return acp1.PermissionSelected(optID), nil
}

type subagentPermRecordingV2Client struct {
	dummyV2Client
	permMu     sync.Mutex
	permTitles []string
}

func (c *subagentPermRecordingV2Client) RequestPermission(ctx context.Context, req *acp2.RequestPermissionRequest) (*acp2.RequestPermissionResponse, error) {
	c.permMu.Lock()
	c.permTitles = append(c.permTitles, req.Title)
	c.permMu.Unlock()

	optID := acp2.PermissionOptionID(acp2.PermissionOptionKindAllowOnce)
	if len(req.Options) > 0 {
		optID = req.Options[0].OptionID
	}
	return &acp2.RequestPermissionResponse{
		Outcome: schemav2.NewRequestPermissionOutcome(schemav2.RequestPermissionOutcomeSelected{
			OptionID: optID,
		}),
	}, nil
}

func newSubagentForwardingMockBackend(t *testing.T) *testCustomMockBackend {
	return &testCustomMockBackend{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			out := make(chan backend.StreamChunk, 2)
			go func() {
				defer close(out)
				var isSubagent bool
				var toolCount int
				var lastToolText string
				for _, m := range req.Messages {
					if strings.Contains(m.Content, "SUBAGENT_TASK_MARKER") {
						isSubagent = true
					}
					if m.Role == "tool" {
						toolCount++
						lastToolText = m.Content
					}
				}
				if isSubagent {
					switch toolCount {
					case 0:
						args, _ := json.Marshal(map[string]any{
							"CommandLine":       "touch subagent-ran.txt",
							"toolSummary":       "Subagent touch",
							"WaitMsBeforeAsync": 2000,
						})
						out <- backend.StreamChunk{
							ToolCalls: []backend.ToolCall{{
								ID:        "sub_call_cmd",
								Name:      "run_command",
								Arguments: args,
							}},
							Done: true,
						}
					case 1:
						args, _ := json.Marshal(map[string]any{
							"questions": []map[string]any{{
								"question": "Subagent question: proceed?",
								"options":  []string{"Yes (Recommended)", "No"},
							}},
						})
						out <- backend.StreamChunk{
							ToolCalls: []backend.ToolCall{{
								ID:        "sub_call_ask",
								Name:      "ask_question",
								Arguments: args,
							}},
							Done: true,
						}
					default:
						out <- backend.StreamChunk{
							Text: "Subagent finished with answer: " + lastToolText,
							Done: true,
						}
					}
					return
				}

				// Parent turn
				if toolCount == 0 {
					args, _ := json.Marshal(map[string]any{
						"TypeName": "self",
						"Role":     "Worker",
						"Prompt":   "SUBAGENT_TASK_MARKER run command and ask question",
					})
					out <- backend.StreamChunk{
						ToolCalls: []backend.ToolCall{{
							ID:        "parent_call_sub",
							Name:      "invoke_subagent",
							Arguments: args,
						}},
						Done: true,
					}
					return
				}
				out <- backend.StreamChunk{
					Text: "Parent completed after subagent.",
					Done: true,
				}
			}()
			return out, nil
		},
	}
}

func TestACPV1_SubagentPermissionAndQuestionForwarding(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("OPENAGY_PERMISSION_POLICY", "strict")
	ws := t.TempDir()

	srv, err := New(Config{
		Model:          "test-model",
		Workspace:      ws,
		PermissionMode: "accept-edits",
		BackendFor:     func(string) backend.Client { return newSubagentForwardingMockBackend(t) },
	})
	if err != nil {
		t.Fatal(err)
	}

	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := &subagentPermRecordingV1Client{}
	agentConn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return NewACPV1Agent(srv, c)
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	clientConn := acp1.NewClientSideConnection(func(c *acp1.ClientSideConnection) acp1.Client {
		return client
	}, acp.NewStdioTransport(clientR, clientW))
	go func() { _ = clientConn.Start(ctx) }()

	if _, err := clientConn.Initialize(ctx, &acp1.InitializeRequest{ProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}
	sessResp, err := clientConn.NewSession(ctx, &acp1.NewSessionRequest{Cwd: ws})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := clientConn.Prompt(ctx, &acp1.PromptRequest{
		SessionID: sessResp.SessionID,
		Prompt:    []acp1.ContentBlock{acp1.TextBlock("Delegate to subagent")},
	})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}
	if resp.StopReason != acp1.StopReasonEndTurn {
		t.Fatalf("StopReason = %q, want end_turn", resp.StopReason)
	}

	client.permMu.Lock()
	titles := append([]string(nil), client.permTitles...)
	client.permMu.Unlock()

	var sawCmdPerm, sawQuestion bool
	for _, title := range titles {
		if strings.Contains(title, "run_command") {
			sawCmdPerm = true
		}
		if strings.Contains(title, "Subagent question: proceed?") {
			sawQuestion = true
		}
	}
	if !sawCmdPerm {
		t.Fatalf("expected subagent run_command permission forwarded to ACP v1 client, got titles: %v", titles)
	}
	if !sawQuestion {
		t.Fatalf("expected subagent ask_question forwarded to ACP v1 client, got titles: %v", titles)
	}
}

func TestACPV2_SubagentPermissionAndQuestionForwarding(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("OPENAGY_PERMISSION_POLICY", "strict")
	ws := t.TempDir()

	srv, err := New(Config{
		Model:          "test-model",
		Workspace:      ws,
		PermissionMode: "accept-edits",
		BackendFor:     func(string) backend.Client { return newSubagentForwardingMockBackend(t) },
	})
	if err != nil {
		t.Fatal(err)
	}

	agentR, clientW := io.Pipe()
	clientR, agentW := io.Pipe()
	defer agentR.Close()
	defer clientW.Close()
	defer clientR.Close()
	defer agentW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := &subagentPermRecordingV2Client{}
	agentConn := acp2.NewAgentSideConnection(func(c *acp2.AgentSideConnection) acp2.Agent {
		return NewACPV2Agent(srv, c)
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	clientConn := acp2.NewClientSideConnection(func(c *acp2.ClientSideConnection) acp2.Client {
		return client
	}, acp.NewStdioTransport(clientR, clientW))
	go func() { _ = clientConn.Start(ctx) }()

	if _, err := clientConn.Initialize(ctx, &acp2.InitializeRequest{ProtocolVersion: 2}); err != nil {
		t.Fatal(err)
	}
	sessResp, err := clientConn.NewSession(ctx, &acp2.NewSessionRequest{Cwd: acp2.AbsolutePath(ws)})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := clientConn.Prompt(ctx, &acp2.PromptRequest{
		SessionID: sessResp.SessionID,
		Prompt:    []acp2.ContentBlock{acp2.TextBlock("Delegate to subagent")},
	})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}
	if resp.MessageID == "" {
		t.Fatalf("expected non-empty MessageID")
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		client.permMu.Lock()
		n := len(client.permTitles)
		client.permMu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	client.permMu.Lock()
	titles := append([]string(nil), client.permTitles...)
	client.permMu.Unlock()

	var sawCmdPerm, sawQuestion bool
	for _, title := range titles {
		if strings.Contains(title, "run_command") {
			sawCmdPerm = true
		}
		if strings.Contains(title, "Subagent question: proceed?") {
			sawQuestion = true
		}
	}
	if !sawCmdPerm {
		t.Fatalf("expected subagent run_command permission forwarded to ACP v2 client, got titles: %v", titles)
	}
	if !sawQuestion {
		t.Fatalf("expected subagent ask_question forwarded to ACP v2 client, got titles: %v", titles)
	}
}
