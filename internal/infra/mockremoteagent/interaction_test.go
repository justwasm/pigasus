package mockremoteagent

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/interaction"
)

// TestAskUserPublishesWaitingStep pins the server half of the interaction
// round trip: the question becomes a WAITING (9) ask_question step (138)
// with the questions in both proto places, and the answer closes it.
func TestAskUserPublishesWaitingStep(t *testing.T) {
	cs := NewCascadeSession("cascade-1", "", "gemini-3.8-flash-high", nil, nil)
	cs.SetStatus("RUNNING")

	type result struct {
		answer interaction.Answer
		err    error
	}
	done := make(chan result, 1)
	go func() {
		answer, err := cs.AskUser(context.Background(), interaction.Request{
			Kind:     interaction.KindQuestion,
			ToolName: "ask_question",
			ArgsJSON: `{"questions":[]}`,
			Questions: []interaction.Question{{
				Prompt:  "Would you like tea or coffee?",
				Options: []string{"Tea", "Coffee"},
			}},
		})
		done <- result{answer: answer, err: err}
	}()

	step := waitForWaitingStep(t, cs)
	frame := formatProtoStep(cs, step, step.StepIndex)
	if got := frame["type"]; got != stepTypeAskQuestion {
		t.Errorf("type = %v, want %d", got, stepTypeAskQuestion)
	}
	if got := frame["status"]; got != stepStatusWaiting {
		t.Errorf("status = %v, want %d", got, stepStatusWaiting)
	}
	if _, ok := frame["requestedInteraction"].(map[string]any)["askQuestion"]; !ok {
		t.Errorf("requestedInteraction = %#v", frame["requestedInteraction"])
	}
	if _, ok := frame["askQuestion"].(map[string]any)["questions"]; !ok {
		t.Errorf("askQuestion = %#v", frame["askQuestion"])
	}

	response := map[string]any{
		"trajectoryId": "traj-cascade-1",
		"stepIndex":    step.StepIndex,
		"askQuestion": map[string]any{"responses": []any{
			map[string]any{"question": "Would you like tea or coffee?", "selectedOptionIds": []any{"2"}},
		}},
	}
	if !cs.ResolveInteraction(step.StepIndex, interaction.Answer{Text: "Coffee"}, response) {
		t.Fatal("ResolveInteraction did not match the waiting step")
	}

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("AskUser: %v", res.err)
		}
		if res.answer.Text != "Coffee" {
			t.Errorf("answer = %+v", res.answer)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AskUser did not return after the answer")
	}

	after := cs.StepsSnapshot()[step.StepIndex]
	if after.Status != "DONE" {
		t.Errorf("step status = %q, want DONE", after.Status)
	}
	if len(after.CompletedInteractions) != 1 {
		t.Errorf("completedInteractions = %#v", after.CompletedInteractions)
	}
}

// TestPermissionInteractionIsGeneric pins the permission card's step shape.
func TestPermissionInteractionIsGeneric(t *testing.T) {
	cs := NewCascadeSession("cascade-1", "", "m", nil, nil)
	go func() {
		_, _ = cs.AskUser(context.Background(), interaction.Request{
			Kind:     interaction.KindPermission,
			ToolName: "ask_permission",
			Action:   "command",
			Target:   "git status",
			Reason:   "inspect the tree",
		})
	}()

	step := waitForWaitingStep(t, cs)
	frame := formatProtoStep(cs, step, step.StepIndex)
	if got := frame["type"]; got != stepTypeGeneric {
		t.Errorf("type = %v, want %d", got, stepTypeGeneric)
	}
	permission := frame["requestedInteraction"].(map[string]any)["permission"].(map[string]any)
	resource := permission["resource"].(map[string]any)
	if resource["action"] != "command" || resource["target"] != "git status" {
		t.Errorf("resource = %#v", resource)
	}
	if permission["reason"] != "inspect the tree" {
		t.Errorf("reason = %#v", permission["reason"])
	}
	cs.ResolveInteraction(step.StepIndex, interaction.Answer{Allowed: true}, nil)
}

// TestHandleCascadeUserInteractionAnswersTheWait drives the RPC the client
// sends against the session that is parked.
func TestHandleCascadeUserInteractionAnswersTheWait(t *testing.T) {
	srv, err := NewServer(WithAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	cs := srv.service.cascades.CreateSession("cascade-1", "", "m", nil, nil)

	done := make(chan interaction.Answer, 1)
	go func() {
		answer, _ := cs.AskUser(context.Background(), interaction.Request{
			Kind: interaction.KindQuestion,
			Questions: []interaction.Question{{
				Prompt:  "Pick one",
				Options: []string{"red", "blue"},
			}},
		})
		done <- answer
	}()
	step := waitForWaitingStep(t, cs)

	body, _ := json.Marshal(map[string]any{
		"cascadeId": "cascade-1",
		"interaction": map[string]any{
			"trajectoryId": "traj-cascade-1",
			"stepIndex":    step.StepIndex,
			"askQuestion": map[string]any{
				"responses": []any{map[string]any{
					"question":          "Pick one",
					"options":           []any{map[string]any{"id": "1", "text": "red"}, map[string]any{"id": "2", "text": "blue"}},
					"selectedOptionIds": []any{"2"},
				}},
			},
		},
	})
	rec := httptest.NewRecorder()
	srv.service.handleCascadeUserInteraction(rec, body)

	select {
	case answer := <-done:
		if answer.Text != "blue" {
			t.Errorf("answer = %+v", answer)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the pending question was not resolved")
	}
}

func waitForWaitingStep(t *testing.T, cs *CascadeSession) TrajectoryStep {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		steps := cs.StepsSnapshot()
		if len(steps) > 0 {
			last := steps[len(steps)-1]
			if last.Status == "WAITING" {
				return last
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no WAITING step appeared; steps=%+v", cs.StepsSnapshot())
	return TrajectoryStep{}
}

// TestPermissionPresetRoundTrip pins the proxy's read-after-write: a client
// that sets the preset sees the new value on the next read.
func TestPermissionPresetRoundTrip(t *testing.T) {
	srv, err := NewServer(WithAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	rec := httptest.NewRecorder()
	srv.service.handleSetPermissionPreset(rec, []byte(`{"preset":"TURBO"}`))

	rec = httptest.NewRecorder()
	srv.service.handleGetPermissionSettings(rec)
	var got struct {
		PermissionPreset string `json:"permissionPreset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PermissionPreset != "TURBO" {
		t.Errorf("permissionPreset = %q, want TURBO", got.PermissionPreset)
	}
}
