package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
)

// TestRemoteDiffFeedsThePanel pins the host seam: getTurnDiff's file map
// becomes the panel's change list, and a file whose code-action step is
// known renders that step's getCodeActionDiff block in the inline view.
func TestRemoteDiffFeedsThePanel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ":ensureEnvironment"):
			_ = json.NewEncoder(w).Encode(map[string]any{"environment": map[string]any{"connectionState": "CONNECTED"}})
		case strings.HasSuffix(r.URL.Path, ":startCascade"):
			_ = json.NewEncoder(w).Encode(map[string]any{"cascadeId": "cascade-9"})
		case strings.HasSuffix(r.URL.Path, ":streamAgentStateUpdates"):
			update := map[string]any{
				"fullyIdle": true,
				"mainTrajectoryUpdate": map[string]any{"stepsUpdate": map[string]any{"steps": []any{
					map[string]any{
						"type":   86,
						"status": 3,
						"fileChange": map[string]any{
							"absolutePathUri": "file:///ws/main.go",
						},
					},
				}}},
			}
			payload, _ := json.Marshal(map[string]any{"update": update, "resumeToken": "tok-1"})
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		case strings.HasSuffix(r.URL.Path, ":getTurnDiff"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"fileDiffs": map[string]any{
					"main.go": map[string]any{
						"additions":        2,
						"deletions":        1,
						"originalContents": "package main\n\nfunc old() {}\n",
						"modifiedContents": "package main\n\nfunc new() {}\n",
					},
				},
				"totalAdditions": 2,
				"totalDeletions": 1,
				"turnStartIndex": 2,
			})
		case strings.HasSuffix(r.URL.Path, ":getCodeActionDiff"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["stepIndex"] != float64(0) {
				t.Errorf("getCodeActionDiff stepIndex = %v", body["stepIndex"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"diff": map[string]any{
				"startLine": 3,
				"endLine":   3,
				"unifiedDiff": map[string]any{"lines": []any{
					map[string]any{"type": "UNIFIED_DIFF_LINE_TYPE_DELETE", "text": "func old() {}"},
					map[string]any{"type": "UNIFIED_DIFF_LINE_TYPE_INSERT", "text": "func new() {}"},
				}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: srv.URL})
	engine := agentturn.NewRemoteAgentEngine(agentturn.RemoteAgentConfig{Client: client, Model: "gemini-3.8-flash-high", Workspace: "/ws"})
	ch, err := engine.Turn(context.Background(), agentturn.Turn{History: []agentlink.Message{{Role: "user", Content: "edit it"}}})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	for range ch {
	}

	m := &RootModel{
		session: sessionState{remoteAgent: true, cwd: "/ws"},
		turn:    TurnService{engine: engine},
	}
	if err := m.prepareRemoteDiff(); err != nil {
		t.Fatalf("prepareRemoteDiff: %v", err)
	}

	tracked, untracked, ok := m.RemoteDiffChanges()
	if !ok || len(untracked) != 0 || len(tracked) != 1 {
		t.Fatalf("changes = %+v / %+v (ok=%v)", tracked, untracked, ok)
	}
	if tracked[0].Path != "main.go" || tracked[0].Added != 2 || tracked[0].Deleted != 1 {
		t.Errorf("change = %+v", tracked[0])
	}
	if got := m.RemoteDiffVCS(); got != "remote" {
		t.Errorf("vcs = %q", got)
	}

	body, ok := m.RemoteDiffBody("main.go", "file")
	if !ok {
		t.Fatal("RemoteDiffBody reported no override")
	}
	joined := strings.Join(body, "\n")
	if !strings.Contains(joined, "-func old() {}") || !strings.Contains(joined, "+func new() {}") {
		t.Errorf("body did not render the code-action block: %q", joined)
	}
}

// TestRemoteDiffFallsBackToTurnContents covers a file with no recorded
// code-action step: the inline body is synthesized from the turn diff's
// two versions.
func TestRemoteDiffFallsBackToTurnContents(t *testing.T) {
	m := &RootModel{session: sessionState{remoteAgent: true}}
	m.diff.remote = &remoteDiffCache{
		files: map[string]remoteFileDiff{
			"a.go": {added: 1, deleted: 1, original: "old\n", modified: "new\n"},
		},
		loaded: true,
	}
	body, ok := m.RemoteDiffBody("a.go", "file")
	if !ok {
		t.Fatal("RemoteDiffBody reported no override")
	}
	joined := strings.Join(body, "\n")
	if !strings.Contains(joined, "-old") || !strings.Contains(joined, "+new") {
		t.Errorf("body = %q", joined)
	}
}

// TestRemoteDiffSeamIsInertLocally pins that a local session keeps the
// panel's own git path.
func TestRemoteDiffSeamIsInertLocally(t *testing.T) {
	m := &RootModel{}
	if _, _, ok := m.RemoteDiffChanges(); ok {
		t.Error("local session claimed a remote change set")
	}
	if _, ok := m.RemoteDiffBody("x", "file"); ok {
		t.Error("local session claimed a remote body")
	}
	if got := m.RemoteDiffVCS(); got != "" {
		t.Errorf("vcs = %q, want empty", got)
	}
}

// TestPrepareRemoteDiffBeforeTheFirstTurn keeps /diff friendly before a
// conversation exists: an empty list, not an error.
func TestPrepareRemoteDiffBeforeTheFirstTurn(t *testing.T) {
	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: "http://127.0.0.1:1"})
	engine := agentturn.NewRemoteAgentEngine(agentturn.RemoteAgentConfig{Client: client, Model: "m"})
	m := &RootModel{session: sessionState{remoteAgent: true}, turn: TurnService{engine: engine}}
	if err := m.prepareRemoteDiff(); err != nil {
		t.Fatalf("prepareRemoteDiff: %v", err)
	}
	tracked, untracked, ok := m.RemoteDiffChanges()
	if !ok || len(tracked) != 0 || len(untracked) != 0 {
		t.Errorf("changes = %+v / %+v (ok=%v)", tracked, untracked, ok)
	}
}
