package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
)

func TestSlashTriggersLifecycle(t *testing.T) {
	var mu sync.Mutex
	var triggers []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ":listTriggers"):
			mu.Lock()
			_ = json.NewEncoder(w).Encode(map[string]any{"triggers": triggers})
			mu.Unlock()
		case strings.HasSuffix(r.URL.Path, ":listTriggerOptions"):
			_ = json.NewEncoder(w).Encode(map[string]any{"triggerOptions": []any{
				map[string]any{"id": "time", "displayName": "Scheduled"},
			}})
		case strings.HasSuffix(r.URL.Path, ":createTrigger"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			trigger := body["trigger"].(map[string]any)
			trigger["triggerId"] = "trig-1"
			trigger["state"] = "TRIGGER_STATE_ACTIVE"
			mu.Lock()
			triggers = append(triggers, trigger)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"trigger": trigger})
		case strings.HasSuffix(r.URL.Path, ":updateTrigger"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			trigger := body["trigger"].(map[string]any)
			mu.Lock()
			for i := range triggers {
				if triggers[i]["triggerId"] == trigger["triggerId"] {
					triggers[i] = trigger
				}
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"trigger": trigger})
		case strings.HasSuffix(r.URL.Path, ":deleteTrigger"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			out := triggers[:0]
			for _, tr := range triggers {
				if tr["triggerId"] != body["triggerId"] {
					out = append(out, tr)
				}
			}
			triggers = out
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: srv.URL})
	engine := agentturn.NewRemoteAgentEngine(agentturn.RemoteAgentConfig{Client: client, Model: "m"})
	m := &RootModel{session: sessionState{remoteAgent: true}, turn: TurnService{engine: engine}}

	m.slashTriggers("")
	if !strings.Contains(strings.Join(m.conversation.response, "\n"), "No remote triggers") {
		t.Errorf("empty listing = %q", m.conversation.response)
	}

	m.conversation.response = nil
	m.slashTriggers("add nightly checks")
	if !strings.Contains(strings.Join(m.conversation.response, "\n"), "Created trigger trig-1") {
		t.Errorf("create = %q", m.conversation.response)
	}

	m.conversation.response = nil
	m.slashTriggers("")
	joined := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joined, "trig-1") || !strings.Contains(joined, "TRIGGER_STATE_ACTIVE") {
		t.Errorf("listing = %q", joined)
	}

	m.conversation.response = nil
	m.slashTriggers("pause trig-1")
	if !strings.Contains(strings.Join(m.conversation.response, "\n"), "TRIGGER_STATE_PAUSED") {
		t.Errorf("pause = %q", m.conversation.response)
	}

	m.conversation.response = nil
	m.slashTriggers("delete trig-1")
	if !strings.Contains(strings.Join(m.conversation.response, "\n"), "Trigger trig-1 deleted") {
		t.Errorf("delete = %q", m.conversation.response)
	}
}

func TestSlashTriggersIsRemoteOnly(t *testing.T) {
	m := &RootModel{}
	m.slashTriggers("")
	if !strings.Contains(strings.Join(m.conversation.response, "\n"), "only available in a --remote-agent session") {
		t.Errorf("response = %q", m.conversation.response)
	}
}
