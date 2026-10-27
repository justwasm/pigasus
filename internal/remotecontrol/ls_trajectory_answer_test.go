package remotecontrol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/convstore"
)

// newLSTrajectoryDispatcher returns a dispatcher whose store holds one
// conversation with two steps.
func newLSTrajectoryDispatcher(t *testing.T, conversationID string) *Dispatcher {
	t.Helper()
	d, err := NewDispatcher(Config{Session: NewSessionState("1.3.4-test")})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	traj := d.trajectories.Get(conversationID)
	traj.Append(map[string]any{
		"type":     14,
		"userInput": map[string]any{"items": []any{map[string]any{"text": "hello there"}}},
	})
	traj.Append(map[string]any{
		"type":            15,
		"plannerResponse": map[string]any{"response": "mock reply"},
	})
	return d
}

// TestLSGetCascadeTrajectory covers the conversation's own trajectory:
// the answer carries the trajectory id, the cascade id, the step list
// and the run status.
//
// 【一手】 2026-10-27: the reference answers
// {"trajectory":{"trajectoryId":"mock-trajectory-id","metadata":{}},"status":"CASCADE_RUN_STATUS_IDLE"}
// (out/langserver/coverage-351.json); the clone answers the same
// shapes from its own store.
func TestLSGetCascadeTrajectory(t *testing.T) {
	d := newLSTrajectoryDispatcher(t, "conv-1")
	answer, rpcErr, ok := d.handleLSGetCascadeTrajectory(json.RawMessage(`{"cascadeId":"conv-1"}`))
	if !ok {
		t.Fatal("the trajectory was not answered")
	}
	if rpcErr != nil {
		t.Fatalf("the answer failed: %v", rpcErr)
	}
	body := map[string]any{}
	raw, _ := json.Marshal(answer)
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	if body["status"] != "CASCADE_RUN_STATUS_IDLE" {
		t.Errorf("status = %v, want the idle status", body["status"])
	}
	if body["numTotalSteps"] != float64(2) {
		t.Errorf("numTotalSteps = %v, want 2", body["numTotalSteps"])
	}
	traj, _ := body["trajectory"].(map[string]any)
	if traj == nil {
		t.Fatalf("no trajectory in %s", raw)
	}
	if traj["trajectoryId"] != "traj-conv-1" || traj["cascadeId"] != "conv-1" {
		t.Errorf("trajectory = %v, want the conversation's own ids", traj)
	}
	steps, _ := traj["steps"].([]any)
	if len(steps) != 2 {
		t.Errorf("steps = %d, want 2", len(steps))
	}
}

// TestLSGetCascadeTrajectoryUnknown covers that a conversation the
// clone has never seen is "not found" — and that asking does not
// invent one: the reader must not create a trajectory.
func TestLSGetCascadeTrajectoryUnknown(t *testing.T) {
	d := newLSTrajectoryDispatcher(t, "conv-1")
	_, rpcErr, ok := d.handleLSGetCascadeTrajectory(json.RawMessage(`{"cascadeId":"conv-nope"}`))
	if !ok {
		t.Fatal("the request was not answered")
	}
	if rpcErr == nil {
		t.Fatal("an unknown conversation was answered")
	}
	if rpcErr.Code != -32019 {
		t.Errorf("code = %d, want -32019", rpcErr.Code)
	}
	if d.trajectories.Lookup("conv-nope") != nil {
		t.Error("the reader created a trajectory")
	}
}

// TestLSGetCascadeTrajectoryNeedsTheID covers the parameter check.
func TestLSGetCascadeTrajectoryNeedsTheID(t *testing.T) {
	d := newLSTrajectoryDispatcher(t, "conv-1")
	_, rpcErr, _ := d.handleLSGetCascadeTrajectory(json.RawMessage(`{}`))
	if rpcErr == nil {
		t.Fatal("an empty request was answered")
	}
	if rpcErr.Code != -32602 {
		t.Errorf("code = %d, want -32602", rpcErr.Code)
	}
}

