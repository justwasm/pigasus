package agentturn

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
)

func TestCodexRemote_MockTurn(t *testing.T) {
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()
	defer clientR.Close()
	defer serverW.Close()
	defer serverR.Close()
	defer clientW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Mock Codex app-server goroutine
	go func() {
		scanner := bufio.NewScanner(serverR)
		for scanner.Scan() {
			var req codexRPCRequest
			if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
				return
			}

			switch req.Method {
			case "initialize":
				resp := map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result": map[string]any{
						"userAgent": "codex/mock",
						"codexHome": "/tmp/mock",
					},
				}
				data, _ := json.Marshal(resp)
				_, _ = fmt.Fprintf(serverW, "%s\n", data)

			case "thread/start":
				resp := map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result": map[string]any{
						"thread": map[string]any{
							"id": "thread_mock_001",
						},
					},
				}
				data, _ := json.Marshal(resp)
				_, _ = fmt.Fprintf(serverW, "%s\n", data)

			case "turn/start":
				// 1. Send accepted response
				resp := map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result": map[string]any{
						"turn": map[string]any{
							"id": "turn_mock_001",
						},
					},
				}
				data, _ := json.Marshal(resp)
				_, _ = fmt.Fprintf(serverW, "%s\n", data)

				// 2. Stream notifications
				// a) reasoning
				n1 := map[string]any{
					"jsonrpc": "2.0",
					"method":  "item/reasoning/textDelta",
					"params":  map[string]any{"delta": "thinking about solution"},
				}
				d1, _ := json.Marshal(n1)
				_, _ = fmt.Fprintf(serverW, "%s\n", d1)

				// b) agentMessage
				n2 := map[string]any{
					"jsonrpc": "2.0",
					"method":  "item/agentMessage/delta",
					"params":  map[string]any{"delta": "Hello from mock Codex!"},
				}
				d2, _ := json.Marshal(n2)
				_, _ = fmt.Fprintf(serverW, "%s\n", d2)

				// c) tool start
				n3 := map[string]any{
					"jsonrpc": "2.0",
					"method":  "item/started",
					"params": map[string]any{
						"item": map[string]any{
							"id":      "call_101",
							"type":    "commandExecution",
							"command": "ls -l",
						},
					},
				}
				d3, _ := json.Marshal(n3)
				_, _ = fmt.Fprintf(serverW, "%s\n", d3)

				// d) tool completed
				exitZero := 0
				n4 := map[string]any{
					"jsonrpc": "2.0",
					"method":  "item/completed",
					"params": map[string]any{
						"item": map[string]any{
							"id":       "call_101",
							"type":     "commandExecution",
							"exitCode": &exitZero,
							"output":   "total 0\n-rw-r--r-- file.txt",
						},
					},
				}
				d4, _ := json.Marshal(n4)
				_, _ = fmt.Fprintf(serverW, "%s\n", d4)

				// e) token usage
				n5 := map[string]any{
					"jsonrpc": "2.0",
					"method":  "thread/tokenUsage/updated",
					"params": map[string]any{
						"tokenUsage": map[string]any{
							"promptTokens":     120,
							"completionTokens": 45,
							"totalTokens":      165,
						},
					},
				}
				d5, _ := json.Marshal(n5)
				_, _ = fmt.Fprintf(serverW, "%s\n", d5)

				// f) turn completed
				n6 := map[string]any{
					"jsonrpc": "2.0",
					"method":  "turn/completed",
					"params": map[string]any{
						"turn": map[string]any{
							"id":     "turn_mock_001",
							"status": "completed",
						},
					},
				}
				d6, _ := json.Marshal(n6)
				_, _ = fmt.Fprintf(serverW, "%s\n", d6)
			}
		}
	}()

	remote, err := NewCodexRemote(ctx, clientW, clientR, nil, "/mock/cwd", "gpt-6.1-sol")
	if err != nil {
		t.Fatalf("NewCodexRemote failed: %v", err)
	}
	defer remote.Close()

	if remote.ThreadID() != "thread_mock_001" {
		t.Fatalf("unexpected thread ID: %s", remote.ThreadID())
	}

	framesCh, err := remote.Turn(ctx, Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Hello Codex"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn failed: %v", err)
	}

	var gotThought, gotContent, gotCall, gotResult, gotUsage bool
	for f := range framesCh {
		switch f.Kind {
		case agentlink.KindThought:
			if f.Text == "thinking about solution" {
				gotThought = true
			}
		case agentlink.KindText:
			if f.Text == "Hello from mock Codex!" {
				gotContent = true
			}
		case agentlink.KindTool:
			if f.Tool != nil && f.Tool.Phase == agentlink.ToolStart && f.Tool.Args == "ls -l" {
				gotCall = true
			}
			if f.Tool != nil && f.Tool.Phase == agentlink.ToolEnd && f.Tool.Result == "total 0\n-rw-r--r-- file.txt" {
				gotResult = true
			}
		case agentlink.KindResult:
			if f.Usage != nil && f.Usage.PromptTokens == 120 && f.Usage.CompletionTokens == 45 {
				gotUsage = true
			}
		}
	}

	if !gotThought {
		t.Errorf("missing reasoning thought frame")
	}
	if !gotContent {
		t.Errorf("missing content delta frame")
	}
	if !gotCall {
		t.Errorf("missing tool call frame")
	}
	if !gotResult {
		t.Errorf("missing tool result frame")
	}
	if !gotUsage {
		t.Errorf("missing token usage frame")
	}
}

