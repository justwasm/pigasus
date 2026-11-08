package agentturn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/core/interaction"
	"github.com/justwasm/pigasus/internal/infra/mockremoteagent"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
)

// engineProxy is a minimal remote agent proxy: it records the cascade calls
// and replays one scripted list of state updates per stream request.
type engineProxy struct {
	mu                sync.Mutex
	startCalls        int
	startBody         map[string]any
	sendTexts         []string
	scripts           [][]map[string]any
	scriptIdx         int
	commands          []remoteagentclient.Command
	interactions      []map[string]any
	conversation      map[string]any
	conversationIDReq string
}

func (p *engineProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, ":ensureEnvironment"):
		_ = json.NewEncoder(w).Encode(map[string]any{"environment": map[string]any{"connectionState": "CONNECTION_STATE_CONNECTED"}})
	case strings.HasSuffix(r.URL.Path, ":startCascade"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		p.mu.Lock()
		p.startCalls++
		p.startBody = body
		p.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"cascadeId": "cascade-1"})
	case strings.HasSuffix(r.URL.Path, ":sendUserCascadeMessage"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		p.mu.Lock()
		if text, _ := body["messageText"].(string); text != "" {
			p.sendTexts = append(p.sendTexts, text)
		}
		p.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"cascadeId": "cascade-1"})
	case strings.HasSuffix(r.URL.Path, ":getConversation"):
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		p.mu.Lock()
		p.conversationIDReq = body["conversationId"]
		conv := p.conversation
		p.mu.Unlock()
		_ = json.NewEncoder(w).Encode(conv)
	case strings.HasSuffix(r.URL.Path, ":handleCascadeUserInteraction"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		p.mu.Lock()
		p.interactions = append(p.interactions, body)
		p.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{})
	case strings.HasSuffix(r.URL.Path, ":listSlashCommands"):
		_ = json.NewEncoder(w).Encode(map[string]any{"slashCommands": p.commands})
	case strings.HasSuffix(r.URL.Path, ":streamAgentStateUpdates"):
		p.mu.Lock()
		script := p.scripts[len(p.scripts)-1]
		if p.scriptIdx < len(p.scripts) {
			script = p.scripts[p.scriptIdx]
			p.scriptIdx++
		}
		p.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for i, update := range script {
			payload, _ := json.Marshal(map[string]any{
				"update":      update,
				"resumeToken": fmt.Sprintf("tok-%d", i+1),
			})
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	default:
		http.NotFound(w, r)
	}
}

func newEngine(t *testing.T, proxy *engineProxy) (*RemoteAgentEngine, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(proxy)
	client := remoteagentclient.New(remoteagentclient.Config{
		BaseURL:   srv.URL,
		TokenFunc: func(context.Context) (string, error) { return "test-token", nil },
	})
	return NewRemoteAgentEngine(RemoteAgentConfig{
		Client:    client,
		Model:     "gemini-3.8-flash-high",
		Workspace: t.TempDir(),
	}), srv
}

func collectFrames(t *testing.T, ch <-chan agentlink.Frame) []agentlink.Frame {
	t.Helper()
	var frames []agentlink.Frame
	timeout := time.After(5 * time.Second)
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				return frames
			}
			frames = append(frames, f)
		case <-timeout:
			t.Fatalf("turn did not close; frames so far: %+v", frames)
		}
	}
}

func plannerStep(index int, status int, response string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{"sourceTrajectoryStepInfo": map[string]any{"stepIndex": index}},
		"type":     15,
		"status":   status,
		"plannerResponse": map[string]any{
			"response": response,
		},
	}
}

func updateFrame(fullyIdle bool, steps ...map[string]any) map[string]any {
	anySteps := make([]any, len(steps))
	for i, s := range steps {
		anySteps[i] = s
	}
	return map[string]any{
		"fullyIdle": fullyIdle,
		"mainTrajectoryUpdate": map[string]any{
			"stepsUpdate": map[string]any{"steps": anySteps},
		},
	}
}

func userStep(text string) map[string]any {
	return map[string]any{
		"type":      14,
		"status":    3,
		"userInput": map[string]any{"query": text},
	}
}

