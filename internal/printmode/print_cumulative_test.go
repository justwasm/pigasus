// print_cumulative_test.go — cycle 312: the implicit trajectory reaches the
// model in the print path too.
//
// The reference's cumulative-prompt handler is not a TUI feature: it builds
// the prompt for every turn, headless or not. A `!` command the user ran
// outside the conversation therefore reaches a `-p` turn as well.
package printmode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/implicit"
	"github.com/justwasm/pigasus/internal/pigrt"
	"github.com/justwasm/pigasus/internal/tools"
)

// TestRunPigPrintTurnCarriesTheImplicitTrajectory pins the delivery: a `!`
// command in the implicit trajectory reaches the model with the turn, in the
// reference's own shape, and it comes before the turn's own prompt.
func TestRunPigPrintTurnCarriesTheImplicitTrajectory(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())

	var mu sync.Mutex
	var lastReq struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		_ = json.Unmarshal(body, &lastReq)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"model\":\"MiniMax-M3\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}],\"created\":1}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"model\":\"MiniMax-M3\",\"choices\":[],\"created\":1,\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
	}))
	defer srv.Close()

	dir := t.TempDir()
	client := backend.NewGatewayWithConfig(backend.GatewayConfig{URL: srv.URL, ExternalModel: "MiniMax-M3", HTTPClient: srv.Client()})

	step := implicit.Step{
		Status: implicit.StepStatusDone,
		RunCommand: implicit.RunCommand{
			CommandLine:         "ls -la",
			ProposedCommandLine: "ls -la",
			CWD:                 dir,
			CombinedOutput:      "total 12\n",
			ExitCode:            0,
			HasExitCode:         true,
		},
		CreatedAt:   time.Unix(1792644998, 0).UTC(),
		CompletedAt: time.Unix(1792644998, 196151000).UTC(),
	}
	store := implicit.Open(convstore.StoreDir())
	if err := store.Record(implicit.WorkspaceURI(dir), step); err != nil {
		t.Fatalf("record: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := runPigPrintTurn(ctx, pigPrintConfig{
		Backend:   client,
		Model:     "MiniMax-M3",
		Prompt:    "and then?",
		Catalog:   tools.Catalog(dir),
		Handler:   func(backend.StreamChunk) error { return nil },
		Workspace: dir,
		AgentDir:  filepath.Join(dir, "agent"),
	})
	if err != nil {
		t.Fatalf("runPigPrintTurn: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	contents := make([]string, 0, len(lastReq.Messages))
	for _, m := range lastReq.Messages {
		contents = append(contents, m.Content)
	}
	joined := strings.Join(contents, "\n")
	for _, want := range []string{
		"The USER performed the following action:\nCommand: ls -la",
		"The command exited with code 0.\nOutput:\ntotal 12",
		"and then?",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("request body missing %q; contents=%q", want, contents)
		}
	}
}

// TestPigSeedHistoryKeepsTheConversationOrder: the implicit trajectory's
// messages come first, then the resumed conversation's earlier turns.
func TestPigSeedHistoryKeepsTheConversationOrder(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())

	dir := t.TempDir()
	step := implicit.Step{
		Status: implicit.StepStatusDone,
		RunCommand: implicit.RunCommand{
			CommandLine:         "ls -la",
			ProposedCommandLine: "ls -la",
			CWD:                 dir,
			CombinedOutput:      "total 12\n",
		},
		CreatedAt:   time.Unix(1792644998, 0).UTC(),
		CompletedAt: time.Unix(1792644998, 196151000).UTC(),
	}
	store := implicit.Open(convstore.StoreDir())
	if err := store.Record(implicit.WorkspaceURI(dir), step); err != nil {
		t.Fatalf("record: %v", err)
	}

	seed := pigSeedHistory(pigPrintConfig{
		Workspace: dir,
		History:   []pigrt.HistoryTurn{{Role: "user", Text: "what is the capital of France?"}},
	})
	if len(seed) != 3 {
		t.Fatalf("the seed holds %d turns, want 3", len(seed))
	}
	if !strings.Contains(seed[0].Text, "The USER performed the following action:\nCommand: ls -la") {
		t.Errorf("the first turn does not name the action:\n%s", seed[0].Text)
	}
	if seed[2].Text != "what is the capital of France?" {
		t.Errorf("the last turn is %q, want the conversation's first turn", seed[2].Text)
	}
}