func TestCodexRemote_ServerRequestApproval(t *testing.T) {
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()
	defer clientR.Close()
	defer serverW.Close()
	defer serverR.Close()
	defer clientW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	approvalReceived := make(chan bool, 1)

	go func() {
		scanner := bufio.NewScanner(serverR)
		for scanner.Scan() {
			var raw map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
				return
			}

			// If it's a response to approval request
			if id, ok := raw["id"].(float64); ok && uint64(id) == 999 {
				if res, ok := raw["result"].(map[string]any); ok {
					if res["decision"] == "approved" {
						approvalReceived <- true
					}
				}
				return
			}

			method, _ := raw["method"].(string)
			reqID, _ := raw["id"].(float64)

			switch method {
			case "initialize":
				resp := map[string]any{
					"jsonrpc": "2.0",
					"id":      uint64(reqID),
					"result":  map[string]any{"userAgent": "mock"},
				}
				d, _ := json.Marshal(resp)
				_, _ = fmt.Fprintf(serverW, "%s\n", d)

			case "thread/start":
				resp := map[string]any{
					"jsonrpc": "2.0",
					"id":      uint64(reqID),
					"result":  map[string]any{"thread": map[string]any{"id": "th_1"}},
				}
				d, _ := json.Marshal(resp)
				_, _ = fmt.Fprintf(serverW, "%s\n", d)

				// Server sends an approval request to the client
				req := map[string]any{
					"jsonrpc": "2.0",
					"id":      999,
					"method":  "item/commandExecution/requestApproval",
					"params":  map[string]any{"command": "rm -rf /tmp/test"},
				}
				rd, _ := json.Marshal(req)
				_, _ = fmt.Fprintf(serverW, "%s\n", rd)
			}
		}
	}()

	remote, err := NewCodexRemote(ctx, clientW, clientR, nil, "/tmp", "default")
	if err != nil {
		t.Fatalf("NewCodexRemote failed: %v", err)
	}
	defer remote.Close()

	select {
	case ok := <-approvalReceived:
		if !ok {
			t.Errorf("approval decision not approved")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for approval response")
	}
}