func TestRemoteAgentEngineStreamsTextAndResult(t *testing.T) {
	proxy := &engineProxy{scripts: [][]map[string]any{{
		updateFrame(false, userStep("hi"), plannerStep(1, 8, "Hel")),
		updateFrame(false, userStep("hi"), plannerStep(1, 3, "Hello")),
		updateFrame(true, userStep("hi"), plannerStep(1, 3, "Hello")),
	}}}
	engine, srv := newEngine(t, proxy)
	defer srv.Close()
	defer engine.Close()

	ch, err := engine.Turn(context.Background(), Turn{
		History: []agentlink.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	frames := collectFrames(t, ch)

	var text string
	var result *agentlink.Result
	for _, f := range frames {
		switch f.Kind {
		case agentlink.KindText:
			text += f.Text
		case agentlink.KindResult:
			result = f.Result
		}
	}
	if text != "Hello" {
		t.Errorf("text = %q, want Hello", text)
	}
	if result == nil || result.Reply != "Hello" {
		t.Fatalf("result = %+v, want reply Hello", result)
	}
	if proxy.startCalls != 1 {
		t.Errorf("startCalls = %d, want 1", proxy.startCalls)
	}
	if got := proxy.startBody["model"]; got != "gemini-3.8-flash-high" {
		t.Errorf("start model = %v", got)
	}
	if got := proxy.startBody["initialPrompt"]; got != "hi" {
		t.Errorf("initialPrompt = %v", got)
	}
}

// TestRemoteAgentEngineSendsWireModelID pins /model: the TUI stores the
// catalog label ("Gemini 3.8 Flash (High)"), but the proxy expects the wire
// id ("gemini-3.8-flash-high").
func TestRemoteAgentEngineSendsWireModelID(t *testing.T) {
	proxy := &engineProxy{scripts: [][]map[string]any{{
		updateFrame(true, userStep("hi"), plannerStep(1, 3, "ok")),
	}}}
	engine, srv := newEngine(t, proxy)
	defer srv.Close()
	defer engine.Close()

	ch, err := engine.Turn(context.Background(), Turn{
		Model:   "Gemini 3.8 Flash (High)",
		History: []agentlink.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	collectFrames(t, ch)

	if got := proxy.startBody["model"]; got != "gemini-3.8-flash-high" {
		t.Errorf("start model = %v, want gemini-3.8-flash-high", got)
	}
}

func TestRemoteAgentEngineMapsRunCommandTool(t *testing.T) {
	running := map[string]any{
		"type":   21,
		"status": 2,
		"runCommand": map[string]any{
			"commandLine": "date",
		},
	}
	done := map[string]any{
		"type":   21,
		"status": 3,
		"runCommand": map[string]any{
			"commandLine":    "date",
			"exitCode":       0,
			"combinedOutput": map[string]any{"full": "Sun Nov  8 02:00:00 CST 2026\n"},
		},
	}
	proxy := &engineProxy{scripts: [][]map[string]any{{
		updateFrame(false, userStep("run date"), running),
		updateFrame(false, userStep("run date"), done),
		updateFrame(true, userStep("run date"), done),
	}}}
	engine, srv := newEngine(t, proxy)
	defer srv.Close()
	defer engine.Close()

	ch, err := engine.Turn(context.Background(), Turn{
		History: []agentlink.Message{{Role: "user", Content: "run date"}},
	})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	frames := collectFrames(t, ch)

	var started, ended int
	var resultText string
	for _, f := range frames {
		if f.Kind != agentlink.KindTool || f.Tool == nil {
			continue
		}
		switch f.Tool.Phase {
		case agentlink.ToolStart:
			started++
			if f.Tool.Name != "run_command" {
				t.Errorf("tool start name = %q", f.Tool.Name)
			}
		case agentlink.ToolEnd:
			ended++
			resultText = f.Tool.Result
			if f.Tool.Name != "run_command" {
				t.Errorf("tool end name = %q", f.Tool.Name)
			}
		}
	}
	if started != 1 || ended != 1 {
		t.Fatalf("tool starts/ends = %d/%d, want 1/1", started, ended)
	}
	if !strings.Contains(resultText, "Sun Nov") {
		t.Errorf("tool result = %q", resultText)
	}
}

// TestRemoteAgentEngineMapsGenericTool pins the step shape a real remote
// uses for every tool that is not run_command: the name and arguments ride on
// metadata.toolCall, the output on generic.result.
// TestRemoteAgentEngineStreamsThinking pins the planner's thinking field:
// the remote stream can extend it separately from the visible response.
func TestRemoteAgentEngineStreamsThinking(t *testing.T) {
	think := func(thinking, response string) map[string]any {
		return map[string]any{
			"type":   15,
			"status": 8,
			"plannerResponse": map[string]any{
				"thinking": thinking,
				"response": response,
			},
		}
	}
	proxy := &engineProxy{scripts: [][]map[string]any{{
		updateFrame(false, userStep("hi"), think("Thin", "")),
		updateFrame(false, userStep("hi"), think("Thinking", "Hi")),
		updateFrame(true, userStep("hi"), think("Thinking", "Hi")),
	}}}
	engine, srv := newEngine(t, proxy)
	defer srv.Close()
	defer engine.Close()

	ch, err := engine.Turn(context.Background(), Turn{
		History: []agentlink.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	frames := collectFrames(t, ch)

	var thought, text string
	for _, f := range frames {
		switch f.Kind {
		case agentlink.KindThought:
			thought += f.Text
		case agentlink.KindText:
			text += f.Text
		}
	}
	if thought != "Thinking" {
		t.Errorf("thought = %q, want Thinking", thought)
	}
	if text != "Hi" {
		t.Errorf("text = %q, want Hi", text)
	}
}

func TestRemoteAgentEngineMapsGenericTool(t *testing.T) {
	generic := map[string]any{
		"type":   132,
		"status": 3,
		"metadata": map[string]any{
			"toolCall": map[string]any{
				"name":          "view_file",
				"argumentsJson": `{"AbsolutePath":"/tmp/x.txt"}`,
			},
		},
		"generic": map[string]any{
			"result": map[string]any{"result": "file contents"},
		},
	}
	proxy := &engineProxy{scripts: [][]map[string]any{{
		updateFrame(false, userStep("read it"), generic),
		updateFrame(true, userStep("read it"), generic),
	}}}
	engine, srv := newEngine(t, proxy)
	defer srv.Close()
	defer engine.Close()

	ch, err := engine.Turn(context.Background(), Turn{
		History: []agentlink.Message{{Role: "user", Content: "read it"}},
	})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	frames := collectFrames(t, ch)

	var started, ended int
	for _, f := range frames {
		if f.Kind != agentlink.KindTool || f.Tool == nil {
			continue
		}
		switch f.Tool.Phase {
		case agentlink.ToolStart:
			started++
			if f.Tool.Name != "view_file" {
				t.Errorf("tool start name = %q, want view_file", f.Tool.Name)
			}
		case agentlink.ToolEnd:
			ended++
			if f.Tool.Name != "view_file" {
				t.Errorf("tool end name = %q, want view_file", f.Tool.Name)
			}
			if f.Tool.Result != "file contents" {
				t.Errorf("tool end result = %q", f.Tool.Result)
			}
		}
	}
	if started != 1 || ended != 1 {
		t.Fatalf("generic tool starts/ends = %d/%d, want 1/1", started, ended)
	}
}

func TestRemoteAgentEngineSecondTurnReplaysOnlyNewSteps(t *testing.T) {
	firstTurn := []map[string]any{
		updateFrame(false, userStep("one"), plannerStep(1, 3, "first")),
		updateFrame(true, userStep("one"), plannerStep(1, 3, "first")),
	}
	secondTurn := []map[string]any{
		updateFrame(false,
			userStep("one"), plannerStep(1, 3, "first"),
			userStep("two"), plannerStep(3, 8, "sec")),
		updateFrame(false,
			userStep("one"), plannerStep(1, 3, "first"),
			userStep("two"), plannerStep(3, 3, "second")),
		updateFrame(true,
			userStep("one"), plannerStep(1, 3, "first"),
			userStep("two"), plannerStep(3, 3, "second")),
	}
	proxy := &engineProxy{scripts: [][]map[string]any{firstTurn, secondTurn}}
	engine, srv := newEngine(t, proxy)
	defer srv.Close()
	defer engine.Close()

	ch, err := engine.Turn(context.Background(), Turn{
		History: []agentlink.Message{{Role: "user", Content: "one"}},
	})
	if err != nil {
		t.Fatalf("Turn 1: %v", err)
	}
	collectFrames(t, ch)

	ch, err = engine.Turn(context.Background(), Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "one"},
			{Role: "assistant", Content: "first"},
			{Role: "user", Content: "two"},
		},
	})
	if err != nil {
		t.Fatalf("Turn 2: %v", err)
	}
	frames := collectFrames(t, ch)

	var text string
	var result *agentlink.Result
	for _, f := range frames {
		if f.Kind == agentlink.KindText {
			text += f.Text
		}
		if f.Kind == agentlink.KindResult {
			result = f.Result
		}
	}
	if text != "second" {
		t.Errorf("second turn text = %q, want second", text)
	}
	if result == nil || result.Reply != "second" {
		t.Errorf("second turn result = %+v", result)
	}
	if len(proxy.sendTexts) != 1 || proxy.sendTexts[0] != "two" {
		t.Errorf("SendMessage texts = %v", proxy.sendTexts)
	}
	if proxy.startCalls != 1 {
		t.Errorf("startCalls = %d, want 1 (the cascade is reused)", proxy.startCalls)
	}
}

// TestRemoteAgentEngineAdvancesBaseWhenTheStreamEnds pins the fallback where
// the proxy closes the stream without sending a final idle frame: the next
// turn must still start after the steps it already rendered.
func TestRemoteAgentEngineAdvancesBaseWhenTheStreamEnds(t *testing.T) {
	firstTurn := []map[string]any{
		updateFrame(false, userStep("one"), plannerStep(1, 3, "first")),
	}
	secondTurn := []map[string]any{
		updateFrame(false,
			userStep("one"), plannerStep(1, 3, "first"),
			userStep("two"), plannerStep(3, 3, "second")),
		updateFrame(true,
			userStep("one"), plannerStep(1, 3, "first"),
			userStep("two"), plannerStep(3, 3, "second")),
	}
	proxy := &engineProxy{scripts: [][]map[string]any{firstTurn, secondTurn}}
	engine, srv := newEngine(t, proxy)
	defer srv.Close()
	defer engine.Close()

	ch, err := engine.Turn(context.Background(), Turn{
		History: []agentlink.Message{{Role: "user", Content: "one"}},
	})
	if err != nil {
		t.Fatalf("Turn 1: %v", err)
	}
	collectFrames(t, ch)

	ch, err = engine.Turn(context.Background(), Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "one"},
			{Role: "assistant", Content: "first"},
			{Role: "user", Content: "two"},
		},
	})
	if err != nil {
		t.Fatalf("Turn 2: %v", err)
	}
	frames := collectFrames(t, ch)
	var text string
	for _, f := range frames {
		if f.Kind == agentlink.KindText {
			text += f.Text
		}
	}
	if text != "second" {
		t.Errorf("second turn text = %q, want second (the first turn's steps must not replay)", text)
	}
}

