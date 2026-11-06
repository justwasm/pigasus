package crush

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
)

func TestCrushRemote_DriverRegistration(t *testing.T) {
	d, ok := agentturn.Lookup("crush")
	if !ok {
		t.Fatal("expected 'crush' driver to be registered")
	}
	if d.Name != "crush" {
		t.Errorf("expected driver name 'crush', got %q", d.Name)
	}
	dAlias, ok := agentturn.Lookup("crush-app-server")
	if !ok || dAlias.Name != "crush" {
		t.Fatalf("expected 'crush-app-server' alias to resolve to 'crush'")
	}
}

func TestCrushRemote_AvailableCommands(t *testing.T) {
	r := &CrushRemote{}
	cmds := r.AvailableCommands()
	if len(cmds) == 0 {
		t.Fatalf("expected non-empty available commands for CrushRemote")
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
		t.Errorf("expected review in CrushRemote commands: %+v", cmds)
	}
}

func TestCrushRemote_Capabilities(t *testing.T) {
	r := &CrushRemote{}
	caps := r.Capabilities()
	if caps&agentturn.CapModelSwitch == 0 {
		t.Errorf("expected CapModelSwitch in CrushRemote capabilities")
	}
}

func TestCrushRemote_MockTurn(t *testing.T) {
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()
	defer clientR.Close()
	defer serverW.Close()
	defer serverR.Close()
	defer clientW.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Mock Crush AppServer goroutine
	go func() {
		scanner := bufio.NewScanner(serverR)
		for scanner.Scan() {
			var req map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
				return
			}
			id := req["id"]
			method, _ := req["method"].(string)

			switch method {
			case "initialize":
				resp := map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"result": map[string]any{
						"userAgent": "crush/1.0",
						"codexHome": "/tmp/.crush",
					},
				}
				data, _ := json.Marshal(resp)
				_, _ = serverW.Write(append(data, '\n'))

			case "thread/start":
				resp := map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"result": map[string]any{
						"model": "gpt-5.6-sol",
						"thread": map[string]any{
							"id": "th_12345",
						},
					},
				}
				data, _ := json.Marshal(resp)
				_, _ = serverW.Write(append(data, '\n'))

			case "turn/start":
				resp := map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"result": map[string]any{
						"turnId": "turn_123",
					},
				}
				data, _ := json.Marshal(resp)
				_, _ = serverW.Write(append(data, '\n'))

				// Send reasoning delta
				notif1 := map[string]any{
					"jsonrpc": "2.0",
					"method":  "item/reasoning/textDelta",
					"params":  map[string]any{"delta": "Thinking hard..."},
				}
				d1, _ := json.Marshal(notif1)
				_, _ = serverW.Write(append(d1, '\n'))

				// Send text delta
				notif2 := map[string]any{
					"jsonrpc": "2.0",
					"method":  "item/agentMessage/delta",
					"params":  map[string]any{"delta": "Hello from crush!"},
				}
				d2, _ := json.Marshal(notif2)
				_, _ = serverW.Write(append(d2, '\n'))

				// Send turn completed
				notif3 := map[string]any{
					"jsonrpc": "2.0",
					"method":  "turn/completed",
					"params": map[string]any{
						"threadId": "th_12345",
						"turn": map[string]any{
							"id":     "turn_123",
							"status": "completed",
						},
					},
				}
				d3, _ := json.Marshal(notif3)
				_, _ = serverW.Write(append(d3, '\n'))

			case "model/list":
				resp := map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"result": map[string]any{
						"data": []map[string]any{
							{
								"id":          "gpt-5.6-sol",
								"displayName": "GPT-5.6 Sol",
								"isDefault":   true,
							},
						},
					},
				}
				data, _ := json.Marshal(resp)
				_, _ = serverW.Write(append(data, '\n'))
			}
		}
	}()

	remote, err := NewCrushRemote(ctx, clientW, clientR, nil, "/tmp", "gpt-5.6-sol")
	if err != nil {
		t.Fatalf("failed to create CrushRemote: %v", err)
	}
	defer remote.Close()

	if remote.ThreadID() != "th_12345" {
		t.Errorf("expected thread ID th_12345, got %q", remote.ThreadID())
	}
	if remote.DefaultModel() != "gpt-5.6-sol" {
		t.Errorf("expected default model gpt-5.6-sol, got %q", remote.DefaultModel())
	}

	framesCh, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Turn returned error: %v", err)
	}

	var thoughts, text string
	for f := range framesCh {
		switch f.Kind {
		case agentlink.KindThought:
			thoughts += f.Text
		case agentlink.KindText:
			text += f.Text
		}
	}

	if thoughts != "Thinking hard..." {
		t.Errorf("expected thoughts 'Thinking hard...', got %q", thoughts)
	}
	if text != "Hello from crush!" {
		t.Errorf("expected text 'Hello from crush!', got %q", text)
	}

	models, err := remote.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	if len(models) != 1 || models[0].ID != "gpt-5.6-sol" {
		t.Errorf("unexpected models: %+v", models)
	}
}
