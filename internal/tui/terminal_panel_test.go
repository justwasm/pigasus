package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

func TestTerminalPanel_InteractiveSessionAndModalIsolation(t *testing.T) {
	var mu sync.Mutex
	var receivedInput []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, ":listTerminals"):
			_ = json.NewEncoder(w).Encode(map[string]any{"terminals": []any{}})
		case strings.HasSuffix(r.URL.Path, ":createTerminal"):
			_ = json.NewEncoder(w).Encode(map[string]any{"terminal": map[string]any{
				"terminalId": "term-test-42", "title": "Interactive Shell", "cwd": "/workspace", "pid": 1234,
			}})
		case strings.HasSuffix(r.URL.Path, ":sendTerminalInput"):
			var body struct {
				Input []byte `json:"input"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			receivedInput = append(receivedInput, body.Input...)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		case strings.HasSuffix(r.URL.Path, ":closeTerminal"):
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: srv.URL})
	engine := agentturn.NewRemoteAgentEngine(agentturn.RemoteAgentConfig{Client: client, Model: "m"})
	m := &RootModel{
		session: sessionState{remoteAgent: true},
		turn:    TurnService{engine: engine},
	}
	m.viewport.width = 100
	m.viewport.height = 30

	// 1. Launch /terminal -> creates terminal and opens modal overlay
	handled, _ := m.slashTerminal("")
	if !handled {
		t.Fatal("/terminal was not handled")
	}
	if m.overlays.panel.kind != panelregistry.Terminal {
		t.Fatalf("expected panel kind Terminal, got %v", m.overlays.panel.kind)
	}
	if m.remote.terminalID != "term-test-42" {
		t.Fatalf("expected terminalID term-test-42, got %q", m.remote.terminalID)
	}

	term := m.terminalPanel()
	if term == nil {
		t.Fatal("terminal panel is nil")
	}

	// 2. Stream chunk into terminal panel
	_, _, handled = m.updateStreamingMsg(terminalChunkMsg{Text: "Welcome to Pigasus Terminal\r\n$ "})
	if !handled {
		t.Fatalf("terminalChunkMsg was not handled")
	}
	screen := strings.Join(term.ScreenLines(), "\n")
	if !strings.Contains(screen, "Welcome to Pigasus Terminal") {
		t.Errorf("screen did not reflect streamed chunk:\n%s", screen)
	}

	// 3. User types "ls -la" + Enter
	for _, ch := range "ls -la" {
		term.HandleKey(tea.Key{Code: rune(ch), Text: string(ch)})
	}
	term.HandleKey(tea.Key{Code: tea.KeyEnter})

	mu.Lock()
	gotInput := string(receivedInput)
	receivedInput = nil
	mu.Unlock()
	if gotInput != "ls -la\r" {
		t.Errorf("expected received input 'ls -la\\r', got %q", gotInput)
	}

	// 4. Ctrl+C modal isolation (AGENTS.md §10):
	// Ctrl+C MUST be sent to PTY as 0x03 and MUST NOT dismiss modal
	term.HandleKey(tea.Key{Code: 'c', Mod: tea.ModCtrl})
	mu.Lock()
	gotCtrlC := receivedInput
	receivedInput = nil
	mu.Unlock()
	if len(gotCtrlC) != 1 || gotCtrlC[0] != 0x03 {
		t.Errorf("expected Ctrl+C to send 0x03, got %v", gotCtrlC)
	}
	if m.overlays.panel.kind != panelregistry.Terminal {
		t.Fatalf("Ctrl+C improperly closed the panel modal")
	}

	// 5. Esc modal isolation (AGENTS.md §10):
	// Esc detaches the panel modal, returning to None, leaving terminal ID active
	handled, cmd := term.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Errorf("Esc was not handled")
	}
	if cmd != nil {
		_ = cmd() // execute detach cmd (calls closePanel)
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("Esc did not detach panel, kind=%v", m.overlays.panel.kind)
	}
	if m.remote.terminalID != "term-test-42" {
		t.Errorf("terminal session was improperly terminated on detach")
	}
}