// TestRemoteAgentEngineResetSessionStartsANewCascade pins /clear: the next
// turn must start a fresh remote conversation instead of continuing the one
// whose transcript the TUI just discarded.
func TestRemoteAgentEngineResetSessionStartsANewCascade(t *testing.T) {
	script := []map[string]any{
		updateFrame(true, userStep("one"), plannerStep(1, 3, "first")),
	}
	proxy := &engineProxy{scripts: [][]map[string]any{script, script}}
	engine, srv := newEngine(t, proxy)
	defer srv.Close()
	defer engine.Close()

	ch, err := engine.Turn(context.Background(), Turn{
		History: []agentlink.Message{{Role: "user", Content: "one"}},
	})
	if err != nil {
		t.Fatalf("Turn 1: %v", err)
	}
	collectFrames(t, ch)

	engine.ResetSession()

	ch, err = engine.Turn(context.Background(), Turn{
		History: []agentlink.Message{{Role: "user", Content: "two"}},
	})
	if err != nil {
		t.Fatalf("Turn 2: %v", err)
	}
	collectFrames(t, ch)

	if proxy.startCalls != 2 {
		t.Errorf("startCalls = %d, want 2 after ResetSession", proxy.startCalls)
	}
	if len(proxy.sendTexts) != 0 {
		t.Errorf("SendMessage called after ResetSession: %v", proxy.sendTexts)
	}
	if got := proxy.startBody["initialPrompt"]; got != "two" {
		t.Errorf("new cascade prompt = %v, want two", got)
	}
}

