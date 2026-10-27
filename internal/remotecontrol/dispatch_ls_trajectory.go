// Package remotecontrol — dispatch_ls_trajectory.go: the trajectory
// readers of the LanguageServerService — the methods that read a
// conversation's trajectory back.
//
// The workbench renders a conversation from the
// StreamAgentStateUpdates server stream, but it also *re-opens* one:
// the trajectory behind a conversation id (GetCascadeTrajectory,
// GetCascadeTrajectorySteps) and the trajectories of the user
// (GetUserTrajectory and its two siblings) are the read side of the
// same data.
//
// 【一手】 2026-10-27, measured against the reference binary in its
// mocked standalone mode (out/langserver/coverage-351.json and
// out/langserver/conversation.json):
//
//   - GetCascadeTrajectory answers
//     {"trajectory":{"trajectoryId":"traj-XYZ-9","cascadeId":"XYZ-9","metadata":{}},"status":"CASCADE_RUN_STATUS_IDLE"}
//     — the trajectory id is the cascade id with the prefix "traj-";
//   - the trajectory id of a cascade the *client* minted is derived
//     from the cascade id the client sent, not from a server-side id;
//   - GetUserTrajectory answers
//     "not_found: failed to get trajectory: trajectory not found for id <id>"
//     for a trajectory the reference does not have.
//
// The answer shapes are proto-JSON: the enum members are their names
// ("CASCADE_RUN_STATUS_IDLE"), the timestamps are RFC 3339.
package remotecontrol

import (
	"encoding/json"
	"strings"
	"time"
)

// routeLanguageServerTrajectoryMethods handles the trajectory batch —
// the methods that read a conversation's trajectory back.
//
// The batch has its own router because it is a *read* side: the
// cascade methods write the trajectory, these answer it.
func (d *Dispatcher) routeLanguageServerTrajectoryMethods(method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "GetCascadeTrajectory":
		return d.handleLSGetCascadeTrajectory(params)
	case "GetCascadeTrajectorySteps":
		return d.handleLSGetCascadeTrajectorySteps(params)
	case "GetUserTrajectory":
		return d.handleLSGetUserTrajectory(params)
	case "GetUserTrajectoryDebug":
		return d.handleLSGetUserTrajectoryDebug(params)
	case "GetUserTrajectoryDescriptions":
		return d.handleLSGetUserTrajectoryDescriptions(params)
	}
	return nil, nil, false
}

// lsTrajectoryID is the trajectory id of a conversation.
//
// 【一手】 2026-10-27: the reference answers "traj-XYZ-9" for the
// cascade "XYZ-9" and "traj-ref-probe-1" for the cascade
// "ref-probe-1" — the trajectory id is the cascade id with the prefix
// "traj-". The Web UI never sends a trajectory id (it sends the
// conversation id it minted), so the id is the server's own.
func lsTrajectoryID(conversationID string) string {
	return "traj-" + conversationID
}

// lsTrajectoryJSON renders one trajectory as gemini_coder.Trajectory.
//
// The step list is the trajectory's own proto-JSON (the same slices
// the StreamAgentStateUpdates frames carry), so a conversation that
// is re-opened renders exactly as it did when it was live.
//
// 【一手】 2026-10-27: the reference answers the trajectory id, the
// cascade id and an empty metadata for a cascade it holds
// ({"trajectory":{"trajectoryId":"traj-XYZ-9","cascadeId":"XYZ-9","metadata":{}}}).
func lsTrajectoryJSON(snap lsSnapshot) map[string]any {
	steps := snap.Steps
	if steps == nil {
		steps = []map[string]any{}
	}
	return map[string]any{
		"trajectoryId": snap.TrajectoryID,
		"cascadeId":    snap.ConversationID,
		"steps":        steps,
		"metadata":     map[string]any{},
	}
}

