package remotecontrol

import (
	"encoding/json"
	"testing"
)

// newLSConversationDispatcher returns a dispatcher whose store holds one
// conversation with a user message and a reply.
func newLSConversationDispatcher(t *testing.T, conversationID string) *Dispatcher {
	t.Helper()
	d, err := NewDispatcher(Config{Session: NewSessionState("1.3.4-test")})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.cfg.Session.Workspace = "/tmp/ls-ws"
	traj := d.trajectories.Get(conversationID)
	traj.Append(map[string]any{
		"type":     14,
		"userInput": map[string]any{"items": []any{map[string]any{"text": "first question"}}},
	})
	traj.Append(map[string]any{
		"type":            15,
		"plannerResponse": map[string]any{"response": "first answer"},
	})
	traj.Append(map[string]any{
		"type":     14,
		"userInput": map[string]any{"items": []any{map[string]any{"text": "second question"}}},
	})
	traj.Append(map[string]any{
		"type":            15,
		"plannerResponse": map[string]any{"response": "second answer"},
	})
	return d
}

// TestLSGetConversationMetadata covers the metadata of a conversation:
// the trajectory id, the number of steps and the workspace it ran in.
//
// 【一手】 2026-10-27: the reference rejects a missing conversation id
// with "invalid_argument: conversation id is required".
func TestLSGetConversationMetadata(t *testing.T) {
	d := newLSConversationDispatcher(t, "conv-1")
	answer, rpcErr, ok := d.handleLSGetConversationMetadata(json.RawMessage(`{"conversationId":"conv-1"}`))
	if !ok || rpcErr != nil {
		t.Fatalf("the metadata was not answered (%v)", rpcErr)
	}
	raw, _ := json.Marshal(answer)
	var body struct {
		Metadata struct {
			WorkspaceURIs []string `json:"workspaceUris"`
			CreatedAt     string   `json:"createdAt"`
		} `json:"metadata"`
		NumSteps    int    `json:"numSteps"`
		TrajectoryID string `json:"trajectoryId"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	if body.TrajectoryID != "traj-conv-1" {
		t.Errorf("trajectoryId = %q, want traj-conv-1", body.TrajectoryID)
	}
	if body.NumSteps != 4 {
		t.Errorf("numSteps = %d, want 4", body.NumSteps)
	}
	if len(body.Metadata.WorkspaceURIs) != 1 || body.Metadata.WorkspaceURIs[0] != "file:///tmp/ls-ws" {
		t.Errorf("workspaceUris = %v, want the workspace's own uri", body.Metadata.WorkspaceURIs)
	}
	if body.Metadata.CreatedAt == "" {
		t.Error("the metadata carries no creation time")
	}
}

// TestLSGetConversationMetadataNeedsTheID covers the parameter check.
func TestLSGetConversationMetadataNeedsTheID(t *testing.T) {
	d := newLSConversationDispatcher(t, "conv-1")
	_, rpcErr, _ := d.handleLSGetConversationMetadata(json.RawMessage(`{}`))
	if rpcErr == nil {
		t.Fatal("an empty request was answered")
	}
	if rpcErr.Code != -32602 {
		t.Errorf("code = %d, want -32602", rpcErr.Code)
	}
}

// TestLSGetConversationMetadataUnknown covers that a conversation the
// clone has never seen is "not found".
func TestLSGetConversationMetadataUnknown(t *testing.T) {
	d := newLSConversationDispatcher(t, "conv-1")
	_, rpcErr, _ := d.handleLSGetConversationMetadata(json.RawMessage(`{"conversationId":"conv-nope"}`))
	if rpcErr == nil || rpcErr.Code != -32019 {
		t.Fatalf("an unknown conversation was answered (%v)", rpcErr)
	}
}

// TestLSGetTrajectoryTurns covers the turn list: a turn starts at a
// user message and runs until the next one.
func TestLSGetTrajectoryTurns(t *testing.T) {
	d := newLSConversationDispatcher(t, "conv-1")
	answer, rpcErr, ok := d.handleLSGetTrajectoryTurns(json.RawMessage(`{"cascadeId":"conv-1"}`))
	if !ok || rpcErr != nil {
		t.Fatalf("the turns were not answered (%v)", rpcErr)
	}
	raw, _ := json.Marshal(answer)
	var body struct {
		Turns []struct {
			StartStepIndex int `json:"startStepIndex"`
			FirstStep      struct {
				UserInput struct {
					Items []struct {
						Text string `json:"text"`
					} `json:"items"`
				} `json:"userInput"`
			} `json:"firstStep"`
			ResponsePreview []string `json:"responsePreview"`
		} `json:"turns"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	if len(body.Turns) != 2 {
		t.Fatalf("turns = %d, want 2", len(body.Turns))
	}
	if body.Turns[0].StartStepIndex != 0 || body.Turns[1].StartStepIndex != 2 {
		t.Errorf("startStepIndex = %d, %d; want 0, 2", body.Turns[0].StartStepIndex, body.Turns[1].StartStepIndex)
	}
	if got := body.Turns[0].FirstStep.UserInput.Items[0].Text; got != "first question" {
		t.Errorf("firstStep text = %q, want the user's message", got)
	}
	if len(body.Turns[0].ResponsePreview) != 1 || body.Turns[0].ResponsePreview[0] != "first answer" {
		t.Errorf("responsePreview = %v, want the reply", body.Turns[0].ResponsePreview)
	}
}

// TestLSGetTrajectoryTurnsSelectedSteps covers the step filter: the
// answer holds the turns the asked-for steps belong to.
func TestLSGetTrajectoryTurnsSelectedSteps(t *testing.T) {
	d := newLSConversationDispatcher(t, "conv-1")
	answer, rpcErr, ok := d.handleLSGetTrajectoryTurns(json.RawMessage(`{"cascadeId":"conv-1","stepIndices":[3]}`))
	if !ok || rpcErr != nil {
		t.Fatalf("the turns were not answered (%v)", rpcErr)
	}
	raw, _ := json.Marshal(answer)
	var body struct {
		Turns []struct {
			StartStepIndex int `json:"startStepIndex"`
		} `json:"turns"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	if len(body.Turns) != 1 || body.Turns[0].StartStepIndex != 2 {
		t.Fatalf("turns = %v, want the second turn only", body.Turns)
	}
}

// TestLSGetTrajectoryTurnsUnknown covers that an unknown conversation is
// "not found".
func TestLSGetTrajectoryTurnsUnknown(t *testing.T) {
	d := newLSConversationDispatcher(t, "conv-1")
	_, rpcErr, _ := d.handleLSGetTrajectoryTurns(json.RawMessage(`{"cascadeId":"conv-nope"}`))
	if rpcErr == nil || rpcErr.Code != -32019 {
		t.Fatalf("an unknown conversation was answered (%v)", rpcErr)
	}
}

// TestLSGetAllCascadeTrajectories covers the summary map: the
// conversation id is the key and the summary is the value.
//
// 【一手】 2026-10-27: the reference answers an empty map — its mocked
// store holds no conversation.
func TestLSGetAllCascadeTrajectories(t *testing.T) {
	d := newLSConversationDispatcher(t, "conv-1")
	answer, rpcErr, ok := d.handleLSGetAllCascadeTrajectories(json.RawMessage(`{}`))
	if !ok || rpcErr != nil {
		t.Fatalf("the summaries were not answered (%v)", rpcErr)
	}
	raw, _ := json.Marshal(answer)
	var body struct {
		Summaries map[string]struct {
			Summary      string `json:"summary"`
			StepCount    int    `json:"stepCount"`
			TrajectoryID string `json:"trajectoryId"`
			Status       string `json:"status"`
			CreatedTime  string `json:"createdTime"`
		} `json:"trajectorySummaries"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	entry, ok := body.Summaries["conv-1"]
	if !ok {
		t.Fatalf("summaries = %v, want the conversation's own entry", body.Summaries)
	}
	if entry.StepCount != 4 {
		t.Errorf("stepCount = %d, want 4", entry.StepCount)
	}
	if entry.TrajectoryID != "traj-conv-1" {
		t.Errorf("trajectoryId = %q, want traj-conv-1", entry.TrajectoryID)
	}
	if entry.Status != "CASCADE_RUN_STATUS_IDLE" {
		t.Errorf("status = %q, want the idle status", entry.Status)
	}
	if entry.Summary != "first question" {
		t.Errorf("summary = %q, want the user's first message", entry.Summary)
	}
	if entry.CreatedTime == "" {
		t.Error("the summary carries no creation time")
	}
}

// TestLSDeleteCascadeTrajectory covers the delete: the trajectory goes
// away and a second delete finds nothing.
func TestLSDeleteCascadeTrajectory(t *testing.T) {
	d := newLSConversationDispatcher(t, "conv-1")
	answer, rpcErr, ok := d.handleLSDeleteCascadeTrajectory(json.RawMessage(`{"cascadeId":"conv-1"}`))
	if !ok || rpcErr != nil {
		t.Fatalf("the delete was not answered (%v)", rpcErr)
	}
	if raw, _ := json.Marshal(answer); string(raw) != "{}" {
		t.Errorf("answer = %s, want {}", raw)
	}
	if d.trajectories.Lookup("conv-1") != nil {
		t.Error("the trajectory is still there")
	}
}

// TestLSTrajectoryStoreLookupByTrajectory covers the reverse lookup:
// the store is keyed by the conversation id and the reader holds the
// trajectory id.
func TestLSTrajectoryStoreLookupByTrajectory(t *testing.T) {
	store := newLSTrajectoryStore()
	traj := store.Get("conv-1")
	if got := traj.TrajectoryID(); got != "traj-conv-1" {
		t.Fatalf("trajectoryId = %q, want traj-conv-1", got)
	}
	if store.LookupByTrajectory("traj-conv-1") != traj {
		t.Error("the trajectory was not found by its own id")
	}
	if store.LookupByTrajectory("traj-nope") != nil {
		t.Error("an unknown trajectory id found a trajectory")
	}
	if store.LookupByTrajectory("") != nil {
		t.Error("an empty trajectory id found a trajectory")
	}
}

// TestLSTrajectoryStoreDelete covers the delete: it reports whether
// there was something to delete.
func TestLSTrajectoryStoreDelete(t *testing.T) {
	store := newLSTrajectoryStore()
	store.Get("conv-1")
	if !store.Delete("conv-1") {
		t.Error("the delete reported nothing to delete")
	}
	if store.Delete("conv-1") {
		t.Error("a second delete found a trajectory")
	}
	if store.Delete("") {
		t.Error("an empty id deleted a trajectory")
	}
}