func TestRemoteAgentEngineAvailableCommands(t *testing.T) {
	proxy := &engineProxy{commands: []remoteagentclient.Command{
		{Name: "compact", Description: "compact the conversation"},
		{Name: "/clear", Description: "clear"},
		{Name: "  ", Description: "ignored"},
	}}
	engine, srv := newEngine(t, proxy)
	defer srv.Close()
	defer engine.Close()

	cmds := engine.AvailableCommands()
	if len(cmds) != 2 {
		t.Fatalf("commands = %+v", cmds)
	}
	if cmds[0].Name != "/compact" || cmds[1].Name != "/clear" {
		t.Errorf("command names = %q, %q", cmds[0].Name, cmds[1].Name)
	}
}

func TestRemoteAgentEngineRejectsEmptyPrompt(t *testing.T) {
	engine, srv := newEngine(t, &engineProxy{})
	defer srv.Close()
	defer engine.Close()

	if _, err := engine.Turn(context.Background(), Turn{}); err == nil {
		t.Fatal("Turn with no user message = nil error")
	}
}

// waitingAskQuestionUpdate is a frame carrying one WAITING (9) ask_question
// step (138), the shape the reference publishes.
func waitingAskQuestionUpdate() map[string]any {
	return map[string]any{
		"fullyIdle": false,
		"mainTrajectoryUpdate": map[string]any{
			"stepsUpdate": map[string]any{
				"indices": []any{0},
				"steps": []any{
					map[string]any{
						"type":   138,
						"status": 9,
						"metadata": map[string]any{
							"toolCall": map[string]any{"name": "ask_question", "argumentsJson": "{}"},
							"sourceTrajectoryStepInfo": map[string]any{
								"trajectoryId": "traj-cascade-1",
								"stepIndex":    0,
							},
						},
						"askQuestion": map[string]any{"questions": []any{
							map[string]any{
								"question": "Would you like tea or coffee?",
								"options": []any{
									map[string]any{"id": "1", "text": "Tea"},
									map[string]any{"id": "2", "text": "Coffee"},
								},
							},
						}},
						"requestedInteraction": map[string]any{"askQuestion": map[string]any{"questions": []any{
							map[string]any{
								"question": "Would you like tea or coffee?",
								"options": []any{
									map[string]any{"id": "1", "text": "Tea"},
									map[string]any{"id": "2", "text": "Coffee"},
								},
							},
						}}},
					},
				},
			},
		},
	}
}