// lsUserTrajectoryJSON renders one trajectory of the user.
//
// 【一手】 2026-10-27: the reference's user trajectory carries its type
// and its metadata — the creation time and the initialisation state
// id:
//
//	{"mainline":[{"trajectory":{"trajectoryId":"<uuid>","cascadeId":"<uuid>","trajectoryType":"CORTEX_TRAJECTORY_TYPE_USER_MAINLINE","metadata":{"createdAt":"<timestamp>","initializationStateId":"<uuid>"}},"trajectoryScope":{}}]}
func lsUserTrajectoryJSON(snap lsSnapshot) map[string]any {
	out := lsTrajectoryJSON(snap)
	out["trajectoryType"] = "CORTEX_TRAJECTORY_TYPE_USER_MAINLINE"
	metadata := map[string]any{}
	if !snap.CreatedAt.IsZero() {
		metadata["createdAt"] = snap.CreatedAt.UTC().Format(time.RFC3339)
	}
	if snap.InitStateID != "" {
		metadata["initializationStateId"] = snap.InitStateID
	}
	out["metadata"] = metadata
	return out
}

// lsRunStatusName renders the run status as its proto-JSON enum name.
//
// 【反编译】 exa.cortex_pb.CascadeRunStatus: IDLE=1, RUNNING=2,
// CANCELING=3, BUSY=4. The reference answers the name (the answer is
// read byte for byte against out/langserver/coverage-351.json).
func lsRunStatusName(running bool) string {
	if running {
		return "CASCADE_RUN_STATUS_RUNNING"
	}
	return "CASCADE_RUN_STATUS_IDLE"
}

// handleLSGetCascadeTrajectory answers the trajectory of a
// conversation.
//
// 【一手】 2026-10-27: the reference answers the trajectory id, the
// cascade id, the step list and the run status. The clone answers its
// own trajectory; a conversation it has never seen is "not found" —
// the reference's own failure for the same case.
func (d *Dispatcher) handleLSGetCascadeTrajectory(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		CascadeID string `json:"cascadeId"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	cascadeID := strings.TrimSpace(req.CascadeID)
	if cascadeID == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "cascadeId is required"}, true
	}
	snap, ok := d.lsSnapshot(cascadeID)
	if !ok {
		return nil, &rpcErrorBody{Code: -32019, Message: "not_found", Data: "trajectory not found"}, true
	}
	answer := map[string]any{
		"trajectory": lsTrajectoryJSON(snap),
		"status":     lsRunStatusName(snap.Running),
	}
	if n := len(snap.Steps); n > 0 {
		answer["numTotalSteps"] = n
	}
	return answer, nil, true
}

// handleLSGetCascadeTrajectorySteps answers the step list of a
// conversation, from an offset.
//
// 【反编译】 exa.language_server_pb.GetCascadeTrajectoryStepsRequest:
// cascade_id=1, step_offset=2 — the offset is the number of steps the
// caller already has.
func (d *Dispatcher) handleLSGetCascadeTrajectorySteps(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		CascadeID  string `json:"cascadeId"`
		StepOffset int    `json:"stepOffset"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	cascadeID := strings.TrimSpace(req.CascadeID)
	if cascadeID == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "cascadeId is required"}, true
	}
	snap, ok := d.lsSnapshot(cascadeID)
	if !ok {
		return nil, &rpcErrorBody{Code: -32019, Message: "not_found", Data: "trajectory not found"}, true
	}
	steps := snap.Steps
	if req.StepOffset > 0 {
		if req.StepOffset >= len(steps) {
			steps = nil
		} else {
			steps = steps[req.StepOffset:]
		}
	}
	if steps == nil {
		steps = []map[string]any{}
	}
	return map[string]any{"steps": steps}, nil, true
}