// TestLSGetCascadeTrajectoryStepsOffset covers the offset: the caller
// says how many steps it already has.
func TestLSGetCascadeTrajectoryStepsOffset(t *testing.T) {
	d := newLSTrajectoryDispatcher(t, "conv-1")
	answer, rpcErr, ok := d.handleLSGetCascadeTrajectorySteps(json.RawMessage(`{"cascadeId":"conv-1","stepOffset":1}`))
	if !ok || rpcErr != nil {
		t.Fatalf("the steps were not answered (%v)", rpcErr)
	}
	raw, _ := json.Marshal(answer)
	var body struct {
		Steps []map[string]any `json:"steps"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	if len(body.Steps) != 1 {
		t.Errorf("steps = %d, want 1", len(body.Steps))
	}
}

// TestLSGetUserTrajectory covers the lookup by trajectory id.
//
// 【一手】 2026-10-27: the reference's trajectory id is the cascade id
// with the prefix "traj-" (out/langserver/conversation.json), so the
// lookup is by the server's own id — not by the conversation id.
func TestLSGetUserTrajectory(t *testing.T) {
	d := newLSTrajectoryDispatcher(t, "conv-1")
	answer, rpcErr, ok := d.handleLSGetUserTrajectory(json.RawMessage(`{"trajectoryId":"traj-conv-1"}`))
	if !ok || rpcErr != nil {
		t.Fatalf("the trajectory was not answered (%v)", rpcErr)
	}
	raw, _ := json.Marshal(answer)
	var body struct {
		Trajectory struct {
			TrajectoryID string           `json:"trajectoryId"`
			Steps        []map[string]any `json:"steps"`
		} `json:"trajectory"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	if body.Trajectory.TrajectoryID != "traj-conv-1" {
		t.Errorf("trajectoryId = %q, want traj-conv-1", body.Trajectory.TrajectoryID)
	}
	if len(body.Trajectory.Steps) != 2 {
		t.Errorf("steps = %d, want 2", len(body.Trajectory.Steps))
	}
}

// TestLSGetUserTrajectoryNotFound covers the reference's own failure
// text for a trajectory the server does not have.
//
// 【一手】 2026-10-27: the reference answers
// "not_found: failed to get trajectory: trajectory not found for id <id>".
func TestLSGetUserTrajectoryNotFound(t *testing.T) {
	d := newLSTrajectoryDispatcher(t, "conv-1")
	_, rpcErr, ok := d.handleLSGetUserTrajectory(json.RawMessage(`{"trajectoryId":"traj-nope"}`))
	if !ok || rpcErr == nil {
		t.Fatalf("an unknown trajectory was answered (%v)", rpcErr)
	}
	want := "failed to get trajectory: trajectory not found for id traj-nope"
	if rpcErr.Data != want {
		t.Errorf("data = %q, want %q", rpcErr.Data, want)
	}
}

// TestLSRunStatusName covers the enum's name — the reference answers
// the name, not the number.
func TestLSRunStatusName(t *testing.T) {
	if got := lsRunStatusName(false); got != "CASCADE_RUN_STATUS_IDLE" {
		t.Errorf("lsRunStatusName(false) = %q", got)
	}
	if got := lsRunStatusName(true); got != "CASCADE_RUN_STATUS_RUNNING" {
		t.Errorf("lsRunStatusName(true) = %q", got)
	}
}

// TestLSSearchResults covers the conversation search: the query
// matches the title, case-insensitively, and the limit caps the list.
func TestLSSearchResults(t *testing.T) {
	metas := []convstore.ConversationMeta{
		{ID: "a", Title: "Fix the parser"},
		{ID: "b", Title: "write the report"},
		{ID: "c", Title: "Fix the parser again"},
	}
	cases := []struct {
		name  string
		query string
		limit int
		want  []string
	}{
		{name: "an empty query matches everything", want: []string{"a", "b", "c"}},
		{name: "the query matches the title", query: "fix", want: []string{"a", "c"}},
		{name: "the query is case-insensitive", query: "WRITE", want: []string{"b"}},
		{name: "the limit caps the list", limit: 2, want: []string{"a", "b"}},
		{name: "nothing matches", query: "zzz", want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := lsSearchResults(metas, tc.query, tc.limit)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d results, want %d", len(got), len(tc.want))
			}
			for i, entry := range got {
				if entry["cascadeId"] != tc.want[i] {
					t.Errorf("result %d = %v, want %q", i, entry["cascadeId"], tc.want[i])
				}
			}
		})
	}
}

// TestLSMostRecentSnapshot covers the read of the trajectory that was
// updated last.
func TestLSMostRecentSnapshot(t *testing.T) {
	d := newLSTrajectoryDispatcher(t, "conv-1")
	d.trajectories.Get("conv-2")
	snap, ok := d.trajectories.MostRecent()
	if !ok {
		t.Fatal("the store holds no trajectory")
	}
	if snap.ConversationID != "conv-1" {
		t.Errorf("conversation = %q, want conv-1", snap.ConversationID)
	}
}

// TestLSMostRecentSnapshotEmpty covers that an empty store says so.
func TestLSMostRecentSnapshotEmpty(t *testing.T) {
	store := newLSTrajectoryStore()
	if _, ok := store.MostRecent(); ok {
		t.Error("an empty store answered a snapshot")
	}
}

// TestLSUserTrajectoryDescriptions covers the trajectory list the
// debug panel reads.
func TestLSUserTrajectoryDescriptions(t *testing.T) {
	d := newLSTrajectoryDispatcher(t, "conv-1")
	answer, rpcErr, ok := d.handleLSGetUserTrajectoryDescriptions(json.RawMessage(`{}`))
	if !ok || rpcErr != nil {
		t.Fatalf("the descriptions were not answered (%v)", rpcErr)
	}
	raw, _ := json.Marshal(answer)
	var body struct {
		Trajectories []struct {
			TrajectoryID string `json:"trajectoryId"`
			Current      bool   `json:"current"`
		} `json:"trajectories"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	if len(body.Trajectories) != 1 {
		t.Fatalf("trajectories = %d, want 1", len(body.Trajectories))
	}
	if body.Trajectories[0].TrajectoryID != "traj-conv-1" {
		t.Errorf("trajectoryId = %q, want traj-conv-1", body.Trajectories[0].TrajectoryID)
	}
	if !body.Trajectories[0].Current {
		t.Error("the trajectory is not marked as the current one")
	}
}

// TestLSSearchResultsTimeFormat covers the timestamp's shape: the
// reference answers RFC 3339, and the workbench parses it as a
// google.protobuf.Timestamp.
func TestLSSearchResultsTimeFormat(t *testing.T) {
	metas := []convstore.ConversationMeta{
		{ID: "a", Title: "one", ModTime: time.Date(2026, 10, 27, 6, 0, 0, 0, time.UTC)},
	}
	got := lsSearchResults(metas, "", 0)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	stamp, _ := got[0]["lastModifiedTime"].(string)
	if _, err := time.Parse(time.RFC3339, stamp); err != nil {
		t.Errorf("lastModifiedTime = %q: %v", stamp, err)
	}
}