func idleUpdate() map[string]any {
	return map[string]any{"fullyIdle": true}
}

// TestRemoteInteractionQuestionAnswersThroughTheRPC pins the client half:
// a WAITING step reaches the host, and the host's answer is posted as the
// reference's cascadeId + interaction.askQuestion payload.
func TestRemoteInteractionQuestionAnswersThroughTheRPC(t *testing.T) {
	proxy := &engineProxy{scripts: [][]map[string]any{{
		waitingAskQuestionUpdate(),
		idleUpdate(),
	}}}
	engine, srv := newEngine(t, proxy)
	defer srv.Close()

	var got RemoteInteraction
	engine.SetRemoteInteractionHandler(func(ctx context.Context, req RemoteInteraction) (RemoteInteractionAnswer, error) {
		got = req
		return RemoteInteractionAnswer{Answers: []RemoteQuestionAnswer{
			AnswerFromText(req.Questions[0], "Coffee"),
		}}, nil
	})

	ch, err := engine.Turn(context.Background(), Turn{
		History: []agentlink.Message{{Role: "user", Content: "ask me"}},
	})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	collectFrames(t, ch)

	if got.Kind != RemoteInteractionQuestion || got.CascadeID != "cascade-1" || got.TrajectoryID != "traj-cascade-1" || got.StepIndex != 0 {
		t.Fatalf("interaction = %+v", got)
	}
	if len(got.Questions) != 1 || len(got.Questions[0].Options) != 2 {
		t.Fatalf("questions = %+v", got.Questions)
	}

	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	if len(proxy.interactions) != 1 {
		t.Fatalf("posted %d answers", len(proxy.interactions))
	}
	body := proxy.interactions[0]
	if body["conversationId"] != "cascade-1" {
		t.Errorf("conversationId = %v", body["conversationId"])
	}
	answer := body["interaction"].(map[string]any)
	if answer["trajectoryId"] != "traj-cascade-1" || answer["stepIndex"] != float64(0) {
		t.Errorf("interaction = %+v", answer)
	}
	responses := answer["askQuestion"].(map[string]any)["responses"].([]any)
	first := responses[0].(map[string]any)
	selected := first["selectedOptionIds"].([]any)
	if len(selected) != 1 || selected[0] != "2" {
		t.Errorf("selectedOptionIds = %v", selected)
	}
}

