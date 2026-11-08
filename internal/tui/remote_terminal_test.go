package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
)

func TestSlashTerminalCreatesAndLists(t *testing.T) {
	var created bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ":listTerminals"):
			if created {
				_ = json.NewEncoder(w).Encode(map[string]any{"terminals": []any{
					map[string]any{"terminalId": "term-1", "title": "Terminal", "cwd": "/ws"},
				}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"terminals": []any{}})
		case strings.HasSuffix(r.URL.Path, ":closeTerminal"):
			created = false
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		case strings.HasSuffix(r.URL.Path, ":createTerminal"):
			created = true
			_ = json.NewEncoder(w).Encode(map[string]any{"terminal": map[string]any{
				"terminalId": "term-1", "title": "Terminal", "cwd": "/ws", "pid": 7,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: srv.URL})
	engine := agentturn.NewRemoteAgentEngine(agentturn.RemoteAgentConfig{Client: client, Model: "m"})
	m := &RootModel{session: sessionState{remoteAgent: true}, turn: TurnService{engine: engine}}

	if handled, _ := m.slashTerminal(""); !handled {
		t.Fatal("/terminal was not handled")
	}
	if m.remote.terminalID != "term-1" {
		t.Fatalf("terminal id = %q", m.remote.terminalID)
	}
	joined := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joined, "Terminal term-1 created") {
		t.Errorf("response = %q", joined)
	}

	m.conversation.response = nil
	if handled, _ := m.slashTerminal("list"); !handled {
		t.Fatal("/terminal list was not handled")
	}
	joined = strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joined, "term-1") || !strings.Contains(joined, "/ws") {
		t.Errorf("listing = %q", joined)
	}

	if handled, _ := m.slashTerminal("close"); !handled {
		t.Fatal("/terminal close was not handled")
	}
	if m.remote.terminalID != "" {
		t.Errorf("terminal id survived close: %q", m.remote.terminalID)
	}
}

func TestSlashTerminalIsRemoteOnly(t *testing.T) {
	m := &RootModel{}
	if handled, _ := m.slashTerminal(""); !handled {
		t.Fatal("/terminal was not handled")
	}
	if !strings.Contains(strings.Join(m.conversation.response, "\n"), "only available in a --remote-agent session") {
		t.Errorf("response = %q", m.conversation.response)
	}
}

func TestTerminalOutputAndExitRows(t *testing.T) {
	rows := terminalOutputRows("\x1b[32mhello\x1b[0m\r\nworld\r\n")
	if len(rows) != 2 || rows[0] != "hello" || rows[1] != "world" {
		t.Fatalf("rows = %q", rows)
	}

	m := &RootModel{}
	for _, msg := range []any{
		terminalChunkMsg{Text: "one\ntwo\n"},
		terminalExitMsg{Code: 0},
		terminalExitMsg{Code: 2},
		terminalExitMsg{Err: context.Canceled},
	} {
		_, _, handled := m.updateStreamingMsg(msg.(interface{}))
		if !handled {
			t.Fatalf("%T was not handled", msg)
		}
	}
	joined := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joined, "one") || !strings.Contains(joined, "Process exited with code 0") ||
		!strings.Contains(joined, "Process exited with code 2") || !strings.Contains(joined, "context canceled") {
		t.Errorf("rows = %q", joined)
	}
}