// handleLSGetUserTrajectory answers one trajectory of the user, by
// trajectory id.
//
// 【一手】 2026-10-27: the reference answers
// "not_found: failed to get trajectory: trajectory not found for id <id>"
// for a trajectory it does not have. The clone answers the trajectory
// it has, and the same failure for the one it has not.
func (d *Dispatcher) handleLSGetUserTrajectory(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		TrajectoryID string `json:"trajectoryId"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	id := strings.TrimSpace(req.TrajectoryID)
	snap, ok := d.lsSnapshotByTrajectory(id)
	if !ok {
		return nil, &rpcErrorBody{
			Code:    -32019,
			Message: "not_found",
			Data:    "failed to get trajectory: trajectory not found for id " + id,
		}, true
	}
	return map[string]any{"trajectory": lsUserTrajectoryJSON(snap)}, nil, true
}

// handleLSGetUserTrajectoryDebug answers the mainline trajectory of
// the user, in the shape the debug panel reads.
//
// 【一手】 2026-10-27: the reference's answer holds a *list* of
// mainline entries, each with the trajectory and the scope it ran in:
//
//	{"mainline":[{"trajectory":{…},"trajectoryScope":{}}]}
func (d *Dispatcher) handleLSGetUserTrajectoryDebug(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		IncludeAllTrajectories bool `json:"includeAllTrajectories"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	snap, ok := d.lsMostRecentSnapshot()
	if !ok {
		return map[string]any{}, nil, true
	}
	return map[string]any{
		"mainline": []map[string]any{
			{
				"trajectory":      lsUserTrajectoryJSON(snap),
				"trajectoryScope": d.lsTrajectoryScope(),
			},
		},
	}, nil, true
}

// handleLSGetUserTrajectoryDescriptions answers the trajectory list the
// debug panel shows.
//
// 【反编译】 exa.cortex_pb.ImplicitTrajectoryDescription:
// trajectory_id=1, trajectory_scope=2, current=3.
func (d *Dispatcher) handleLSGetUserTrajectoryDescriptions(params json.RawMessage) (any, *rpcErrorBody, bool) {
	snap, ok := d.lsMostRecentSnapshot()
	if !ok {
		return map[string]any{}, nil, true
	}
	return map[string]any{
		"trajectories": []map[string]any{
			{
				"trajectoryId":    snap.TrajectoryID,
				"trajectoryScope": d.lsTrajectoryScope(),
				"current":         true,
			},
		},
	}, nil, true
}

// lsTrajectoryScope is the scope a trajectory ran in: the workspace it
// belongs to.
//
// 【反编译】 exa.cortex_pb.TrajectoryScope: workspace_uri=1,
// git_root_uri=2, branch_name=3.
func (d *Dispatcher) lsTrajectoryScope() map[string]any {
	scope := map[string]any{}
	if ws := d.lsWorkspace(); ws != "" {
		scope["workspaceUri"] = lsNormalizedURI(ws)
	}
	return scope
}

// lsWorkspace is the workspace the session runs in.
func (d *Dispatcher) lsWorkspace() string {
	if d == nil || d.cfg.Session == nil {
		return ""
	}
	return d.cfg.Session.Workspace
}

// lsSnapshot is the trajectory of one conversation.
func (d *Dispatcher) lsSnapshot(conversationID string) (lsSnapshot, bool) {
	if d == nil || d.trajectories == nil {
		return lsSnapshot{}, false
	}
	traj := d.trajectories.Lookup(conversationID)
	if traj == nil {
		return lsSnapshot{}, false
	}
	return traj.Snapshot(), true
}

// lsSnapshotByTrajectory is the trajectory of one trajectory id.
//
// The store is keyed by the conversation id and the trajectory id is
// derived from it, so the lookup walks the store.
func (d *Dispatcher) lsSnapshotByTrajectory(trajectoryID string) (lsSnapshot, bool) {
	if d == nil || d.trajectories == nil {
		return lsSnapshot{}, false
	}
	traj := d.trajectories.LookupByTrajectory(trajectoryID)
	if traj == nil {
		return lsSnapshot{}, false
	}
	return traj.Snapshot(), true
}

// lsMostRecentSnapshot is the trajectory of the conversation that was
// updated last.
func (d *Dispatcher) lsMostRecentSnapshot() (lsSnapshot, bool) {
	if d == nil || d.trajectories == nil {
		return lsSnapshot{}, false
	}
	return d.trajectories.MostRecent()
}

// lsSnapshots is every trajectory the clone holds.
func (d *Dispatcher) lsSnapshots() []lsSnapshot {
	if d == nil || d.trajectories == nil {
		return nil
	}
	return d.trajectories.Snapshots()
}