// TestRemoteInteractionPermissionAnswersAllow covers the permission step.
func TestRemoteInteractionPermissionAnswersAllow(t *testing.T) {
	update := map[string]any{
		"fullyIdle": false,
		"mainTrajectoryUpdate": map[string]any{
			"stepsUpdate": map[string]any{
				"indices": []any{0},
				"steps": []any{map[string]any{
					"type":   132,
					"status": 9,
					"metadata": map[string]any{
						"toolCall": map[string]any{"name": "ask_permission", "argumentsJson": "{}"},
					},
					"requestedInteraction": map[string]any{"permission": map[string]any{
						"resource":          map[string]any{"action": "command", "target": "git status"},
						"reason":            "inspect the tree",
						"actionDescription": "run a command",
					}},
				}},
			},
		},
	}
	proxy := &engineProxy{scripts: [][]map[string]any{{update, idleUpdate()}}}
	engine, srv := newEngine(t, proxy)
	defer srv.Close()
	engine.SetRemoteInteractionHandler(func(ctx context.Context, req RemoteInteraction) (RemoteInteractionAnswer, error) {
		return RemoteInteractionAnswer{Allowed: true}, nil
	})

	ch, err := engine.Turn(context.Background(), Turn{
		History: []agentlink.Message{{Role: "user", Content: "go"}},
	})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	collectFrames(t, ch)

	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	answer := proxy.interactions[0]["interaction"].(map[string]any)
	permission := answer["permission"].(map[string]any)
	if permission["allow"] != true {
		t.Errorf("permission = %+v", permission)
	}
}

// TestRemoteInteractionEndToEnd runs the client engine against the real
// mock proxy: the server parks on ask_question, the engine's host answers,
// and the posted answer resumes the server's turn.
func TestRemoteInteractionEndToEnd(t *testing.T) {
	proxy, err := mockremoteagent.NewServer(mockremoteagent.WithCustomTurnHandler(
		func(ctx context.Context, session *mockremoteagent.CascadeSession, prompt string) {
			go func() {
				_, cancel := session.BeginTurn()
				defer cancel()
				answer, _ := session.AskUser(context.Background(), interaction.Request{
					Kind:     interaction.KindQuestion,
					ToolName: "ask_question",
					Questions: []interaction.Question{{
						Prompt:  "Pick a drink",
						Options: []string{"Tea", "Coffee"},
					}},
				})
				session.AddStep("PLANNER_RESPONSE", "you picked "+answer.Text, "", nil)
				session.SetStatus("IDLE")
			}()
		},
	))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer proxy.Close()

	client := remoteagentclient.New(remoteagentclient.Config{
		BaseURL:   proxy.URL(),
		TokenFunc: func(context.Context) (string, error) { return "test-token", nil },
	})
	engine := NewRemoteAgentEngine(RemoteAgentConfig{Client: client, Model: "gemini-3.8-flash-high"})
	engine.SetRemoteInteractionHandler(func(ctx context.Context, req RemoteInteraction) (RemoteInteractionAnswer, error) {
		return RemoteInteractionAnswer{Answers: []RemoteQuestionAnswer{
			AnswerFromText(req.Questions[0], "Tea"),
		}}, nil
	})

	ch, err := engine.Turn(context.Background(), Turn{
		History: []agentlink.Message{{Role: "user", Content: "ask me a question"}},
	})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	frames := collectFrames(t, ch)

	var reply string
	for _, f := range frames {
		if f.Kind == agentlink.KindResult && f.Result != nil {
			reply = f.Result.Reply
		}
	}
	if !strings.Contains(reply, "you picked Tea") {
		t.Errorf("final reply = %q, frames = %+v", reply, frames)
	}
}

// TestResumeCascadeSendsIntoTheExistingConversation pins remote /continue:
// the engine reads the conversation, skips the steps already stored, and
// the next turn sends instead of starting a cascade.
func TestResumeCascadeSendsIntoTheExistingConversation(t *testing.T) {
	proxy := &engineProxy{
		conversation: map[string]any{
			"conversationId": "cascade-9",
			"update": map[string]any{
				"mainTrajectoryUpdate": map[string]any{
					"stepsUpdate": map[string]any{"steps": []any{
						map[string]any{"type": 14, "status": 3},
						map[string]any{"type": 15, "status": 3, "plannerResponse": map[string]any{"response": "old reply"}},
					}},
				},
			},
		},
		scripts: [][]map[string]any{{idleUpdate()}},
	}
	engine, srv := newEngine(t, proxy)
	defer srv.Close()

	steps, err := engine.ResumeCascade(context.Background(), "cascade-9")
	if err != nil {
		t.Fatalf("ResumeCascade: %v", err)
	}
	if steps != 2 {
		t.Errorf("steps = %d, want 2", steps)
	}
	proxy.mu.Lock()
	if proxy.conversationIDReq != "cascade-9" {
		t.Errorf("getConversation id = %q", proxy.conversationIDReq)
	}
	proxy.mu.Unlock()

	frames := collectFrames(t, mustTurn(t, engine, "next"))
	for _, f := range frames {
		if f.Kind == agentlink.KindText && strings.Contains(f.Text, "old reply") {
			t.Errorf("resume replayed the stored steps: %+v", frames)
		}
	}
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	if proxy.startCalls != 0 {
		t.Errorf("resume started %d new cascades", proxy.startCalls)
	}
	if len(proxy.sendTexts) != 1 || proxy.sendTexts[0] != "next" {
		t.Errorf("sendTexts = %v", proxy.sendTexts)
	}
}

