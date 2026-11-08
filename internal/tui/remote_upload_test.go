package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
)

func TestSlashUpload(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ":uploadProjectFile") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"resource": map[string]any{
			"resourceId": "note.txt", "displayName": "note.txt", "sizeBytes": 5, "projectId": "p1",
		}})
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: srv.URL, ProjectID: "p1"})
	engine := agentturn.NewRemoteAgentEngine(agentturn.RemoteAgentConfig{Client: client, Model: "m"})
	m := &RootModel{session: sessionState{remoteAgent: true}, turn: TurnService{engine: engine}}

	if handled, _ := m.slashUpload(path); !handled {
		t.Fatal("/upload was not handled")
	}
	joined := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joined, "Uploaded note.txt as note.txt (5 bytes)") {
		t.Errorf("response = %q", joined)
	}
	if got["fileName"] != "note.txt" || got["projectId"] != "p1" || got["content"] != "aGVsbG8=" {
		t.Errorf("request = %+v", got)
	}
}

func TestSlashUploadIsRemoteOnly(t *testing.T) {
	m := &RootModel{}
	if handled, _ := m.slashUpload("x"); !handled {
		t.Fatal("/upload was not handled")
	}
	if !strings.Contains(strings.Join(m.conversation.response, "\n"), "only available in a --remote-agent session") {
		t.Errorf("response = %q", m.conversation.response)
	}
}
