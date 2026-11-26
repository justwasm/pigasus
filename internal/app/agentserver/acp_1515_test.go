package agentserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	"github.com/ironpark/acp-go/acp2"
	schemav1 "github.com/ironpark/acp-go/schema/v1"
	schemav2 "github.com/ironpark/acp-go/schema/v2"
	"github.com/justwasm/pigasus/internal/core/backend"
)

func TestACPPromptParsing_1515(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(home, ".gemini", "antigravity-cli"))
	t.Setenv("HOME", home)

	// Create a user skill under ~/.gemini/skills/my-skill/SKILL.md
	skillDir := filepath.Join(home, ".gemini", "skills", "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my-skill\ndescription: test skill\n---\nRun the custom skill steps carefully."), 0o644); err != nil {
		t.Fatal(err)
	}

	emptyRes, err := schemav1.NewEmbeddedResourceResource(schemav1.TextResourceContents{
		URI:  "file:///tmp/empty.txt",
		Text: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	title := "Docs"
	blocks := []acp1.ContentBlock{
		// Attachment BEFORE text block!
		schemav1.NewContentBlock(schemav1.ContentBlockResource{
			Resource: emptyRes,
		}),
		schemav1.NewContentBlock(schemav1.ContentBlockResourceLink{
			URI:   "https://example.com/docs",
			Name:  "docs",
			Title: &title,
		}),
		schemav1.NewContentBlock(schemav1.ContentBlockResourceLink{
			URI:  "file:///tmp/main.go#L10-L20",
			Name: "main.go#L10-L20",
		}),
		schemav1.NewContentBlock(schemav1.ContentBlockResourceLink{
			URI:  "file:///tmp/whole.go",
			Name: "whole.go",
		}),
		acp1.TextBlock("/my-skill check this"),
	}

	parsed := parseACPPromptV1(blocks, Config{Workspace: home})
	if parsed.Command != "my-skill" {
		t.Fatalf("Command = %q, want %q", parsed.Command, "my-skill")
	}
	if parsed.CommandArgs != "check this" {
		t.Fatalf("CommandArgs = %q, want %q", parsed.CommandArgs, "check this")
	}
	if !strings.Contains(parsed.ModelPrompt, "Run the custom skill steps carefully.") {
		t.Errorf("expected expanded skill body in ModelPrompt, got: %q", parsed.ModelPrompt)
	}
	if !strings.Contains(parsed.ModelPrompt, "[Attached Resource: /tmp/empty.txt (editor contents)]") {
		t.Errorf("expected 0-byte embedded file preserved in ModelPrompt, got: %q", parsed.ModelPrompt)
	}
	if !strings.Contains(parsed.ModelPrompt, "Resource link: https://example.com/docs (Docs)") {
		t.Errorf("expected web URL formatted as Resource link, got: %q", parsed.ModelPrompt)
	}
	if !strings.Contains(parsed.ModelPrompt, "Resource link: file:///tmp/main.go#L10-L20") {
		t.Errorf("expected line-range link formatted as Resource link, got: %q", parsed.ModelPrompt)
	}
	if !strings.Contains(parsed.ModelPrompt, "[Attached File: /tmp/whole.go]") {
		t.Errorf("expected whole file URI formatted as Attached File, got: %q", parsed.ModelPrompt)
	}

	// Also verify v2 leading slash command when attachment precedes text.
	emptyResV2, err := schemav2.NewEmbeddedResourceResource(schemav2.TextResourceContents{
		URI:  "file:///tmp/zero.txt",
		Text: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	parsedV2 := parseACPPromptV2([]acp2.ContentBlock{
		schemav2.NewContentBlock(schemav2.ContentBlockResource{Resource: emptyResV2}),
		acp2.TextBlock("/clear"),
	}, Config{Workspace: home})
	if parsedV2.Command != "clear" {
		t.Fatalf("v2 Command = %q, want %q", parsedV2.Command, "clear")
	}
	if !strings.Contains(parsedV2.ModelPrompt, "[Attached Resource: /tmp/zero.txt (editor contents)]") {
		t.Errorf("expected 0-byte embedded file preserved in v2 ModelPrompt, got: %q", parsedV2.ModelPrompt)
	}
}

func TestACPV1_LoadSession_UnknownReturns32002(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	srv, err := New(Config{
		Model:      "test-model",
		Workspace:  t.TempDir(),
		BackendFor: func(string) backend.Client { return backend.NewMock() },
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agentConn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return NewACPV1Agent(srv, c)
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	clientConn := acp1.NewClientSideConnection(func(c *acp1.ClientSideConnection) acp1.Client {
		return &dummyV1Client{}
	}, acp.NewStdioTransport(clientR, clientW))
	go func() { _ = clientConn.Start(ctx) }()

	if _, err := clientConn.Initialize(ctx, &acp1.InitializeRequest{ProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}

	_, err = clientConn.LoadSession(ctx, &acp1.LoadSessionRequest{
		SessionID: "nonexistent-session-id",
		Cwd:       srv.cfg.Workspace,
	})
	if err == nil {
		t.Fatalf("expected error for unknown session ID")
	}
	var rpcErr *acp.RequestError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *acp.RequestError, got %T: %v", err, err)
	}
	if rpcErr.Code != -32002 {
		t.Fatalf("RequestError.Code = %d, want -32002", rpcErr.Code)
	}
	if id, ok := acp.SessionNotFoundID(err); !ok || id != "nonexistent-session-id" {
		t.Fatalf("SessionNotFoundID = (%q, %v), want (%q, true)", id, ok, "nonexistent-session-id")
	}
}

func TestACPV2_ResumeSession_UnknownReturns32002(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	srv, err := New(Config{
		Model:      "test-model",
		Workspace:  t.TempDir(),
		BackendFor: func(string) backend.Client { return backend.NewMock() },
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agentConn := acp2.NewAgentSideConnection(func(c *acp2.AgentSideConnection) acp2.Agent {
		return NewACPV2Agent(srv, c)
	}, acp.NewStdioTransport(agentR, agentW))
	go func() { _ = agentConn.Start(ctx) }()

	clientConn := acp2.NewClientSideConnection(func(c *acp2.ClientSideConnection) acp2.Client {
		return &dummyV2Client{}
	}, acp.NewStdioTransport(clientR, clientW))
	go func() { _ = clientConn.Start(ctx) }()

	if _, err := clientConn.Initialize(ctx, &acp2.InitializeRequest{ProtocolVersion: 2}); err != nil {
		t.Fatal(err)
	}

	_, err = clientConn.ResumeSession(ctx, &acp2.ResumeSessionRequest{
		SessionID: "nonexistent-v2-session",
		Cwd:       acp2.AbsolutePath(srv.cfg.Workspace),
	})
	if err == nil {
		t.Fatalf("expected error for unknown v2 session ID")
	}
	var rpcErr *acp.RequestError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *acp.RequestError, got %T: %v", err, err)
	}
	if rpcErr.Code != -32002 {
		t.Fatalf("RequestError.Code = %d, want -32002", rpcErr.Code)
	}
	if id, ok := acp.SessionNotFoundID(err); !ok || id != "nonexistent-v2-session" {
		t.Fatalf("SessionNotFoundID = (%q, %v), want (%q, true)", id, ok, "nonexistent-v2-session")
	}
}

func TestACPV1_BackgroundTask_KeepsPromptOpenAndCombinesUsage_DaemonEndsImmediately(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	ws := t.TempDir()

	var callCount atomic.Int32
	mock := &testCustomMockBackend{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			out := make(chan backend.StreamChunk, 4)
			go func() {
				defer close(out)
				n := callCount.Add(1)
				hasTool := false
				hasNotif := false
				for _, m := range req.Messages {
					if m.Role == "tool" {
						hasTool = true
					}
					if m.Role == "user" && strings.Contains(m.Content, "<SYSTEM_MESSAGE>") {
						hasNotif = true
					}
				}
				if !hasTool && !hasNotif {
					args, _ := json.Marshal(map[string]any{
						"DurationSeconds": 1,
						"Prompt":          "timer done",
					})
					out <- backend.StreamChunk{
						ToolCalls: []backend.ToolCall{{
							ID:        "call_sched_1",
							Name:      "schedule",
							Arguments: args,
						}},
						Meta: &backend.ChunkMeta{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
						Done: true,
					}
					return
				}
				if hasTool && !hasNotif {
					out <- backend.StreamChunk{
						Text: "Scheduled timer.",
						Meta: &backend.ChunkMeta{PromptTokens: 20, CompletionTokens: 5, TotalTokens: 25},
						Done: true,
					}
					return
				}
				_ = n
				out <- backend.StreamChunk{
					Text: "Timer completed!",
					Meta: &backend.ChunkMeta{PromptTokens: 30, CompletionTokens: 10, TotalTokens: 40},
					Done: true,
				}
			}()
			return out, nil
		},
	}

	srv, err := New(Config{
		Model:           "test-model",
		Workspace:       ws,
		SkipPermissions: true,
		BackendFor:      func(string) backend.Client { return mock },
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := &e2eRecordingV1Client{}
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

	promptResp, err := clientConn.Prompt(ctx, &acp1.PromptRequest{
		SessionID: sessResp.SessionID,
		Prompt:    []acp1.ContentBlock{acp1.TextBlock("Schedule a 1s timer")},
	})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}
	if promptResp.StopReason != acp1.StopReasonEndTurn {
		t.Fatalf("StopReason = %q, want end_turn", promptResp.StopReason)
	}

	_, assists := client.historySummary()
	joined := strings.Join(assists, "")
	if !strings.Contains(joined, "Scheduled timer.") || !strings.Contains(joined, "Timer completed!") {
		t.Fatalf("expected both initial and follow-up assistant text before Prompt returned, got: %q", joined)
	}
	if promptResp.Usage == nil || promptResp.Usage.TotalTokens < 65 {
		t.Fatalf("expected combined token usage across initial + follow-up turns (>=65), got: %+v", promptResp.Usage)
	}

	// Now verify daemon task ends turn immediately without waiting.
	daemonMock := &testCustomMockBackend{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			out := make(chan backend.StreamChunk, 2)
			go func() {
				defer close(out)
				hasTool := false
				for _, m := range req.Messages {
					if m.Role == "tool" {
						hasTool = true
					}
				}
				if !hasTool {
					args, _ := json.Marshal(map[string]any{
						"CronExpression": "*/5 * * * *",
						"IsDaemon":       true,
						"Prompt":         "daemon tick",
					})
					out <- backend.StreamChunk{
						ToolCalls: []backend.ToolCall{{
							ID:        "call_daemon_1",
							Name:      "schedule",
							Arguments: args,
						}},
						Meta: &backend.ChunkMeta{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
						Done: true,
					}
					return
				}
				out <- backend.StreamChunk{
					Text: "Daemon started.",
					Meta: &backend.ChunkMeta{PromptTokens: 12, CompletionTokens: 3, TotalTokens: 15},
					Done: true,
				}
			}()
			return out, nil
		},
	}
	srvDaemon, err := New(Config{
		Model:           "test-model",
		Workspace:       ws,
		SkipPermissions: true,
		BackendFor:      func(string) backend.Client { return daemonMock },
	})
	if err != nil {
		t.Fatal(err)
	}
	dAgentR, dClientW := io.Pipe()
	dClientR, dAgentW := io.Pipe()
	defer dAgentR.Close()
	defer dClientW.Close()
	defer dClientR.Close()
	defer dAgentW.Close()

	dClient := &e2eRecordingV1Client{}
	dAgentConn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return NewACPV1Agent(srvDaemon, c)
	}, acp.NewStdioTransport(dAgentR, dAgentW))
	go func() { _ = dAgentConn.Start(ctx) }()
	dClientConn := acp1.NewClientSideConnection(func(c *acp1.ClientSideConnection) acp1.Client {
		return dClient
	}, acp.NewStdioTransport(dClientR, dClientW))
	go func() { _ = dClientConn.Start(ctx) }()

	if _, err := dClientConn.Initialize(ctx, &acp1.InitializeRequest{ProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}
	dSess, err := dClientConn.NewSession(ctx, &acp1.NewSessionRequest{Cwd: ws})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	dResp, err := dClientConn.Prompt(ctx, &acp1.PromptRequest{
		SessionID: dSess.SessionID,
		Prompt:    []acp1.ContentBlock{acp1.TextBlock("Start daemon cron")},
	})
	if err != nil {
		t.Fatalf("daemon Prompt failed: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("daemon Prompt took too long (%v); should end turn immediately", time.Since(start))
	}
	if dResp.StopReason != acp1.StopReasonEndTurn {
		t.Fatalf("daemon StopReason = %q, want end_turn", dResp.StopReason)
	}
	_, _ = dClientConn.CloseSession(ctx, &acp1.CloseSessionRequest{SessionID: dSess.SessionID})
}

type blockingPermV1Client struct {
	e2eRecordingV1Client
	permSeen    chan struct{}
	blockCh     chan struct{}
	failOnBlock bool
}

func (c *blockingPermV1Client) RequestPermission(ctx context.Context, req *acp1.RequestPermissionRequest) (*acp1.RequestPermissionResponse, error) {
	select {
	case c.permSeen <- struct{}{}:
	default:
	}
	select {
	case <-c.blockCh:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if c.failOnBlock {
		return nil, errors.New("client disconnected")
	}
	optID := acp1.PermissionOptionID(acp1.PermissionOptionKindAllowOnce)
	if len(req.Options) > 0 {
		optID = req.Options[0].OptionID
	}
	return acp1.PermissionSelected(optID), nil
}

func TestACPV1_LoadSession_RestoresPendingPermissionPrompt(t *testing.T) {
	// The scenario requires Ask, not the clone's allow-by-default fallback or
	// an ambient user's auto mode. Isolate the highest-precedence data root
	// and explicitly configure review before creating the server session.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", home)
	dataDir := t.TempDir()
	t.Setenv("OPENAGY_DATA_DIR", dataDir)
	t.Setenv("ANTIGRAVITY_HOME", dataDir)
	if err := os.WriteFile(filepath.Join(dataDir, "settings.json"), []byte(`{"agentMode":"","toolPermission":"strict","permissions":{"ask":["command(touch perm-restored.txt)"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()

	mock := &testCustomMockBackend{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			out := make(chan backend.StreamChunk, 2)
			go func() {
				defer close(out)
				hasTool := false
				for _, m := range req.Messages {
					if m.Role == "tool" {
						hasTool = true
					}
				}
				if !hasTool {
					args, _ := json.Marshal(map[string]any{
						"CommandLine":       "touch perm-restored.txt",
						"toolSummary":       "Touch perm file",
						"WaitMsBeforeAsync": 1000,
					})
					out <- backend.StreamChunk{
						ToolCalls: []backend.ToolCall{{
							ID:        "call_perm_1",
							Name:      "run_command",
							Arguments: args,
						}},
						Done: true,
					}
					return
				}
				out <- backend.StreamChunk{
					Text: "Permission approved after reconnect!",
					Done: true,
				}
			}()
			return out, nil
		},
	}

	srv, err := New(Config{
		Model:      "test-model",
		Workspace:  ws,
		BackendFor: func(string) backend.Client { return mock },
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	agentR1, clientW1 := io.Pipe()
	clientR1, agentW1 := io.Pipe()
	client1 := &blockingPermV1Client{
		permSeen:    make(chan struct{}, 1),
		blockCh:     make(chan struct{}),
		failOnBlock: true,
	}
	clientConn1 := acp1.NewClientSideConnection(func(c *acp1.ClientSideConnection) acp1.Client {
		return client1
	}, acp.NewStdioTransport(clientR1, clientW1))
	go func() { _ = srv.ServeACP(ctx, agentW1, agentR1) }()
	go func() { _ = clientConn1.Start(ctx) }()

	if _, err := clientConn1.Initialize(ctx, &acp1.InitializeRequest{ProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}
	sessResp, err := clientConn1.NewSession(ctx, &acp1.NewSessionRequest{Cwd: ws})
	if err != nil {
		t.Fatal(err)
	}

	promptDone := make(chan error, 1)
	go func() {
		_, pErr := clientConn1.Prompt(ctx, &acp1.PromptRequest{
			SessionID: sessResp.SessionID,
			Prompt:    []acp1.ContentBlock{acp1.TextBlock("Run echo")},
		})
		promptDone <- pErr
	}()

	select {
	case <-client1.permSeen:
	case pErr := <-promptDone:
		_, assistants := client1.historySummary()
		t.Fatalf("turn ended before its required permission request: err=%v, text=%q", pErr, assistants)
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for permission prompt on client 1")
	}

	// Simulate client 1 disconnecting while permission prompt is pending.
	_ = clientW1.Close()
	_ = clientR1.Close()
	_ = agentW1.Close()
	_ = agentR1.Close()
	close(client1.blockCh)

	// Reconnect with client 2 and call LoadSession.
	agentR2, clientW2 := io.Pipe()
	clientR2, agentW2 := io.Pipe()
	defer agentR2.Close()
	defer clientW2.Close()
	defer clientR2.Close()
	defer agentW2.Close()

	var permReemitted atomic.Bool
	client2 := &blockingPermV1Client{
		permSeen: make(chan struct{}, 1),
		blockCh:  make(chan struct{}),
	}
	close(client2.blockCh) // auto-approve immediately on client 2
	clientConn2 := acp1.NewClientSideConnection(func(c *acp1.ClientSideConnection) acp1.Client {
		return client2
	}, acp.NewStdioTransport(clientR2, clientW2))
	go func() { _ = srv.ServeACP(ctx, agentW2, agentR2) }()
	go func() { _ = clientConn2.Start(ctx) }()

	if _, err := clientConn2.Initialize(ctx, &acp1.InitializeRequest{ProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := clientConn2.LoadSession(ctx, &acp1.LoadSessionRequest{
		SessionID: sessResp.SessionID,
		Cwd:       ws,
	}); err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	select {
	case <-client2.permSeen:
		permReemitted.Store(true)
	case <-time.After(10 * time.Second):
		t.Fatalf("expected pending permission prompt to be re-emitted on LoadSession")
	}
	if !permReemitted.Load() {
		t.Fatalf("permission prompt was not re-emitted")
	}

	select {
	case pErr := <-promptDone:
		if pErr != nil {
			t.Logf("client 1 prompt returned after disconnect: %v", pErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for original turn to finish after client 2 approved permission")
	}

	// Verify the approved tool actually executed on the server after client 2 approved.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(ws, "perm-restored.txt")); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected perm-restored.txt to be created after client 2 approved restored permission prompt")
}

type toolUpdateRecordingV1Client struct {
	e2eRecordingV1Client
	toolStarted chan struct{}
	toolUpdates []schemav1.SessionUpdateToolCallUpdate
}

func (c *toolUpdateRecordingV1Client) SessionUpdate(ctx context.Context, n *acp1.SessionNotification) error {
	_ = c.e2eRecordingV1Client.SessionUpdate(ctx, n)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := n.Update.As[schemav1.SessionUpdateToolCall](); ok {
		select {
		case c.toolStarted <- struct{}{}:
		default:
		}
	}
	if tu, ok := n.Update.As[schemav1.SessionUpdateToolCallUpdate](); ok {
		c.toolUpdates = append(c.toolUpdates, tu)
	}
	return nil
}

func TestACPV1_CancelClosesInProgressTool_And_FailedToolIncludesError(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	ws := t.TempDir()

	// 1. Failed tool with empty output includes error message text.
	failMock := &testCustomMockBackend{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			out := make(chan backend.StreamChunk, 2)
			go func() {
				defer close(out)
				hasTool := false
				for _, m := range req.Messages {
					if m.Role == "tool" {
						hasTool = true
					}
				}
				if !hasTool {
					// Missing required Path triggers tool error with empty output.
					args, _ := json.Marshal(map[string]any{})
					out <- backend.StreamChunk{
						ToolCalls: []backend.ToolCall{{
							ID:        "call_fail_empty",
							Name:      "read_file",
							Arguments: args,
						}},
						Done: true,
					}
					return
				}
				out <- backend.StreamChunk{Text: "Handled error.", Done: true}
			}()
			return out, nil
		},
	}
	srvFail, err := New(Config{
		Model:           "test-model",
		Workspace:       ws,
		SkipPermissions: true,
		BackendFor:      func(string) backend.Client { return failMock },
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fAgentR, fClientW := io.Pipe()
	fClientR, fAgentW := io.Pipe()
	defer fAgentR.Close()
	defer fClientW.Close()
	defer fClientR.Close()
	defer fAgentW.Close()

	fClient := &toolUpdateRecordingV1Client{toolStarted: make(chan struct{}, 4)}
	fAgentConn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return NewACPV1Agent(srvFail, c)
	}, acp.NewStdioTransport(fAgentR, fAgentW))
	go func() { _ = fAgentConn.Start(ctx) }()
	fClientConn := acp1.NewClientSideConnection(func(c *acp1.ClientSideConnection) acp1.Client {
		return fClient
	}, acp.NewStdioTransport(fClientR, fClientW))
	go func() { _ = fClientConn.Start(ctx) }()

	if _, err := fClientConn.Initialize(ctx, &acp1.InitializeRequest{ProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}
	fSess, err := fClientConn.NewSession(ctx, &acp1.NewSessionRequest{Cwd: ws})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fClientConn.Prompt(ctx, &acp1.PromptRequest{
		SessionID: fSess.SessionID,
		Prompt:    []acp1.ContentBlock{acp1.TextBlock("Trigger failed tool")},
	}); err != nil {
		t.Fatal(err)
	}

	fClient.mu.Lock()
	var sawFailedWithMsg bool
	for _, tu := range fClient.toolUpdates {
		if tu.Status != nil && *tu.Status == schemav1.ToolCallStatusFailed {
			if len(tu.Content) > 0 || tu.RawOutput != nil {
				sawFailedWithMsg = true
			}
		}
	}
	fClient.mu.Unlock()
	if !sawFailedWithMsg {
		t.Fatalf("expected failed tool call update to include error message content/rawOutput")
	}

	// 2. CancelSession closes stuck in-progress tool call as failed.
	slowMock := &testCustomMockBackend{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			out := make(chan backend.StreamChunk, 2)
			go func() {
				defer close(out)
				for _, m := range req.Messages {
					if m.Role == "tool" {
						out <- backend.StreamChunk{Text: "Cancelled.", Done: true}
						return
					}
				}
				args, _ := json.Marshal(map[string]any{
					"CommandLine":       "sleep 30",
					"toolSummary":       "Slow command",
					"WaitMsBeforeAsync": 10000,
				})
				out <- backend.StreamChunk{
					ToolCalls: []backend.ToolCall{{
						ID:        "call_slow_cancel",
						Name:      "run_command",
						Arguments: args,
					}},
					Done: true,
				}
			}()
			return out, nil
		},
	}
	srvSlow, err := New(Config{
		Model:           "test-model",
		Workspace:       ws,
		SkipPermissions: true,
		BackendFor:      func(string) backend.Client { return slowMock },
	})
	if err != nil {
		t.Fatal(err)
	}

	sAgentR, sClientW := io.Pipe()
	sClientR, sAgentW := io.Pipe()
	defer sAgentR.Close()
	defer sClientW.Close()
	defer sClientR.Close()
	defer sAgentW.Close()

	sClient := &toolUpdateRecordingV1Client{toolStarted: make(chan struct{}, 4)}
	sAgentConn := acp1.NewAgentSideConnection(func(c *acp1.AgentSideConnection) acp1.Agent {
		return NewACPV1Agent(srvSlow, c)
	}, acp.NewStdioTransport(sAgentR, sAgentW))
	go func() { _ = sAgentConn.Start(ctx) }()
	sClientConn := acp1.NewClientSideConnection(func(c *acp1.ClientSideConnection) acp1.Client {
		return sClient
	}, acp.NewStdioTransport(sClientR, sClientW))
	go func() { _ = sClientConn.Start(ctx) }()

	if _, err := sClientConn.Initialize(ctx, &acp1.InitializeRequest{ProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}
	sSess, err := sClientConn.NewSession(ctx, &acp1.NewSessionRequest{Cwd: ws})
	if err != nil {
		t.Fatal(err)
	}

	promptResCh := make(chan acp1.StopReason, 1)
	go func() {
		resp, _ := sClientConn.Prompt(ctx, &acp1.PromptRequest{
			SessionID: sSess.SessionID,
			Prompt:    []acp1.ContentBlock{acp1.TextBlock("Run slow command")},
		})
		if resp != nil {
			promptResCh <- resp.StopReason
		} else {
			promptResCh <- ""
		}
	}()

	select {
	case <-sClient.toolStarted:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for slow tool call to start")
	}

	if err := sClientConn.CancelSession(ctx, &acp1.CancelNotification{SessionID: sSess.SessionID}); err != nil {
		t.Fatalf("CancelSession failed: %v", err)
	}

	select {
	case sr := <-promptResCh:
		if sr != acp1.StopReasonCancelled {
			t.Fatalf("StopReason = %q, want cancelled", sr)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for cancelled Prompt to return")
	}

	sClient.mu.Lock()
	var sawCancelledFailed bool
	for _, tu := range sClient.toolUpdates {
		if tu.Status != nil && *tu.Status == schemav1.ToolCallStatusFailed {
			sawCancelledFailed = true
		}
	}
	sClient.mu.Unlock()
	if !sawCancelledFailed {
		t.Fatalf("expected in-progress tool call to be closed with ToolCallStatusFailed on cancel")
	}
}

var _ = errors.New