func TestResumeCascadeRejectsAnUnknownConversation(t *testing.T) {
	proxy := &engineProxy{conversation: map[string]any{"conversationId": "nope"}}
	engine, srv := newEngine(t, proxy)
	defer srv.Close()
	if _, err := engine.ResumeCascade(context.Background(), "nope"); err != ErrNoRemoteConversation {
		t.Fatalf("ResumeCascade error = %v, want ErrNoRemoteConversation", err)
	}
}

func mustTurn(t *testing.T, engine *RemoteAgentEngine, prompt string) <-chan agentlink.Frame {
	t.Helper()
	ch, err := engine.Turn(context.Background(), Turn{History: []agentlink.Message{{Role: "user", Content: prompt}}})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	return ch
}

// TestRemoteInteractionPermissionEndToEnd runs the permission card against
// the real mock proxy: the server parks on ask_permission, the engine's
// host allows it, and the server's turn sees the verdict.
func TestRemoteInteractionPermissionEndToEnd(t *testing.T) {
	proxy, err := mockremoteagent.NewServer(mockremoteagent.WithCustomTurnHandler(
		func(ctx context.Context, session *mockremoteagent.CascadeSession, prompt string) {
			go func() {
				_, cancel := session.BeginTurn()
				defer cancel()
				answer, _ := session.AskUser(context.Background(), interaction.Request{
					Kind:     interaction.KindPermission,
					ToolName: "ask_permission",
					Action:   "command",
					Target:   "git status",
					Reason:   "inspect the working tree",
				})
				session.AddStep("PLANNER_RESPONSE", "allowed="+strconv.FormatBool(answer.Allowed), "", nil)
				session.SetStatus("IDLE")
			}()
		},
	))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer proxy.Close()

	client := remoteagentclient.New(remoteagentclient.Config{
		BaseURL:   proxy.URL(),
		TokenFunc: func(context.Context) (string, error) { return "test-token", nil },
	})
	engine := NewRemoteAgentEngine(RemoteAgentConfig{Client: client, Model: "gemini-3.8-flash-high"})
	var seen RemoteInteraction
	engine.SetRemoteInteractionHandler(func(ctx context.Context, req RemoteInteraction) (RemoteInteractionAnswer, error) {
		seen = req
		return RemoteInteractionAnswer{Allowed: true}, nil
	})

	ch, err := engine.Turn(context.Background(), Turn{
		History: []agentlink.Message{{Role: "user", Content: "may I run it"}},
	})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	frames := collectFrames(t, ch)

	if seen.Kind != RemoteInteractionPermission || seen.Action != "command" || seen.Target != "git status" {
		t.Fatalf("interaction = %+v", seen)
	}
	var reply string
	for _, f := range frames {
		if f.Kind == agentlink.KindResult && f.Result != nil {
			reply = f.Result.Reply
		}
	}
	if !strings.Contains(reply, "allowed=true") {
		t.Errorf("final reply = %q, frames = %+v", reply, frames)
	}
}

// TestTurnDiffAgainstMockServer runs a turn, then reads its diff from the
// proxy: the engine sends the workspace URI, the mock computes the working
// copy's changes, and the client maps them back.
func TestTurnDiffAgainstMockServer(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runGitTest(t, git, dir, "init", "-q")
	runGitTest(t, git, dir, "config", "user.email", "test@example.com")
	runGitTest(t, git, dir, "config", "user.name", "Test")
	path := filepath.Join(dir, "app.go")
	if err := os.WriteFile(path, []byte("package app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, dir, "add", "app.go")
	runGitTest(t, git, dir, "commit", "-q", "-m", "init")
	if err := os.WriteFile(path, []byte("package app\n\nfunc Added() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	proxy, err := mockremoteagent.NewServer(mockremoteagent.WithCustomTurnHandler(
		func(ctx context.Context, session *mockremoteagent.CascadeSession, prompt string) {
			go func() {
				_, cancel := session.BeginTurn()
				defer cancel()
				session.AddStep("PLANNER_RESPONSE", "done", "", nil)
				session.SetStatus("IDLE")
			}()
		},
	))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer proxy.Close()

	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: proxy.URL()})
	engine := NewRemoteAgentEngine(RemoteAgentConfig{Client: client, Model: "gemini-3.8-flash-high", Workspace: dir})
	ch := mustTurn(t, engine, "edit it")
	collectFrames(t, ch)

	diff, err := engine.TurnDiff(context.Background())
	if err != nil {
		t.Fatalf("TurnDiff: %v", err)
	}
	fd, ok := diff.FileDiffs["app.go"]
	if !ok {
		t.Fatalf("fileDiffs = %+v", diff.FileDiffs)
	}
	if fd.Additions != 2 || fd.OriginalContents != "package app\n" {
		t.Errorf("fileDiff = %+v", fd)
	}
}