func TestCodexRemote_LiveInitAndThread(t *testing.T) {
	bin := "/opt/homebrew/bin/codex"
	if _, err := os.Stat(bin); os.IsNotExist(err) {
		t.Skip("codex binary not found at /opt/homebrew/bin/codex")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote, err := SpawnCodexRemote(ctx, bin, t.TempDir(), "gpt-6.1-sol")
	if err != nil {
		t.Fatalf("SpawnCodexRemote failed: %v", err)
	}
	defer remote.Close()

	if remote.ThreadID() == "" {
		t.Fatalf("expected non-empty thread ID from live codex app-server")
	}
	t.Logf("Live codex app-server initialized with thread ID: %s", remote.ThreadID())
}

func TestCodexRemote_NdjsonModelDiscovery(t *testing.T) {
	bin := "/opt/homebrew/bin/codex"
	if _, err := os.Stat(bin); os.IsNotExist(err) {
		t.Skip("codex binary not found at /opt/homebrew/bin/codex")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Spawn without passing any model: Codex app-server will resolve its configuration
	// and return the effective model in the thread/start ndjson response.
	remote, err := SpawnCodexRemote(ctx, bin, t.TempDir(), "")
	if err != nil {
		t.Fatalf("SpawnCodexRemote failed: %v", err)
	}
	defer remote.Close()

	defModel := remote.DefaultModel()
	if defModel == "" {
		t.Fatalf("expected DefaultModel() to be populated from ndjson response, got empty")
	}
	t.Logf("Discovered model from ndjson: %s", defModel)
}

func TestCodexRemote_ErrorHandlingAndRetry(t *testing.T) {
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()
	defer clientR.Close()
	defer serverW.Close()
	defer serverR.Close()
	defer clientW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		scanner := bufio.NewScanner(serverR)
		for scanner.Scan() {
			var req codexRPCRequest
			if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
				return
			}
			switch req.Method {
			case "initialize":
				resp := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"userAgent": "mock"}}
				d, _ := json.Marshal(resp)
				_, _ = fmt.Fprintf(serverW, "%s\n", d)
			case "thread/start":
				resp := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"thread": map[string]any{"id": "th_1"}}}
				d, _ := json.Marshal(resp)
				_, _ = fmt.Fprintf(serverW, "%s\n", d)
			case "turn/start":
				resp := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"turn": map[string]any{"id": "tu_1"}}}
				d, _ := json.Marshal(resp)
				_, _ = fmt.Fprintf(serverW, "%s\n", d)

				// 1. Send warning
				w := map[string]any{"jsonrpc": "2.0", "method": "warning", "params": map[string]any{"message": "test warning"}}
				dw, _ := json.Marshal(w)
				_, _ = fmt.Fprintf(serverW, "%s\n", dw)

				// 2. Send retry error
				e1 := map[string]any{
					"jsonrpc": "2.0",
					"method":  "error",
					"params": map[string]any{
						"error":     map[string]any{"message": "Reconnecting... 1/5"},
						"willRetry": true,
					},
				}
				de1, _ := json.Marshal(e1)
				_, _ = fmt.Fprintf(serverW, "%s\n", de1)

				// 3. Send turn/completed with error object
				tc := map[string]any{
					"jsonrpc": "2.0",
					"method":  "turn/completed",
					"params": map[string]any{
						"threadId": "th_1",
						"turn": map[string]any{
							"id":     "tu_1",
							"status": "failed",
							"error":  map[string]any{"message": "unexpected status 402 Payment Required"},
						},
					},
				}
				dtc, _ := json.Marshal(tc)
				_, _ = fmt.Fprintf(serverW, "%s\n", dtc)
			}
		}
	}()

	remote, err := NewCodexRemote(ctx, clientW, clientR, nil, "/tmp", "codex")
	if err != nil {
		t.Fatalf("NewCodexRemote failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, Turn{
		Model: "codex",
		History: []agentlink.Message{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn failed: %v", err)
	}

	var hasNotice, hasResultError bool
	for f := range frames {
		if f.Kind == agentlink.KindNotice {
			hasNotice = true
		}
		if f.Kind == agentlink.KindResult && f.Error != "" {
			hasResultError = true
		}
	}

	if !hasNotice {
		t.Errorf("expected KindNotice for retry error or warning")
	}
	if !hasResultError {
		t.Errorf("expected KindResult with error for completed failed turn")
	}
}

func TestCodexRemote_InterruptOnCancel(t *testing.T) {
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()
	defer clientR.Close()
	defer serverW.Close()
	defer serverR.Close()
	defer clientW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	interrupted := make(chan bool, 1)

	go func() {
		scanner := bufio.NewScanner(serverR)
		for scanner.Scan() {
			var req codexRPCRequest
			if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
				return
			}
			switch req.Method {
			case "initialize":
				resp := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"userAgent": "mock"}}
				d, _ := json.Marshal(resp)
				_, _ = fmt.Fprintf(serverW, "%s\n", d)
			case "thread/start":
				resp := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"thread": map[string]any{"id": "th_2"}}}
				d, _ := json.Marshal(resp)
				_, _ = fmt.Fprintf(serverW, "%s\n", d)
			case "turn/start":
				resp := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"turn": map[string]any{"id": "tu_2"}}}
				d, _ := json.Marshal(resp)
				_, _ = fmt.Fprintf(serverW, "%s\n", d)
			case "turn/interrupt":
				var p struct {
					ThreadID string `json:"threadId"`
					TurnID   string `json:"turnId"`
				}
				raw, _ := json.Marshal(req.Params)
				_ = json.Unmarshal(raw, &p)
				if p.ThreadID == "th_2" && p.TurnID == "tu_2" {
					interrupted <- true
				}
				resp := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"status": "interrupted"}}
				d, _ := json.Marshal(resp)
				_, _ = fmt.Fprintf(serverW, "%s\n", d)
			}
		}
	}()

	remote, err := NewCodexRemote(ctx, clientW, clientR, nil, "/tmp", "")
	if err != nil {
		t.Fatalf("NewCodexRemote failed: %v", err)
	}
	defer remote.Close()

	turnCtx, turnCancel := context.WithCancel(ctx)
	frames, err := remote.Turn(turnCtx, Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "do long work"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn failed: %v", err)
	}

	// Cancel context during turn
	time.Sleep(50 * time.Millisecond)
	turnCancel()

	select {
	case ok := <-interrupted:
		if !ok {
			t.Errorf("expected valid interrupt with threadId and turnId")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for turn/interrupt call")
	}

	// Channel should close promptly after cancel
	select {
	case _, ok := <-frames:
		if ok {
			// read until close
			for range frames {
			}
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("expected frames channel to close after turn cancellation")
	}
}