func runGitTest(t *testing.T, git, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(git, append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestCodeActionStepPaths pins the path extraction the diff panel relies on:
// FileChange (86) and the three CodeAction (5) action-spec branches all map
// to the workspace-relative key getTurnDiff uses.
func TestCodeActionStepPaths(t *testing.T) {
	e := NewRemoteAgentEngine(RemoteAgentConfig{Workspace: "/ws"})
	cases := []struct {
		name string
		step map[string]any
		want string
	}{
		{
			name: "file change",
			step: map[string]any{
				"type":       86,
				"fileChange": map[string]any{"absolutePathUri": "file:///ws/main.go"},
			},
			want: "main.go",
		},
		{
			name: "code action command",
			step: map[string]any{
				"type": 5,
				"codeAction": map[string]any{"actionSpec": map[string]any{
					"command": map[string]any{"file": map[string]any{"absoluteUri": "file:///ws/pkg/a.go"}},
				}},
			},
			want: "pkg/a.go",
		},
		{
			name: "code action create file",
			step: map[string]any{
				"type": 5,
				"codeAction": map[string]any{"actionSpec": map[string]any{
					"createFile": map[string]any{"path": map[string]any{"absoluteUri": "file:///ws/new.go"}},
				}},
			},
			want: "new.go",
		},
		{
			name: "code action sed",
			step: map[string]any{
				"type": 5,
				"codeAction": map[string]any{"actionSpec": map[string]any{
					"sed": map[string]any{"path": map[string]any{"absoluteUri": "file:///ws/sed.go"}},
				}},
			},
			want: "sed.go",
		},
		{
			name: "outside the workspace",
			step: map[string]any{
				"type":       86,
				"fileChange": map[string]any{"absolutePathUri": "file:///etc/passwd"},
			},
			want: "",
		},
	}
	for _, c := range cases {
		if got := e.codeActionPath(c.step); got != c.want {
			t.Errorf("%s: path = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestRemoteSkillsCacheAndMapping pins the ListCustomizations mapping and
// the cache the slash menu reads.
func TestRemoteSkillsCacheAndMapping(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ":listCustomizations") {
			http.NotFound(w, r)
			return
		}
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"skills": []any{
			map[string]any{
				"path": "/s/a/SKILL.md", "name": "a", "displayName": "A", "description": "desc",
				"isBuiltin": true, "pluginName": "plug", "icon": "i",
			},
			map[string]any{"name": "hidden", "disableSlashCommand": true},
			map[string]any{"name": "blocked", "disableModelInvocation": true},
		}})
	}))
	defer srv.Close()

	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: srv.URL})
	engine := NewRemoteAgentEngine(RemoteAgentConfig{Client: client, Model: "m", Workspace: "/ws"})

	list, err := engine.Skills(context.Background())
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("skills = %+v", list)
	}
	if list[0].Name != "A" || list[0].Plugin != "plug" || !list[0].Builtin || !list[0].Remote || list[0].Icon != "i" {
		t.Errorf("skill = %+v", list[0])
	}
	if list[1].Name != "blocked" || list[1].ModelInvocable {
		t.Errorf("blocked skill = %+v", list[1])
	}
	if cached := engine.CachedSkills(); len(cached) != 2 {
		t.Errorf("cached = %+v", cached)
	}
	if _, err := engine.Skills(context.Background()); err != nil {
		t.Fatalf("second Skills: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (cached)", calls)
	}
	if _, err := engine.ReloadSkills(context.Background()); err != nil {
		t.Fatalf("ReloadSkills: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls after reload = %d, want 2", calls)
	}
}
